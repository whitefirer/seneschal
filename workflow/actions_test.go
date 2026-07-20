package workflow

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestActionForeach drives the foreach container end-to-end via Execute
// (top-level foreach → executeContainerDAG → executeForeach).
func TestActionForeach(t *testing.T) {
	tests := []struct {
		name           string
		vars           map[string]string
		step           Step
		wantStatus     string // workflow status
		wantStepStatus string // container step status
		wantChildren   int
		wantOutputs    []string // ordered substrings of children outputs
		wantErr        string   // substring of the container error
	}{
		{
			name: "iterates array and collects per-iteration output",
			step: Step{
				Name: "iterate", Action: "foreach",
				Items: []interface{}{"alpha", "beta", "gamma"}, ItemVar: "item",
				Do: []Step{{Name: "process", Action: "shell", Command: "echo out-{{.item}}"}},
			},
			wantStatus: "success", wantStepStatus: "success",
			wantChildren: 3,
			wantOutputs:  []string{"out-alpha", "out-beta", "out-gamma"},
		},
		{
			name: "items resolved from comma-separated variable",
			vars: map[string]string{"targets": "x,y,z"},
			step: Step{
				Name: "iterate", Action: "foreach", Items: "targets",
				Do: []Step{{Name: "process", Action: "log", Message: "got {{.item}}"}},
			},
			wantStatus: "success", wantStepStatus: "success",
			wantChildren: 3,
			wantOutputs:  []string{"got x", "got y", "got z"},
		},
		{
			name: "custom item variable name",
			step: Step{
				Name: "deploy", Action: "foreach",
				Items: []interface{}{"web", "api"}, ItemVar: "svc",
				Do: []Step{{Name: "restart", Action: "log", Message: "restarting {{.svc}}"}},
			},
			wantStatus: "success", wantStepStatus: "success",
			wantChildren: 2,
			wantOutputs:  []string{"restarting web", "restarting api"},
		},
		{
			name: "empty items succeeds without children",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{},
				Do: []Step{{Name: "process", Action: "log", Message: "never"}},
			},
			wantStatus: "success", wantStepStatus: "success",
			wantChildren: 0,
		},
		{
			// A failing iteration fails the container but keeps the children
			// collected so far (here: the failed step of iteration 0).
			name: "failing iteration fails the container",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{"a", "b"},
				Do: []Step{{Name: "process", Action: "shell", Command: "exit 1"}},
			},
			wantStatus: "failed", wantStepStatus: "failed",
			wantChildren: 1,
			wantErr:      "iteration 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(tt.vars)
			wf := &Workflow{Name: "foreach-test", Steps: []Step{tt.step}}
			result := e.Execute(wf)
			if result.Status != tt.wantStatus {
				t.Fatalf("workflow status=%s, want %s (err=%s)", result.Status, tt.wantStatus, result.Error)
			}
			if len(result.Steps) != 1 {
				t.Fatalf("steps=%d, want 1 container step", len(result.Steps))
			}
			sr := result.Steps[0]
			if sr.Status != tt.wantStepStatus {
				t.Errorf("container status=%s, want %s", sr.Status, tt.wantStepStatus)
			}
			if tt.wantErr != "" && !strings.Contains(sr.Error, tt.wantErr) {
				t.Errorf("container error=%q, want substring %q", sr.Error, tt.wantErr)
			}
			if len(sr.Children) != tt.wantChildren {
				t.Fatalf("children=%d, want %d", len(sr.Children), tt.wantChildren)
			}
			for i, want := range tt.wantOutputs {
				if !strings.Contains(sr.Children[i].Output, want) {
					t.Errorf("child[%d] output=%q, want substring %q", i, sr.Children[i].Output, want)
				}
			}
		})
	}
}

