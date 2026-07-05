package ai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestStream_DeepSeekLive is a live integration test against DeepSeek's
// Anthropic-compatible endpoint. It only runs when DEEPSEEK_API_KEY is set,
// so CI / default `go test` skips it. Run manually:
//
//	DEEPSEEK_API_KEY=sk-... go test ./workflow/ai/ -run TestStream_DeepSeekLive -v
func TestStream_DeepSeekLive(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set; skipping live test")
	}

	p := &AnthropicProvider{
		APIKey:       key,
		DefaultModel: "deepseek-chat",
		HTTPClient:   &http.Client{Timeout: 60 * time.Second},
	}

	var tokenCount int
	var last string
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := p.Stream(ctx, Request{Prompt: "用一句话(20字内)说明 Go 是什么。"}, func(tok string) {
		tokenCount++
		last = tok
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if strings.TrimSpace(resp.Text) == "" {
		t.Fatal("empty response text")
	}
	if tokenCount < 2 {
		t.Errorf("expected multiple incremental tokens, got %d (streaming may have fallen back to single delivery)", tokenCount)
	}
	t.Logf("tokens received: %d | in=%d out=%d stop=%s", tokenCount, resp.InputTokens, resp.OutputTokens, resp.StopReason)
	t.Logf("last delta: %q", last)
	t.Logf("full text: %s", resp.Text)
}
