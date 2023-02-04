package workflow

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"text/template"
	"time"
)

// Context holds the execution state of a workflow.
type Context struct {
	Variables map[string]string
	Results   map[string]string // step output results
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

// Set sets a variable in the context.
func (c *Context) Set(key, value string) {
	c.Variables[key] = value
}

// Get retrieves a variable from the context.
func (c *Context) Get(key string) string {
	return c.Variables[key]
}

// ResolveTemplate substitutes variables in a template string.
// Supports {{.var}} syntax.
func (c *Context) ResolveTemplate(tmplStr string) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	// Build a data map from variables
	data := make(map[string]string)
	for k, v := range c.Variables {
		data[k] = v
	}
	for k, v := range c.Results {
		data[k] = v
	}

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
	// Resolve path template first
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
	env := make(map[string]string)

	// First add context variables
	for k, v := range c.Variables {
		env[k] = v
	}

	// Then override with step env (after resolving templates)
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
