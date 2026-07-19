package workflow

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
			// NOTE: engine behavior — an empty foreach reports container status
			// "completed" (not the usual "success") and produces no children.
			name: "empty items completes without children",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{},
				Do: []Step{{Name: "process", Action: "log", Message: "never"}},
			},
			wantStatus: "success", wantStepStatus: "completed",
			wantChildren: 0,
		},
		{
			name: "failing iteration fails the container",
			step: Step{
				Name: "iterate", Action: "foreach", Items: []interface{}{"a", "b"},
				Do: []Step{{Name: "process", Action: "shell", Command: "exit 1"}},
			},
			wantStatus: "failed", wantStepStatus: "failed",
			wantErr: "iteration 0",
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

// TestExecCondition_BranchExecution covers execCondition directly: branch
// step outputs are joined into the container output, the skipped branch gets
// synthetic skipped results. (execCondition is only reachable when a
// condition step is dispatched through executeStep — see report.)
func TestExecCondition_BranchExecution(t *testing.T) {
	tests := []struct {
		name          string
		vars          map[string]string
		expression    string
		wantCond      bool
		wantOutput    string
		wantExecCount int
		wantSkipCount int
	}{
		{
			name: "true runs then branch and skips else",
			vars: map[string]string{"env": "prod"}, expression: "{{.env}} == prod",
			wantCond: true, wantOutput: "[INFO] t1\n[INFO] t2",
			wantExecCount: 2, wantSkipCount: 1,
		},
		{
			name: "false runs else branch and skips then",
			vars: map[string]string{"env": "dev"}, expression: "{{.env}} == prod",
			wantCond: false, wantOutput: "[INFO] e1",
			wantExecCount: 1, wantSkipCount: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(tt.vars)
			step := Step{
				Name: "check", Action: "condition", Expression: tt.expression,
				Then: []Step{
					{Name: "t1", Action: "log", Message: "t1"},
					{Name: "t2", Action: "log", Message: "t2"},
				},
				Else: []Step{
					{Name: "e1", Action: "log", Message: "e1"},
				},
			}
			wfResult := &WorkflowResult{Name: "cond-direct", Variables: map[string]string{}}
			output, execChildren, skippedChildren, condResult, err := e.execCondition(step, 0, wfResult, "cond-1")
			if err != nil {
				t.Fatalf("execCondition: %v", err)
			}
			if condResult != tt.wantCond {
				t.Errorf("condResult=%v, want %v", condResult, tt.wantCond)
			}
			if output != tt.wantOutput {
				t.Errorf("output=%q, want %q", output, tt.wantOutput)
			}
			if len(execChildren) != tt.wantExecCount {
				t.Fatalf("execChildren=%d, want %d", len(execChildren), tt.wantExecCount)
			}
			for _, c := range execChildren {
				if c.Status != "success" {
					t.Errorf("exec child %s status=%s, want success", c.Name, c.Status)
				}
			}
			if len(skippedChildren) != tt.wantSkipCount {
				t.Fatalf("skippedChildren=%d, want %d", len(skippedChildren), tt.wantSkipCount)
			}
			for _, c := range skippedChildren {
				if c.Status != "skipped" {
					t.Errorf("skipped child %s status=%s, want skipped", c.Name, c.Status)
				}
			}
		})
	}
}

// TestActionParallel drives the parallel container end-to-end via Execute.
// Child concurrency is covered separately by
// TestExecuteContainerDAG_ParallelConcurrency (Execute currently chains
// parallel children — see TestActionParallel_ExecuteChainsChildren).
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

// TestActionParallel_ExecuteChainsChildren documents CURRENT engine behavior:
// driven through Execute(), parallel children are linearly chained by
// InferDependencies (inferLinearDependencies recurses into parallel.Steps),
// so they execute one per wave in definition order instead of concurrently.
// This contradicts the "parallel 子节点默认并行（不添加依赖）" intent in
// parser.go's inferContainerDependenciesRecursive — reported as a bug; this
// test pins the actual behavior so a fix flips it loudly.
func TestActionParallel_ExecuteChainsChildren(t *testing.T) {
	dir := t.TempDir()
	e := newQuietExecutor(nil)
	wf := &Workflow{
		Name: "parallel-chained",
		Steps: []Step{{
			Name: "par", Action: "parallel",
			Steps: []Step{
				{Name: "p1", Action: "shell", Command: "touch " + dir + "/p1.done"},
				// Succeeds only if p1 already finished — under the current
				// linear-chain inference this always holds.
				{Name: "p2", Action: "shell", Command: "test -f " + dir + "/p1.done"},
				{Name: "p3", Action: "log", Message: "tail"},
			},
		}},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s (p2 ran before p1 finished → children NOT chained?)", result.Status, result.Error)
	}
	sr := result.Steps[0]
	// Chained children run one per wave, so container children appear in
	// definition order (a parallel wave would not guarantee p1 before p2).
	got := stepNames(sr.Children)
	if want := []string{"p1", "p2", "p3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("children order=%v, want %v (linear chain via inference)", got, want)
	}
}

// TestExecParallel_Summary covers execParallel directly: the summary output
// line, per-child results, and error aggregation. (execParallel is only
// reachable when a parallel step is dispatched through executeStep — see
// report; the Execute path routes parallel through executeContainerDAG.)
func TestExecParallel_Summary(t *testing.T) {
	tests := []struct {
		name        string
		children    []Step
		wantOutput  []string // substrings of the aggregated output
		wantErr     string   // substring of the returned error
		wantSuccess int
	}{
		{
			name: "all children succeed",
			children: []Step{
				{Name: "m1", Action: "log", Message: "m1"},
				{Name: "m2", Action: "log", Message: "m2"},
			},
			wantOutput:  []string{"并行完成: 2个任务, 2成功, 0失败"},
			wantSuccess: 2,
		},
		{
			name: "one child fails",
			children: []Step{
				{Name: "m1", Action: "log", Message: "m1"},
				{Name: "bad", Action: "shell", Command: "exit 1"},
			},
			wantOutput:  []string{"1成功, 1失败"},
			wantErr:     "parallel step 'bad' failed",
			wantSuccess: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(nil)
			step := Step{Name: "par", Action: "parallel", Steps: tt.children}
			wfResult := &WorkflowResult{Name: "par-direct", Variables: map[string]string{}}
			output, children, err := e.execParallel(step, 0, wfResult, "par-1")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("execParallel: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error=%v, want substring %q", err, tt.wantErr)
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(output, want) {
					t.Errorf("output=%q, want substring %q", output, want)
				}
			}
			if len(children) != len(tt.children) {
				t.Fatalf("children=%d, want %d", len(children), len(tt.children))
			}
			success := 0
			for _, c := range children {
				if c.Status == "success" {
					success++
				}
			}
			if success != tt.wantSuccess {
				t.Errorf("successful children=%d, want %d", success, tt.wantSuccess)
			}
		})
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
			// NOTE: engine behavior — a non-2xx response is NOT an error; the
			// step succeeds and the status code is recorded in the output.
			name:       "500 response still succeeds",
			step:       Step{Name: "get-fail", Action: "http", URL: srv.URL + "/fail"},
			wantStatus: "success", wantStep: "success",
			wantOut: []string{"Status: 500", "boom"},
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
