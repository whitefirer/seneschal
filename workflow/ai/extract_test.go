package ai

import (
	"encoding/json"
	"testing"
)

func TestExtractYAML_NoFence(t *testing.T) {
	got := extractYAML("name: test\nsteps: []")
	if got != "name: test\nsteps: []" {
		t.Errorf("got %q", got)
	}
}

func TestExtractYAML_WithFence(t *testing.T) {
	input := "Here is the YAML:\n```yaml\nname: test\n```\nDone."
	got := extractYAML(input)
	if got != "name: test" {
		t.Errorf("got %q want 'name: test'", got)
	}
}

func TestExtractYAML_FenceNoLang(t *testing.T) {
	input := "```\nname: test\n```"
	got := extractYAML(input)
	if got != "name: test" {
		t.Errorf("got %q", got)
	}
}

func TestExtractJSON_NoFence(t *testing.T) {
	got := extractJSON(`{"workflow": "test", "confidence": 0.9}`)
	if got != `{"workflow": "test", "confidence": 0.9}` {
		t.Errorf("got %q", got)
	}
}

func TestExtractJSON_WithFence(t *testing.T) {
	input := "```json\n{\"workflow\": \"x\"}\n```"
	got := extractJSON(input)
	if got != `{"workflow": "x"}` {
		t.Errorf("got %q", got)
	}
}

func TestExtractJSON_SurroundedByProse(t *testing.T) {
	input := `Sure! Here you go: {"workflow": "x"} hope that helps.`
	got := extractJSON(input)
	if got != `{"workflow": "x"}` {
		t.Errorf("got %q", got)
	}
}

func TestExtractJSON_Empty(t *testing.T) {
	got := extractJSON("")
	if got != "" {
		t.Errorf("got %q want empty", got)
	}
}

func TestParseSelection_Basic(t *testing.T) {
	raw := `{"workflow":"deploy","variables":{"env":"prod"},"confidence":0.95}`
	var sel parseSelection
	if err := jsonUnmarshal(raw, &sel); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sel.Workflow != "deploy" {
		t.Errorf("workflow=%q", sel.Workflow)
	}
	if sel.Variables["env"] != "prod" {
		t.Errorf("env=%q", sel.Variables["env"])
	}
	if sel.Confidence != 0.95 {
		t.Errorf("confidence=%v", sel.Confidence)
	}
}

func TestParseSelection_Empty(t *testing.T) {
	raw := `{"workflow":"","variables":{},"confidence":0}`
	var sel parseSelection
	if err := jsonUnmarshal(raw, &sel); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sel.Workflow != "" {
		t.Errorf("workflow=%q want empty", sel.Workflow)
	}
}

// jsonUnmarshal wraps json.Unmarshal for test use.
func jsonUnmarshal(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}
