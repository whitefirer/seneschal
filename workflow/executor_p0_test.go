package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whitefirer/seneschal/workflow/ai"
)

// TestExecute_ParallelAISteps_TokenAttribution runs two AI steps concurrently
// in a parallel container and verifies each step is attributed the token
// counts of ITS OWN provider response (previously both read shared
// lastAIInputTokens/lastAIOutputTokens slots, so counts could be swapped
// between steps — and racy). This is the go test -race gatekeeper for the
// concurrent-execution fixes.
func TestExecute_ParallelAISteps_TokenAttribution(t *testing.T) {
	mock := &MockProvider{
		Responses: []ai.Response{
			{Text: "resp-A", InputTokens: 11, OutputTokens: 21},
			{Text: "resp-B", InputTokens: 12, OutputTokens: 22},
		},
	}
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name: "parallel-ai",
		AI:   ai.Config{Model: "mock-model"},
		Steps: []Step{
			{Name: "par", Action: "parallel", Steps: []Step{
				{Name: "ai-a", Action: "ai", Prompt: "prompt a"},
				{Name: "ai-b", Action: "ai", Prompt: "prompt b"},
			}},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("want 1 container step, got %d", len(result.Steps))
	}
	children := result.Steps[0].Children
	if len(children) != 2 {
		t.Fatalf("want 2 children, got %d", len(children))
	}

	// MockProvider hands out responses FIFO but either goroutine may run
	// first, so match tokens to the response text each step actually got.
	for _, c := range children {
		switch {
		case strings.Contains(c.Output, "resp-A"):
			if c.InputTokens != 11 || c.OutputTokens != 21 {
				t.Errorf("step %s: tokens (%d,%d), want (11,21) for resp-A",
					c.Name, c.InputTokens, c.OutputTokens)
			}
		case strings.Contains(c.Output, "resp-B"):
			if c.InputTokens != 12 || c.OutputTokens != 22 {
				t.Errorf("step %s: tokens (%d,%d), want (12,22) for resp-B",
					c.Name, c.InputTokens, c.OutputTokens)
			}
		default:
			t.Errorf("step %s: unexpected output %q", c.Name, c.Output)
		}
	}

	// Totals aggregate regardless of scheduling.
	if result.TotalInputTokens != 23 || result.TotalOutputTokens != 43 {
		t.Errorf("totals (%d,%d), want (23,43)", result.TotalInputTokens, result.TotalOutputTokens)
	}
	// Both turns must land in history (guarded append — no lost updates).
	if got := len(e.aiHistoryCopy()); got != 4 {
		t.Errorf("aiHistory len=%d, want 4 (2 steps x user+assistant)", got)
	}
	if got := e.cumulativeAITokens(); got != 66 {
		t.Errorf("cumulativeTokens=%d, want 66", got)
	}
}

// TestTopologicalSort_ExplicitDependsOnOnly is a regression test: nodes whose
// dependencies exist only as explicit depends_on entries (no matching next
// edge) used to deadlock Kahn's algorithm into a false cycle error, because
// in-degrees were counted from DependsOn but decremented along Next.
func TestTopologicalSort_ExplicitDependsOnOnly(t *testing.T) {
	e := NewExecutor(nil)
	steps := []Step{
		{Name: "a", Action: "log", Message: "a"},
		{Name: "b", Action: "log", Message: "b", DependsOn: []string{"a"}},
		{Name: "c", Action: "log", Message: "c", DependsOn: []string{"a"}},
	}
	graph, err := e.buildDAGGraph(steps)
	if err != nil {
		t.Fatalf("buildDAGGraph: %v", err)
	}
	order, err := e.topologicalSort(graph)
	if err != nil {
		t.Fatalf("topologicalSort: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("order=%v, want 3 nodes", order)
	}
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	if pos["a"] > pos["b"] || pos["a"] > pos["c"] {
		t.Errorf("order %v violates dependencies (a must precede b and c)", order)
	}
}

// TestTopologicalSort_RealCycle verifies a genuine dependency cycle is still
// reported as an error.
func TestTopologicalSort_RealCycle(t *testing.T) {
	e := NewExecutor(nil)
	steps := []Step{
		{Name: "a", Action: "log", Message: "a", DependsOn: []string{"b"}},
		{Name: "b", Action: "log", Message: "b", DependsOn: []string{"a"}},
	}
	graph, err := e.buildDAGGraph(steps)
	if err != nil {
		t.Fatalf("buildDAGGraph: %v", err)
	}
	if _, err := e.topologicalSort(graph); err == nil {
		t.Fatal("expected cycle error, got nil")
	} else if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error %q should mention a cycle", err)
	}
}

