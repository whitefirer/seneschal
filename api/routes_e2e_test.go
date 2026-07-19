package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// runbookYAML is a minimal manual-trigger runbook pointing at the simple.yaml
// workflow that setupE2E writes into the workflows dir. Pure shell — no AI.
const runbookYAML = `name: nightly
workflow: simple.yaml
triggers:
  - type: manual
`

// ── Runbook lifecycle ─────────────────────────────────────────────────────────

func TestE2E_RunbookLifecycle(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Save (PUT raw YAML body).
	code, result := e.put("/api/runbooks/nightly.yaml", runbookYAML)
	if code != 200 {
		t.Fatalf("save status=%d result=%v", code, result)
	}
	data, _ := result["data"].(map[string]interface{})
	if p, _ := data["path"].(string); p == "" {
		t.Error("save: expected non-empty path in response")
	}

	// List contains it. Note: RunbookConfig has only yaml tags, so the JSON
	// keys are the Go field names ("Name", "Workflow", ...).
	code, result = e.get("/api/runbooks")
	if code != 200 {
		t.Fatalf("list status=%d", code)
	}
	items, _ := result["data"].([]interface{})
	found := false
	for _, it := range items {
		m, _ := it.(map[string]interface{})
		if m["Name"] == "nightly" {
			found = true
		}
	}
	if !found {
		t.Errorf("list: runbook 'nightly' not found in %v", items)
	}

	// Get by name (the route key is the runbook name, not the file name).
	code, result = e.get("/api/runbooks/nightly")
	if code != 200 {
		t.Fatalf("get status=%d", code)
	}
	data, _ = result["data"].(map[string]interface{})
	if wf, _ := data["Workflow"].(string); wf != "simple.yaml" {
		t.Errorf("get: Workflow=%q want simple.yaml", wf)
	}

	// Delete (file name form; safeJoin normalizes the suffix either way).
	code, result = e.delete("/api/runbooks/nightly.yaml")
	if code != 200 {
		t.Fatalf("delete status=%d result=%v", code, result)
	}

	// Get after delete → 404, and the list is empty again.
	code, _ = e.get("/api/runbooks/nightly")
	if code != 404 {
		t.Errorf("get after delete: status=%d want 404", code)
	}
	_, result = e.get("/api/runbooks")
	items, _ = result["data"].([]interface{})
	if len(items) != 0 {
		t.Errorf("list after delete: expected empty, got %v", items)
	}
}

func TestE2E_TriggerRunbook(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	if code, result := e.put("/api/runbooks/nightly.yaml", runbookYAML); code != 200 {
		t.Fatalf("save status=%d result=%v", code, result)
	}

	// Trigger confirms dispatch AND returns the new execution's ID. The run
	// itself is still dispatched asynchronously by the trigger callback.
	code, result := e.post("/api/runbooks/nightly/trigger", map[string]interface{}{})
	if code != 200 {
		t.Fatalf("trigger status=%d result=%v", code, result)
	}
	data, _ := result["data"].(map[string]interface{})
	if s, _ := data["status"].(string); s != "triggered" {
		t.Errorf("trigger: status=%q want triggered", s)
	}
	if n, _ := data["runbook"].(string); n != "nightly" {
		t.Errorf("trigger: runbook=%q want nightly", n)
	}
	execID, _ := data["executionId"].(string)
	if execID == "" {
		t.Fatal("trigger: expected a non-empty executionId in the response")
	}

	// Poll that very execution to completion. The callback persists the
	// snapshot only after the workflow finishes, so pollExecution tolerates
	// the initial 404s while it runs.
	exec := e.pollExecution(execID)
	if exec == nil {
		t.Fatalf("runbook execution %s never completed", execID)
	}
	if st, _ := exec["status"].(string); st != "success" {
		t.Errorf("runbook execution status=%q want success", st)
	}
}

// ── Runbook negative paths ────────────────────────────────────────────────────

func TestE2E_GetRunbook_NotFound(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	code, _ := e.get("/api/runbooks/ghost")
	if code != 404 {
		t.Errorf("status=%d want 404", code)
	}
}

