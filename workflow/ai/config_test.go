package ai

import (
	"strings"
	"testing"
)

func TestConfig_IsZero(t *testing.T) {
	if !(Config{}).IsZero() {
		t.Error("empty config should be zero")
	}
	if (Config{Model: "m"}).IsZero() {
		t.Error("config with model should not be zero")
	}
	if (Config{Temperature: 0.5}).IsZero() {
		t.Error("config with temperature should not be zero")
	}
}

func TestBuildProvider_Anthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	p, err := BuildProvider(Config{Provider: "anthropic", Model: "claude-x"})
	if err != nil {
		t.Fatalf("BuildProvider: %v", err)
	}
	ap, ok := p.(*AnthropicProvider)
	if !ok {
		t.Fatalf("expected *AnthropicProvider, got %T", p)
	}
	if ap.APIKey != "sk-ant-test" || ap.DefaultModel != "claude-x" {
		t.Errorf("unexpected provider fields: %+v", ap)
	}
}

func TestBuildProvider_Anthropic_DefaultsAndEnvBaseURL(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-deepseek-test")
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.deepseek.com/anthropic")
	p, err := BuildProvider(Config{Model: "deepseek-v4"})
	if err != nil {
		t.Fatalf("BuildProvider default provider: %v", err)
	}
	ap := p.(*AnthropicProvider)
	if ap.APIKey != "sk-deepseek-test" {
		t.Errorf("expected DEEPSEEK_API_KEY to be used, got %q", ap.APIKey)
	}
	if ap.BaseURL != "https://api.deepseek.com/anthropic" {
		t.Errorf("expected env base URL override, got %q", ap.BaseURL)
	}
}

func TestBuildProvider_Anthropic_MissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err := BuildProvider(Config{Provider: "anthropic", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("expected API key error, got %v", err)
	}
}

func TestBuildProvider_Anthropic_MissingModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	_, err := BuildProvider(Config{Provider: "anthropic"})
	if err == nil || !strings.Contains(err.Error(), "requires a model") {
		t.Fatalf("expected model error, got %v", err)
	}
}

func TestBuildProvider_OpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
	p, err := BuildProvider(Config{Provider: "openai", Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("BuildProvider openai: %v", err)
	}
	op := p.(*OpenAIProvider)
	if op.APIKey != "sk-openai-test" || op.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("unexpected openai fields: %+v", op)
	}
}

func TestBuildProvider_OpenAI_MissingModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")
	_, err := BuildProvider(Config{Provider: "openai"})
	if err == nil || !strings.Contains(err.Error(), "requires a model") {
		t.Fatalf("expected model error, got %v", err)
	}
}

func TestBuildProvider_OpenAI_OfficialEndpointRequiresKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	_, err := BuildProvider(Config{Provider: "openai", Model: "gpt-4o"})
	if err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("expected API key error for official endpoint, got %v", err)
	}
}

func TestBuildProvider_OpenAI_LocalBaseURLAllowsEmptyKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "http://localhost:1234/v1")
	p, err := BuildProvider(Config{Provider: "openai", Model: "local-model"})
	if err != nil {
		t.Fatalf("local openai-compatible provider should not require a key: %v", err)
	}
	op := p.(*OpenAIProvider)
	if op.BaseURL != "http://localhost:1234/v1" || op.DefaultModel != "local-model" {
		t.Errorf("unexpected provider fields: %+v", op)
	}
}

func TestBuildProvider_Ollama(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "127.0.0.1:11434")
	p, err := BuildProvider(Config{Provider: "ollama", Model: "llama3.2"})
	if err != nil {
		t.Fatalf("BuildProvider ollama: %v", err)
	}
	op := p.(*OllamaProvider)
	if op.BaseURL != "http://127.0.0.1:11434" || op.DefaultModel != "llama3.2" {
		t.Errorf("unexpected ollama fields: %+v", op)
	}
}

func TestBuildProvider_Ollama_HTTPPrefixPreserved(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://localhost:11434")
	p, err := BuildProvider(Config{Provider: "ollama", Model: "m"})
	if err != nil {
		t.Fatalf("BuildProvider ollama: %v", err)
	}
	if got := p.(*OllamaProvider).BaseURL; got != "http://localhost:11434" {
		t.Errorf("base URL with http prefix should be preserved, got %q", got)
	}
}

func TestBuildProvider_UnknownProvider(t *testing.T) {
	_, err := BuildProvider(Config{Provider: "unknown", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "unknown ai provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}
