package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestComplete_RetryOn429 verifies that Complete retries on 429 and succeeds
// once the server returns 200.
func TestComplete_RetryOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests) // 429
			fmt.Fprint(w, `{"error":"rate limited"}`)
			return
		}
		// Third attempt succeeds.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"m","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		BaseURL:        srv.URL,
		APIKey:         "test",
		DefaultModel:   "m",
		MaxRetries:     3,
		RetryBaseDelay: 10 * time.Millisecond, // fast for tests
	}

	resp, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "hello" {
		t.Errorf("Text = %q, want hello", resp.Text)
	}
	if resp.Retries != 2 {
		t.Errorf("Retries = %d, want 2", resp.Retries)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("server attempts = %d, want 3", attempts)
	}
}

// TestComplete_NoRetryOn400 verifies that 400 is NOT retried.
func TestComplete_NoRetryOn400(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		BaseURL:        srv.URL,
		APIKey:         "test",
		DefaultModel:   "m",
		MaxRetries:     3,
		RetryBaseDelay: 10 * time.Millisecond,
	}

	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("server attempts = %d, want 1 (no retry on 400)", attempts)
	}
}

// TestComplete_RetryExhausted verifies that retries stop after MaxRetries.
func TestComplete_RetryExhausted(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable) // 503
	}))
	defer srv.Close()

	p := &AnthropicProvider{
		BaseURL:        srv.URL,
		APIKey:         "test",
		DefaultModel:   "m",
		MaxRetries:     2,
		RetryBaseDelay: 10 * time.Millisecond,
	}

	_, err := p.Complete(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	// MaxRetries=2 → 3 attempts total (1 initial + 2 retries)
	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Errorf("server attempts = %d, want 3", n)
	}
}
