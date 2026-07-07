package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOllama_Complete verifies non-streaming Complete against a mock Ollama server.
func TestOllama_Complete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"llama3.2","message":{"role":"assistant","content":"Hello!"},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":3}`)
	}))
	defer srv.Close()

	p := &OllamaProvider{BaseURL: srv.URL, DefaultModel: "llama3.2"}
	resp, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "Hello!" {
		t.Errorf("Text=%q want Hello!", resp.Text)
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason=%q want stop", resp.StopReason)
	}
	if resp.InputTokens != 5 {
		t.Errorf("InputTokens=%d want 5", resp.InputTokens)
	}
	if resp.OutputTokens != 3 {
		t.Errorf("OutputTokens=%d want 3", resp.OutputTokens)
	}
}

// TestOllama_Stream verifies NDJSON streaming against a mock server.
func TestOllama_Stream(t *testing.T) {
	// Simulate Ollama NDJSON: 3 chunks, last has done=true.
	ndjson := `{"model":"llama3.2","message":{"role":"assistant","content":"Hello"},"done":false}
{"model":"llama3.2","message":{"role":"assistant","content":" world"},"done":false}
{"model":"llama3.2","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":12}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, ndjson)
	}))
	defer srv.Close()

	var tokens []string
	p := &OllamaProvider{BaseURL: srv.URL, DefaultModel: "llama3.2"}
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
		t.Errorf("tokens=%v want 2 non-empty chunks", tokens)
	}
	if resp.OutputTokens != 12 {
		t.Errorf("OutputTokens=%d want 12", resp.OutputTokens)
	}
}

// TestOllama_HTTPError verifies error on non-2xx.
func TestOllama_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"model not found"}`)
	}))
	defer srv.Close()

	p := &OllamaProvider{BaseURL: srv.URL, DefaultModel: "nonexistent"}
	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

// TestOllama_BuildOllamaMessages verifies message format (system + history + prompt).
func TestOllama_BuildOllamaMessages(t *testing.T) {
	msgs := buildOllamaMessages(Request{
		System: "You are helpful.",
		Messages: []Message{
			{Role: "user", Content: "previous question"},
			{Role: "assistant", Content: "previous answer"},
		},
		Prompt: "current question",
	})
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "You are helpful." {
		t.Errorf("msg[0] = %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "previous question" {
		t.Errorf("msg[1] = %+v", msgs[1])
	}
	if msgs[2].Role != "assistant" {
		t.Errorf("msg[2] role = %q", msgs[2].Role)
	}
	if msgs[3].Role != "user" || msgs[3].Content != "current question" {
		t.Errorf("msg[3] = %+v", msgs[3])
	}
}
