package workflow

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"text/template"
	"time"
)

// Context holds the execution state of a workflow.
type Context struct {
	mu        sync.RWMutex
	Variables map[string]string
	Results   map[string]string
}

// NewContext creates a new execution context with initial variables.
func NewContext(variables map[string]string) *Context {
	if variables == nil {
		variables = make(map[string]string)
	}
	return &Context{
		Variables: variables,
		Results:   make(map[string]string),
	}
}

// Lock locks the context for writing.
func (c *Context) Lock()   { c.mu.Lock() }
func (c *Context) Unlock() { c.mu.Unlock() }

// RLock locks the context for reading.
func (c *Context) RLock()   { c.mu.RLock() }
func (c *Context) RUnlock() { c.mu.RUnlock() }

// Set sets a variable in the context.
func (c *Context) Set(key, value string) {
	c.mu.Lock()
	c.Variables[key] = value
	c.mu.Unlock()
}

// Get retrieves a variable from the context.
func (c *Context) Get(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Variables[key]
}

// GetOK retrieves a variable and reports whether it was set. Useful when an
// empty string is a legitimate value that should not be overwritten.
func (c *Context) GetOK(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.Variables[key]
	return v, ok
}

// SetResult stores a step result.
func (c *Context) SetResult(key, value string) {
	c.mu.Lock()
	c.Results[key] = value
	c.mu.Unlock()
}

// Snapshot returns a copy of all variables (for safe iteration).
func (c *Context) Snapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := make(map[string]string, len(c.Variables))
	for k, v := range c.Variables {
		m[k] = v
	}
	return m
}

// ResolveTemplate substitutes variables in a template string.
func (c *Context) ResolveTemplate(tmplStr string) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	c.mu.RLock()
	data := make(map[string]string)
	for k, v := range c.Variables {
		data[k] = v
	}
	for k, v := range c.Results {
		data[k] = v
	}
	c.mu.RUnlock()

	tmpl, err := template.New("").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute error: %w", err)
	}

	return buf.String(), nil
}

// ResolveTemplateFromFile reads a template file and substitutes variables.
func (c *Context) ResolveTemplateFromFile(filePath string) (string, error) {
	filePath, err := c.ResolveTemplate(filePath)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read template file: %w", err)
	}

	return c.ResolveTemplate(string(content))
}

// MergeEnv merges step-level env with context variables, resolving templates.
func (c *Context) MergeEnv(stepEnv map[string]string) (map[string]string, error) {
	c.mu.RLock()
	env := make(map[string]string, len(c.Variables))
	for k, v := range c.Variables {
		env[k] = v
	}
	c.mu.RUnlock()

	for k, v := range stepEnv {
		resolved, err := c.ResolveTemplate(v)
		if err != nil {
			return nil, err
		}
		env[k] = resolved
	}

	return env, nil
}

// Now returns the current time formatted as RFC3339.
func Now() string {
	return time.Now().Format(time.RFC3339)
}

// ParseDuration parses a duration string like "5s", "1m", "2h".
func ParseDuration(d string) (time.Duration, error) {
	return time.ParseDuration(d)
}

// Copy the reader to a string.
func ReadAll(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