// TestActionForeach_IterationVariables verifies the per-iteration variables
// (item, item_index) visible to do-steps, and that the last iteration's
// values remain in the context afterwards (documented engine behavior).
func TestActionForeach_IterationVariables(t *testing.T) {
	e := newQuietExecutor(nil)
	wf := &Workflow{
		Name: "foreach-vars",
		Steps: []Step{{
			Name: "iterate", Action: "foreach",
			Items: []interface{}{"a", "b", "c"}, ItemVar: "item",
			Do: []Step{{Name: "show", Action: "shell", Command: "echo {{.item}}:{{.item_index}}"}},
		}},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	children := result.Steps[0].Children
	if len(children) != 3 {
		t.Fatalf("children=%d, want 3", len(children))
	}
	for i, want := range []string{"a:0", "b:1", "c:2"} {
		if !strings.Contains(children[i].Output, want) {
			t.Errorf("child[%d] output=%q, want substring %q", i, children[i].Output, want)
		}
	}
	// Iteration variables leak into the context with the last item's values.
	if got := e.GetContext().Get("item"); got != "c" {
		t.Errorf("context item=%q, want c (last iteration value)", got)
	}
	if got := e.GetContext().Get("item_index"); got != "2" {
		t.Errorf("context item_index=%q, want 2 (last iteration index)", got)
	}
}

// TestActionForeach_FailureRetainsChildren verifies that a mid-iteration
// failure keeps the children collected so far (previous iterations' results
// plus the failed step itself) on the container result instead of dropping
// them.
func TestActionForeach_FailureRetainsChildren(t *testing.T) {
	e := newQuietExecutor(nil)
	wf := &Workflow{
		Name: "foreach-partial",
		Steps: []Step{{
			Name: "iterate", Action: "foreach",
			Items: []interface{}{"a", "b", "c"}, ItemVar: "item",
			Do: []Step{{Name: "process", Action: "shell", Command: `test "{{.item}}" != "b"`}},
		}},
	}
	result := e.Execute(wf)
	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed (err=%s)", result.Status, result.Error)
	}
	sr := result.Steps[0]
	if sr.Status != "failed" {
		t.Fatalf("container status=%s, want failed", sr.Status)
	}
	// Iteration 0 succeeded, iteration 1 failed → 2 children retained;
	// iteration 2 never ran.
	if len(sr.Children) != 2 {
		t.Fatalf("children=%d, want 2 (success of iteration 0 + failure of iteration 1)", len(sr.Children))
	}
	if sr.Children[0].Status != "success" {
		t.Errorf("child[0] status=%s, want success", sr.Children[0].Status)
	}
	if sr.Children[1].Status != "failed" {
		t.Errorf("child[1] status=%s, want failed", sr.Children[1].Status)
	}
}

// TestActionForeach_ContainerCompleteEvents verifies that a top-level foreach
// (which goes through executeContainerDAG's early-return branch) still emits
// step_complete — previously the TUI/WS showed the container running forever.
func TestActionForeach_ContainerCompleteEvents(t *testing.T) {
	tests := []struct {
		name       string
		step       Step
		wantStatus string
	}{
		{
			name: "success emits step_complete",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{"a"},
				Do: []Step{{Name: "process", Action: "log", Message: "hi"}},
			},
			wantStatus: "success",
		},
		{
			name: "empty items emits step_complete",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{},
				Do: []Step{{Name: "process", Action: "log", Message: "never"}},
			},
			wantStatus: "success",
		},
		{
			name: "failure emits step_complete",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{"a"},
				Do: []Step{{Name: "process", Action: "shell", Command: "exit 1"}},
			},
			wantStatus: "failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(nil)
			var mu sync.Mutex
			var starts, completes []string
			e.OnProgress = func(ev ProgressEvent) {
				// Only the container's own events, not the do-children's.
				if ev.Name != "iterate" {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				switch ev.Type {
				case "step_start":
					starts = append(starts, ev.Status)
				case "step_complete":
					completes = append(completes, ev.Status)
				}
			}
			e.Execute(&Workflow{Name: "foreach-events", Steps: []Step{tt.step}})
			mu.Lock()
			defer mu.Unlock()
			if len(starts) != 1 {
				t.Errorf("step_start events for container=%d (%v), want 1", len(starts), starts)
			}
			if len(completes) != 1 || completes[0] != tt.wantStatus {
				t.Errorf("step_complete events=%v, want exactly one with status %q", completes, tt.wantStatus)
			}
		})
	}
}

