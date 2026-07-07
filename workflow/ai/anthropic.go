package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicProvider implements Provider against the Anthropic Messages API
// (/v1/messages).
//
// The same implementation serves Claude (https://api.anthropic.com) and
// DeepSeek's Anthropic-compatible endpoint (https://api.deepseek.com/anthropic)
// via BaseURL. See docs/PRODUCT.md "Provider 架构".
type AnthropicProvider struct {
	// BaseURL is the API root without a trailing slash. Defaults to
	// "https://api.deepseek.com/anthropic" so DeepSeek works out of the box
	// (set "https://api.anthropic.com" for Claude).
	BaseURL string

	// APIKey is the authentication key (sent as x-api-key). Required.
	APIKey string

	// DefaultModel is used when Request.Model is empty.
	DefaultModel string

	// HTTPClient is used for all requests. If nil, http.DefaultClient is used.
	// A per-request timeout should be applied via the request context.
	HTTPClient *http.Client

	// MaxRetries is the maximum number of automatic retries on transient
	// errors (429/5xx/timeout). Default 3. Set to 0 to disable retries.
	MaxRetries int

	// RetryBaseDelay is the initial backoff delay; doubled each retry.
	// Default 1s, capped at 30s.
	RetryBaseDelay time.Duration
}

// Name returns the provider identifier.
func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) baseURL() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	// Default to DeepSeek's Anthropic-compatible endpoint so the provider is
	// usable without extra configuration during development. Production users
	// targeting Claude set BaseURL explicitly.
	return "https://api.deepseek.com/anthropic"
}

// GetModel returns the provider's default model id. Used by the agent loop
// to know which model to pass to CompleteRaw.
func (p *AnthropicProvider) GetModel() string { return p.DefaultModel }

