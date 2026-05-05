package workflow

import (
	"os"
	"testing"
)

func TestNewContext(t *testing.T) {
	ctx := NewContext(map[string]string{"key": "val"})
	if ctx.Get("key") != "val" {
		t.Errorf("expected 'val', got '%s'", ctx.Get("key"))
	}
}

func TestNewContextNil(t *testing.T) {
	ctx := NewContext(nil)
	if ctx.Variables == nil {
		t.Error("Variables map should be initialized for nil input")
	}
	if ctx.Results == nil {
		t.Error("Results map should be initialized for nil input")
	}
}

func TestContextSetGet(t *testing.T) {
	ctx := NewContext(nil)
	ctx.Set("foo", "bar")
	if ctx.Get("foo") != "bar" {
		t.Errorf("expected 'bar', got '%s'", ctx.Get("foo"))
	}
}

func TestResolveTemplate(t *testing.T) {
	ctx := NewContext(map[string]string{"name": "world", "count": "42"})
	ctx.Results["step1"] = "hello"

	tests := []struct {
		name     string
		tmpl     string
		expected string
		wantErr  bool
	}{
		{"simple var", "{{.name}}", "world", false},
		{"no template", "hello there", "hello there", false},
		{"empty string", "", "", false},
		{"result var", "step1={{.step1}}", "step1=hello", false},
		{"mixed", "{{.name}} has {{.count}} items", "world has 42 items", false},
		{"missing var", "{{.missing}}", "<no value>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ctx.ResolveTemplate(tt.tmpl)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestResolveTemplateError(t *testing.T) {
	ctx := NewContext(nil)
	_, err := ctx.ResolveTemplate("{{.name")
	if err == nil {
		t.Error("expected template parse error")
	}
}

func TestMergeEnv(t *testing.T) {
	ctx := NewContext(map[string]string{"BASE": "val1", "OVERRIDE": "orig"})
	env, err := ctx.MergeEnv(map[string]string{"STEP_KEY": "step-val", "OVERRIDE": "{{.BASE}}"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env["BASE"] != "val1" {
		t.Errorf("BASE: expected 'val1', got '%s'", env["BASE"])
	}
	if env["STEP_KEY"] != "step-val" {
		t.Errorf("STEP_KEY: expected 'step-val', got '%s'", env["STEP_KEY"])
	}
	if env["OVERRIDE"] != "val1" {
		t.Errorf("OVERRIDE: expected 'val1' (resolved), got '%s'", env["OVERRIDE"])
	}
}

func TestNow(t *testing.T) {
	ts := Now()
	if len(ts) < 20 {
		t.Errorf("timestamp too short: %s", ts)
	}
}

func TestParseDuration(t *testing.T) {
	d, err := ParseDuration("5s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Seconds() != 5 {
		t.Errorf("expected 5s, got %v", d)
	}

	_, err = ParseDuration("invalid")
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestResolveTemplateFromFile(t *testing.T) {
	ctx := NewContext(map[string]string{"var": "world"})

	tmpFile, err := os.CreateTemp("", "template-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "Hello, {{.var}}!"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tmpFile.Close()

	result, err := ctx.ResolveTemplateFromFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got '%s'", result)
	}
}

func TestResolveTemplateFromFileNotFound(t *testing.T) {
	ctx := NewContext(nil)
	_, err := ctx.ResolveTemplateFromFile("/nonexistent/path/template.txt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
