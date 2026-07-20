package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

// The full /api/chat SSE flow needs a real AI provider (keys + network), so
// the masking contract is verified at the enrichSelection level — the exact
// function that builds the selection payload the frontend card renders from.

// selectionToolOutput builds a select_workflow tool result the way
// ExecuteTool does: human-readable text plus the hidden [JSON:...] marker.
func selectionToolOutput(t *testing.T, sel ai.Selection) string {
	t.Helper()
	out, err := json.Marshal(sel)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("找到匹配的工作流: %s\n建议变量:\n\n[JSON:%s]", sel.Workflow, out)
}

func setupChatRegistry(t *testing.T, fileName, yaml string) *chatToolExecutor {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	return &chatToolExecutor{registry: workflow.NewDirRegistry(dir)}
}

func TestEnrichSelection_SensitiveKeys(t *testing.T) {
	exec := setupChatRegistry(t, "deploy.yaml", `name: deploy
sensitive:
  - "*_key"
  - token
variables:
  env: staging
steps:
  - name: hi
    action: shell
    command: echo hi
`)

	raw := selectionToolOutput(t, ai.Selection{
		Workflow:   "deploy",
		Confidence: 0.9,
		Variables: map[string]string{
			"api_key": "real-secret-value",
			"token":   "tok-123",
			"env":     "prod",
		},
	})
	data := exec.enrichSelection(raw)
	if data == nil {
		t.Fatal("enrichSelection returned nil")
	}

	// Execution contract: suggested values stay real — the frontend posts
	// selection.variables to /run on confirm, so masking them in the payload
	// would execute the workflow with literal "***".
	vars, _ := data["variables"].(map[string]string)
	if vars["api_key"] != "real-secret-value" || vars["token"] != "tok-123" || vars["env"] != "prod" {
		t.Errorf("variables must keep real values, got %v", vars)
	}

	// Display contract: the keys matching the workflow's sensitive: patterns
	// are listed so the card can mask them (sorted, glob "*_key" matched).
	keys, _ := data["sensitiveKeys"].([]string)
	want := []string{"api_key", "token"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("sensitiveKeys=%v want %v", keys, want)
	}
}

func TestEnrichSelection_NoSensitiveDeclaration(t *testing.T) {
	exec := setupChatRegistry(t, "plain.yaml", `name: plain
variables:
  env: staging
steps:
  - name: hi
    action: shell
    command: echo hi
`)

	raw := selectionToolOutput(t, ai.Selection{
		Workflow:   "plain",
		Confidence: 0.8,
		Variables:  map[string]string{"env": "prod"},
	})
	data := exec.enrichSelection(raw)
	if data == nil {
		t.Fatal("enrichSelection returned nil")
	}
	// Without a sensitive: declaration the payload must be exactly as before.
	if _, ok := data["sensitiveKeys"]; ok {
		t.Errorf("sensitiveKeys must be absent without a sensitive: declaration, got %v", data["sensitiveKeys"])
	}
	vars, _ := data["variables"].(map[string]string)
	if vars["env"] != "prod" {
		t.Errorf("variables=%v", vars)
	}
}

func TestSensitiveDisplayKeys(t *testing.T) {
	vars := map[string]string{"password": "s3cret", "DB_PASS": "x", "env": "prod"}

	// Glob + exact patterns, sorted output; value "***" passes through
	// undetected by design (it displays as "***" anyway).
	got := sensitiveDisplayKeys(vars, []string{"*_PASS", "password"})
	want := []string{"DB_PASS", "password"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}

	if got := sensitiveDisplayKeys(vars, nil); got != nil {
		t.Errorf("nil patterns: got %v want nil", got)
	}
	if got := sensitiveDisplayKeys(nil, []string{"*"}); got != nil {
		t.Errorf("nil vars: got %v want nil", got)
	}
}