func (p *AnthropicProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

func (p *AnthropicProvider) maxRetries() int {
	if p.MaxRetries > 0 {
		return p.MaxRetries
	}
	return 3 // default
}

func (p *AnthropicProvider) retryBaseDelay() time.Duration {
	if p.RetryBaseDelay > 0 {
		return p.RetryBaseDelay
	}
	return 1 * time.Second
}

// isRetryableStatus reports whether an HTTP status code indicates a transient
// error worth retrying (rate limit, server error). 4xx (except 429) are not
// retryable — they indicate a request-level problem (auth, bad input).
func isRetryableStatus(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// isRetryableNetErr reports whether a network-level error is transient
// (timeout, connection reset). Non-timeout errors (e.g. DNS failure) are also
// retried — the model API is generally reachable, and a blip shouldn't fail
// the whole workflow.
func isRetryableNetErr(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation is NOT retryable — the caller deliberately cancelled.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true // network errors are generally worth one retry
}

// retryableHTTPDo wraps httpClient.Do with automatic retries on transient
// errors (429/5xx/timeout). Returns the final response (which the caller must
// close) and the total number of retries performed.
func (p *AnthropicProvider) retryableHTTPDo(ctx context.Context, req *http.Request) (*http.Response, int, error) {
	maxR := p.maxRetries()
	baseDelay := p.retryBaseDelay()

	for attempt := 0; ; attempt++ {
		resp, err := p.httpClient().Do(req)

		if err != nil {
			// Network error — retry if transient and we haven't exhausted.
			if isRetryableNetErr(err) && attempt < maxR {
				delay := backoffDelay(baseDelay, attempt)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil, attempt, ctx.Err()
				}
				continue
			}
			return nil, attempt, err
		}

		// Got an HTTP response. If retryable status and retries left, close
		// the body and retry. Otherwise return the response as-is (the caller
		// reads the body, including error details for non-2xx).
		if isRetryableStatus(resp.StatusCode) && attempt < maxR {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			delay := backoffDelay(baseDelay, attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, attempt, ctx.Err()
			}
			continue
		}

		// Non-retryable or out of retries: return what we have.
		return resp, attempt, nil
	}
}

func backoffDelay(base time.Duration, attempt int) time.Duration {
	d := base * (1 << attempt)
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// anthropicRequest is the request body for POST /v1/messages.
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"` // "user" or "assistant"
	Content []anthropicContent `json:"content"`
}

// anthropicContent is polymorphic: a text block, a tool_use block (model wants
// to call a tool), or a tool_result block (caller sends the result back).
type anthropicContent struct {
	Type string `json:"type"`
	// text block
	Text string `json:"text,omitempty"`
	// tool_use block (in assistant responses)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result block (in user messages, answering a tool_use)
	// Per Anthropic API spec, tool_result content goes in "content" not "text".
	// We use Content for tool_result payload; Text is reused for text blocks.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"` // tool_result: the result text
	IsError   bool   `json:"is_error,omitempty"`
}

// buildAnthropicMessages converts the Request into Anthropic-format messages:
// the conversation history (req.Messages) followed by the current Prompt as
// the final user turn. When there is no history, it's a single user message
// (backward compatible).
func buildAnthropicMessages(req Request) []anthropicMessage {
	msgs := make([]anthropicMessage, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		msgs = append(msgs, anthropicMessage{
			Role:    m.Role,
			Content: []anthropicContent{{Type: "text", Text: m.Content}},
		})
	}
	// Current prompt as the final user turn.
	msgs = append(msgs, anthropicMessage{
		Role:    "user",
		Content: []anthropicContent{{Type: "text", Text: req.Prompt}},
	})
	return msgs
}

// anthropicResponse is the non-streaming response from /v1/messages.
type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []anthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence any                `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete performs a non-streaming completion.
func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if p.APIKey == "" {
		return Response{}, fmt.Errorf("anthropic provider: API key is empty (set ANTHROPIC_API_KEY or DEEPSEEK_API_KEY)")
	}

	model := req.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("anthropic provider: model is empty (set ai.model in the workflow or a default)")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	body := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		System:      req.System,
		Messages:    buildAnthropicMessages(req),
		Tools:       toAnthropicTools(req.Tools),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	// anthropic-version is required by the Messages API. This version is
	// stable and widely supported by both Anthropic and the DeepSeek
	// Anthropic-compatible endpoint.
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, retries, err := p.retryableHTTPDo(ctx, httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("anthropic api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}

	// Parse response: concatenate text blocks + collect tool_use blocks.
	var sb strings.Builder
	var toolCalls []ToolCall
	for _, c := range ar.Content {
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:    c.ID,
				Name:  c.Name,
				Input: c.Input,
			})
		}
	}

	return Response{
		Text:         sb.String(),
		ToolCalls:    toolCalls,
		StopReason:   ar.StopReason,
		InputTokens:  ar.Usage.InputTokens,
		OutputTokens: ar.Usage.OutputTokens,
		Retries:      retries,
	}, nil
}

// toAnthropicTools converts ai.ToolDef to the wire format.
func toAnthropicTools(tools []ToolDef) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, len(tools))
	for i, t := range tools {
		out[i] = anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return out
}

// AnthropicRawMessage is exported so callers (the agent loop) can construct
// messages with tool_result content blocks, which the plain Message type
// (role + string content) can't express.
type AnthropicRawMessage = anthropicMessage

// AnthropicRawContent is exported for the same reason.
type AnthropicRawContent = anthropicContent

// CompleteRaw is a lower-level Complete that accepts pre-built messages
// (including tool_use assistant turns and tool_result user turns). Used by the
// multi-turn agent loop: after executing tools, the caller appends the
// assistant's tool_use turn + a user turn with tool_result blocks, then calls
// CompleteRaw to continue.
func (p *AnthropicProvider) CompleteRaw(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error) {
	if p.APIKey == "" {
		return Response{}, fmt.Errorf("anthropic provider: API key is empty")
	}
	if model == "" {
		model = p.DefaultModel
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	body := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		System:      system,
		Messages:    messages,
		Tools:       toAnthropicTools(tools),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, retries, err := p.retryableHTTPDo(ctx, httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("anthropic api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}

	var sb strings.Builder
	var toolCalls []ToolCall
	for _, c := range ar.Content {
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{ID: c.ID, Name: c.Name, Input: c.Input})
		}
	}
	return Response{
		Text:         sb.String(),
		ToolCalls:    toolCalls,
		StopReason:   ar.StopReason,
		InputTokens:  ar.Usage.InputTokens,
		OutputTokens: ar.Usage.OutputTokens,
		Retries:      retries,
	}, nil
}

// RawMessageFromResponse builds an assistant-turn anthropicMessage from a
// Response that contains tool_use blocks. Used by the agent loop to echo the
// assistant's tool_use turn back in the next request.
func RawMessageFromResponse(resp Response) AnthropicRawMessage {
	msg := AnthropicRawMessage{Role: "assistant"}
	if resp.Text != "" {
		msg.Content = append(msg.Content, AnthropicRawContent{Type: "text", Text: resp.Text})
	}
	for _, tc := range resp.ToolCalls {
		msg.Content = append(msg.Content, AnthropicRawContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		})
	}
	return msg
}

// RawMessageWithToolResults builds a user-turn message containing tool_result
// blocks for each executed tool call.
func RawMessageWithToolResults(results []ToolResult) AnthropicRawMessage {
	msg := AnthropicRawMessage{Role: "user"}
	for _, r := range results {
		msg.Content = append(msg.Content, AnthropicRawContent{
			Type:      "tool_result",
			ToolUseID: r.ToolUseID,
			Content:   r.Output, // Anthropic API: tool_result content goes here
			IsError:   r.IsError,
		})
	}
	return msg
}

// ToolResult is the result of executing a tool call, to be sent back to the model.
type ToolResult struct {
	ToolUseID string
	Output    string
	IsError   bool
}

// Stream performs a streaming completion via Anthropic SSE.
//
// The Messages API is called with stream:true. The response is a sequence of
// SSE events (lines prefixed with "data: "). Each event has a "type"; we
// accumulate text from "content_block_delta" events (delta.text) and read
// usage from "message_delta". The terminal "message_stop" event ends the
// stream. onToken is invoked for each text delta as it arrives, enabling
// token-by-token display.
//
// If onToken is nil, behavior is equivalent to Complete but still uses a
// single streaming request (use Complete instead in that case).
func (p *AnthropicProvider) Stream(ctx context.Context, req Request, onToken func(string)) (Response, error) {
	if p.APIKey == "" {
		return Response{}, fmt.Errorf("anthropic provider: API key is empty (set ANTHROPIC_API_KEY or DEEPSEEK_API_KEY)")
	}

	model := req.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("anthropic provider: model is empty (set ai.model in the workflow or a default)")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	body := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		System:      req.System,
		Messages:    buildAnthropicMessages(req),
		Stream:      true,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, retries, err := p.retryableHTTPDo(ctx, httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return Response{}, fmt.Errorf("anthropic api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	// Parse SSE: events are "event: <type>\ndata: <json>\n\n".
	scanner := bufio.NewScanner(resp.Body)
	// Anthropic lines can be long (especially with large tool calls); raise the
	// per-line limit well above the default 64KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out Response
	var sb strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var ev streamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			// Skip malformed lines rather than aborting the whole stream.
			continue
		}

		switch ev.Type {
		case "content_block_delta":
			if ev.Delta != nil && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				sb.WriteString(ev.Delta.Text)
				if onToken != nil {
					onToken(ev.Delta.Text)
				}
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				out.StopReason = ev.Delta.StopReason
			}
			// message_delta may carry final usage (output_tokens) on some
			// providers; the initial message_start carries input_tokens.
			if ev.Usage != nil {
				if ev.Usage.OutputTokens > 0 {
					out.OutputTokens = ev.Usage.OutputTokens
				}
			}
		case "message_start":
			// message_start wraps the message object with usage.input_tokens.
			if ev.Message != nil && ev.Message.Usage != nil {
				out.InputTokens = ev.Message.Usage.InputTokens
				out.OutputTokens = ev.Message.Usage.OutputTokens
			}
		case "message_stop":
			// Terminal event; stop reading.
			out.Text = sb.String()
			out.Retries = retries
			return out, nil
		case "error":
			em := ev.Error
			if em == nil {
				em = &streamError{Message: "unknown stream error"}
			}
			return Response{}, fmt.Errorf("anthropic stream error: %s", em.Message)
		}
	}

	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read stream: %w", err)
	}

	out.Text = sb.String()
	out.Retries = retries
	return out, nil
}

// streamEvent is a union over the SSE event types emitted by the Messages API.
// Only the fields relevant to text accumulation are modeled; extra fields are
// ignored by json.Unmarshal.
type streamEvent struct {
	Type    string        `json:"type"`
	Delta   *streamDelta  `json:"delta,omitempty"`
	Usage   *streamUsage  `json:"usage,omitempty"`
	Message *streamMessage `json:"message,omitempty"`
	Error   *streamError  `json:"error,omitempty"`
}

type streamDelta struct {
	Type       string `json:"type"`        // "text_delta"
	Text       string `json:"text"`
	StopReason string `json:"stop_reason"` // present on message_delta
}

type streamUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type streamMessage struct {
	Usage *streamUsage `json:"usage,omitempty"`
}

type streamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
