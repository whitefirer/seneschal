package workflow

import (
	"strings"
	"testing"
)

func TestMaskVariables_Exact(t *testing.T) {
	vars := map[string]string{
		"api_key":  "sk-12345",
		"password": "secret",
		"normal":   "visible",
	}
	masked := MaskVariables(vars, []string{"api_key", "password"})
	if masked["api_key"] != "***" {
		t.Errorf("api_key=%q want ***", masked["api_key"])
	}
	if masked["password"] != "***" {
		t.Errorf("password=%q want ***", masked["password"])
	}
	if masked["normal"] != "visible" {
		t.Errorf("normal=%q want visible", masked["normal"])
	}
}

func TestMaskVariables_Glob(t *testing.T) {
	vars := map[string]string{
		"db_password": "secret",
		"api_key":     "sk-xxx",
		"db_host":     "localhost",
		"user_secret": "hidden",
	}
	masked := MaskVariables(vars, []string{"*_password", "*_key", "*secret*"})
	if masked["db_password"] != "***" {
		t.Errorf("db_password=%q", masked["db_password"])
	}
	if masked["api_key"] != "***" {
		t.Errorf("api_key=%q", masked["api_key"])
	}
	if masked["db_host"] != "localhost" {
		t.Errorf("db_host=%q want localhost", masked["db_host"])
	}
	if masked["user_secret"] != "***" {
		t.Errorf("user_secret=%q want ***", masked["user_secret"])
	}
}

func TestMaskVariables_NoPatterns(t *testing.T) {
	vars := map[string]string{"key": "val"}
	masked := MaskVariables(vars, nil)
	if masked["key"] != "val" {
		t.Errorf("key=%q want val", masked["key"])
	}
}

