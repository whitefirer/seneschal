package config

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ServerConfig holds server configuration.
type ServerConfig struct {
	// Host is the bind address. Defaults to "127.0.0.1" so the server is only
	// reachable locally; set to "0.0.0.0" explicitly to expose it (make sure
	// you understand the security implications — see ARCHITECTURE.md).
	Host         string `yaml:"host"`
	Port         string `yaml:"port"`
	WorkflowsDir string `yaml:"workflows_dir"`
	// ExecutionsDir is where execution history snapshots are persisted.
	// Defaults to "./executions". Contains potentially sensitive data and is
	// gitignored — do not commit this directory.
	ExecutionsDir string `yaml:"executions_dir"`
	// RunbooksDir is where runbook (trigger/schedule) files live.
	// Defaults to "./runbooks". Hot-reloaded by the server.
	RunbooksDir    string   `yaml:"runbooks_dir"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	// AuthToken, when non-empty, protects all /api/ endpoints with Bearer-token
	// auth (`Authorization: Bearer <token>`; /api/ws also accepts ?token= for
	// browser WebSocket clients, which cannot set headers). Leave empty to
	// disable auth — acceptable only on a loopback bind; the server logs a
	// loud warning at startup when binding non-loopback without a token.
	AuthToken string `yaml:"auth_token"`
	// AI is the server-level AI configuration. Serves as the global default
	// for all workflows; a workflow's ai: block overrides per-workflow, and
	// step.model overrides per-step. API key still comes from environment
	// variables only (never written to YAML).
	AI AIConfig `yaml:"ai,omitempty"`
	// Hooks are server-level lifecycle hooks applied to ALL workflows.
	// Individual workflows and steps can add their own; all three levels merge.
	Hooks []HookConfig `yaml:"hooks,omitempty"`
}

// HookConfig is a server-level hook declaration. Mirrors workflow.HookConfig
// structurally; the server converts to workflow.HookConfig at runtime.
type HookConfig struct {
	On      string `yaml:"on"`
	When    string `yaml:"when,omitempty"`
	Type    string `yaml:"type"`
	URL     string `yaml:"url,omitempty"`
	Message string `yaml:"message,omitempty"`
	Command string `yaml:"command,omitempty"`
	Mode    string `yaml:"mode,omitempty"`
	Prompt  string `yaml:"prompt,omitempty"`
}

// AIConfig is the server-level AI provider configuration. It mirrors
// workflow.AI but lives in server.yaml as a global default.
type AIConfig struct {
	Provider    string  `yaml:"provider,omitempty"`
	Model       string  `yaml:"model,omitempty"`
	BaseURL     string  `yaml:"base_url,omitempty"`
	MaxTokens   int     `yaml:"max_tokens,omitempty"`
	Temperature float64 `yaml:"temperature,omitempty"`
}

// Default returns a config with safe defaults.
func Default() *ServerConfig {
	return &ServerConfig{
		Host:          "127.0.0.1",
		Port:          "8888",
		WorkflowsDir:  "./workflows/user",
		ExecutionsDir: "./executions",
		RunbooksDir:   "./runbooks",
		AllowedOrigins: []string{
			"http://localhost:8888",
			"http://localhost:5173",
			"http://127.0.0.1:8888",
			"http://127.0.0.1:5173",
		},
	}
}

// Addr returns the "host:port" listen address.
func (c *ServerConfig) Addr() string {
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return host + ":" + c.Port
}

// Load reads config from a YAML file. Falls back to Default on error.
func Load(path string) (*ServerConfig, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// CheckOrigin returns an origin checker for the WebSocket upgrader.
//
// Policy:
//   - AllowedOrigins empty: allow everything, including requests with no
//     Origin header. This exists for non-browser clients (curl, wscat,
//     server-to-server) that send no Origin header — do not use it on an
//     exposed interface.
//   - Otherwise the request's Origin must exactly match a whitelist entry
//     (full scheme://host:port equality, port included). Requests without an
//     Origin header are rejected; browsers always send Origin on WS upgrades.
func (c *ServerConfig) CheckOrigin() func(r *http.Request) bool {
	if len(c.AllowedOrigins) == 0 {
		return func(r *http.Request) bool { return true }
	}

	return func(r *http.Request) bool {
		return c.OriginAllowed(r.Header.Get("Origin"))
	}
}

// OriginAllowed reports whether origin exactly matches one of the configured
// AllowedOrigins entries (scheme://host:port — no wildcard, no port folding).
func (c *ServerConfig) OriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range c.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// PortFlag parses --port from os.Args (both "--port 8888" and "--port=8888"
// forms). Returns "" if not set.
func PortFlag() string {
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(a, "--port=") {
			return strings.TrimPrefix(a, "--port=")
		}
	}
	return ""
}

// HostFlag parses --host from os.Args (both "--host X" and "--host=X" forms).
// Returns "" if not set. Lets the bind address be overridden on the command
// line (e.g. by dev-dash) without relying on server.yaml.
func HostFlag() string {
	for i, a := range os.Args {
		if a == "--host" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(a, "--host=") {
			return strings.TrimPrefix(a, "--host=")
		}
	}
	return ""
}

// ConfigFlag parses --config from os.Args. Returns "" if not set.
func ConfigFlag() string {
	for i, a := range os.Args {
		if a == "--config" || a == "-c" {
			if i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
	}
	return ""
}
