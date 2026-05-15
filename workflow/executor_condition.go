package workflow

import (
	"fmt"
	"strconv"
	"strings"
	exprlib "github.com/expr-lang/expr"
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

func (e *Executor) execCondition(step Step, depth int, result *WorkflowResult) (string, []StepResult, []StepResult, bool, error) {
	expr, err := e.context.ResolveTemplate(step.Expression)
	if err != nil {
		return "", nil, nil, false, fmt.Errorf("resolve expression: %w", err)
	}

	// Simple expression evaluation
	// Supports: variable comparisons, string contains, empty checks
	// Syntax: "{{.var}} == value", "{{.var}} != value", "{{.var}} contains value"
	// Also supports: "var1 == var2" (resolves both sides)
	evalResult, err := e.evaluateExpression(expr)
	if err != nil {
		return "", nil, nil, false, fmt.Errorf("evaluate condition: %w", err)
	}

	// Print condition with pretty output
	if e.richPrinter != nil {
		e.richPrinter.PrintCondition(expr, evalResult)
	} else if e.printer != nil {
		e.printer.PrintCondition(expr, evalResult)
	}

	// Determine which branch to execute
	var execSteps []Step
	var skippedSteps []Step
	if evalResult {
		execSteps = step.Then
		skippedSteps = step.Else
	} else {
		execSteps = step.Else
		skippedSteps = step.Then
	}

	// Execute the selected branch
	var outputs []string
	var execChildren []StepResult
	for _, s := range execSteps {
		sr := e.executeStep(s, depth+1, result)
		if sr.Output != "" {
			outputs = append(outputs, sr.Output)
		}
		execChildren = append(execChildren, sr)
		if sr.Status == "failed" && !s.ContinueOnError {
			return strings.Join(outputs, "\n"), execChildren, nil, evalResult, fmt.Errorf("sub-step '%s' failed: %s", s.Name, sr.Error)
		}
	}

	// Create skipped branch results (pending status, not executed)
	// 使用递归函数处理嵌套 condition
	var skippedChildren []StepResult
	for _, s := range skippedSteps {
		skippedChildren = append(skippedChildren, createSkippedStepResult(s))
	}

	return strings.Join(outputs, "\n"), execChildren, skippedChildren, evalResult, nil
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

