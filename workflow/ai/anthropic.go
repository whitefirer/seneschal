package ai

import (
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
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContent{{Type: "text", Text: req.Prompt}}},
		},
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

// Stream is implemented in M2. For now it falls back to Complete, invoking
// onToken once with the full text so callers coded against streaming still
// work (just without incremental display).
func (p *AnthropicProvider) Stream(ctx context.Context, req Request, onToken func(string)) (Response, error) {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	if onToken != nil && resp.Text != "" {
		onToken(resp.Text)
	}
	return resp, nil
}
