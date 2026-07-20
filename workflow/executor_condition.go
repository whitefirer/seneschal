package workflow

import (
	"fmt"
	exprlib "github.com/expr-lang/expr"
	"strconv"
	"strings"
)

func createSkippedStepResult(s Step) StepResult {
	sr := StepResult{
		Name:        s.Name,
		ID:          s.ID,
		Action:      s.Action,
		Description: s.Description,
		Status:      "skipped",
	}
	if sr.ID == "" && sr.Name != "" {
		sr.ID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(sr.Name, " ", "-")))
	}

	// 如果是 condition，递归处理 then/else 子步骤
	if s.Action == "condition" {
		if len(s.Then) > 0 {
			sr.ThenChildren = make([]StepResult, len(s.Then))
			for i, subStep := range s.Then {
				sr.ThenChildren[i] = createSkippedStepResult(subStep)
			}
		}
		if len(s.Else) > 0 {
			sr.ElseChildren = make([]StepResult, len(s.Else))
			for i, subStep := range s.Else {
				sr.ElseChildren[i] = createSkippedStepResult(subStep)
			}
		}
	}

	return sr
}

func (e *Executor) evaluateExpression(expr string) (bool, error) {
	expression := strings.TrimSpace(expr)

	// Resolve template variables for backward compat ({{.var}} → value)
	resolved, err := e.context.ResolveTemplate(expression)
	if err != nil {
		return false, err
	}
	expression = strings.TrimSpace(resolved)

	// Boolean-like literals
	lower := strings.ToLower(expression)
	if lower == "true" || lower == "1" || lower == "yes" {
		return true, nil
	}
	if lower == "false" || lower == "0" || lower == "no" || lower == "" {
		return false, nil
	}

	// Try expr-lang/expr with context variables as environment
	snapshot := e.context.Snapshot()
	env := make(map[string]interface{}, len(snapshot)+2)
	for k, v := range snapshot {
		env[k] = v
		// Also store numeric version for comparison operators
		if n, nerr := strconv.ParseFloat(v, 64); nerr == nil {
			env[k] = n
		}
	}
	env["contains"] = func(s, substr string) bool { return strings.Contains(s, substr) }

	program, compileErr := exprlib.Compile(expression, exprlib.Env(env))
	if compileErr != nil {
		return evaluateLegacy(expression)
	}

	result, runErr := exprlib.Run(program, env)
	if runErr != nil {
		return evaluateLegacy(expression)
	}

	if b, ok := result.(bool); ok {
		return b, nil
	}

	return false, fmt.Errorf("expression does not evaluate to boolean: %s", expression)
}

func evaluateLegacy(expr string) (bool, error) {
	// contains
	if strings.Contains(expr, " contains ") {
		parts := strings.SplitN(expr, " contains ", 2)
		return strings.Contains(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])), nil
	}
	// ==
	if strings.Contains(expr, " == ") {
		parts := strings.SplitN(expr, " == ", 2)
		return strings.TrimSpace(parts[0]) == strings.TrimSpace(parts[1]), nil
	}
	// !=
	if strings.Contains(expr, " != ") {
		parts := strings.SplitN(expr, " != ", 2)
		return strings.TrimSpace(parts[0]) != strings.TrimSpace(parts[1]), nil
	}
	// >=
	if strings.Contains(expr, " >= ") {
		parts := strings.SplitN(expr, " >= ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), ">=")
	}
	// <=
	if strings.Contains(expr, " <= ") {
		parts := strings.SplitN(expr, " <= ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), "<=")
	}
	// >
	if strings.Contains(expr, " > ") {
		parts := strings.SplitN(expr, " > ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), ">")
	}
	// <
	if strings.Contains(expr, " < ") {
		parts := strings.SplitN(expr, " < ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), "<")
	}

	return expr != "", nil
}
