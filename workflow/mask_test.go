package workflow

import "testing"

func TestMaskVariables_Exact(t *testing.T) {
	vars := map[string]string{
		"api_key":   "sk-12345",
		"password":  "secret",
		"normal":    "visible",
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
			"token":  "sk-secret",
			"count":  "3",
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