// TestActionCondition drives the condition container end-to-end via Execute
// and checks branch selection, skipped-branch markers, and ConditionResult.
func TestActionCondition(t *testing.T) {
	tests := []struct {
		name         string
		vars         map[string]string
		expression   string
		wantCond     bool
		wantExecuted string // "then" or "else"
	}{
		{name: "legacy template comparison true", vars: map[string]string{"env": "prod"},
			expression: "{{.env}} == prod", wantCond: true, wantExecuted: "then"},
		{name: "legacy template comparison false", vars: map[string]string{"env": "dev"},
			expression: "{{.env}} == prod", wantCond: false, wantExecuted: "else"},
		{name: "expr-lang expression true", vars: map[string]string{"env": "prod"},
			expression: `env == "prod"`, wantCond: true, wantExecuted: "then"},
		{name: "expr-lang expression false", vars: map[string]string{"env": "dev"},
			expression: `env == "prod"`, wantCond: false, wantExecuted: "else"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(tt.vars)
			wf := &Workflow{
				Name: "condition-test",
				Steps: []Step{{
					Name: "check", Action: "condition", Expression: tt.expression,
					Then: []Step{{Name: "yes", Action: "log", Message: "prod-mode"}},
					Else: []Step{{Name: "no", Action: "log", Message: "dev-mode"}},
				}},
			}
			result := e.Execute(wf)
			if result.Status != "success" {
				t.Fatalf("status=%s err=%s", result.Status, result.Error)
			}
			if len(result.Steps) != 1 {
				t.Fatalf("steps=%d, want 1", len(result.Steps))
			}
			sr := result.Steps[0]
			if sr.ConditionResult == nil || *sr.ConditionResult != tt.wantCond {
				t.Errorf("ConditionResult=%v, want %v", sr.ConditionResult, tt.wantCond)
			}
			if len(sr.ThenChildren) != 1 || len(sr.ElseChildren) != 1 {
				t.Fatalf("then=%d else=%d children, want 1 each", len(sr.ThenChildren), len(sr.ElseChildren))
			}
			if tt.wantExecuted == "then" {
				if sr.ThenChildren[0].Status != "success" || !strings.Contains(sr.ThenChildren[0].Output, "prod-mode") {
					t.Errorf("then child: %+v, want executed with prod-mode output", sr.ThenChildren[0])
				}
				if sr.ElseChildren[0].Status != "skipped" {
					t.Errorf("else child: status=%s, want skipped", sr.ElseChildren[0].Status)
				}
			} else {
				if sr.ElseChildren[0].Status != "success" || !strings.Contains(sr.ElseChildren[0].Output, "dev-mode") {
					t.Errorf("else child: %+v, want executed with dev-mode output", sr.ElseChildren[0])
				}
				if sr.ThenChildren[0].Status != "skipped" {
					t.Errorf("then child: status=%s, want skipped", sr.ThenChildren[0].Status)
				}
			}
		})
	}
}

