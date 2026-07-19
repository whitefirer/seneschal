package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aiAutoHook returns an after_step ai hook in ai_auto (auto-acting) mode.
func aiAutoHook() HookConfig {
	return HookConfig{On: HookAfterStep, Type: "ai", Mode: "ai_auto"}
}

// TestHook_AIAutoSkip verifies an ai_auto after_step hook can mark a step
// skipped and the workflow continues.
func TestHook_AIAutoSkip(t *testing.T) {
	mock := NewMockProvider(`{"action":"skip","reason":"not needed"}`)
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name: "hook-skip",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo ok", Hooks: []HookConfig{aiAutoHook()}},
			{Name: "b", Action: "shell", Command: "echo done"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s (skipped step must not fail the workflow)", result.Status, result.Error)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(result.Steps))
	}
	if result.Steps[0].Status != "skipped" {
		t.Errorf("step a status=%s, want skipped", result.Steps[0].Status)
	}
	if !strings.Contains(result.Steps[0].Error, "not needed") {
		t.Errorf("step a error=%q, want hook reason", result.Steps[0].Error)
	}
	if result.Steps[1].Status != "success" {
		t.Errorf("step b status=%s, want success (workflow continued)", result.Steps[1].Status)
	}
	if mock.CallCount() != 1 {
		t.Errorf("hook AI called %d times, want 1 (only step a has the hook)", mock.CallCount())
	}
}

// TestHook_AIAutoAbort verifies an ai_auto hook can abort the workflow by
// failing the step.
func TestHook_AIAutoAbort(t *testing.T) {
	mock := NewMockProvider(`{"action":"abort","reason":"critical failure"}`)
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name: "hook-abort",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo ok", Hooks: []HookConfig{aiAutoHook()}},
			{Name: "b", Action: "shell", Command: "echo never"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed (hook aborted)", result.Status)
	}
	if result.Steps[0].Status != "failed" {
		t.Errorf("step a status=%s, want failed", result.Steps[0].Status)
	}
	if !strings.Contains(result.Steps[0].Error, "critical failure") {
		t.Errorf("step a error=%q, want hook reason", result.Steps[0].Error)
	}
	// Step b must not have run: the DAG stops on the failed step.
	if len(result.Steps) != 2 || result.Steps[1].Status != "skipped" {
		t.Errorf("step b should be skipped after abort, got %+v", result.Steps)
	}
}

// TestHook_AIAutoRetry verifies an ai_auto hook can force a re-run, and that
// hook-driven retries are capped (a hook that always says "retry" must not
// loop forever).
func TestHook_AIAutoRetry(t *testing.T) {
	mock := NewMockProvider(`{"action":"retry","reason":"looks flaky"}`)
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	countFile := filepath.Join(t.TempDir(), "runs")
	wf := &Workflow{
		Name: "hook-retry",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo run >> " + countFile, Hooks: []HookConfig{aiAutoHook()}},
		},
	}
	result := e.Execute(wf)
	// The step itself succeeds; after the retry budget (3) is exhausted the
	// last attempt's result stands.
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}

	data, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("read count file: %v", err)
	}
	runs := strings.Count(strings.TrimSpace(string(data)), "\n") + 1
	if runs != 4 {
		t.Errorf("step executed %d times, want 4 (1 initial + 3 capped hook retries)", runs)
	}
	if mock.CallCount() != 4 {
		t.Errorf("hook AI called %d times, want 4 (once per attempt)", mock.CallCount())
	}
}

// TestHook_SuggestModeDoesNotAct verifies a non-auto ai hook (mode "ai")
// only suggests — it never changes control flow even if the model says
// skip/abort/retry.
func TestHook_SuggestModeDoesNotAct(t *testing.T) {
	mock := NewMockProvider(`{"action":"skip","reason":"should not be applied"}`)
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name: "hook-suggest",
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo ok",
				Hooks: []HookConfig{{On: HookAfterStep, Type: "ai", Mode: "ai"}}},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if result.Steps[0].Status != "success" {
		t.Errorf("step a status=%s, want success (suggest mode must not act)", result.Steps[0].Status)
	}
}

// TestHook_WorkflowEndAbort verifies a workflow_end ai_auto hook can veto a
// completed workflow (abort flips the final status to failed).
func TestHook_WorkflowEndAbort(t *testing.T) {
	mock := NewMockProvider(`{"action":"abort","reason":"postmortem check failed"}`)
	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name:  "hook-wf-abort",
		Hooks: []HookConfig{{On: HookWorkflowEnd, Type: "ai", Mode: "ai_auto"}},
		Steps: []Step{
			{Name: "a", Action: "shell", Command: "echo ok"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "failed" {
		t.Fatalf("status=%s, want failed (workflow_end hook aborted)", result.Status)
	}
	if !strings.Contains(result.Error, "postmortem check failed") {
		t.Errorf("error=%q, want hook reason", result.Error)
	}
	// The step itself still completed successfully.
	if result.Steps[0].Status != "success" {
		t.Errorf("step a status=%s, want success", result.Steps[0].Status)
	}
}

// TestMergeHookDecision verifies the abort > retry > skip priority used when
// several hooks fire for the same step.
func TestMergeHookDecision(t *testing.T) {
	skip := HookResult{Action: "skip", Reason: "s"}
	retry := HookResult{Action: "retry", Reason: "r"}
	abort := HookResult{Action: "abort", Reason: "a"}
	none := HookResult{}

	if got := mergeHookDecision(none, skip); got.Action != "skip" {
		t.Errorf("none+skip = %s, want skip", got.Action)
	}
	if got := mergeHookDecision(skip, retry); got.Action != "retry" {
		t.Errorf("skip+retry = %s, want retry", got.Action)
	}
	if got := mergeHookDecision(abort, retry); got.Action != "abort" {
		t.Errorf("abort+retry = %s, want abort", got.Action)
	}
	if got := mergeHookDecision(retry, skip); got.Action != "retry" {
		t.Errorf("retry+skip = %s, want retry", got.Action)
	}
	// "suggest" never overrides a real decision.
	if got := mergeHookDecision(skip, HookResult{Action: "suggest", Reason: "x"}); got.Action != "skip" {
		t.Errorf("skip+suggest = %s, want skip", got.Action)
	}
}