func TestMaskVariables_OriginalUnchanged(t *testing.T) {
	vars := map[string]string{"api_key": "sk-123"}
	_ = MaskVariables(vars, []string{"api_key"})
	if vars["api_key"] != "sk-123" {
		t.Error("original map was modified")
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"api_key", "api_key", true},
		{"api_key", "other", false},
		{"*_key", "api_key", true},
		{"*_key", "password", false},
		{"*secret*", "db_secret_token", true},
		{"*secret*", "normal", false},
		{"db_*", "db_host", true},
		{"db_*", "api_host", false},
		{"*", "anything", true},
		{"prefix*suffix", "prefixXYZsuffix", true},
		{"prefix*suffix", "prefixXYZ", false},
	}
	for _, tt := range tests {
		got := globMatch(tt.pattern, tt.name)
		if got != tt.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestMaskWorkflowResult(t *testing.T) {
	result := &WorkflowResult{
		Variables: map[string]string{
			"token": "sk-secret",
			"count": "3",
		},
		SensitivePatterns: []string{"token"},
	}
	MaskWorkflowResult(result, result.SensitivePatterns)
	if result.Variables["token"] != "***" {
		t.Errorf("token=%q want ***", result.Variables["token"])
	}
	if result.Variables["count"] != "3" {
		t.Errorf("count=%q want 3", result.Variables["count"])
	}
}

func TestMaskStepResultVariables_OutputMasked(t *testing.T) {
	vars := map[string]string{
		"token": "sk-secret-123",
		"other": "visible",
	}
	steps := []StepResult{
		{Name: "a", Output: "fetched with sk-secret-123 ok"},
	}
	MaskStepResultVariables(steps, []string{"token"}, vars)
	if strings.Contains(steps[0].Output, "sk-secret-123") {
		t.Errorf("output not masked: %q", steps[0].Output)
	}
	if !strings.Contains(steps[0].Output, "******") {
		t.Errorf("output %q should contain ******", steps[0].Output)
	}
	if !strings.Contains(steps[0].Output, "fetched with") {
		t.Errorf("output %q should keep non-sensitive content", steps[0].Output)
	}
}

func TestMaskStepResultVariables_NestedTree(t *testing.T) {
	vars := map[string]string{"token": "sk-nested-secret"}
	steps := []StepResult{
		{Name: "par", Children: []StepResult{
			{Name: "c", Output: "child sk-nested-secret leak"},
		}},
		{Name: "cond", ThenChildren: []StepResult{
			{Name: "t", Output: "then sk-nested-secret"},
		}, ElseChildren: []StepResult{
			{Name: "e", Output: "else sk-nested-secret"},
		}},
	}
	MaskStepResultVariables(steps, []string{"token"}, vars)
	if got := steps[0].Children[0].Output; strings.Contains(got, "sk-nested-secret") {
		t.Errorf("child output not masked: %q", got)
	}
	if got := steps[1].ThenChildren[0].Output; strings.Contains(got, "sk-nested-secret") {
		t.Errorf("then-child output not masked: %q", got)
	}
	if got := steps[1].ElseChildren[0].Output; strings.Contains(got, "sk-nested-secret") {
		t.Errorf("else-child output not masked: %q", got)
	}
}

func TestMaskStepResultVariables_GlobPattern(t *testing.T) {
	vars := map[string]string{
		"api_key": "ABC123XYZ",
		"note":    "unrelated",
	}
	steps := []StepResult{
		{Name: "a", Output: "key was ABC123XYZ done"},
	}
	MaskStepResultVariables(steps, []string{"*_key"}, vars)
	if strings.Contains(steps[0].Output, "ABC123XYZ") {
		t.Errorf("output not masked by glob pattern: %q", steps[0].Output)
	}
}

func TestMaskStepResultVariables_ShortAndEmptyValuesSkipped(t *testing.T) {
	vars := map[string]string{
		"token": "x",  // too short — masking it would mangle normal output
		"pwd":   "",   // empty
		"ok":    "ab", // length 2 is fine
	}
	steps := []StepResult{
		{Name: "a", Output: "x marks the spot, ab stays masked"},
	}
	MaskStepResultVariables(steps, []string{"token", "pwd", "ok"}, vars)
	if !strings.Contains(steps[0].Output, "x marks") {
		t.Errorf("single-char value should not be masked: %q", steps[0].Output)
	}
	if strings.Contains(steps[0].Output, "ab stays") {
		t.Errorf("2-char value should be masked: %q", steps[0].Output)
	}
}

func TestMaskStepResultVariables_NoPatterns(t *testing.T) {
	steps := []StepResult{{Name: "a", Output: "plain"}}
	MaskStepResultVariables(steps, nil, map[string]string{"k": "v"})
	if steps[0].Output != "plain" {
		t.Errorf("output changed with no patterns: %q", steps[0].Output)
	}
}

// TestExecute_MasksSensitiveOutput is the end-to-end check: a workflow with a
// sensitive variable whose value leaks into a step's output must have the
// finalized result masked (store/export/API snapshots are then safe), while
// the live execution context keeps the real value.
func TestExecute_MasksSensitiveOutput(t *testing.T) {
	e := NewExecutor(map[string]string{"token": "sk-live-secret"})
	wf := &Workflow{
		Name:      "mask-e2e",
		Sensitive: []string{"token"},
		Steps: []Step{
			{Name: "leak", Action: "shell", Command: "echo value is {{.token}}"},
		},
	}
	result := e.Execute(wf)
	if result.Status != "success" {
		t.Fatalf("status=%s err=%s", result.Status, result.Error)
	}
	if strings.Contains(result.Steps[0].Output, "sk-live-secret") {
		t.Errorf("finalized output not masked: %q", result.Steps[0].Output)
	}
	if !strings.Contains(result.Steps[0].Output, "******") {
		t.Errorf("finalized output %q should contain ******", result.Steps[0].Output)
	}
	// The live context keeps the real value (execution is not affected).
	if got := e.GetContext().Get("token"); got != "sk-live-secret" {
		t.Errorf("context token=%q, want real value", got)
	}
	// Result variables keep real values at this layer: they are masked at
	// display time by MaskWorkflowResult, and stored snapshots must keep them
	// so replay can restore the original variables.
	if got := result.Variables["token"]; got != "sk-live-secret" {
		t.Errorf("result variable token=%q, want real value preserved", got)
	}
}
