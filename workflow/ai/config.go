package ai

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config is the workflow-level AI configuration (the `ai:` block in YAML).
//
// It intentionally never contains an API key — keys are read from environment
// variables at provider construction time (see BuildProvider). This enforces
// the security rule that keys never enter workflow YAML and thus never get
// persisted to disk by SaveWorkflow.
type Config struct {
	// Provider selects the backend protocol. Currently only "anthropic"
	// (covers Claude and DeepSeek via base URL). "openai" and "ollama" are
	// planned (ROADMAP Phase 8).
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`

	// Model is the default model id for ai/ai_decide steps that do not
	// specify their own (e.g. "deepseek-v4-flash", "claude-sonnet-4-5-20250929").
	Model string `yaml:"model,omitempty" json:"model,omitempty"`

	// BaseURL overrides the provider's default API root. Useful for
	// switching between Claude (api.anthropic.com) and DeepSeek
	// (api.deepseek.com/anthropic), or a self-hosted gateway. If empty the
	// provider picks a sensible default.
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"`

	// MaxTokens is the default response cap for steps that do not set their
	// own. Defaults to 1024.
	MaxTokens int `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`

	// Temperature is the default sampling temperature. Defaults to 0.
	Temperature float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`

	// Budget is the workflow-level total token budget (input + output combined).
	// 0 = unlimited (default). When the cumulative token usage across all AI
	// steps in a workflow exceeds this, subsequent AI steps are skipped.
	Budget int `yaml:"budget,omitempty" json:"budget,omitempty"`

	// MemoryWindow is the max number of prior AI turns to include as context
	// for each AI step (0 = unlimited, default). Older turns are truncated.
	// Each turn = one user + one assistant message (2 items in aiHistory).
	MemoryWindow int `yaml:"memory_window,omitempty" json:"memory_window,omitempty"`

	// OnError enables AI-assisted error recovery for ALL steps when set to
	// "ai" (suggest-only, safest) or "ai_auto" (auto retry/skip/abort).
	// Individual steps can override via step.on_error. "" = off (default).
	OnError string `yaml:"on_error,omitempty" json:"on_error,omitempty"`
}

// IsZero reports whether no AI configuration was provided.
func (c Config) IsZero() bool {
	return c.Provider == "" && c.Model == "" && c.BaseURL == "" && c.MaxTokens == 0 && c.Temperature == 0
}

// BuildProvider constructs a Provider from the given workflow-level config,
// reading credentials and base URL from environment variables.
//
// Environment variables (checked in order, first non-empty wins):
//   - API key:   ANTHROPIC_API_KEY, then DEEPSEEK_API_KEY
//   - Base URL:  ANTHROPIC_BASE_URL (overrides Config.BaseURL)
//
// Returns an error if the provider is unknown or the required key is missing.
func BuildProvider(cfg Config) (Provider, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = "anthropic" // default; matches the first implemented provider
	}

	switch provider {
	case "anthropic":
		apiKey := firstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("DEEPSEEK_API_KEY"))
		if apiKey == "" {
			return nil, fmt.Errorf("ai provider %q requires an API key: set ANTHROPIC_API_KEY or DEEPSEEK_API_KEY in the environment", provider)
		}
		baseURL := cfg.BaseURL
		if env := os.Getenv("ANTHROPIC_BASE_URL"); env != "" {
			baseURL = env
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("ai provider %q requires a model: set ai.model in the workflow YAML or ai.Config.Model (e.g. \"deepseek-v4-flash\", \"claude-sonnet-4-5-20250929\")", provider)
		}
		return &AnthropicProvider{
			BaseURL:      baseURL,
			APIKey:       apiKey,
			DefaultModel: cfg.Model,
			HTTPClient: &http.Client{
				Timeout: 120 * time.Second,
			},
		}, nil
	case "ollama":
		baseURL := cfg.BaseURL
		if env := os.Getenv("OLLAMA_HOST"); env != "" {
			// OLLAMA_HOST may be "host:port" without scheme.
			if !strings.HasPrefix(env, "http") {
				env = "http://" + env
			}
			baseURL = env
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("ai provider %q requires a model: set ai.model (e.g. \"llama3.2\", \"qwen3:32b\")", provider)
		}
		return &OllamaProvider{
			BaseURL:      baseURL,
			DefaultModel: cfg.Model,
			HTTPClient: &http.Client{
				Timeout: 300 * time.Second, // local models can be slow
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown ai provider %q (supported: anthropic, ollama)", provider)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