func TestE2E_TriggerRunbook_NotFound(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Triggering an unknown runbook fails in the manager ("not found"), which
	// TriggerRunbook maps to 400.
	code, _ := e.post("/api/runbooks/ghost/trigger", map[string]interface{}{})
	if code != 400 {
		t.Errorf("status=%d want 400", code)
	}
}

func TestE2E_SaveRunbook_InvalidYAML(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// SaveRunbook validates the body before writing: unparsable YAML is
	// rejected with 400 and never reaches disk (previously it returned 200
	// and the manager's loader then silently skipped the file). The positive
	// case — valid YAML saves with 200 and becomes visible — is covered by
	// TestE2E_RunbookLifecycle.
	code, result := e.put("/api/runbooks/broken.yaml", "{{{{not yaml")
	if code != 400 {
		t.Fatalf("save status=%d want 400 result=%v", code, result)
	}
	if msg, _ := result["error"].(string); !strings.Contains(msg, "invalid runbook YAML") {
		t.Errorf("error=%q want it to mention 'invalid runbook YAML'", msg)
	}
	if code, _ := e.get("/api/runbooks/broken"); code != 404 {
		t.Errorf("get broken runbook: status=%d want 404 (invalid YAML must not be stored)", code)
	}
	_, list := e.get("/api/runbooks")
	items, _ := list["data"].([]interface{})
	if len(items) != 0 {
		t.Errorf("invalid runbook must not be listed, got %v", items)
	}

	// Valid YAML without the required 'workflow' field is rejected too —
	// the loader would skip such a file for the same reason.
	code, _ = e.put("/api/runbooks/noworkflow.yaml", "name: noworkflow\n")
	if code != 400 {
		t.Errorf("save without 'workflow' field: status=%d want 400", code)
	}
}

func TestE2E_SaveRunbook_BodyTooLarge(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// The reachable 400 on save: bodies are capped at maxRequestBodyBytes
	// (1 MiB) by http.MaxBytesReader.
	big := bytes.Repeat([]byte("x"), maxRequestBodyBytes+1)
	code, _ := e.put("/api/runbooks/too-big.yaml", string(big))
	if code != 400 {
		t.Errorf("status=%d want 400", code)
	}
}

func TestE2E_TriggerRunbook_BodyTooLarge(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Trigger bodies are capped at maxRequestBodyBytes (1 MiB), enforced
	// before the runbook is even looked up.
	big := bytes.Repeat([]byte("x"), maxRequestBodyBytes+1)
	resp, err := http.Post(e.server.URL+"/api/runbooks/nightly/trigger", "application/json", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status=%d want 400", resp.StatusCode)
	}
}

func TestE2E_TriggerByPath_BodyTooLarge(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	big := bytes.Repeat([]byte("x"), maxRequestBodyBytes+1)
	resp, err := http.Post(e.server.URL+"/api/triggers/hook", "application/json", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status=%d want 400", resp.StatusCode)
	}
}

func TestE2E_AskExecution_BodyTooLarge(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// AskExecution rejects an oversized question body with 413 before it
	// even looks up the execution. The payload is a valid JSON envelope
	// around a huge string: with a plain invalid-JSON body the decoder
	// would fail before ever reading up to the limit.
	big := bytes.Repeat([]byte("x"), maxRequestBodyBytes)
	body := append([]byte(`{"question":"`), big...)
	body = append(body, []byte(`"}`)...)
	resp, err := http.Post(e.server.URL+"/api/executions/exec-nope/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d want 413", resp.StatusCode)
	}
}

// ── Replay ────────────────────────────────────────────────────────────────────

