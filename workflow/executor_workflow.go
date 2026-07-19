package workflow

import (
	"fmt"
	"path/filepath"
)

// execWorkflow runs another workflow YAML file as a sub-workflow. Variables
// are passed in (isolated scope — the sub-workflow doesn't see the parent's
// variables unless explicitly passed). The sub-workflow's status becomes the
// step output. If save_output is set, the sub-workflow's final variables JSON
// is stored.
//
// This enables composition: a parent workflow orchestrates multiple
// sub-workflows via DAG dependencies, parallel, or sequential steps.
func (e *Executor) execWorkflow(step Step) (string, []StepResult, error) {
	if step.Source == "" && step.Command == "" {
		// Source is reused for the file path; also accept "file:" in Command for ergonomics.
		return "", nil, fmt.Errorf("workflow step '%s': requires 'source' (path to sub-workflow YAML)", step.Name)
	}
	filePath := step.Source
	if filePath == "" {
		filePath = step.Command // fallback
	}

	// Resolve template in path.
	resolvedPath, err := e.context.ResolveTemplate(filePath)
	if err != nil {
		return "", nil, fmt.Errorf("workflow step '%s': resolve path: %w", step.Name, err)
	}

	// Resolve relative paths against the parent workflow's directory.
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(e.workflowDir), resolvedPath)
	}

	// Parse the sub-workflow.
	subWf, err := ParseFile(resolvedPath)
	if err != nil {
		return "", nil, fmt.Errorf("workflow step '%s': parse %s: %w", step.Name, resolvedPath, err)
	}

	// Build isolated variables: start with sub-workflow's own defaults, then
	// overlay the variables passed from the parent step (resolved templates).
	subVars := make(map[string]string)
	for k, v := range subWf.Variables {
		subVars[k] = v
	}
	for k, v := range step.Env {
		resolved, err := e.context.ResolveTemplate(v)
		if err != nil {
			return "", nil, fmt.Errorf("workflow step '%s': resolve variable %s: %w", step.Name, k, err)
		}
		subVars[k] = resolved
	}
	// Also pass through any explicitly declared variables.
	for _, varName := range step.Inputs {
		if val := e.context.Get(varName); val != "" {
			subVars[varName] = val
		}
	}

	// Execute the sub-workflow with a fresh executor (isolated context, but
	// shares the AI provider so model config works).
	subExecutor := NewExecutor(subVars)
	// Propagate the cancellation context so quitting the TUI (or another
	// abort) also stops the sub-workflow's in-flight steps.
	if e.execCtx != nil {
		subExecutor.execCtx = e.execCtx
	}
	if e.aiProvider != nil {
		subExecutor.SetAIProvider(e.aiProvider)
	}
	subExecutor.aiModel = e.aiModel
	subExecutor.aiMaxTokens = e.aiMaxTokens
	subExecutor.aiTemperature = e.aiTemperature
	subExecutor.aiBudget = e.aiBudget
	subExecutor.aiMemoryWindow = e.aiMemoryWindow
	subExecutor.SetVerbose(e.verbose)
	subExecutor.SetDryRun(e.dryRun)

	subResult := subExecutor.Execute(subWf)

	// Map sub-workflow result to step output.
	output := fmt.Sprintf("sub-workflow '%s': %s (%d steps)", subWf.Name, subResult.Status, len(subResult.Steps))
	if subResult.Error != "" {
		output += "\n" + subResult.Error
	}

	// If save_output is set, also store resolved variables from the sub-workflow.
	if step.SaveOutput != "" {
		// Store the full sub-workflow status as the output variable.
		e.context.Set(step.SaveOutput, subResult.Status)
		// Also expand each sub-workflow variable as prefix.key.
		for k, v := range subResult.Variables {
			e.context.Set(step.SaveOutput+"."+k, v)
		}
	}
	e.context.SetResult(step.Name, subResult.Status)

	// If sub-workflow failed, propagate error.
	if subResult.Status == "failed" {
		return output, subResult.Steps, fmt.Errorf("sub-workflow '%s' failed: %s", subWf.Name, subResult.Error)
	}

	return output, subResult.Steps, nil
}

// workflowDir is set by Execute to the directory of the workflow file being
// run, so sub-workflow relative paths resolve correctly.