// TestActionCondition_NestedSkippedBranch verifies the container path's
// recursive skipped-result synthesis: when the non-executed branch contains
// a nested condition, its synthesized skipped result carries skipped
// Then/Else children as well (createSkippedStepResult recursion).
func TestActionCondition_NestedSkippedBranch(t *testing.T) {
	e := newQuietExecutor(nil)
	wf := &Workflow{
		Name: "condition-nested-skip",
		Steps: []Step{{
			Name: "check", Action: "condition", Expression: "true",
			Then: []Step{{Name: "yes", Action: "log", Message: "hi"}},
			Else: []Step{{
				Name: "inner", Action: "condition", Expression: "false",
				Then: []Step{{Name: "inner-then", Action: "log", Message: "x"}},
				Else: []Step{{Name: "inner-else", Action: "log", Message: "y"}},
			}},
		}},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("steps=%d, want 1", len(result.Steps))
	}
	sr := result.Steps[0]
	if len(sr.ThenChildren) != 1 || sr.ThenChildren[0].Status != "success" {
		t.Fatalf("then children=%+v, want 1 executed", sr.ThenChildren)
	}
	if len(sr.ElseChildren) != 1 {
		t.Fatalf("else children=%d, want 1 (nested condition)", len(sr.ElseChildren))
	}
	inner := sr.ElseChildren[0]
	if inner.Name != "inner" || inner.Status != "skipped" {
		t.Errorf("nested condition: name=%q status=%s, want skipped", inner.Name, inner.Status)
	}
	if len(inner.ThenChildren) != 1 || inner.ThenChildren[0].Status != "skipped" {
		t.Errorf("nested then children=%+v, want 1 skipped", inner.ThenChildren)
	}
	if len(inner.ElseChildren) != 1 || inner.ElseChildren[0].Status != "skipped" {
		t.Errorf("nested else children=%+v, want 1 skipped", inner.ElseChildren)
	}
}

// TestActionParallel drives the parallel container end-to-end via Execute.
// Child concurrency is covered separately by
// TestExecuteContainerDAG_ParallelConcurrency (direct executeContainerDAG) and
// TestActionParallel_ExecuteRunsChildrenConcurrently (end-to-end via Execute).
func TestActionParallel(t *testing.T) {
	tests := []struct {
		name          string
		childSteps    func(t *testing.T) []Step
		wantStatus    string // workflow status
		wantContainer string // container step status
		wantChildren  int
		wantErr       string // substring of the container error
	}{
		{
			name: "children execute and results are collected",
			childSteps: func(t *testing.T) []Step {
				return []Step{
					{Name: "p1", Action: "shell", Command: "echo one"},
					{Name: "p2", Action: "shell", Command: "echo two"},
					{Name: "p3", Action: "log", Message: "quick"},
				}
			},
			wantStatus: "success", wantContainer: "success", wantChildren: 3,
		},
		{
			name: "failing child fails the container",
			childSteps: func(t *testing.T) []Step {
				return []Step{
					{Name: "ok", Action: "log", Message: "fine"},
					{Name: "bad", Action: "shell", Command: "exit 1"},
				}
			},
			wantStatus: "failed", wantContainer: "failed",
			wantErr: "child step 'bad' failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(nil)
			wf := &Workflow{
				Name: "parallel-test",
				Steps: []Step{{
					Name: "par", Action: "parallel", Steps: tt.childSteps(t),
				}},
			}
			result := e.Execute(wf)
			if result.Status != tt.wantStatus {
				t.Fatalf("workflow status=%s, want %s (err=%s)", result.Status, tt.wantStatus, result.Error)
			}
			if len(result.Steps) != 1 {
				t.Fatalf("steps=%d, want 1 container step", len(result.Steps))
			}
			sr := result.Steps[0]
			if sr.Status != tt.wantContainer {
				t.Errorf("container status=%s, want %s", sr.Status, tt.wantContainer)
			}
			if tt.wantErr != "" && !strings.Contains(sr.Error, tt.wantErr) {
				t.Errorf("container error=%q, want substring %q", sr.Error, tt.wantErr)
			}
			if tt.wantChildren > 0 {
				if len(sr.Children) != tt.wantChildren {
					t.Fatalf("children=%d, want %d", len(sr.Children), tt.wantChildren)
				}
				for _, c := range sr.Children {
					if c.Status != "success" {
						t.Errorf("child %s status=%s, want success", c.Name, c.Status)
					}
				}
			}
		})
	}
}

