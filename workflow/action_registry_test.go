package workflow

import (
	"fmt"
	"strings"
	"testing"
)

// registerForTest registers a spec, tolerating re-registration so tests can
// run repeatedly in the same process (e.g. go test -count=2).
func registerForTest(t *testing.T, spec ActionSpec) {
	t.Helper()
	if err := RegisterAction(spec); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("RegisterAction(%q): %v", spec.Name, err)
	}
}

// TestRegisterAction_RunsAndFlags verifies a custom action registered via
// RegisterAction is dispatched by executeStep, its output and side-effect flag
// are recorded, and it can write variables for downstream steps to consume.
func TestRegisterAction_RunsAndFlags(t *testing.T) {
	registerForTest(t, ActionSpec{
		Name:          "shout",
		SideEffecting: true,
		Run: func(e *Executor, step Step, result *StepResult, stepID string, depth int, parentID string) error {
			out := strings.ToUpper(step.Prompt)
			result.Output = out
			if step.SaveOutput != "" {
				e.GetContext().Set(step.SaveOutput, out)
			}
			return nil
		},
		Validate: func(step Step, index int) []error {
			if step.Prompt == "" {
				return []error{fmt.Errorf("step[%d] (%s): shout action requires 'prompt'", index, step.Name)}
			}
			return nil
		},
	})

	wf := &Workflow{
		Name: "test-shout",
		Steps: []Step{
			{Name: "shout", Action: "shout", Prompt: "hello", SaveOutput: "loud"},
			{Name: "echo", Action: "log", Message: "{{.loud}}"},
		},
	}
	e := NewExecutor(nil)
	res := e.Execute(wf)
	if res.Status != "success" {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	if res.Steps[0].Output != "HELLO" {
		t.Errorf("expected output HELLO, got %q", res.Steps[0].Output)
	}
	if !res.Steps[0].SideEffecting {
		t.Error("custom action marked SideEffecting should propagate to its result")
	}
	if got := e.GetContext().Get("loud"); got != "HELLO" {
		t.Errorf("expected variable loud=HELLO, got %q", got)
	}
	if res.Steps[1].Output == "" {
		t.Error("downstream log step should have resolved {{.loud}}")
	}
}

// TestRegisterAction_Validation verifies the custom action's Validate hook is
// consulted by ValidateStep, and that unknown actions are still rejected.
func TestRegisterAction_Validation(t *testing.T) {
	registerForTest(t, ActionSpec{
		Name: "needprompt",
		Run: func(e *Executor, step Step, result *StepResult, stepID string, depth int, parentID string) error {
			return nil
		},
		Validate: func(step Step, index int) []error {
			if step.Prompt == "" {
				return []error{fmt.Errorf("step[%d] (%s): needprompt action requires 'prompt'", index, step.Name)}
			}
			return nil
		},
	})

	if errs := ValidateStep(Step{Name: "x", Action: "needprompt"}, 0); len(errs) == 0 {
		t.Error("expected validation error for missing prompt")
	}
	if errs := ValidateStep(Step{Name: "x", Action: "needprompt", Prompt: "ok"}, 0); len(errs) != 0 {
		t.Errorf("expected valid step, got %v", errs)
	}
	if errs := ValidateStep(Step{Name: "x", Action: "does-not-exist"}, 0); len(errs) == 0 {
		t.Error("expected unknown action error")
	}
}

// TestRegisterAction_Duplicate verifies RegisterAction rejects a name that is
// already registered (built-in or custom).
func TestRegisterAction_Duplicate(t *testing.T) {
	err := RegisterAction(ActionSpec{
		Name: "shell",
		Run: func(e *Executor, step Step, result *StepResult, stepID string, depth int, parentID string) error {
			return nil
		},
	})
	if err == nil {
		t.Error("expected error re-registering built-in 'shell'")
	}
}

// TestRegisterAction_NondeterministicTaint verifies a custom action marked
// Nondeterministic taints its downstream (chained) step and the workflow
// result.
func TestRegisterAction_NondeterministicTaint(t *testing.T) {
	registerForTest(t, ActionSpec{
		Name:             "non_det",
		Nondeterministic: true,
		Run: func(e *Executor, step Step, result *StepResult, stepID string, depth int, parentID string) error {
			result.Output = "maybe"
			return nil
		},
	})

	wf := &Workflow{
		Name: "test-taint",
		Steps: []Step{
			{Name: "a", Action: "non_det"},
			{Name: "b", Action: "log", Message: "downstream"},
		},
	}
	res := NewExecutor(nil).Execute(wf)
	if res.Status != "success" {
		t.Fatalf("status=%s err=%s", res.Status, res.Error)
	}
	if !res.Nondeterministic {
		t.Error("workflow with a nondeterministic custom action should be Nondeterministic")
	}
	for _, s := range res.Steps {
		if s.Name == "b" && !s.Nondeterministic {
			t.Error("downstream step b should be tainted nondeterministic")
		}
	}
}
