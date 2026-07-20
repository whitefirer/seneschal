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
	"time"
)

// OllamaProvider implements Provider against a local Ollama server
// (POST /api/chat). Ollama is zero-config: no API key, no network, runs
// entirely locally. The API uses NDJSON streaming (one JSON object per line),
// which differs from Anthropic's SSE.
//
// Tool use is not supported in this implementation (Ollama's tool format
// differs from Anthropic's). The agent loop falls back to plain Complete.
type OllamaProvider struct {
	// BaseURL is the Ollama server root. Defaults to http://localhost:11434.
	BaseURL string

	// DefaultModel is the Ollama model tag, e.g. "llama3.2", "qwen3:32b".
	DefaultModel string

	// HTTPClient used for requests. If nil, http.DefaultClient.
	HTTPClient *http.Client

	// MaxRetries is the maximum number of automatic retries on transient
	// errors (429/5xx/timeout). 0 or negative (the default) disables retries,
	// preserving the pre-retry behavior.
	MaxRetries int

	// RetryBaseDelay is the initial backoff delay; doubled each retry
	// (capped at 30s). 0 = 1s default.
	RetryBaseDelay time.Duration
}

func (p *OllamaProvider) Name() string { return "ollama" }

func (p *OllamaProvider) GetModel() string { return p.DefaultModel }

func (p *OllamaProvider) baseURL() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return "http://localhost:11434"
}

func (p *OllamaProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

func (p *OllamaProvider) maxRetries() int {
	if p.MaxRetries > 0 {
		return p.MaxRetries
	}
	return 0 // retries are opt-in for Ollama (unlike Anthropic's default 3)
}

func (p *OllamaProvider) retryBaseDelay() time.Duration {
	if p.RetryBaseDelay > 0 {
		return p.RetryBaseDelay
	}
	return 1 * time.Second
}

// ollamaChatRequest is the body for POST /api/chat.
type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"` // max tokens
}

// ollamaChatResponse is the non-streaming response (stream:false).
type ollamaChatResponse struct {
	Model      string        `json:"model"`
	Message    ollamaMessage `json:"message"`
	Done       bool          `json:"done"`
	DoneReason string        `json:"done_reason,omitempty"`
	// Usage fields (present when done=true in streaming; in non-streaming
	// they're at the top level).
	PromptEvalCount int `json:"prompt_eval_count,omitempty"`
	EvalCount       int `json:"eval_count,omitempty"`
}

// ollamaStreamChunk is one NDJSON line in a streaming response.
type ollamaStreamChunk struct {
	Model           string        `json:"model"`
	Message         ollamaMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason,omitempty"`
	PromptEvalCount int           `json:"prompt_eval_count,omitempty"`
	EvalCount       int           `json:"eval_count,omitempty"`
}

// Complete sends a non-streaming chat request.
func (p *OllamaProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("ollama provider: model is empty (set ai.model)")
	}

	body := ollamaChatRequest{
		Model:    model,
		Messages: buildOllamaMessages(req),
		Stream:   false,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, retries, err := retryableHTTPDo(ctx, p.httpClient(), p.maxRetries(), p.retryBaseDelay(), httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("ollama api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var oresp ollamaChatResponse
	if err := json.Unmarshal(raw, &oresp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}

	stopReason := "end_turn"
	if oresp.DoneReason != "" {
		stopReason = oresp.DoneReason
	}

	return Response{
		Text:         oresp.Message.Content,
		StopReason:   stopReason,
		InputTokens:  oresp.PromptEvalCount,
		OutputTokens: oresp.EvalCount,
		Retries:      retries,
	}, nil
}

// Stream sends a streaming chat request. Ollama streams NDJSON (one JSON
// object per line). Each chunk's message.content is an incremental text
// fragment.
func (p *OllamaProvider) Stream(ctx context.Context, req Request, onToken func(string)) (Response, error) {
	model := req.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("ollama provider: model is empty (set ai.model)")
	}

	body := ollamaChatRequest{
		Model:    model,
		Messages: buildOllamaMessages(req),
		Stream:   true,
		Options: ollamaOptions{
			Temperature: req.Temperature,
			NumPredict:  req.MaxTokens,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, retries, err := retryableHTTPDo(ctx, p.httpClient(), p.maxRetries(), p.retryBaseDelay(), httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return Response{}, fmt.Errorf("ollama api error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var sb strings.Builder
	out := Response{StopReason: "end_turn", Retries: retries}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ollamaStreamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue // skip malformed lines
		}
		if chunk.Message.Content != "" {
			sb.WriteString(chunk.Message.Content)
			if onToken != nil {
				onToken(chunk.Message.Content)
			}
		}
		if chunk.Done {
			out.InputTokens = chunk.PromptEvalCount
			out.OutputTokens = chunk.EvalCount
			if chunk.DoneReason != "" {
				out.StopReason = chunk.DoneReason
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("read stream: %w", err)
	}

	out.Text = sb.String()
	return out, nil
}

// buildOllamaMessages converts a Request into Ollama's messages format.
// Ollama uses a flat messages array with role system/user/assistant, and a
// separate top-level system is not supported (system goes as a message).
func buildOllamaMessages(req Request) []ollamaMessage {
	msgs := make([]ollamaMessage, 0, len(req.Messages)+2)

	// System prompt as the first message (Ollama convention).
	if req.System != "" {
		msgs = append(msgs, ollamaMessage{Role: "system", Content: req.System})
	}

	// Conversation history.
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaMessage{Role: m.Role, Content: m.Content})
	}

	// Current user prompt.
	msgs = append(msgs, ollamaMessage{Role: "user", Content: req.Prompt})

	return msgs
}