// TestExecuteContainerDAG_ParallelConcurrency drives executeContainerDAG
// directly (bypassing dependency inference) and proves the container's wave
// scheduler runs independent children concurrently: p1/p2 each wait for the
// other's start marker, so serial execution fails them deterministically.
func TestExecuteContainerDAG_ParallelConcurrency(t *testing.T) {
	dir := t.TempDir()
	container := Step{
		Name: "par", Action: "parallel",
		Steps: []Step{
			{Name: "p1", Action: "shell", Command: rendezvousScript(dir, "p1", "p2")},
			{Name: "p2", Action: "shell", Command: rendezvousScript(dir, "p2", "p1")},
		},
	}
	e := newQuietExecutor(nil)

	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	e.OnProgress = func(ev ProgressEvent) {
		// Only count the child steps — the container emits start/complete too.
		if ev.Name != "p1" && ev.Name != "p2" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case "step_start":
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
		case "step_complete":
			inFlight--
		}
	}

	wfResult := &WorkflowResult{Name: "par-container", Variables: map[string]string{}}
	sr := e.executeContainerDAG(container, 0, wfResult, "")
	if sr.Status != "success" {
		t.Fatalf("container status=%s err=%s (rendezvous failed: children did not run concurrently)", sr.Status, sr.Error)
	}
	if maxInFlight != 2 {
		t.Errorf("maxInFlight=%d, want 2 (p1 and p2 overlapped)", maxInFlight)
	}
	if len(sr.Children) != 2 {
		t.Fatalf("children=%d, want 2", len(sr.Children))
	}
	for _, c := range sr.Children {
		if c.Status != "success" {
			t.Errorf("child %s status=%s, want success", c.Name, c.Status)
		}
	}
}

// TestActionParallel_ExecuteRunsChildrenConcurrently drives a parallel
// container end-to-end via Execute() and asserts the children really run
// concurrently: InferDependencies must not linear-chain parallel children
// (regression — inferLinearDependencies used to recurse into parallel.Steps,
// serializing the children one per wave). p1/p2 use a rendezvous script that
// only succeeds when both overlap; the in-flight child count is tracked
// through progress events.
func TestActionParallel_ExecuteRunsChildrenConcurrently(t *testing.T) {
	dir := t.TempDir()
	e := newQuietExecutor(nil)

	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	e.OnProgress = func(ev ProgressEvent) {
		// Only count the child steps — the container emits start/complete too.
		if ev.Name != "p1" && ev.Name != "p2" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case "step_start":
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
		case "step_complete":
			inFlight--
		}
	}

	wf := &Workflow{
		Name: "parallel-concurrent",
		Steps: []Step{{
			Name: "par", Action: "parallel",
			Steps: []Step{
				{Name: "p1", Action: "shell", Command: rendezvousScript(dir, "p1", "p2")},
				{Name: "p2", Action: "shell", Command: rendezvousScript(dir, "p2", "p1")},
				{Name: "p3", Action: "log", Message: "tail"},
			},
		}},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s (rendezvous failed: children did not run concurrently)", result.Status, result.Error)
	}
	if maxInFlight < 2 {
		t.Errorf("maxInFlight=%d, want >=2 (p1 and p2 overlapped in one wave)", maxInFlight)
	}
	sr := result.Steps[0]
	if len(sr.Children) != 3 {
		t.Fatalf("children=%d, want 3", len(sr.Children))
	}
	for _, c := range sr.Children {
		if c.Status != "success" {
			t.Errorf("child %s status=%s, want success", c.Name, c.Status)
		}
	}
}

