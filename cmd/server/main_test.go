package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/whitefirer/seneschal/config"
	"github.com/whitefirer/seneschal/workflow"
)

func TestResolveDir_AbsolutePassthrough(t *testing.T) {
	got, err := resolveDir("/tmp/foo", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/foo" {
		t.Errorf("absolute path should pass through, got %q", got)
	}
}

func TestResolveDir_Relative(t *testing.T) {
	wd, _ := os.Getwd()
	got, err := resolveDir("foo/bar", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wd, "foo", "bar")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolveDir_EmptyUsesFallback(t *testing.T) {
	wd, _ := os.Getwd()
	got, err := resolveDir("", "./executions")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wd, "executions")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestBuildWorkflowAIConfig(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.AI.Provider = "openai"
	cfg.AI.Model = "gpt-4o"
	cfg.AI.BaseURL = "https://api.openai.com/v1"
	cfg.AI.MaxTokens = 4096
	cfg.AI.Temperature = 0.7

	got := buildWorkflowAIConfig(cfg)
	if got.Provider != "openai" || got.Model != "gpt-4o" || got.BaseURL != "https://api.openai.com/v1" || got.MaxTokens != 4096 || got.Temperature != 0.7 {
		t.Errorf("unexpected conversion: %+v", got)
	}
}

func TestBuildGlobalHooks(t *testing.T) {
	cfg := &config.ServerConfig{}
	cfg.Hooks = []config.HookConfig{
		{On: "after_step", Type: "webhook", URL: "https://example.com/hook", Mode: "ai_auto"},
		{On: "workflow_end", Type: "shell", Command: "echo done"},
	}
	hooks := buildGlobalHooks(cfg)
	if len(hooks) != 2 {
		t.Fatalf("want 2 hooks, got %d", len(hooks))
	}
	if hooks[0].On != workflow.HookAfterStep || hooks[0].URL != "https://example.com/hook" {
		t.Errorf("unexpected hook[0]: %+v", hooks[0])
	}
	if hooks[1].On != workflow.HookWorkflowEnd || hooks[1].Command != "echo done" {
		t.Errorf("unexpected hook[1]: %+v", hooks[1])
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"":            true,
		"localhost":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"0.0.0.0":     false,
		"::":          false,
		"192.168.1.5": false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q)=%v, want %v", host, got, want)
		}
	}
}

func TestStaticFiles_ContainsIndexHTML(t *testing.T) {
	data, err := fs.ReadFile(staticFiles, "static/index.html")
	if err != nil {
		t.Fatalf("embedded static/index.html not found: %v", err)
	}
	if len(data) < 64 {
		t.Errorf("index.html looks too small: %d bytes", len(data))
	}
}