func TestE2E_ReplayExecution(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Run a workflow to completion so the store holds a snapshot with the
	// workflow YAML (required by ReplayExecution).
	_, result := e.post("/api/workflows/simple.yaml/run", map[string]interface{}{})
	data, _ := result["data"].(map[string]interface{})
	execID, _ := data["executionId"].(string)
	if execID == "" {
		t.Fatal("no executionId")
	}
	exec := e.pollExecution(execID)
	if exec == nil {
		t.Fatal("initial run timed out")
	}
	if st, _ := exec["status"].(string); st != "success" {
		t.Fatalf("initial run status=%q want success", st)
	}

	// Replay it. Returns a new executionId for the replay run.
	code, result := e.post("/api/executions/"+execID+"/replay", nil)
	if code != 200 {
		t.Fatalf("replay status=%d result=%v", code, result)
	}
	data, _ = result["data"].(map[string]interface{})
	replayID, _ := data["executionId"].(string)
	if replayID == "" {
		t.Fatal("replay: no executionId in response")
	}
	if of, _ := data["replayOf"].(string); of != execID {
		t.Errorf("replayOf=%q want %q", of, execID)
	}

	// The replay run executes in the background and is persisted to the store
	// on completion; pollExecution tolerates the initial 404s while it runs.
	replay := e.pollExecution(replayID)
	if replay == nil {
		t.Fatal("replay execution timed out")
	}
	if st, _ := replay["status"].(string); st != "success" {
		t.Errorf("replay status=%q want success", st)
	}
}

func TestE2E_ReplayExecution_NotFound(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	code, _ := e.post("/api/executions/exec-does-not-exist/replay", nil)
	if code != 404 {
		t.Errorf("status=%d want 404", code)
	}
}

// ── Ask ───────────────────────────────────────────────────────────────────────
//
// AskExecution cannot be exercised end-to-end into the AI layer in tests:
// there is no provider injection point — the handler calls ai.BuildProvider
// directly, which needs real API keys (env) and network access. The paths
// that ARE deterministically testable are covered here:
//   - unknown execution            → 404 (checked before the provider)
//   - no provider configured       → 503 "AI unavailable"
//   - malformed JSON body          → tolerated (decode error ignored), still 503
// The SSE streaming path (thinking/token/done) sits behind a working provider
// and is intentionally left out.

func TestE2E_AskExecution_NotFound(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	code, _ := e.post("/api/executions/exec-nope/ask", map[string]interface{}{"question": "why?"})
	if code != 404 {
		t.Errorf("status=%d want 404", code)
	}

	// An empty question is valid input (general explanation) — the 404 must
	// still come from the missing execution, not from input validation.
	code, _ = e.post("/api/executions/exec-nope/ask", map[string]interface{}{})
	if code != 404 {
		t.Errorf("empty question: status=%d want 404", code)
	}
}

func TestE2E_AskExecution_NoProvider(t *testing.T) {
	// Guarantee BuildProvider fails regardless of the developer's shell env:
	// with an empty AIConfig the provider defaults to anthropic, which errors
	// out when no API key is set.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	e := setupE2E(t)
	defer e.close()

	// Ask requires an existing execution; run one to completion first.
	_, result := e.post("/api/workflows/simple.yaml/run", map[string]interface{}{})
	data, _ := result["data"].(map[string]interface{})
	execID, _ := data["executionId"].(string)
	if execID == "" {
		t.Fatal("no executionId")
	}
	if exec := e.pollExecution(execID); exec == nil {
		t.Fatal("timed out")
	}

	code, result := e.post("/api/executions/"+execID+"/ask", map[string]interface{}{"question": "what happened?"})
	if code != 503 {
		t.Fatalf("status=%d want 503 result=%v", code, result)
	}
	if msg, _ := result["error"].(string); !strings.Contains(msg, "AI unavailable") {
		t.Errorf("error=%q want it to contain 'AI unavailable'", msg)
	}

	// Malformed JSON body is ignored by design (the question is optional), so
	// it reaches the same provider error instead of a 400.
	resp, err := http.Post(e.server.URL+"/api/executions/"+execID+"/ask", "application/json", bytes.NewBufferString("{{{not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&raw)
	if resp.StatusCode != 503 {
		t.Errorf("malformed body: status=%d want 503", resp.StatusCode)
	}
}
