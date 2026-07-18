package workflow

import (
	"os"
	"testing"

	"github.com/whitefirer/seneschal/workflow/ai"
)

// TestExecuteStep_Shell tests a basic shell action via the executor.
func TestExecuteStep_Shell(t *testing.T) {
	e := NewExecutor(nil)
	e.SetVerbose(false)

	wf := &Workflow{
		Name: "test-shell",
		Steps: []Step{
			{Name: "echo", Action: "shell", Command: "echo hello"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(result.Steps))
	}
	if result.Steps[0].Output == "" {
		t.Error("expected non-empty output")
	}
	if !result.Steps[0].SideEffecting {
		t.Error("shell step should be SideEffecting")
	}
}

// TestExecuteStep_Log tests a log action.
func TestExecuteStep_Log(t *testing.T) {
	e := NewExecutor(nil)
	wf := &Workflow{
		Name: "test-log",
		Steps: []Step{
			{Name: "msg", Action: "log", Message: "hello"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s", result.Status)
	}
}

// TestExecuteStep_Set tests a set action and variable persistence.
func TestExecuteStep_Set(t *testing.T) {
	e := NewExecutor(nil)
	wf := &Workflow{
		Name: "test-set",
		Steps: []Step{
			{Name: "setvar", Action: "set", Value: "testval"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s", result.Status)
	}
	// "setvar" step sets its value; output format is "Set <name> = <value>"
	if result.Steps[0].Output == "" {
		t.Errorf("expected non-empty output")
	}
}

// TestExecuteStep_Retry tests that retry causes multiple execution attempts.
func TestExecuteStep_Retry(t *testing.T) {
	e := NewExecutor(nil)
	wf := &Workflow{
		Name: "test-retry",
		Steps: []Step{
			{Name: "fail", Action: "shell", Command: "exit 1", Retry: 2, RetryDelay: "0s"},
		},
	}
	result := e.Execute(wf)
	// Should fail (exit 1 always fails)
	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	// Retries should be 2 (3 attempts total)
	if result.Steps[0].Retries != 2 {
		t.Errorf("retries=%d want 2", result.Steps[0].Retries)
	}
}

// TestExecuteStep_RetrySuccess tests retry eventually succeeding.
func TestExecuteStep_RetrySuccess(t *testing.T) {
	e := NewExecutor(nil)
	// First run creates a flag file; second run succeeds.
	wf := &Workflow{
		Name: "test-retry-ok",
		Variables: map[string]string{
			"_flag": "/tmp/seneschal_test_retry_flag",
		},
		Steps: []Step{
			{Name: "flaky", Action: "shell",
				Command: "test -f {{._flag}} && echo ok || (touch {{._flag}}; exit 1)",
				Retry:   3, RetryDelay: "0s"},
		},
	}
	// Clean flag
	delFile("/tmp/seneschal_test_retry_flag")
	result := e.Execute(wf)
	delFile("/tmp/seneschal_test_retry_flag")

	if result.Status != "success" {
		t.Fatalf("expected success, got %s (err=%s)", result.Status, result.Error)
	}
	if result.Steps[0].Retries != 1 {
		t.Errorf("retries=%d want 1 (first fail, second success)", result.Steps[0].Retries)
	}
}

// TestExecuteStep_AI_WithMock tests ai action using MockProvider.
func TestExecuteStep_AI_WithMock(t *testing.T) {
	mock := NewMockProvider("AI response text")
	mock.InputTokens = 10
	mock.OutputTokens = 20

	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name: "test-ai",
		AI:   ai.Config{Model: "mock-model"},
		Steps: []Step{
			{Name: "summarize", Action: "ai", Prompt: "test prompt", SaveOutput: "summary"},
			{Name: "report", Action: "log", Message: "got: {{.summary}}"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if !result.Steps[0].Nondeterministic {
		t.Error("ai step should be Nondeterministic")
	}
	if result.Steps[0].InputTokens != 10 {
		t.Errorf("inputTokens=%d want 10", result.Steps[0].InputTokens)
	}
	// Workflow-level token totals
	if result.TotalInputTokens != 10 {
		t.Errorf("totalInput=%d want 10", result.TotalInputTokens)
	}
	if result.TotalOutputTokens != 20 {
		t.Errorf("totalOutput=%d want 20", result.TotalOutputTokens)
	}
	if mock.CallCount() != 1 {
		t.Errorf("provider called %d times, want 1", mock.CallCount())
	}
}

// TestExecuteStep_AIDecide_WithMock tests ai_decide action.
func TestExecuteStep_AIDecide_WithMock(t *testing.T) {
	mock := NewMockProvider("true")

	e := NewExecutor(nil)
	e.SetAIProvider(mock)

	wf := &Workflow{
		Name: "test-decide",
		AI:   ai.Config{Model: "mock-model"},
		Steps: []Step{
			{Name: "check", Action: "ai_decide", Question: "is it?", SaveOutput: "result"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if result.Steps[0].ConditionResult == nil || !*result.Steps[0].ConditionResult {
		t.Error("expected ConditionResult=true")
	}
}

// TestSumTokenUsage tests recursive token aggregation.
func TestSumTokenUsage(t *testing.T) {
	steps := []StepResult{
		{Name: "a", InputTokens: 5, OutputTokens: 10},
		{Name: "b", InputTokens: 3, OutputTokens: 7, Children: []StepResult{
			{Name: "b-child", InputTokens: 2, OutputTokens: 4},
		}},
		{Name: "c", ThenChildren: []StepResult{
			{Name: "c-then", InputTokens: 1, OutputTokens: 1},
		}},
	}
	in, out := sumTokenUsage(steps)
	// 5+3+2+1=11 in, 10+7+4+1=22 out
	if in != 11 {
		t.Errorf("input=%d want 11", in)
	}
	if out != 22 {
		t.Errorf("output=%d want 22", out)
	}
}

func delFile(path string) {
	_ = os.Remove(path)
}
