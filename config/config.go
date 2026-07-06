package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ServerConfig holds server configuration.
type ServerConfig struct {
	// Host is the bind address. Defaults to "127.0.0.1" so the server is only
	// reachable locally; set to "0.0.0.0" explicitly to expose it (make sure
	// you understand the security implications — see ARCHITECTURE.md).
	Host           string   `yaml:"host"`
	Port           string   `yaml:"port"`
	WorkflowsDir   string   `yaml:"workflows_dir"`
	// ExecutionsDir is where execution history snapshots are persisted.
	// Defaults to "./executions". Contains potentially sensitive data and is
	// gitignored — do not commit this directory.
	ExecutionsDir  string   `yaml:"executions_dir"`
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// Default returns a config with safe defaults.
func Default() *ServerConfig {
	return &ServerConfig{
		Host:          "127.0.0.1",
		Port:          "8888",
		WorkflowsDir:  "./workflows/user",
		ExecutionsDir: "./executions",
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

// CheckOrigin returns an origin checker for the config's allowed origins.
// An empty list means all origins are allowed.
func (c *ServerConfig) CheckOrigin() func(r *http.Request) bool {
	if len(c.AllowedOrigins) == 0 {
		return func(r *http.Request) bool { return true }
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		for _, allowed := range c.AllowedOrigins {
			if origin == allowed {
				return true
			}
			// Also match scheme+hostname (ignore port differences)
			u, err := url.Parse(origin)
			if err != nil {
				continue
			}
			a, err := url.Parse(allowed)
			if err != nil {
				continue
			}
			if u.Scheme == a.Scheme && u.Hostname() == a.Hostname() {
				return true
			}
		}
		return false
	}
}

// PortFlag parses --port from os.Args. Returns "" if not set.
func PortFlag() string {
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			parts := strings.SplitN(os.Args[i+1], "=", 2)
			if len(parts) == 2 {
				return parts[1]
			}
			return os.Args[i+1]
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
