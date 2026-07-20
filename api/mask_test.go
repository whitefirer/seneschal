package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whitefirer/seneschal/workflow"
)

// TestE2E_SensitiveVariablesMasked runs a workflow that declares a sensitive
// variable (whose value also leaks into a step's output), then asserts:
//   - GET /api/executions/{id} serves the variable map with the sensitive
//     value masked ("***" from workflow.MaskVariables) and the plain one
//     visible;
//   - the raw secret appears nowhere in the response body (step outputs and
//     retained log lines carry the "******" mask instead);
//   - the persisted store snapshot keeps the real value (replay depends on
//     restoring real variables — masking must never touch the store);
//   - GET /api/executions summaries carry no variable map at all.
func TestE2E_SensitiveVariablesMasked(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	const secret = "super-secret-value-123"
	writeTestFile(t, e.dir, "sensitive.yaml", `name: sensitive-test
variables:
  token: super-secret-value-123
  env: prod
sensitive:
  - token
steps:
  - name: show
    action: shell
    command: echo "token={{.token}} env={{.env}}"
`)

	code, result := e.post("/api/workflows/sensitive.yaml/run", map[string]interface{}{})
	if code != 200 {
		t.Fatalf("run status=%d result=%v", code, result)
	}
	data, _ := result["data"].(map[string]interface{})
	execID, _ := data["executionId"].(string)
	if execID == "" {
		t.Fatal("no executionId")
	}

	exec := e.pollExecution(execID)
	if exec == nil {
		t.Fatal("execution timed out")
	}
	if status, _ := exec["status"].(string); status != "success" {
		t.Fatalf("status=%q want success (exec=%v)", status, exec)
	}

	// The detail response carries the variable map: sensitive masked, plain
	// visible. MaskVariables marks masked values with "***" (the "******"
	// marker is used for values scrubbed from output/log text).
	vars, ok := exec["variables"].(map[string]interface{})
	if !ok {
		t.Fatalf("detail response has no variables map: %v", exec)
	}
	if got := vars["token"]; got != "***" {
		t.Errorf("token=%v want ***", got)
	}
	if got := vars["env"]; got != "prod" {
		t.Errorf("env=%v want prod", got)
	}

	// The raw secret must not appear anywhere in the response body — not in
	// the variable map, not in step outputs, not in retained log lines.
	body, _ := json.Marshal(exec)
	if strings.Contains(string(body), secret) {
		t.Errorf("response contains raw secret: %s", body)
	}
	if !strings.Contains(string(body), "******") {
		t.Errorf("expected ****** mask in step output, body: %s", body)
	}

	// The persisted snapshot must keep the real value: replay restores
	// variables from the store, so masking must only happen at response
	// serialization.
	raw, err := os.ReadFile(filepath.Join(e.dir, "executions", execID+".json"))
	if err != nil {
		t.Fatalf("read store snapshot: %v", err)
	}
	if !strings.Contains(string(raw), secret) {
		t.Errorf("store snapshot lost the real secret value")
	}
	var snap workflow.ExecutionSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("parse store snapshot: %v", err)
	}
	if got := snap.Variables["token"]; got != secret {
		t.Errorf("stored token=%q want real value", got)
	}

	// The list endpoint's summaries carry no variable map (nothing to mask).
	listCode, list := e.get("/api/executions")
	if listCode != 200 {
		t.Fatalf("list status=%d", listCode)
	}
	items, _ := list["data"].([]interface{})
	for _, item := range items {
		if m, _ := item.(map[string]interface{}); m != nil {
			if _, has := m["variables"]; has {
				t.Errorf("list summary must not expose variables: %v", m)
			}
		}
	}
}

// TestSnapshotToViewMasksSensitiveVariables verifies the AI-facing execution
// view (POST /api/executions/{id}/ask) never carries raw sensitive values to
// the model — anything the model sees can end up in the streamed answer. The
// snapshot itself must keep real values.
func TestSnapshotToViewMasksSensitiveVariables(t *testing.T) {
	snap := workflow.ExecutionSnapshot{
		Variables: map[string]string{"token": "super-secret-value-123", "env": "prod"},
		Workflow: "name: w\nsensitive:\n  - token\nsteps:\n" +
			"  - name: x\n    action: log\n    message: hi\n",
	}
	view := snapshotToView(snap)
	if got := view.Variables["token"]; got != "***" {
		t.Errorf("view token=%q want ***", got)
	}
	if got := view.Variables["env"]; got != "prod" {
		t.Errorf("view env=%q want prod", got)
	}
	if got := snap.Variables["token"]; got != "super-secret-value-123" {
		t.Errorf("snapshot mutated: token=%q want real value", got)
	}
}

// TestDetailToViewMasksSensitiveVariables covers the in-memory variant of the
// ask path: variables on a cached detail are masked before reaching the AI.
func TestDetailToViewMasksSensitiveVariables(t *testing.T) {
	d := &ExecutionDetail{
		Variables:         map[string]string{"token": "super-secret-value-123", "env": "prod"},
		SensitivePatterns: []string{"token"},
	}
	view := detailToView(d)
	if got := view.Variables["token"]; got != "***" {
		t.Errorf("view token=%q want ***", got)
	}
	if got := view.Variables["env"]; got != "prod" {
		t.Errorf("view env=%q want prod", got)
	}
	// The source detail keeps real values (MaskVariables must not mutate).
	if got := d.Variables["token"]; got != "super-secret-value-123" {
		t.Errorf("detail mutated: token=%q want real value", got)
	}
}

// TestMaskForResponseScrubsLogs exercises the response-exit scrub of retained
// log lines: live step_output events are stored raw (real-time streams are
// intentionally unmasked), so the secret must be cleaned when the detail is
// serialized, without touching the cached original.
func TestMaskForResponseScrubsLogs(t *testing.T) {
	d := &ExecutionDetail{
		Variables:         map[string]string{"token": "super-secret-value-123"},
		SensitivePatterns: []string{"token"},
		Logs: []LogEntry{
			{Message: "token=super-secret-value-123 env=prod"},
			{Message: "nothing sensitive here"},
		},
	}
	cp := d.deepCopy()
	cp.maskForResponse()

	if got := cp.Variables["token"]; got != "***" {
		t.Errorf("copy token=%q want ***", got)
	}
	if strings.Contains(cp.Logs[0].Message, "super-secret-value-123") {
		t.Errorf("log line not scrubbed: %q", cp.Logs[0].Message)
	}
	if !strings.Contains(cp.Logs[0].Message, "******") {
		t.Errorf("log line should contain ******: %q", cp.Logs[0].Message)
	}
	if cp.Logs[1].Message != "nothing sensitive here" {
		t.Errorf("unrelated log line changed: %q", cp.Logs[1].Message)
	}
	// The cached original is untouched: real values stay in memory.
	if got := d.Variables["token"]; got != "super-secret-value-123" {
		t.Errorf("original mutated: token=%q want real value", got)
	}
	if strings.Contains(d.Logs[0].Message, "******") {
		t.Errorf("original log line must stay raw: %q", d.Logs[0].Message)
	}
}
