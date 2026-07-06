package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func (p *AnthropicProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

// anthropicRequest is the request body for POST /v1/messages.
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"` // "user" or "assistant"
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
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

	resp, err := p.httpClient().Do(httpReq)
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

	// Concatenate all text blocks (the common case is a single block, but the
	// spec allows multiple).
	var sb strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}

	return Response{
		Text:         sb.String(),
		StopReason:   ar.StopReason,
		InputTokens:  ar.Usage.InputTokens,
		OutputTokens: ar.Usage.OutputTokens,
	}, nil
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

	resp, err := p.httpClient().Do(httpReq)
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
