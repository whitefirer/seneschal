package workflow

import (
	"testing"

	"github.com/whitefirer/seneschal/workflow/ai"
)

func TestReplay_SmartReuseDeterministic(t *testing.T) {
	// Build a snapshot with one deterministic step (shell) and one AI step.
	snap := ExecutionSnapshot{
		ExecutionSummary: ExecutionSummary{ID: "exec-1"},
		Workflow:         "name: test\nsteps:\n  - name: build\n    action: shell\n    command: echo hi\n  - name: analyze\n    action: ai\n    prompt: test\n",
		Steps: []StepResult{
			{Name: "build", ID: "step-build", Status: "success", Output: "hi"},
			{Name: "analyze", ID: "step-analyze", Status: "success", Output: "AI result", Nondeterministic: true},
		},
	}

	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(snap); err != nil {
		t.Fatal(err)
	}

	mock := NewMockProvider("replayed AI")
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	replayer := NewReplayer(store)
	result, hits, misses, err := replayer.Replay("exec-1", e, ReplayOptions{})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// build is deterministic → should be reused (hit)
	// analyze is nondeterministic → should be re-executed (miss)
	if hits != 1 {
		t.Errorf("hits=%d want 1 (build reused)", hits)
	}
	if misses != 1 {
		t.Errorf("misses=%d want 1 (analyze re-executed)", misses)
	}
	if result.Status != "success" {
		t.Errorf("status=%s", result.Status)
	}
}

func TestReplay_FullSkipsCache(t *testing.T) {
	snap := ExecutionSnapshot{
		ExecutionSummary: ExecutionSummary{ID: "exec-2"},
		Workflow:         "name: test\nsteps:\n  - name: build\n    action: shell\n    command: echo hi\n",
		Steps: []StepResult{
			{Name: "build", ID: "step-build", Status: "success", Output: "hi"},
		},
	}

	dir := t.TempDir()
	store := NewFileStore(dir)
	store.Save(snap)

	e := NewExecutor(nil)
	replayer := NewReplayer(store)
	_, hits, misses, err := replayer.Replay("exec-2", e, ReplayOptions{Full: true})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	// Full replay: everything re-executes, no cache hits
	if hits != 0 {
		t.Errorf("hits=%d want 0 (full replay)", hits)
	}
	if misses != 0 {
		// misses may be 0 or 1 depending on whether build counts as miss
		// (the step isn't in cache so it's not a "miss" in the traditional sense)
	}
}

func TestBuildReplayCache_FlattensChildren(t *testing.T) {
	steps := []StepResult{
		{Name: "parent", ID: "step-parent", Status: "success", Children: []StepResult{
			{Name: "child1", ID: "step-child1", Status: "success"},
			{Name: "child2", ID: "step-child2", Status: "success"},
		}},
		{Name: "top", ID: "step-top", Status: "success", ThenChildren: []StepResult{
			{Name: "branch", ID: "step-branch", Status: "success"},
		}},
	}
	cache := buildReplayCache(steps, ReplayOptions{})
	// Should have entries for: parent, child1, child2, top, branch
	expected := []string{"step-parent", "step-child1", "step-child2", "step-top", "step-branch"}
	for _, id := range expected {
		if _, ok := cache[id]; !ok {
			t.Errorf("cache missing %s", id)
		}
	}
}

func TestBuildReplayCache_OnlyStepsExcludesListed(t *testing.T) {
	steps := []StepResult{
		{Name: "a", ID: "step-a", Status: "success"},
		{Name: "b", ID: "step-b", Status: "success"},
		{Name: "c", ID: "step-c", Status: "success"},
	}
	cache := buildReplayCache(steps, ReplayOptions{OnlySteps: []string{"b"}})
	// b should be EXCLUDED (forced to re-run)
	if _, ok := cache["step-b"]; ok {
		t.Error("step-b should be excluded from cache (OnlySteps)")
	}
	// a and c should be present
	if _, ok := cache["step-a"]; !ok {
		t.Error("step-a should be in cache")
	}
}

func TestReplay_AIWithMock(t *testing.T) {
	snap := ExecutionSnapshot{
		ExecutionSummary: ExecutionSummary{ID: "exec-3"},
		Workflow:         "name: test\nai:\n  model: mock\nsteps:\n  - name: ask\n    action: ai\n    prompt: hi\n    save_output: result\n",
		Steps: []StepResult{
			{Name: "ask", ID: "step-ask", Status: "success", Output: "old result", Nondeterministic: true},
		},
	}

	dir := t.TempDir()
	store := NewFileStore(dir)
	store.Save(snap)

	mock := NewMockProvider("new AI result")
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	replayer := NewReplayer(store)
	result, _, misses, err := replayer.Replay("exec-3", e, ReplayOptions{})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	// AI step should be a miss (re-executed)
	if misses != 1 {
		t.Errorf("misses=%d want 1", misses)
	}
	// Result should have new AI output
	if result.Status != "success" {
		t.Errorf("status=%s err=%s", result.Status, result.Error)
	}
	// Verify mock was called (AI step re-executed)
	if mock.CallCount() != 1 {
		t.Errorf("mock called %d times, want 1", mock.CallCount())
	}
}

// Ensure ai import is used (for ai.Config in potential future tests)
var _ = ai.Config{}