// TestExecute_ExplicitDependsOnRegression is the end-to-end version of the
// topologicalSort regression: c gains an extra inferred dependency (from the
// linear a→b→c chain) on top of its explicit depends_on: [a], which used to
// trigger a false "DAG contains a cycle" failure.
func TestExecute_ExplicitDependsOnRegression(t *testing.T) {
	e := NewExecutor(nil)
	wf := &Workflow{
		Name: "explicit-deps",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo a"},
			{Name: "b", Action: "shell", Command: "echo b"},
			{Name: "c", Action: "shell", Command: "echo c", DependsOn: []string{"a"}},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("want 3 executed steps, got %d", len(result.Steps))
	}
	for _, s := range result.Steps {
		if s.Status != "success" {
			t.Errorf("step %s status=%s, want success", s.Name, s.Status)
		}
	}
}

// TestExecute_CycleStillFails verifies a real depends_on cycle fails the run.
func TestExecute_CycleStillFails(t *testing.T) {
	e := NewExecutor(nil)
	wf := &Workflow{
		Name: "cycle",
		Steps: []Step{
			{Name: "a", Action: "log", Message: "a", DependsOn: []string{"b"}},
			{Name: "b", Action: "log", Message: "b", DependsOn: []string{"a"}},
		},
	}
	result := e.Execute(wf)
	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "cycle") {
		t.Errorf("error %q should mention a cycle", result.Error)
	}
}

