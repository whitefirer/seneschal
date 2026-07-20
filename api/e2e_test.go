package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/config"
	"github.com/whitefirer/seneschal/workflow"
)

// e2eTestServer is a fully-wired test server with a real Handler, real
// ExecutionStore (FileStore in temp dir), real workflow files, and a real
// RunbookHandler backed by its own temp runbooks dir.
type e2eTestServer struct {
	server      *httptest.Server
	hub         *WSHub
	dir         string
	runbooksDir string
}

func setupE2E(t *testing.T) *e2eTestServer {
	t.Helper()
	dir := t.TempDir()

	writeTestFile(t, dir, "simple.yaml", "name: simple\nsteps:\n  - name: greet\n    action: log\n    message: hello\n  - name: echo\n    action: shell\n    command: echo hi\n")
	writeTestFile(t, dir, "with-condition.yaml", "name: conditional\nvariables:\n  env: prod\nsteps:\n  - name: check\n    action: condition\n    expression: \"{{.env}} == prod\"\n    then:\n      - name: prod-step\n        action: log\n        message: production\n    else:\n      - name: dev-step\n        action: log\n        message: dev\n")

	hub := NewWSHub()
	go hub.Run()
	store := workflow.NewFileStore(filepath.Join(dir, "executions"))
	handler := NewHandler(hub, dir, store, workflow.AIConfig{}, nil, func(r *http.Request) bool { return true })

	// Runbook wiring mirrors cmd/server/main.go: a RunbookManager over a temp
	// runbooks dir, with the same trigger callback the server uses so a
	// triggered runbook really executes its workflow and persists the run.
	runbooksDir := filepath.Join(dir, "runbooks")
	if err := os.MkdirAll(runbooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	runbookMgr := workflow.NewRunbookManager(runbooksDir, dir,
		MakeTriggerCallback(store, hub, dir, workflow.AIConfig{}), nil)
	runbookMgr.LoadDir()
	runbookHandler := NewRunbookHandler(runbookMgr, runbooksDir, dir)

	router := buildTestRouter(handler, runbookHandler)
	server := httptest.NewServer(router)

	return &e2eTestServer{server: server, hub: hub, dir: dir, runbooksDir: runbooksDir}
}

// buildTestRouter registers the shared route table (api.RegisterRoutes — the
// same function cmd/server/main.go uses, so the test server cannot drift from
// production) and wraps it in the same middleware chain as main.go.
func buildTestRouter(handler *Handler, runbookHandler *RunbookHandler) http.Handler {
	r := mux.NewRouter()
	RegisterRoutes(r, handler, runbookHandler)
	// Mirror cmd/server/main.go: CORS then auth wrap the router. An empty
	// ServerConfig makes both no-ops for these tests (no Origin headers are
	// sent, and no auth token is configured).
	cfg := &config.ServerConfig{}
	var h http.Handler = r
	h = cfg.AuthMiddleware(h)
	h = cfg.CORSMiddleware(h)
	return h
}

func (e *e2eTestServer) close() { e.server.Close() }

func (e *e2eTestServer) post(path string, body interface{}) (int, map[string]interface{}) {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	resp, err := http.Post(e.server.URL+path, "application/json", &buf)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func (e *e2eTestServer) get(path string) (int, map[string]interface{}) {
	resp, err := http.Get(e.server.URL + path)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

// put sends a PUT with a raw string body (e.g. workflow/runbook YAML).
func (e *e2eTestServer) put(path, body string) (int, map[string]interface{}) {
	req, err := http.NewRequest("PUT", e.server.URL+path, bytes.NewBufferString(body))
	if err != nil {
		return 0, nil
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func (e *e2eTestServer) delete(path string) (int, map[string]interface{}) {
	req, err := http.NewRequest("DELETE", e.server.URL+path, nil)
	if err != nil {
		return 0, nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func (e *e2eTestServer) pollExecution(execID string) map[string]interface{} {
	// Poll until the execution reaches a terminal state. GetExecution is
	// race-safe (it serializes a deep copy made under the lock), so polling
	// during the run is fine.
	for i := 0; i < 100; i++ {
		_, result := e.get("/api/executions/" + execID)
		data, ok := result["data"].(map[string]interface{})
		if !ok {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		status, _ := data["status"].(string)
		if status != "running" && status != "" {
			return data
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ── E2E Tests ──────────────────────────────────────────────────────────────────

func TestE2E_ListWorkflows(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	code, result := e.get("/api/workflows")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	data, _ := result["data"].([]interface{})
	if len(data) < 2 {
		t.Errorf("expected >=2 workflows, got %d", len(data))
	}
}

func TestE2E_RunSimpleWorkflow(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	code, result := e.post("/api/workflows/simple.yaml/run", map[string]interface{}{})
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
	status, _ := exec["status"].(string)
	if status != "success" {
		t.Errorf("status=%q want success", status)
	}
	steps, _ := exec["steps"].([]interface{})
	if len(steps) != 2 {
		t.Errorf("steps=%d want 2", len(steps))
	}
}

func TestE2E_RunConditionalWorkflow(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	code, result := e.post("/api/workflows/with-condition.yaml/run", map[string]interface{}{})
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	data, _ := result["data"].(map[string]interface{})
	execID, _ := data["executionId"].(string)

	exec := e.pollExecution(execID)
	if exec == nil {
		t.Fatal("timed out")
	}
	status, _ := exec["status"].(string)
	if status != "success" {
		t.Errorf("status=%q", status)
	}
}

func TestE2E_GetExecution(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Run a workflow first.
	_, result := e.post("/api/workflows/simple.yaml/run", map[string]interface{}{})
	data, _ := result["data"].(map[string]interface{})
	execID, _ := data["executionId"].(string)

	exec := e.pollExecution(execID)
	if exec == nil {
		t.Fatal("timed out")
	}

	// Now GET the execution by ID.
	code, getResult := e.get("/api/executions/" + execID)
	if code != 200 {
		t.Fatalf("get status=%d", code)
	}
	getData, _ := getResult["data"].(map[string]interface{})
	if getData["id"] != execID {
		t.Errorf("id mismatch")
	}
}

func TestE2E_ListExecutions(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Run two workflows.
	e.post("/api/workflows/simple.yaml/run", map[string]interface{}{})
	e.post("/api/workflows/with-condition.yaml/run", map[string]interface{}{})
	time.Sleep(1 * time.Second)

	code, result := e.get("/api/executions")
	if code != 200 {
		t.Fatalf("status=%d", code)
	}
	data, _ := result["data"].([]interface{})
	if len(data) < 2 {
		t.Errorf("expected >=2 executions, got %d", len(data))
	}
}

func TestE2E_DeleteWorkflow(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// Create a temp workflow to delete.
	e.post("/api/workflows/to-delete.yaml", map[string]interface{}{})
	// Actually need to PUT content, not POST. Use raw HTTP.
	writeTestFile(t, e.dir, "to-delete.yaml", "name: to-delete\nsteps:\n  - name: x\n    action: log\n    message: bye\n")

	// DELETE it.
	req, _ := http.NewRequest("DELETE", e.server.URL+"/api/workflows/to-delete", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("delete status=%d", resp.StatusCode)
	}

	// Verify it's gone.
	code, _ := e.get("/api/workflows/to-delete")
	if code != 404 {
		t.Errorf("expected 404 after delete, got %d", code)
	}
}

func TestE2E_SaveAndGetWorkflow(t *testing.T) {
	e := setupE2E(t)
	defer e.close()

	// PUT a new workflow.
	yamlContent := "name: saved\nsteps:\n  - name: test\n    action: log\n    message: hi\n"
	req, _ := http.NewRequest("PUT", e.server.URL+"/api/workflows/saved.yaml", bytes.NewBufferString(yamlContent))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("save status=%d", resp.StatusCode)
	}

	// GET it back.
	code, result := e.get("/api/workflows/saved.yaml")
	if code != 200 {
		t.Fatalf("get status=%d", code)
	}
	data, _ := result["data"].(map[string]interface{})
	content, _ := data["content"].(string)
	if content == "" {
		t.Error("expected non-empty content")
	}
}
