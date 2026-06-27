package ai

import (
	"fmt"
	"net/http"
	"os"
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
	// specify their own (e.g. "deepseek-chat", "claude-sonnet-4-5-20250929").
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
		return &AnthropicProvider{
			BaseURL:      baseURL,
			APIKey:       apiKey,
			DefaultModel: cfg.Model,
			HTTPClient: &http.Client{
				Timeout: 120 * time.Second, // generous default for model latency
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown ai provider %q (supported: anthropic)", provider)
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
