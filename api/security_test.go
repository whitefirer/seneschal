package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/workflow"
)

// ── safeJoin ────────────────────────────────────────────────────────────────

func TestSafeJoin(t *testing.T) {
	base := t.TempDir()

	cases := []struct {
		name     string
		input    string
		wantErr  bool
		wantPath string // expected absolute path when !wantErr
	}{
		{"plain name", "deploy", false, filepath.Join(base, "deploy.yaml")},
		{"yml suffix kept", "deploy.yml", false, filepath.Join(base, "deploy.yml")},
		{"legit subdirectory", "sub/deploy.yaml", false, filepath.Join(base, "sub", "deploy.yaml")},
		{"dotdot rejected", "..", true, ""},
		{"traversal rejected", "../evil.yaml", true, ""},
		{"deep traversal rejected", "../../etc/passwd.yaml", true, ""},
		// mux decodes %2F before the handler sees it; test the decoded form.
		{"decoded %2F traversal rejected", "../escape.yaml", true, ""},
		{"absolute path rejected", "/etc/evil.yaml", true, ""},
		{"empty rejected", "", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			abs, fileName, err := safeJoin(base, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("safeJoin(%q) = %q, want error", tc.input, abs)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeJoin(%q) error: %v", tc.input, err)
			}
			if abs != tc.wantPath {
				t.Errorf("safeJoin(%q) = %q, want %q", tc.input, abs, tc.wantPath)
			}
			if fileName == "" || !strings.HasSuffix(fileName, filepath.Ext(fileName)) {
				t.Errorf("unexpected normalized name %q", fileName)
			}
		})
	}
}

// ── runbook handler traversal ───────────────────────────────────────────────

func setupRunbookHandler(t *testing.T) (*RunbookHandler, string, string) {
	t.Helper()
	base := t.TempDir()
	runbooksDir := filepath.Join(base, "runbooks")
	workflowsDir := filepath.Join(base, "workflows")
	if err := os.MkdirAll(runbooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}
	mgr := workflow.NewRunbookManager(runbooksDir, workflowsDir,
		func(*workflow.RunbookConfig, map[string]string) {}, nil)
	return NewRunbookHandler(mgr, runbooksDir, workflowsDir), runbooksDir, base
}

func runbookReq(method, name, body string) (*httptest.ResponseRecorder, *http.Request) {
	var rd *bytes.Buffer
	if body != "" {
		rd = bytes.NewBufferString(body)
	} else {
		rd = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, "/api/runbooks/"+name, rd)
	req = mux.SetURLVars(req, map[string]string{"name": name})
	return httptest.NewRecorder(), req
}

func TestSaveRunbook_PathTraversalRejected(t *testing.T) {
	h, _, base := setupRunbookHandler(t)

	// Sentinel outside the runbooks dir must never be touched.
	sentinel := filepath.Join(base, "escape.yaml")
	if err := os.WriteFile(sentinel, []byte("name: sentinel\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../escape", "../../escape.yaml", "/tmp/abs-evil"} {
		rec, req := runbookReq(http.MethodPut, name, "name: evil\nworkflow: x.yaml\n")
		h.SaveRunbook(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("SaveRunbook(%q): status=%d, want 400", name, rec.Code)
		}
	}

	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "name: sentinel\n" {
		t.Errorf("sentinel file modified: %q, err=%v", data, err)
	}
	// Nothing may have been written above runbooksDir.
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		if e.Name() != "runbooks" && e.Name() != "workflows" && e.Name() != "escape.yaml" {
			t.Errorf("unexpected file outside runbooks dir: %s", e.Name())
		}
	}
}

func TestSaveRunbook_Valid(t *testing.T) {
	h, runbooksDir, _ := setupRunbookHandler(t)

	rec, req := runbookReq(http.MethodPut, "deploy", "name: deploy\nworkflow: wf.yaml\ntriggers:\n  - type: manual\n")
	h.SaveRunbook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(runbooksDir, "deploy.yaml")); err != nil {
		t.Errorf("runbook not written: %v", err)
	}
}

