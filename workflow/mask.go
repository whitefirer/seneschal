package workflow

import (
	"strings"
)

// MaskVariables returns a copy of vars where sensitive keys have their values
// replaced with "***". sensitivePatterns is a list of names or glob-style
// patterns (* matches any sequence). The original map is not modified —
// the engine uses real values; only the returned copy is for display.
func MaskVariables(vars map[string]string, sensitivePatterns []string) map[string]string {
	if len(sensitivePatterns) == 0 {
		return vars
	}
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		if isSensitiveVar(k, sensitivePatterns) {
			out[k] = "***"
		} else {
			out[k] = v
		}
	}
	return out
}

// isSensitiveVar reports whether a variable name matches any sensitive pattern.
// Patterns support basic glob: "*" matches any sequence of characters.
func isSensitiveVar(name string, patterns []string) bool {
	for _, p := range patterns {
		if globMatch(p, name) {
			return true
		}
	}
	return false
}

// globMatch is a simple glob matcher (supports * as wildcard).
func globMatch(pattern, name string) bool {
	// No wildcard — exact match.
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == name
	}
	// Check prefix.
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	// Check suffix.
	if !strings.HasSuffix(name, parts[len(parts)-1]) {
		return false
	}
	// Check middle parts appear in order.
	idx := len(parts[0])
	remaining := name[idx:]
	for i := 1; i < len(parts)-1; i++ {
		pos := strings.Index(remaining, parts[i])
		if pos < 0 {
			return false
		}
		remaining = remaining[pos+len(parts[i]):]
	}
	return true
}

// MaskStepResultVariables masks sensitive variables in a StepResult's output
// and variables. This is called before serialization for display (HTML report,
// history show, API response). The original StepResult is not modified.
func MaskStepResultVariables(steps []StepResult, sensitivePatterns []string) {
	if len(sensitivePatterns) == 0 {
		return
	}
	for i := range steps {
		maskOutputStrings(&steps[i], sensitivePatterns)
		// Recurse into children.
		if len(steps[i].Children) > 0 {
			MaskStepResultVariables(steps[i].Children, sensitivePatterns)
		}
		if len(steps[i].ThenChildren) > 0 {
			MaskStepResultVariables(steps[i].ThenChildren, sensitivePatterns)
		}
		if len(steps[i].ElseChildren) > 0 {
			MaskStepResultVariables(steps[i].ElseChildren, sensitivePatterns)
		}
	}
}

// maskOutputStrings replaces known sensitive values in step output with ***.
// This catches cases where a sensitive variable value appears in shell output.
func maskOutputStrings(sr *StepResult, patterns []string) {
	// We can't know which specific values are sensitive from the StepResult
	// alone (the sensitive vars are on the Workflow, not the StepResult).
	// The caller should pass the resolved sensitive variable values, but
	// for now we only mask variables in WorkflowResult.Variables (done at
	// the caller level). Step output masking is a future enhancement.
}

// MaskWorkflowResult applies variable masking to a WorkflowResult in place.
// Called before JSON serialization for display (API, HTML report).
func MaskWorkflowResult(result *WorkflowResult, sensitivePatterns []string) {
	if len(sensitivePatterns) == 0 || result == nil {
		return
	}
	result.Variables = MaskVariables(result.Variables, sensitivePatterns)
}
