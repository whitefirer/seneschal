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

func TestValidate_DuplicateStepNames(t *testing.T) {
	wf := &Workflow{
		Name: "dup",
		Steps: []Step{
			{Name: "a", Action: "log", Message: "1"},
			{Name: "a", Action: "log", Message: "2"},
		},
	}
	errs := wf.Validate()
	found := false
	for _, e := range errs {
		if matchStr(e.Error(), "duplicate step id") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate step id error, got %v", errs)
	}
}

func TestValidate_DuplicateExplicitIDs(t *testing.T) {
	// Different names but the same explicit id collide just the same.
	wf := &Workflow{
		Name: "dup-id",
		Steps: []Step{
			{Name: "a", ID: "shared", Action: "log", Message: "1"},
			{Name: "b", ID: "shared", Action: "log", Message: "2"},
		},
	}
	errs := wf.Validate()
	found := false
	for _, e := range errs {
		if matchStr(e.Error(), "duplicate step id") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate step id error, got %v", errs)
	}
}

func TestValidate_DuplicateStepNamesNested(t *testing.T) {
	// A nested child step colliding with a top-level step (the engine keeps
	// one global name->step map for dependency inference).
	wf := &Workflow{
		Name: "dup-nested",
		Steps: []Step{
			{Name: "a", Action: "log", Message: "top"},
			{Name: "par", Action: "parallel", Steps: []Step{
				{Name: "a", Action: "log", Message: "nested"},
			}},
		},
	}
	errs := wf.Validate()
	found := false
	for _, e := range errs {
		if matchStr(e.Error(), "duplicate step id") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate step id error for nested collision, got %v", errs)
	}
}

func TestValidate_NestedUniqueNamesOK(t *testing.T) {
	wf := &Workflow{
		Name: "nested-ok",
		Steps: []Step{
			{Name: "prep", Action: "log", Message: "p"},
			{Name: "par", Action: "parallel", Steps: []Step{
				{Name: "p1", Action: "log", Message: "1"},
				{Name: "p2", Action: "log", Message: "2"},
			}},
			{Name: "cond", Action: "condition", Expression: "true",
				Then: []Step{{Name: "t1", Action: "log", Message: "t"}},
				Else: []Step{{Name: "e1", Action: "log", Message: "e"}},
			},
			{Name: "each", Action: "foreach", Items: "a,b", Do: []Step{
				{Name: "d1", Action: "log", Message: "d"},
			}},
		},
	}
	errs := wf.Validate()
	for _, e := range errs {
		if matchStr(e.Error(), "duplicate step id") {
			t.Errorf("unexpected duplicate error for unique tree: %v", e)
		}
	}
}
