package ai

import "testing"

func TestAgentTools_ContainsExpectedTools(t *testing.T) {
	tools := AgentTools()
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.Description == "" || len(tool.InputSchema) == 0 {
			t.Errorf("tool %s should have description and input schema", tool.Name)
		}
	}
	for _, want := range []string{"list_workflows", "select_workflow", "generate_workflow", "modify_workflow", "explain_workflow", "validate_workflow", "run_workflow"} {
		if !names[want] {
			t.Errorf("missing tool %s in %v", want, names)
		}
	}
}
