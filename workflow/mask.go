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

// MaskStepResultVariables masks sensitive values in a step result tree's
// outputs, in place. vars is the execution variable set: entries whose names
// match sensitivePatterns supply the values to mask. This is called on the
// finalized WorkflowResult so stored snapshots / exports / API responses are
// masked by construction. Real-time log streams during execution are NOT
// masked (masking there would garble live output).
func MaskStepResultVariables(steps []StepResult, sensitivePatterns []string, vars map[string]string) {
	values := sensitiveValues(vars, sensitivePatterns)
	if len(values) == 0 {
		return
	}
	maskStepResultTree(steps, values)
}

// maskStepResultTree applies maskOutputStrings to every step in the tree
// (children, then/else branches).
func maskStepResultTree(steps []StepResult, values []string) {
	for i := range steps {
		maskOutputStrings(&steps[i], values)
		if len(steps[i].Children) > 0 {
			maskStepResultTree(steps[i].Children, values)
		}
		if len(steps[i].ThenChildren) > 0 {
			maskStepResultTree(steps[i].ThenChildren, values)
		}
		if len(steps[i].ElseChildren) > 0 {
			maskStepResultTree(steps[i].ElseChildren, values)
		}
	}
}

// sensitiveValues returns the values of the variables whose names match the
// sensitive patterns, longest first (so a longer secret is masked before any
// shorter value that happens to be a substring of it). Empty and
// single-character values are excluded — replacing such short strings would
// mangle unrelated output.
func sensitiveValues(vars map[string]string, patterns []string) []string {
	var values []string
	for name, v := range vars {
		if len(v) < 2 {
			continue
		}
		if isSensitiveVar(name, patterns) {
			values = append(values, v)
		}
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && len(values[j-1]) < len(values[j]); j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
	return values
}

// maskOutputStrings replaces known sensitive values in a step's output with
// "******". This catches cases where a sensitive variable value leaks into
// shell/AI output. values carries the resolved sensitive variable values (see
// sensitiveValues), already filtered and sorted.
func maskOutputStrings(sr *StepResult, values []string) {
	for _, v := range values {
		if strings.Contains(sr.Output, v) {
			sr.Output = strings.ReplaceAll(sr.Output, v, "******")
		}
	}
}

// MaskWorkflowResult applies variable masking to a WorkflowResult in place.
// Called before JSON serialization for display (API, HTML report).
func MaskWorkflowResult(result *WorkflowResult, sensitivePatterns []string) {
	if len(sensitivePatterns) == 0 || result == nil {
		return
	}
	result.Variables = MaskVariables(result.Variables, sensitivePatterns)
}