func TestDeleteRunbook_PathTraversalRejected(t *testing.T) {
	h, _, base := setupRunbookHandler(t)

	// A file one level above runbooksDir must survive a traversal delete.
	sentinel := filepath.Join(base, "keep.yaml")
	if err := os.WriteFile(sentinel, []byte("name: keep\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../keep", "../../keep.yaml", "/tmp/abs-evil"} {
		rec, req := runbookReq(http.MethodDelete, name, "")
		h.DeleteRunbook(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("DeleteRunbook(%q): status=%d, want 400", name, rec.Code)
		}
	}

	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel deleted via traversal: %v", err)
	}
}

// ── resolveWorkflowPath ─────────────────────────────────────────────────────

func TestResolveWorkflowPath(t *testing.T) {
	wfDir := t.TempDir()
	writeTestFile(t, wfDir, "ok.yaml", "name: ok\nsteps:\n  - name: x\n    action: log\n    message: hi\n")
	if err := os.MkdirAll(filepath.Join(wfDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(wfDir, "sub"), "nested.yaml", "name: nested\n")

	t.Run("relative inside workflows dir", func(t *testing.T) {
		p, err := resolveWorkflowPath(&workflow.RunbookConfig{Workflow: "ok.yaml"}, wfDir)
		if err != nil {
			t.Fatal(err)
		}
		if p != filepath.Join(wfDir, "ok.yaml") {
			t.Errorf("path=%q", p)
		}
	})

	t.Run("relative subdirectory", func(t *testing.T) {
		p, err := resolveWorkflowPath(&workflow.RunbookConfig{Workflow: "sub/nested.yaml"}, wfDir)
		if err != nil {
			t.Fatal(err)
		}
		if p != filepath.Join(wfDir, "sub", "nested.yaml") {
			t.Errorf("path=%q", p)
		}
	})

	t.Run("absolute path rejected", func(t *testing.T) {
		abs := filepath.Join(wfDir, "ok.yaml")
		if _, err := resolveWorkflowPath(&workflow.RunbookConfig{Workflow: abs}, wfDir); err == nil {
			t.Error("absolute path should be rejected even when the file exists")
		}
	})

	t.Run("dotdot escape rejected", func(t *testing.T) {
		// Plant a file next to the workflows dir; "../sibling.yaml" must not
		// resolve to it.
		parent := filepath.Dir(wfDir)
		writeTestFile(t, parent, "sibling.yaml", "name: sibling\n")
		if _, err := resolveWorkflowPath(&workflow.RunbookConfig{Workflow: "../sibling.yaml"}, wfDir); err == nil {
			t.Error("../ escape should be rejected")
		}
	})

	t.Run("missing file rejected", func(t *testing.T) {
		if _, err := resolveWorkflowPath(&workflow.RunbookConfig{Workflow: "nope.yaml"}, wfDir); err == nil {
			t.Error("missing file should be an error")
		}
	})
}

// ── chat dir validation ─────────────────────────────────────────────────────

func TestResolveChatDir(t *testing.T) {
	base := t.TempDir()

	cases := []struct {
		name    string
		dir     string
		wantErr bool
	}{
		{"empty defaults to workflows dir", "", false},
		{"workflows dir itself", base, false},
		{"subdirectory", filepath.Join(base, "sub"), false},
		{"etc rejected", "/etc", true},
		{"parent rejected", filepath.Dir(base), true},
		{"dotdot escape rejected", filepath.Join(base, "..", "elsewhere"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveChatDir(base, tc.dir)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveChatDir(%q) = %q, want error", tc.dir, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveChatDir(%q) error: %v", tc.dir, err)
			}
		})
	}
}