// TestExecute_CancellationStopsWorkflow presets a cancellable execution
// context (as the TUI quit path does), cancels it from the step callback
// after the first step, and verifies no further steps are dispatched.
func TestExecute_CancellationStopsWorkflow(t *testing.T) {
	e := NewExecutor(nil)
	ctx, cancel := context.WithCancel(context.Background())
	e.execCtx = ctx
	e.execCancel = cancel

	e.SetStepCallback(func(string, StepResult) { cancel() })

	wf := &Workflow{
		Name: "cancel-test",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo a"},
			{Name: "b", Action: "shell", Command: "echo b"},
			{Name: "c", Action: "shell", Command: "echo c"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed (canceled)", result.Status)
	}
	if !strings.Contains(result.Error, "canceled") {
		t.Errorf("error %q should mention cancellation", result.Error)
	}
	// Step a ran before the cancel; no later step may have succeeded.
	// (Nodes still in the waiting list are recorded as skipped; a node that
	// had just become ready but was never dispatched gets no record —
	// pre-existing behavior of the error path.)
	if len(result.Steps) == 0 || result.Steps[0].Name != "a" || result.Steps[0].Status != "success" {
		t.Fatalf("step a should have run successfully before cancel, got %+v", result.Steps)
	}
	for _, s := range result.Steps[1:] {
		if s.Status == "success" {
			t.Errorf("step %s succeeded after cancellation", s.Name)
		}
	}
}

// TestExecute_ContainerCancellationStopsChildWaves verifies that canceling a
// run while a parallel container is mid-schedule stops the container's own
// wave scheduling too: the in-flight wave finishes, the next wave's child is
// never dispatched, and the container ends in the same terminal state a
// top-level cancellation produces ("failed" / "workflow canceled"). Unstarted
// children get no result — containers intentionally don't synthesize skipped
// entries on failure.
func TestExecute_ContainerCancellationStopsChildWaves(t *testing.T) {
	e := NewExecutor(nil)
	ctx, cancel := context.WithCancel(context.Background())
	e.execCtx = ctx
	e.execCancel = cancel

	// Cancel when the slow child completes: wave 1 (slow+fast) has then
	// finished successfully, so only the wave-level cancel check — not a
	// failure cascade — can stop "later" from being dispatched. (The sleep
	// action is not context-aware, so an already-distributed slow step runs
	// to completion despite the cancel.)
	e.SetStepCallback(func(name string, _ StepResult) {
		if name == "slow" {
			cancel()
		}
	})

	var mu sync.Mutex
	var events []ProgressEvent
	e.OnProgress = func(ev ProgressEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	wf := &Workflow{
		Name: "container-cancel-test",
		Steps: []Step{
			{Name: "p", Action: "parallel", Steps: []Step{
				{Name: "slow", Action: "sleep", Duration: "500ms"},
				{Name: "fast", Action: "log", Message: "fast"},
				{Name: "later", Action: "log", Message: "later", DependsOn: []string{"slow"}},
			}},
		},
	}

	start := time.Now()
	result := e.Execute(wf)
	elapsed := time.Since(start)

	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed (canceled)", result.Status)
	}
	if !strings.Contains(result.Error, "canceled") {
		t.Errorf("error %q should mention cancellation", result.Error)
	}

	// The container got a terminal result: failed with the top-level
	// cancellation wording.
	if len(result.Steps) != 1 {
		t.Fatalf("expected exactly the container result, got %+v", result.Steps)
	}
	cont := result.Steps[0]
	if cont.Status != "failed" || cont.Error != "workflow canceled" {
		t.Errorf("container result = %s/%q, want failed/\"workflow canceled\"", cont.Status, cont.Error)
	}

	// slow+fast ran (wave 1); "later" was never dispatched and has no result.
	childStatus := map[string]string{}
	for _, c := range cont.Children {
		childStatus[c.Name] = c.Status
	}
	if childStatus["slow"] != "success" || childStatus["fast"] != "success" {
		t.Errorf("wave-1 children should have succeeded, got %v", childStatus)
	}
	if _, ran := childStatus["later"]; ran {
		t.Error("'later' must not have a result — it should never be dispatched after cancel")
	}

	// Events: the container received a terminal step_complete (failed); no
	// step_start was ever emitted for "later".
	mu.Lock()
	containerCompleted := false
	for _, ev := range events {
		if ev.Type == "step_start" && ev.Name == "later" {
			t.Error("step_start emitted for 'later' after cancellation")
		}
		if ev.Type == "step_complete" && ev.Name == "p" {
			containerCompleted = true
			if ev.Status != "failed" {
				t.Errorf("container step_complete status=%q, want failed", ev.Status)
			}
		}
	}
	mu.Unlock()
	if !containerCompleted {
		t.Error("container never received a terminal step_complete event")
	}

	// No stuck scheduling: the run returns promptly (the only real work is
	// the 500ms sleep). Execute joining its wave goroutines on return is the
	// goroutine-leak guard.
	if elapsed > 10*time.Second {
		t.Errorf("execution took %s — possible stuck wave scheduling", elapsed)
	}
}

// TestExecute_ForeachCancellationStopsIterations verifies that a canceled run
// does not start new foreach iterations: the in-flight iteration finishes,
// later iterations never run, and the container ends failed with the
// top-level cancellation wording.
func TestExecute_ForeachCancellationStopsIterations(t *testing.T) {
	e := NewExecutor(nil)
	ctx, cancel := context.WithCancel(context.Background())
	e.execCtx = ctx
	e.execCancel = cancel

	// Cancel after the first iteration's step completes.
	var once sync.Once
	e.SetStepCallback(func(name string, _ StepResult) {
		if name == "work" {
			once.Do(cancel)
		}
	})

	var mu sync.Mutex
	workStarts := 0
	e.OnProgress = func(ev ProgressEvent) {
		if ev.Type == "step_start" && ev.Name == "work" {
			mu.Lock()
			workStarts++
			mu.Unlock()
		}
	}

	wf := &Workflow{
		Name: "foreach-cancel-test",
		Steps: []Step{
			{Name: "iterate", Action: "foreach", Items: "1,2,3,4,5", Do: []Step{
				{Name: "work", Action: "log", Message: "iter"},
			}},
		},
	}

	start := time.Now()
	result := e.Execute(wf)
	elapsed := time.Since(start)

	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed (canceled)", result.Status)
	}
	if !strings.Contains(result.Error, "canceled") {
		t.Errorf("error %q should mention cancellation", result.Error)
	}

	if len(result.Steps) != 1 {
		t.Fatalf("expected exactly the foreach container result, got %+v", result.Steps)
	}
	cont := result.Steps[0]
	if cont.Status != "failed" || cont.Error != "workflow canceled" {
		t.Errorf("foreach result = %s/%q, want failed/\"workflow canceled\"", cont.Status, cont.Error)
	}

	// Only iteration 1 ran: exactly one child result and one step_start —
	// later iterations must not start.
	if len(cont.Children) != 1 {
		t.Errorf("children=%d, want 1 (only the first iteration)", len(cont.Children))
	}
	mu.Lock()
	if workStarts != 1 {
		t.Errorf("work step_start count=%d, want 1 — later iterations must not start", workStarts)
	}
	mu.Unlock()

	if elapsed > 10*time.Second {
		t.Errorf("execution took %s — possible stuck iteration loop", elapsed)
	}
}
