package workflow

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// newQuietExecutor returns an Executor with terminal printers disabled so
// test runs don't spam stdout.
func newQuietExecutor(vars map[string]string) *Executor {
	e := NewExecutor(vars)
	e.printer = nil
	return e
}

// runDAG drives executeDAG directly, bypassing InferDependencies, so tests
// control the exact next/depends_on edges under test.
func runDAG(e *Executor, wf *Workflow) *WorkflowResult {
	result := &WorkflowResult{
		Name:      wf.Name,
		Status:    "success",
		StartTime: Now(),
		Variables: make(map[string]string),
	}
	e.executeDAG(wf, result)
	return result
}

// findStepResult returns the first step result with the given name, or nil.
func findStepResult(result *WorkflowResult, name string) *StepResult {
	for i := range result.Steps {
		if result.Steps[i].Name == name {
			return &result.Steps[i]
		}
	}
	return nil
}

// stepNames extracts step names in result order.
func stepNames(steps []StepResult) []string {
	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.Name)
	}
	return names
}

// rendezvousScript returns a shell command that signals `self` started and
// then waits (bounded, ~5s) for `peer` to start. Two such steps can only
// both succeed if they execute concurrently — serial execution makes the
// first one time out and fail, which fails the test instead of hanging.
func rendezvousScript(dir, self, peer string) string {
	return fmt.Sprintf(
		"touch %s/%s.start; for i in $(seq 1 100); do [ -f %s/%s.start ] && break; sleep 0.05; done; [ -f %s/%s.start ]",
		dir, self, dir, peer, dir, peer)
}