// TestActionHTTP drives the http action end-to-end via Execute against a
// local httptest server.
func TestActionHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello world")
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s %s %s", r.Method, string(body), r.Header.Get("X-Token"))
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		fmt.Fprint(w, "slow")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// A server that is shut down up-front: connections are refused.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()

	tests := []struct {
		name       string
		vars       map[string]string
		step       Step
		wantStatus string // workflow status
		wantStep   string // step status
		wantOut    []string
		wantErr    string
		check      func(t *testing.T, e *Executor, result *WorkflowResult)
	}{
		{
			name:       "GET 200 body captured in output",
			step:       Step{Name: "get-ok", Action: "http", URL: srv.URL + "/ok"},
			wantStatus: "success", wantStep: "success",
			wantOut: []string{"Status: 200", "hello world"},
			check: func(t *testing.T, e *Executor, result *WorkflowResult) {
				got, ok := e.GetContext().GetResult("get-ok")
				if !ok || !strings.Contains(got, "hello world") {
					t.Errorf("context result for get-ok=%q (ok=%v), want body stored", got, ok)
				}
				if !result.Steps[0].SideEffecting {
					t.Errorf("http step should be marked SideEffecting")
				}
			},
		},
		{
			name: "POST with templated body and header",
			vars: map[string]string{"token": "abc123", "name": "world"},
			step: Step{
				Name: "post-echo", Action: "http", URL: srv.URL + "/echo", Method: "POST",
				Body:    "hello {{.name}}",
				Headers: map[string]string{"X-Token": "{{.token}}"},
			},
			wantStatus: "success", wantStep: "success",
			wantOut: []string{"POST hello world abc123"},
		},
		{
			name:       "save_output stores structured JSON",
			step:       Step{Name: "get-save", Action: "http", URL: srv.URL + "/ok", SaveOutput: "resp"},
			wantStatus: "success", wantStep: "success",
			check: func(t *testing.T, e *Executor, result *WorkflowResult) {
				got := e.GetContext().Get("resp")
				if !strings.Contains(got, `"status":200`) || !strings.Contains(got, "hello world") {
					t.Errorf("saved variable resp=%q, want JSON with status and body", got)
				}
			},
		},
		{
			// A non-2xx response fails the step; the error carries the status
			// code and the response body stays in the step output.
			name:       "500 response fails the step",
			step:       Step{Name: "get-fail", Action: "http", URL: srv.URL + "/fail"},
			wantStatus: "failed", wantStep: "failed",
			wantOut: []string{"Status: 500", "boom"},
			wantErr: "500",
		},
		{
			name:       "connection refused fails the step",
			step:       Step{Name: "get-dead", Action: "http", URL: closedURL + "/ok"},
			wantStatus: "failed", wantStep: "failed",
			wantErr: "HTTP request failed",
		},
		{
			name:       "request timeout fails the step",
			step:       Step{Name: "get-slow", Action: "http", URL: srv.URL + "/slow", Timeout: "1ms"},
			wantStatus: "failed", wantStep: "failed",
			wantErr: "HTTP request failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(tt.vars)
			wf := &Workflow{Name: "http-test", Steps: []Step{tt.step}}
			result := e.Execute(wf)
			if result.Status != tt.wantStatus {
				t.Fatalf("workflow status=%s, want %s (err=%s)", result.Status, tt.wantStatus, result.Error)
			}
			if len(result.Steps) != 1 {
				t.Fatalf("steps=%d, want 1", len(result.Steps))
			}
			sr := result.Steps[0]
			if sr.Status != tt.wantStep {
				t.Errorf("step status=%s, want %s", sr.Status, tt.wantStep)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(sr.Output, want) {
					t.Errorf("step output=%q, want substring %q", sr.Output, want)
				}
			}
			if tt.wantErr != "" && !strings.Contains(sr.Error, tt.wantErr) {
				t.Errorf("step error=%q, want substring %q", sr.Error, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, e, result)
			}
		})
	}
}

