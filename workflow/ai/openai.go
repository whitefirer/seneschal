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

// OpenAIProvider implements Provider against the OpenAI Chat Completions API
// (POST /v1/chat/completions). The same protocol serves OpenAI, Moonshot,
// Zhipu, Groq, and any OpenAI-compatible endpoint via BaseURL.
//
// API key is sent as Bearer token. Streaming uses SSE (data: lines), same
// as the Anthropic streaming format but with a different JSON structure.
//
// Tool use is not yet supported (OpenAI's function_call format differs from
// Anthropic's; planned for a future phase).
type OpenAIProvider struct {
	// BaseURL is the API root. Defaults to https://api.openai.com/v1.
	// For compatible services: https://api.moonshot.cn/v1, etc.
	BaseURL string

	// APIKey is the Bearer token. Required (except for some local servers).
	APIKey string

	// DefaultModel e.g. "gpt-4o", "moonshot-v1-8k", "glm-4".
	DefaultModel string

	// HTTPClient used for requests. If nil, http.DefaultClient.
	HTTPClient *http.Client

	// MaxRetries for transient errors (429/5xx). Default 3.
	MaxRetries     int
	RetryBaseDelay int // nanoseconds; 0 = 1s default
}

func (p *OpenAIProvider) Name() string     { return "openai" }
func (p *OpenAIProvider) GetModel() string { return p.DefaultModel }

func (p *OpenAIProvider) baseURL() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return "https://api.openai.com/v1"
}

func (p *OpenAIProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

// ── Request/Response types ────────────────────────────────────────────────────

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// openAIStreamChunk is one SSE data line in a streaming response.
type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Index int `json:"index"`
	Delta struct {
		Role    string `json:"role,omitempty"`
		Content string `json:"content,omitempty"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// ── Provider implementation ───────────────────────────────────────────────────

// Complete sends a non-streaming chat completion request.
func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("openai provider: model is empty (set ai.model)")
	}

	body := openAIChatRequest{
		Model:       model,
		Messages:    buildOpenAIMessages(req),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var oresp openAIChatResponse
	if err := json.Unmarshal(raw, &oresp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}

	text := ""
	stopReason := "end_turn"
	if len(oresp.Choices) > 0 {
		text = oresp.Choices[0].Message.Content
		if oresp.Choices[0].FinishReason != "" {
			stopReason = oresp.Choices[0].FinishReason
		}
	}

	return Response{
		Text:         text,
		StopReason:   stopReason,
		InputTokens:  oresp.Usage.PromptTokens,
		OutputTokens: oresp.Usage.CompletionTokens,
	}, nil
}

// Stream sends a streaming chat completion. OpenAI uses SSE (data: lines),
// each containing a chunk with delta.content for incremental text.
func (p *OpenAIProvider) Stream(ctx context.Context, req Request, onToken func(string)) (Response, error) {
	model := req.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("openai provider: model is empty (set ai.model)")
	}

	body := openAIChatRequest{
		Model:       model,
		Messages:    buildOpenAIMessages(req),
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.httpClient().Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return Response{}, fmt.Errorf("openai api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var sb strings.Builder
	out := Response{StopReason: "end_turn"}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 {
			c := chunk.Choices[0]
			if c.Delta.Content != "" {
				sb.WriteString(c.Delta.Content)
				if onToken != nil {
					onToken(c.Delta.Content)
				}
			}
			if c.FinishReason != "" {
				out.StopReason = c.FinishReason
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read stream: %w", err)
	}

	out.Text = sb.String()
	return out, nil
}

// buildOpenAIMessages converts a Request into OpenAI's messages format.
// OpenAI uses system/user/assistant roles in a flat array.
func buildOpenAIMessages(req Request) []openAIMessage {
	msgs := make([]openAIMessage, 0, len(req.Messages)+2)

	if req.System != "" {
		msgs = append(msgs, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openAIMessage{Role: m.Role, Content: m.Content})
	}
	msgs = append(msgs, openAIMessage{Role: "user", Content: req.Prompt})

	return msgs
}