// TestBuildDAGGraph covers graph construction: ID normalization, next→depends_on
// folding, unknown-reference filtering, and join_mode propagation.
func TestBuildDAGGraph(t *testing.T) {
	tests := []struct {
		name  string
		steps []Step
		check func(t *testing.T, graph map[string]*DAGNode)
	}{
		{
			name: "step name used as ID when no explicit ID",
			steps: []Step{
				{Name: "a", Action: "log"},
				{Name: "b", Action: "log"},
			},
			check: func(t *testing.T, g map[string]*DAGNode) {
				if len(g) != 2 {
					t.Fatalf("graph size=%d, want 2", len(g))
				}
				if g["a"] == nil || g["b"] == nil {
					t.Errorf("expected nodes keyed by name, got %v", reflect.ValueOf(g).MapKeys())
				}
			},
		},
		{
			name: "depends_on by name normalized to explicit ID",
			steps: []Step{
				{Name: "One", ID: "s1", Action: "log"},
				{Name: "Two", Action: "log", DependsOn: []string{"One"}},
			},
			check: func(t *testing.T, g map[string]*DAGNode) {
				node := g["Two"]
				if node == nil {
					t.Fatalf("node Two missing from graph %v", g)
				}
				if !reflect.DeepEqual(node.DependsOn, []string{"s1"}) {
					t.Errorf("Two.DependsOn=%v, want [s1] (name resolved to explicit ID)", node.DependsOn)
				}
			},
		},
		{
			name: "next edge folds into target depends_on",
			steps: []Step{
				{Name: "A", Action: "log", Next: []string{"B"}},
				{Name: "B", Action: "log"},
			},
			check: func(t *testing.T, g map[string]*DAGNode) {
				if !containsString(g["B"].DependsOn, "A") {
					t.Errorf("B.DependsOn=%v, want it to include A (from A.next)", g["B"].DependsOn)
				}
				if len(g["A"].DependsOn) != 0 {
					t.Errorf("A.DependsOn=%v, want empty", g["A"].DependsOn)
				}
			},
		},
		{
			name: "explicit depends_on not duplicated by next folding",
			steps: []Step{
				{Name: "A", Action: "log", Next: []string{"B"}},
				{Name: "B", Action: "log", DependsOn: []string{"A"}},
			},
			check: func(t *testing.T, g map[string]*DAGNode) {
				if got := len(g["B"].DependsOn); got != 1 {
					t.Errorf("B.DependsOn=%v, want exactly 1 entry (no duplicate)", g["B"].DependsOn)
				}
			},
		},
		{
			name: "unknown references dropped",
			steps: []Step{
				{Name: "A", Action: "log", DependsOn: []string{"ghost"}, Next: []string{"ghost"}},
			},
			check: func(t *testing.T, g map[string]*DAGNode) {
				if len(g["A"].DependsOn) != 0 {
					t.Errorf("A.DependsOn=%v, want empty (unknown dep filtered)", g["A"].DependsOn)
				}
				if len(g["A"].Next) != 0 {
					t.Errorf("A.Next=%v, want empty (unknown next filtered)", g["A"].Next)
				}
			},
		},
		{
			name: "join_mode preserved on node",
			steps: []Step{
				{Name: "A", Action: "log", JoinMode: "any"},
			},
			check: func(t *testing.T, g map[string]*DAGNode) {
				if g["A"].JoinMode != "any" {
					t.Errorf("A.JoinMode=%q, want any", g["A"].JoinMode)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(nil)
			graph, err := e.buildDAGGraph(tt.steps)
			if err != nil {
				t.Fatalf("buildDAGGraph: %v", err)
			}
			tt.check(t, graph)
		})
	}
}

// TestExecuteDAG_ParallelWaves proves independent top-level steps run in the
// same wave concurrently: each step waits for the other's start marker, so
// serial execution would fail them deterministically.
func TestExecuteDAG_ParallelWaves(t *testing.T) {
	dir := t.TempDir()
	wf := &Workflow{
		Name: "parallel-waves",
		Steps: []Step{
			{Name: "A", Action: "shell", Command: rendezvousScript(dir, "a", "b")},
			{Name: "B", Action: "shell", Command: rendezvousScript(dir, "b", "a")},
		},
	}
	e := newQuietExecutor(nil)

	// Track the maximum number of concurrently in-flight steps via events.
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	e.OnProgress = func(ev ProgressEvent) {
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

	result := runDAG(e, wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s (rendezvous failed: steps did not run concurrently)", result.Status, result.Error)
	}
	if maxInFlight != 2 {
		t.Errorf("maxInFlight=%d, want 2 (A and B overlapped in one wave)", maxInFlight)
	}
	for _, name := range []string{"A", "B"} {
		sr := findStepResult(result, name)
		if sr == nil || sr.Status != "success" {
			t.Errorf("step %s: %+v, want success", name, sr)
		}
	}
}

// TestExecuteDAG_DependencyChain verifies an explicit A→B→C depends_on chain
// executes strictly in order (single-node waves → deterministic result order).
func TestExecuteDAG_DependencyChain(t *testing.T) {
	wf := &Workflow{
		Name: "chain",
		Steps: []Step{
			{Name: "A", Action: "shell", Command: "echo a"},
			{Name: "B", Action: "shell", Command: "echo b", DependsOn: []string{"A"}},
			{Name: "C", Action: "shell", Command: "echo c", DependsOn: []string{"B"}},
		},
	}
	e := newQuietExecutor(nil)
	result := runDAG(e, wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if got, want := stepNames(result.Steps), []string{"A", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("execution order=%v, want %v", got, want)
	}
	for _, name := range []string{"A", "B", "C"} {
		sr := findStepResult(result, name)
		if sr == nil || sr.Status != "success" {
			t.Errorf("step %s: %+v, want success", name, sr)
		}
	}
}

// TestExecuteDAG_MixedNextAndDependsOn mixes an explicit next edge (A→B) with
// an explicit depends_on edge (C depends on B) and expects wave order A,B,C.
func TestExecuteDAG_MixedNextAndDependsOn(t *testing.T) {
	wf := &Workflow{
		Name: "mixed-edges",
		Steps: []Step{
			{Name: "A", Action: "log", Message: "a", Next: []string{"B"}},
			{Name: "B", Action: "log", Message: "b"},
			{Name: "C", Action: "log", Message: "c", DependsOn: []string{"B"}},
		},
	}
	e := newQuietExecutor(nil)
	result := runDAG(e, wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if got, want := stepNames(result.Steps), []string{"A", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("execution order=%v, want %v", got, want)
	}
}

// TestExecuteDAG_JoinMode verifies join_mode on a multi-dependency step:
// "any" schedules it as soon as one dependency completes, "all" (the default)
// waits for every dependency. B sleeps 0.5s, so the ordering of C's start
// event against B's completion event is a deterministic discriminator.
func TestExecuteDAG_JoinMode(t *testing.T) {
	tests := []struct {
		name         string
		joinMode     string
		cBeforeBDone bool
	}{
		{name: "any: ready after first completed dependency", joinMode: "any", cBeforeBDone: true},
		{name: "all: waits for every dependency", joinMode: "all", cBeforeBDone: false},
		{name: "default (empty) behaves like all", joinMode: "", cBeforeBDone: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				Name: "join-" + tt.joinMode,
				Steps: []Step{
					{Name: "A", Action: "log", Message: "a"},
					{Name: "B", Action: "shell", Command: "sleep 0.5; echo b", DependsOn: []string{"A"}},
					{Name: "C", Action: "log", Message: "c", DependsOn: []string{"A", "B"}, JoinMode: tt.joinMode},
				},
			}
			e := newQuietExecutor(nil)
			var mu sync.Mutex
			var events []string
			e.OnProgress = func(ev ProgressEvent) {
				if ev.Type != "step_start" && ev.Type != "step_complete" {
					return
				}
				mu.Lock()
				events = append(events, ev.Type+":"+ev.Name)
				mu.Unlock()
			}
			result := runDAG(e, wf)
			if result.Status != "success" {
				t.Fatalf("status=%s err=%s", result.Status, result.Error)
			}
			if len(result.Steps) != 3 {
				t.Fatalf("steps=%d, want 3", len(result.Steps))
			}
			mu.Lock()
			cStart, bDone := -1, -1
			for i, ev := range events {
				if ev == "step_start:C" && cStart < 0 {
					cStart = i
				}
				if ev == "step_complete:B" && bDone < 0 {
					bDone = i
				}
			}
			mu.Unlock()
			if cStart < 0 || bDone < 0 {
				t.Fatalf("missing events (cStart=%d bDone=%d): %v", cStart, bDone, events)
			}
			if tt.cBeforeBDone && cStart > bDone {
				t.Errorf("join_mode=%q: C started AFTER B completed, want before; events=%v", tt.joinMode, events)
			}
			if !tt.cBeforeBDone && cStart < bDone {
				t.Errorf("join_mode=%q: C started BEFORE B completed, want after; events=%v", tt.joinMode, events)
			}
		})
	}
}

// TestExecuteDAG_FailureBlocking covers how a failed step affects the rest of
// the DAG: dependents are skipped, continue_on_error unblocks them, and
// independent wave-mates still execute.
func TestExecuteDAG_FailureBlocking(t *testing.T) {
	tests := []struct {
		name  string
		steps []Step
		check func(t *testing.T, result *WorkflowResult)
	}{
		{
			name: "failed step skips transitive dependents",
			steps: []Step{
				{Name: "A", Action: "shell", Command: "exit 1"},
				{Name: "B", Action: "log", Message: "b", DependsOn: []string{"A"}},
				{Name: "C", Action: "log", Message: "c", DependsOn: []string{"B"}},
			},
			check: func(t *testing.T, r *WorkflowResult) {
				if r.Status != "failed" {
					t.Errorf("status=%s, want failed", r.Status)
				}
				if !strings.Contains(r.Error, "step 'A' failed") {
					t.Errorf("error=%q, want mention of step 'A' failing", r.Error)
				}
				if sr := findStepResult(r, "A"); sr == nil || sr.Status != "failed" {
					t.Errorf("A: %+v, want failed", sr)
				}
				for _, name := range []string{"B", "C"} {
					sr := findStepResult(r, name)
					if sr == nil || sr.Status != "skipped" {
						t.Errorf("%s: %+v, want skipped", name, sr)
						continue
					}
					if !strings.Contains(sr.Error, "skipped due to previous failure") {
						t.Errorf("%s: error=%q, want skip reason", name, sr.Error)
					}
				}
			},
		},
		{
			name: "continue_on_error lets dependents run",
			steps: []Step{
				{Name: "A", Action: "shell", Command: "exit 1", ContinueOnError: true},
				{Name: "B", Action: "log", Message: "b", DependsOn: []string{"A"}},
			},
			check: func(t *testing.T, r *WorkflowResult) {
				// NOTE: engine behavior — the workflow reports "success" even
				// though step A failed, because continue_on_error suppresses
				// the failure entirely.
				if r.Status != "success" {
					t.Errorf("status=%s, want success (continue_on_error)", r.Status)
				}
				if sr := findStepResult(r, "A"); sr == nil || sr.Status != "failed" {
					t.Errorf("A: %+v, want failed", sr)
				}
				if sr := findStepResult(r, "B"); sr == nil || sr.Status != "success" {
					t.Errorf("B: %+v, want success (ran despite A failing)", sr)
				}
			},
		},
		{
			name: "independent wave-mate still executes",
			steps: []Step{
				{Name: "A", Action: "shell", Command: "exit 1"},
				{Name: "X", Action: "log", Message: "x"},
				{Name: "B", Action: "log", Message: "b", DependsOn: []string{"A"}},
			},
			check: func(t *testing.T, r *WorkflowResult) {
				if r.Status != "failed" {
					t.Errorf("status=%s, want failed", r.Status)
				}
				if sr := findStepResult(r, "X"); sr == nil || sr.Status != "success" {
					t.Errorf("X: %+v, want success (same wave as failing A)", sr)
				}
				if sr := findStepResult(r, "B"); sr == nil || sr.Status != "skipped" {
					t.Errorf("B: %+v, want skipped", sr)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newQuietExecutor(nil)
			result := runDAG(e, &Workflow{Name: tt.name, Steps: tt.steps})
			tt.check(t, result)
		})
	}
}

// TestExecuteDAG_SkippedOrderDeterministic pins the synthesized skipped
// results to topological order: A fails in the first wave, and the
// still-waiting B and C must be appended B-before-C on every run. Before
// runWaves sorted the survivors by waveConfig.order, the waiting map's
// random iteration order made their relative order a coin flip.
func TestExecuteDAG_SkippedOrderDeterministic(t *testing.T) {
	// Two waiting nodes come out in the wrong order with ~50% probability per
	// run before the fix; 25 runs make a regression practically impossible to
	// slip through (p ≈ 3e-8).
	for i := 0; i < 25; i++ {
		// Fresh steps per run: execution may annotate the step structs.
		steps := []Step{
			{Name: "A", Action: "shell", Command: "exit 1"},
			{Name: "B", Action: "log", Message: "b", DependsOn: []string{"A"}},
			{Name: "C", Action: "log", Message: "c", DependsOn: []string{"B"}},
		}
		r := runDAG(newQuietExecutor(nil), &Workflow{Name: "det", Steps: steps})
		if r.Status != "failed" {
			t.Fatalf("run %d: status=%s, want failed", i, r.Status)
		}
		got := stepNames(r.Steps)
		want := []string{"A", "B", "C"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: step order %v, want %v (skipped results must follow topological order)", i, got, want)
		}
		if r.Steps[1].Status != "skipped" || r.Steps[2].Status != "skipped" {
			t.Fatalf("run %d: B and C must be skipped: %+v", i, r.Steps)
		}
	}
}

// TestTopologicalSort_DeterministicOrder pins declaration-order determinism:
// with multiple entry nodes, the topological order must follow YAML
// declaration order across runs (map iteration order is random), while
// dependencies still take precedence.
func TestTopologicalSort_DeterministicOrder(t *testing.T) {
	e := newQuietExecutor(nil)
	steps := []Step{
		{Name: "zeta", Action: "log"},
		{Name: "alpha", Action: "log"},
		{Name: "mike", Action: "log", DependsOn: []string{"zeta"}},
		{Name: "beta", Action: "log"},
		{Name: "gamma", Action: "log", DependsOn: []string{"beta"}},
	}
	want := []string{"zeta", "alpha", "beta", "mike", "gamma"}

	for i := 0; i < 50; i++ {
		graph, err := e.buildDAGGraph(steps)
		if err != nil {
			t.Fatalf("buildDAGGraph: %v", err)
		}
		order, err := e.topologicalSort(graph)
		if err != nil {
			t.Fatalf("topologicalSort: %v", err)
		}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("run %d: order=%v, want %v", i, order, want)
		}
	}
}

// TestExecuteDAG_ParallelEntriesStillConcurrent guards the other half of the
// contract: deterministic ordering must not serialize execution — entry
// nodes in the same wave still run concurrently.
func TestExecuteDAG_ParallelEntriesStillConcurrent(t *testing.T) {
	dir := t.TempDir()
	e := newQuietExecutor(nil)
	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0
	e.OnProgress = func(ev ProgressEvent) {
		if ev.Type != "step_start" && ev.Type != "step_complete" {
			return
		}
		mu.Lock()
		if ev.Type == "step_start" {
			inFlight++
			if inFlight > maxInFlight {
				maxInFlight = inFlight
			}
		} else {
			inFlight--
		}
		mu.Unlock()
	}

	wf := &Workflow{
		Name: "parallel-entries",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: rendezvousScript(dir, "a", "b") + " && " + rendezvousScript(dir, "a", "c")},
			{Name: "b", Action: "shell", Command: rendezvousScript(dir, "b", "a") + " && " + rendezvousScript(dir, "b", "c")},
			{Name: "c", Action: "shell", Command: rendezvousScript(dir, "c", "a") + " && " + rendezvousScript(dir, "c", "b")},
			{Name: "tail", Action: "log", Message: "done", DependsOn: []string{"a", "b", "c"}},
		},
	}
	r := runDAG(e, wf)
	if r.Status != "success" {
		t.Fatalf("status=%s, err=%v", r.Status, r.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if maxInFlight < 3 {
		t.Fatalf("maxInFlight=%d, want 3 concurrent entry nodes", maxInFlight)
	}
	// Result collection follows declaration order now.
	got := stepNames(r.Steps)
	want := []string{"a", "b", "c", "tail"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("steps order=%v, want %v", got, want)
	}
}
