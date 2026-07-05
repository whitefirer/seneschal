package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStream_SSE verifies the Anthropic SSE parser accumulates text deltas
// and reports token counts, using a mock SSE server.
func TestStream_SSE(t *testing.T) {
	// Simulate an Anthropic-style SSE stream: message_start (with input
	// tokens), several content_block_delta events, then message_stop.
	const sse = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":", "}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"world!"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sse))
	}))
	defer srv.Close()

	var got []string
	p := &AnthropicProvider{BaseURL: srv.URL, APIKey: "test", DefaultModel: "m"}
	resp, err := p.Stream(context.Background(), Request{Prompt: "hi"}, func(tok string) {
		got = append(got, tok)
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Text != "Hello, world!" {
		t.Errorf("Text = %q, want %q", resp.Text, "Hello, world!")
	}
	if resp.InputTokens != 12 {
		t.Errorf("InputTokens = %d, want 12", resp.InputTokens)
	}
	if resp.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", resp.OutputTokens)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", resp.StopReason)
	}
	// onToken should have been called once per delta.
	if len(got) != 3 || strings.Join(got, "") != "Hello, world!" {
		t.Errorf("onToken calls = %v, want 3 deltas joining to full text", got)
	}
}

// TestStream_NilCallback verifies Stream works when onToken is nil.
func TestStream_NilCallback(t *testing.T) {
	const sse = `event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"abc"}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sse))
	}))
	defer srv.Close()

	p := &AnthropicProvider{BaseURL: srv.URL, APIKey: "test", DefaultModel: "m"}
	resp, err := p.Stream(context.Background(), Request{Prompt: "hi"}, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Text != "abc" {
		t.Errorf("Text = %q, want abc", resp.Text)
	}
}

// TestComplete_HTTPError verifies a non-2xx response surfaces a clear error.
func TestComplete_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","error":{"message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	p := &AnthropicProvider{BaseURL: srv.URL, APIKey: "bad", DefaultModel: "m"}
	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}