// TestActionCondition_ChildDAGCycleEmitsStepComplete covers the container
// failure path where the child DAG cannot be scheduled (here: an explicit
// depends_on cycle inside the then branch). The container must emit exactly
// one step_complete (status failed) — previously the early return left the
// container running forever in the TUI/WS.
func TestActionCondition_ChildDAGCycleEmitsStepComplete(t *testing.T) {
	e := newQuietExecutor(nil)

	var mu sync.Mutex
	var outputs, completes []ProgressEvent
	e.OnProgress = func(ev ProgressEvent) {
		// Only the container's own events, not the (never-run) children's.
		if ev.Name != "check" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case "step_output":
			outputs = append(outputs, ev)
		case "step_complete":
			completes = append(completes, ev)
		}
	}

	wf := &Workflow{
		Name: "condition-child-cycle",
		Steps: []Step{{
			Name: "check", Action: "condition", Expression: "true",
			Then: []Step{
				{Name: "a", Action: "log", Message: "a", DependsOn: []string{"b"}},
				{Name: "b", Action: "log", Message: "b", DependsOn: []string{"a"}},
			},
			Else: []Step{{Name: "no", Action: "log", Message: "no"}},
		}},
	}
	result := e.Execute(wf)
	if result.Status != "failed" {
		t.Fatalf("workflow status=%s, want failed", result.Status)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("steps=%d, want 1 container step", len(result.Steps))
	}
	sr := result.Steps[0]
	if sr.Status != "failed" || !strings.Contains(sr.Error, "cycle") {
		t.Errorf("container: status=%s err=%q, want failed with cycle error", sr.Status, sr.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(completes) != 1 || completes[0].Status != "failed" {
		t.Errorf("step_complete events=%v, want exactly one with status failed", completes)
	}
	if len(outputs) != 1 || !strings.Contains(outputs[0].Output, "cycle") {
		t.Errorf("step_output events=%v, want exactly one carrying the cycle error", outputs)
	}
}

// TestActionCondition_ExpressionEvaluatedOnce pins the decision record: the
// expression is evaluated once at branch selection and the saved result is
// reused for the ConditionResult report. A set step inside the taken branch
// mutates the variable the expression reads; if the engine re-evaluated the
// expression at container close-out, ConditionResult would flip to the
// end-of-branch value. Covers both evaluation paths (legacy template
// comparison and expr-lang).
func TestActionCondition_ExpressionEvaluatedOnce(t *testing.T) {
	tests := []struct {
		name       string
		expression string
	}{
		{name: "legacy template comparison", expression: "{{.env}} == prod"},
		{name: "expr-lang expression", expression: `env == "prod"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(map[string]string{"env": "prod"})
			wf := &Workflow{
				Name: "condition-eval-once",
				Steps: []Step{{
					Name: "check", Action: "condition", Expression: tt.expression,
					Then: []Step{
						{Name: "env", Action: "set", Value: "dev"},
						{Name: "yes", Action: "log", Message: "prod-mode"},
					},
					Else: []Step{{Name: "no", Action: "log", Message: "dev-mode"}},
				}},
			}
			result := e.Execute(wf)
			if result.Status != "success" {
				t.Fatalf("status=%s err=%s", result.Status, result.Error)
			}
			// Sanity: the branch really mutated the variable, so a
			// re-evaluation at close-out would now produce false.
			if got := e.GetContext().Get("env"); got != "dev" {
				t.Fatalf("context env=%q, want dev (branch mutation did not happen)", got)
			}
			if len(result.Steps) != 1 {
				t.Fatalf("steps=%d, want 1", len(result.Steps))
			}
			sr := result.Steps[0]
			if sr.ConditionResult == nil || *sr.ConditionResult != true {
				t.Errorf("ConditionResult=%v, want true (branch-selection-time value)", sr.ConditionResult)
			}
			if len(sr.ThenChildren) != 2 || sr.ThenChildren[0].Status != "success" || sr.ThenChildren[1].Status != "success" {
				t.Errorf("then children=%+v, want 2 executed", sr.ThenChildren)
			}
			if len(sr.ElseChildren) != 1 || sr.ElseChildren[0].Status != "skipped" {
				t.Errorf("else children=%+v, want 1 skipped", sr.ElseChildren)
			}
		})
	}
}

// TestActionForeach_ContainerID verifies the foreach container result carries
// the same derived step ID as condition/parallel containers (step-<slug>),
// so frontends and logs can locate it like any other container.
func TestActionForeach_ContainerID(t *testing.T) {
	e := newQuietExecutor(nil)
	wf := &Workflow{
		Name: "container-ids",
		Steps: []Step{
			{
				Name: "Iterate Items", Action: "foreach", Items: []interface{}{"a"},
				Do: []Step{{Name: "process", Action: "log", Message: "hi"}},
			},
			{
				Name: "Par Block", Action: "parallel",
				Steps: []Step{{Name: "p", Action: "log", Message: "x"}},
			},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	foreachRes := findStepResult(result, "Iterate Items")
	if foreachRes == nil {
		t.Fatal("foreach container result missing")
	}
	if foreachRes.ID != "step-iterate-items" {
		t.Errorf("foreach container ID=%q, want step-iterate-items (same slug rule as other containers)", foreachRes.ID)
	}
	parRes := findStepResult(result, "Par Block")
	if parRes == nil || parRes.ID != "step-par-block" {
		t.Errorf("parallel container ID=%v, want step-par-block (reference format)", parRes)
	}
}

// TestActionForeach_StepOutputEventSymmetry pins the step_output contract for
// foreach containers: success with aggregated non-empty output emits exactly
// one step_output (matching plain steps, where a non-empty output triggers
// the event), empty output emits none, and failure keeps emitting the error.
func TestActionForeach_StepOutputEventSymmetry(t *testing.T) {
	tests := []struct {
		name         string
		step         Step
		wantOutputs  int      // number of step_output events for the container
		wantContains []string // substrings of the event payload
		wantExact    string   // exact payload ("" = skip); pins trailing-newline trimming
	}{
		{
			name: "success with child output emits step_output",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{"alpha", "beta"},
				Do: []Step{{Name: "process", Action: "shell", Command: "echo out-{{.item}}"}},
			},
			wantOutputs:  1,
			wantContains: []string{"out-alpha", "out-beta"},
			// echo adds a trailing newline per child; aggregation must trim so
			// the joined output has no blank lines or dangling newline.
			wantExact: "out-alpha\nout-beta",
		},
		{
			name: "empty items emits no step_output",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{},
				Do: []Step{{Name: "process", Action: "log", Message: "never"}},
			},
			wantOutputs: 0,
		},
		{
			name: "failure still emits step_output with the error",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{"a"},
				Do: []Step{{Name: "process", Action: "shell", Command: "exit 1"}},
			},
			wantOutputs:  1,
			wantContains: []string{"failed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(nil)
			var mu sync.Mutex
			var outputs []ProgressEvent
			e.OnProgress = func(ev ProgressEvent) {
				// Only the container's own events, not the do-children's.
				if ev.Name != "iterate" || ev.Type != "step_output" {
					return
				}
				mu.Lock()
				outputs = append(outputs, ev)
				mu.Unlock()
			}
			e.Execute(&Workflow{Name: "foreach-output", Steps: []Step{tt.step}})
			mu.Lock()
			defer mu.Unlock()
			if len(outputs) != tt.wantOutputs {
				t.Fatalf("step_output events=%d (%v), want %d", len(outputs), outputs, tt.wantOutputs)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(outputs[0].Output, want) {
					t.Errorf("step_output payload=%q, want substring %q", outputs[0].Output, want)
				}
			}
			if tt.wantExact != "" && outputs[0].Output != tt.wantExact {
				t.Errorf("step_output payload=%q, want exact %q", outputs[0].Output, tt.wantExact)
			}
		})
	}
}
