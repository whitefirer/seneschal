package workflow

import "testing"

func TestValidate_Valid(t *testing.T) {
	wf := &Workflow{
		Name: "valid",
		Steps: []Step{
			{Name: "a", Action: "log", Message: "hi"},
			{Name: "b", Action: "shell", Command: "echo ok"},
		},
	}
	errs := wf.Validate()
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidate_MissingName(t *testing.T) {
	wf := &Workflow{
		Steps: []Step{
			{Action: "log", Message: "x"},
		},
	}
	errs := wf.Validate()
	if len(errs) == 0 {
		t.Error("expected error for missing name")
	}
}

func TestValidate_ShellMissingCommand(t *testing.T) {
	wf := &Workflow{
		Name: "test",
		Steps: []Step{
			{Name: "s", Action: "shell"},
		},
	}
	errs := wf.Validate()
	found := false
	for _, e := range errs {
		if matchStr(e.Error(), "requires 'command'") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'shell requires command' error")
	}
}

func TestValidate_UnknownAction(t *testing.T) {
	wf := &Workflow{
		Name: "test",
		Steps: []Step{
			{Name: "x", Action: "frobnicate"},
		},
	}
	errs := wf.Validate()
	found := false
	for _, e := range errs {
		if matchStr(e.Error(), "unknown action") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'unknown action' error")
	}
}

func TestValidate_ScriptRequiresLangAndCode(t *testing.T) {
	wf := &Workflow{
		Name: "test",
		Steps: []Step{
			{Name: "s", Action: "script"},
		},
	}
	errs := wf.Validate()
	if len(errs) < 2 {
		t.Errorf("expected at least 2 errors (lang + code), got %d", len(errs))
	}
}

func TestInferDependencies_Linear(t *testing.T) {
	wf := &Workflow{
		Name: "linear",
		Steps: []Step{
			{Name: "a", Action: "log", Message: "1"},
			{Name: "b", Action: "log", Message: "2"},
			{Name: "c", Action: "log", Message: "3"},
		},
	}
	if err := wf.InferDependencies(); err != nil {
		t.Fatalf("InferDependencies: %v", err)
	}
	// After inference, b should depend on a, c on b.
	if !containsStr(wf.Steps[1].DependsOn, "a") {
		t.Error("step b should depend on a")
	}
	if !containsStr(wf.Steps[2].DependsOn, "b") {
		t.Error("step c should depend on b")
	}
}

func TestInferDependencies_NextToDependsOn(t *testing.T) {
	wf := &Workflow{
		Name: "dag",
		Steps: []Step{
			{Name: "build", Action: "shell", Command: "echo build", Next: []string{"test"}},
			{Name: "test", Action: "shell", Command: "echo test"},
		},
	}
	if err := wf.InferDependencies(); err != nil {
		t.Fatalf("InferDependencies: %v", err)
	}
	if !containsStr(wf.Steps[1].DependsOn, "build") {
		t.Error("test should depend on build (from Next)")
	}
}

func TestParseOutputMode(t *testing.T) {
	tests := map[string]OutputMode{
		"plain":   OutputModePlain,
		"text":    OutputModePlain,
		"rich":    OutputModeRich,
		"json":    OutputModeJSON,
		"html":    OutputModeHTML,
		"tui":     OutputModeTUI,
		"unknown": OutputModePlain, // fallback
	}
	for in, want := range tests {
		got := ParseOutputMode(in)
		if got != want {
			t.Errorf("ParseOutputMode(%q) = %s, want %s", in, got, want)
		}
	}
}

// helpers
func matchStr(s, substr string) bool {
	return len(s) >= len(substr) && containsStr([]string{s}, substr) || len(s) >= 0 && indexOf(s, substr) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
