package workflow

// shouldAnalyzeError reports whether on_error: ai should fire for this step.
func (e *Executor) shouldAnalyzeError(step Step) bool {
	if step.OnError != "" {
		return step.OnError == "ai" || step.OnError == "ai_auto"
	}
	return e.aiOnError == "ai" || e.aiOnError == "ai_auto"
}

// errorAnalysisMode returns "auto" or "suggest" based on the on_error config.
func (e *Executor) errorAnalysisMode(step Step) string {
	mode := step.OnError
	if mode == "" {
		mode = e.aiOnErrorMode
	}
	if mode == "ai_auto" {
		return "auto"
	}
	return "suggest"
}

// stepCommandForError extracts the relevant command for the error prompt.
func (e *Executor) stepCommandForError(step Step) string {
	if spec, ok := LookupAction(step.Action); ok && spec.CommandForError != nil {
		return spec.CommandForError(step)
	}
	return ""
}

// fireStepHooks fires after_step hooks for a completed step. Both step-level
// and workflow-level hooks are checked. The executor must have access to the
// workflow's hooks (stored at Execute time). Returns the merged control-flow
// decision of all fired hooks (ai_auto hooks only; other hook types return no
// decision). When several hooks decide, the highest-priority action wins:
// abort > retry > skip.
func (e *Executor) fireStepHooks(step Step, result StepResult) HookResult {
	event := HookEvent{
		Phase:        HookAfterStep,
		StepName:     step.Name,
		Action:       step.Action,
		Status:       result.Status,
		Output:       result.Output,
		Error:        result.Error,
		Duration:     result.Duration,
		WorkflowName: e.workflowName,
		Variables:    e.context.Snapshot(),
	}
	decision := HookResult{}
	// Step-level hooks.
	for _, hook := range step.Hooks {
		decision = mergeHookDecision(decision, executeHook(hook, event, e))
	}
	// Workflow-level hooks (inherited).
	for _, hook := range e.workflowHooks {
		if hook.On == HookAfterStep {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	// Server-level global hooks.
	for _, hook := range e.globalHooks {
		if hook.On == HookAfterStep {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	return decision
}

// mergeHookDecision picks the higher-priority of two hook decisions.
func mergeHookDecision(cur, next HookResult) HookResult {
	if hookActionPriority(next.Action) > hookActionPriority(cur.Action) {
		return next
	}
	return cur
}

// hookActionPriority ranks control-flow actions: abort > retry > skip; ""
// (no effect) and "suggest" (suggest-only mode) never affect control flow.
func hookActionPriority(action string) int {
	switch action {
	case "abort":
		return 3
	case "retry":
		return 2
	case "skip":
		return 1
	}
	return 0
}

// fireWorkflowHooks fires workflow_end hooks after the entire workflow
// completes. Returns the merged control-flow decision; only "abort" is acted
// upon by the caller.
func (e *Executor) fireWorkflowHooks(wf *Workflow, result *WorkflowResult) HookResult {
	event := HookEvent{
		Phase:        HookWorkflowEnd,
		WorkflowName: wf.Name,
		Status:       result.Status,
		Error:        result.Error,
		Output:       result.Error, // best context for end hooks
		Variables:    result.Variables,
	}
	decision := HookResult{}
	for _, hook := range wf.Hooks {
		if hook.On == HookWorkflowEnd {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	// Server-level global hooks.
	for _, hook := range e.globalHooks {
		if hook.On == HookWorkflowEnd {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	return decision
}