func TestChatHandler_DirValidation(t *testing.T) {
	wfDir := t.TempDir()
	hub := NewWSHub()
	go hub.Run()
	h := NewHandler(hub, wfDir, nil, workflow.AIConfig{}, nil, nil)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.ChatHandler(rec, req)
		return rec
	}

	rec := post(`{"message":"hi","dir":"/etc"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("dir=/etc: status=%d, want 400", rec.Code)
	}

	// A legitimate subdirectory passes dir validation (the request then fails
	// later — e.g. 503 with no AI provider — but must not be a 400).
	sub := filepath.Join(wfDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"message": "hi", "dir": sub})
	rec = post(string(payload))
	if rec.Code == http.StatusBadRequest {
		t.Errorf("legit subdir: status=400 body=%s, want dir validation to pass", rec.Body.String())
	}
}

// ── GetExecution race safety ────────────────────────────────────────────────

func TestExecutionDetail_DeepCopyIsolation(t *testing.T) {
	condTrue := true
	exec := &ExecutionDetail{
		ExecutionRecord: ExecutionRecord{ID: "exec-1", Status: "running"},
		Logs:            []LogEntry{{Message: "log-1"}},
		Steps: []workflow.StepResult{{
			ID: "s1", Name: "step1", Status: "running",
			Next:         []string{"s2"},
			Children:     []workflow.StepResult{{ID: "c1", Status: "pending"}},
			ThenChildren: []workflow.StepResult{{ID: "t1", Status: "pending", ConditionResult: &condTrue}},
		}},
	}

	cp := exec.deepCopy()

	// Mutate every mutable part of the original.
	exec.Logs = append(exec.Logs, LogEntry{Message: "log-2"})
	exec.Steps[0].Status = "success"
	exec.Steps[0].Next[0] = "changed"
	exec.Steps[0].Children = append(exec.Steps[0].Children, workflow.StepResult{ID: "c2"})
	exec.Steps[0].Children[0].Status = "running"
	exec.Steps[0].ThenChildren[0].Status = "success"
	*exec.Steps[0].ThenChildren[0].ConditionResult = false

	if len(cp.Logs) != 1 {
		t.Errorf("copy Logs changed: %d", len(cp.Logs))
	}
	if cp.Steps[0].Status != "running" {
		t.Errorf("copy step status changed: %q", cp.Steps[0].Status)
	}
	if cp.Steps[0].Next[0] != "s2" {
		t.Errorf("copy Next changed: %q", cp.Steps[0].Next[0])
	}
	if len(cp.Steps[0].Children) != 1 || cp.Steps[0].Children[0].Status != "pending" {
		t.Errorf("copy Children changed: %+v", cp.Steps[0].Children)
	}
	if cp.Steps[0].ThenChildren[0].Status != "pending" {
		t.Errorf("copy ThenChildren changed: %+v", cp.Steps[0].ThenChildren[0])
	}
	if *cp.Steps[0].ThenChildren[0].ConditionResult != true {
		t.Error("copy ConditionResult changed")
	}
}

// TestGetExecution_Concurrent hammers GetExecution while a writer goroutine
// mutates the cached ExecutionDetail the same way the executor's OnProgress
// callback does. Run with -race: before the deep-copy fix this raced between
// the JSON encoder and the writer.
func TestGetExecution_Concurrent(t *testing.T) {
	hub := NewWSHub()
	go hub.Run()
	h := NewHandler(hub, t.TempDir(), nil, workflow.AIConfig{}, nil, nil)

	exec := &ExecutionDetail{
		ExecutionRecord: ExecutionRecord{ID: "exec-race", Status: "running", StartTime: time.Now().Format(time.RFC3339)},
		Logs:            []LogEntry{},
		Steps: []workflow.StepResult{{
			ID: "s1", Name: "step1", Status: "running",
			Children: []workflow.StepResult{{ID: "c1", Status: "pending"}},
		}},
	}
	h.execMu.Lock()
	h.executions["exec-race"] = exec
	h.execMu.Unlock()

	router := mux.NewRouter()
	router.HandleFunc("/api/executions/{id}", h.GetExecution).Methods("GET")

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: mirrors the OnProgress mutations (log appends, status flips,
	// step-tree growth) under the write lock. Children are capped so the
	// tree stays small while still churning.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			h.execMu.Lock()
			exec.Logs = append(exec.Logs, LogEntry{Message: "tick", Timestamp: time.Now().Format(time.RFC3339)})
			if len(exec.Logs) > 100 {
				exec.Logs = exec.Logs[:0]
			}
			exec.Steps[0].Status = "running"
			exec.Steps[0].Children = append(exec.Steps[0].Children, workflow.StepResult{ID: "iter", Status: "running"})
			if len(exec.Steps[0].Children) > 100 {
				exec.Steps[0].Children = exec.Steps[0].Children[:1]
			}
			exec.Steps[0].Children[0].Status = "success"
			h.execMu.Unlock()
		}
	}()

	// Readers.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				req := httptest.NewRequest(http.MethodGet, "/api/executions/exec-race", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("status=%d", rec.Code)
					return
				}
				var resp APIResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Errorf("invalid JSON under concurrency: %v", err)
					return
				}
			}
		}()
	}

	// Let the readers run, then stop the writer.
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
