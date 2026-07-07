package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAI_Complete verifies non-streaming Complete against a mock server.
func TestOpenAI_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// Verify Bearer auth.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing Bearer auth: %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`)
	}))
	defer srv.Close()

	p := &OpenAIProvider{BaseURL: srv.URL, APIKey: "test-key", DefaultModel: "gpt-4o"}
	resp, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "Hello!" {
		t.Errorf("Text=%q want Hello!", resp.Text)
	}
	if resp.InputTokens != 5 {
		t.Errorf("InputTokens=%d want 5", resp.InputTokens)
	}
	if resp.OutputTokens != 3 {
		t.Errorf("OutputTokens=%d want 3", resp.OutputTokens)
	}
}

// TestOpenAI_Stream verifies SSE streaming against a mock server.
func TestOpenAI_Stream(t *testing.T) {
	sse := `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}

data: {"id":"1","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()

	var tokens []string
	p := &OpenAIProvider{BaseURL: srv.URL, APIKey: "test-key", DefaultModel: "gpt-4o"}
	resp, err := p.Stream(context.Background(), Request{Prompt: "hi"}, func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.Text != "Hello world" {
		t.Errorf("Text=%q want 'Hello world'", resp.Text)
	}
	if len(tokens) != 2 {
		t.Errorf("tokens=%v want 2", tokens)
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason=%q want stop", resp.StopReason)
	}
}

// TestOpenAI_HTTPError verifies error on non-2xx.
func TestOpenAI_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	p := &OpenAIProvider{BaseURL: srv.URL, APIKey: "bad", DefaultModel: "gpt-4o"}
	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

// TestOpenAI_BuildMessages verifies message format.
func TestOpenAI_BuildMessages(t *testing.T) {
	msgs := buildOpenAIMessages(Request{
		System: "You are helpful.",
		Messages: []Message{
			{Role: "user", Content: "previous"},
			{Role: "assistant", Content: "answer"},
		},
		Prompt: "current",
	})
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "You are helpful." {
		t.Errorf("msg[0] = %+v", msgs[0])
	}
	if msgs[3].Role != "user" || msgs[3].Content != "current" {
		t.Errorf("msg[3] = %+v", msgs[3])
	}
}
