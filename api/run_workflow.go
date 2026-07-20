package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/workflow"
)

// workflowRun carries the per-run context shared across the run phases
// (parse → register → start → reconcile), so each phase stays a small,
// single-purpose function.
type workflowRun struct {
	name        string // route name (may carry the .yaml/.yml suffix)
	path        string // resolved absolute workflow file path
	wf          *workflow.Workflow
	dryRun      bool
	executionID string
	vars        map[string]string
}

// RunWorkflow starts a workflow execution in the background and returns the
// execution ID immediately. Phases: parse/validate the request, register the
// in-memory execution record, then start execution (progress wiring +
// goroutine, which reconciles the record when the run finishes).
func (h *Handler) RunWorkflow(w http.ResponseWriter, r *http.Request) {
	run, ok := h.parseRunRequest(w, r)
	if !ok {
		return // error response already written
	}
	h.registerExecution(run)
	h.startExecution(run)
	writeJSON(w, http.StatusOK, success(map[string]string{
		"executionId": run.executionID,
		"status":      "started",
	}))
}

// parseRunRequest validates the request (method, body size, path, workflow
// file) and derives the per-run context. On failure it writes the error
// response and returns ok == false.
func (h *Handler) parseRunRequest(w http.ResponseWriter, r *http.Request) (run *workflowRun, ok bool) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return nil, false
	}

	name := mux.Vars(r)["name"]

	// Parse request body
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req RunRequest
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResp("Request body too large"))
			return nil, false
		}
		json.Unmarshal(body, &req)
	}

	path, _, err := h.safePath(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return nil, false
	}

	wf, err := workflow.ParseFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResp("Workflow not found: "+err.Error()))
		return nil, false
	}

	// Generate execution ID
	executionID := fmt.Sprintf("exec-%s-%s", time.Now().Format("20060102-150405"), randomHex(4))

	// Merge variables
	vars := make(map[string]string)
	for k, v := range wf.Variables {
		vars[k] = v
	}
	for k, v := range req.Variables {
		vars[k] = v
	}

	return &workflowRun{
		name:        name,
		path:        path,
		wf:          wf,
		dryRun:      req.DryRun,
		executionID: executionID,
		vars:        vars,
	}, true
}

// registerExecution pre-populates the step tree (including nested children)
// and installs the in-memory execution record, evicting the oldest entry
// first when the cache is full.
func (h *Handler) registerExecution(run *workflowRun) {
	steps := buildSteps(run.wf.Steps, "")

	exec := &ExecutionDetail{
		ExecutionRecord: ExecutionRecord{
			ID:           run.executionID,
			WorkflowName: run.wf.Name,
			WorkflowFile: strings.TrimSuffix(strings.TrimSuffix(run.name, ".yaml"), ".yml"),
			Status:       "running",
			StartTime:    time.Now().Format(time.RFC3339),
			StepsCount:   len(run.wf.Steps),
		},
		Logs:     []LogEntry{},
		Steps:    steps,
		Workflow: run.wf.Name,
	}

	h.execMu.Lock()
	h.evictOldest()
	h.executions[run.executionID] = exec
	h.execMu.Unlock()
}

// startExecution builds the executor, wires the progress callback (WebSocket
// broadcast + in-memory log/step sync), and runs the workflow in a goroutine
// that reconciles the execution record when the run finishes.
func (h *Handler) startExecution(run *workflowRun) {
	// Create executor
	executor := workflow.NewExecutor(run.vars)
	executor.SetDryRun(run.dryRun)
	if len(h.globalHooks) > 0 {
		executor.SetGlobalHooks(h.globalHooks)
	}

	// Setup progress callback
	executor.OnProgress = func(event workflow.ProgressEvent) {
		h.onRunProgress(run, event)
	}

	// Execute in goroutine
	go func() {
		result := executor.Execute(run.wf)
		h.reconcileExecution(run, result)
	}()
}

// onRunProgress handles one executor progress event: broadcast it to
// WebSocket clients (with a formatted log message), then append to the
// in-memory logs and sync the pre-populated step tree under execMu.
func (h *Handler) onRunProgress(run *workflowRun, event workflow.ProgressEvent) {
	// Generate StepID if not present (matches buildSteps logic)
	stepID := event.StepId
	if stepID == "" && event.Name != "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(event.Name, " ", "-")))
	}

	// 格式化日志消息
	var logMessage string
	var logLevel string
	switch event.Type {
	case "workflow_start":
		logMessage = fmt.Sprintf("🚀 Starting workflow: %s", run.wf.Name)
		logLevel = "INFO"
	case "workflow_end":
		if event.Status == "success" {
			logMessage = fmt.Sprintf("✅ Workflow completed successfully")
			logLevel = "INFO"
		} else {
			logMessage = fmt.Sprintf("❌ Workflow failed: %s", event.Error)
			logLevel = "ERROR"
		}
	case "step_start":
		logMessage = fmt.Sprintf("▶ Starting: %s", event.Name)
		logLevel = "INFO"
	case "step_complete":
		if event.Status == "failed" {
			logMessage = fmt.Sprintf("✗ Failed: %s", event.Name)
			if event.Error != "" {
				logMessage += fmt.Sprintf(" - %s", event.Error)
			}
			logLevel = "ERROR"
		} else {
			logMessage = fmt.Sprintf("✓ Completed: %s", event.Name)
			if event.Duration != "" {
				logMessage += fmt.Sprintf(" (%s)", event.Duration)
			}
			logLevel = "INFO"
		}
	case "step_output":
		logMessage = event.Output
		logLevel = "INFO"
	}

	// Broadcast to WebSocket clients (包含日志消息)
	h.hub.Broadcast(WSProgressEvent{
		Type:            event.Type,
		ExecutionID:     run.executionID,
		WorkflowName:    run.wf.Name,
		WorkflowFile:    strings.TrimSuffix(strings.TrimSuffix(run.name, ".yaml"), ".yml"),
		StepID:          stepID,
		StepName:        event.Name,
		Action:          event.Action,
		Status:          event.Status,
		Output:          event.Output,
		Error:           event.Error,
		Duration:        event.Duration,
		Timestamp:       event.Time,
		LogMessage:      logMessage,
		LogLevel:        logLevel,
		ConditionResult: event.ConditionResult,
	})
	// Note: "ai_token" events (streaming LLM tokens) are forwarded above as
	// a generic WSProgressEvent so the frontend can render them incrementally
	// (Phase 5). They are intentionally NOT appended to e.Logs below — per-
	// token log entries would flood the log; the final step_output carries
	// the complete text.

	// Store logs and update step status in real-time
	h.execMu.Lock()
	defer h.execMu.Unlock()
	if e, ok := h.executions[run.executionID]; ok {
		// Log meaningful events with messages
		switch event.Type {
		case "step_start":
			e.Logs = append(e.Logs, LogEntry{
				Timestamp: event.Time,
				Level:     "info",
				Message:   fmt.Sprintf("▶ Starting: %s", event.Name),
				Step:      event.Name,
				StepID:    event.StepId,
			})
		case "step_complete":
			msg := fmt.Sprintf("✓ Completed: %s", event.Name)
			if event.Duration != "" {
				msg += fmt.Sprintf(" (%s)", event.Duration)
			}
			if event.Status == "failed" {
				msg = fmt.Sprintf("✗ Failed: %s", event.Name)
				if event.Error != "" {
					msg += fmt.Sprintf(" - %s", event.Error)
				}
			}
			var logLevel string
			if event.Status == "failed" {
				logLevel = "error"
			} else {
				logLevel = "info"
			}
			e.Logs = append(e.Logs, LogEntry{
				Timestamp: event.Time,
				Level:     logLevel,
				Message:   msg,
				Step:      event.Name,
				StepID:    event.StepId,
			})
		case "step_output":
			if event.Output != "" {
				e.Logs = append(e.Logs, LogEntry{
					Timestamp: event.Time,
					Level:     "info",
					Message:   event.Output,
					Step:      event.Name,
					StepID:    event.StepId,
				})
			}
		case "workflow_end":
			var msg string
			var logLevel string
			if event.Status == "success" {
				msg = "✅ Workflow completed successfully"
				logLevel = "info"
			} else {
				msg = fmt.Sprintf("❌ Workflow failed: %s", event.Error)
				logLevel = "error"
			}
			e.Logs = append(e.Logs, LogEntry{
				Timestamp: event.Time,
				Level:     logLevel,
				Message:   msg,
			})
			e.Status = event.Status
		case "workflow_start":
			e.Logs = append(e.Logs, LogEntry{
				Timestamp: event.Time,
				Level:     "info",
				Message:   fmt.Sprintf("🚀 Starting workflow: %s", run.wf.Name),
			})
		}

		// Update step status in real-time
		updateStepStatus(e.Steps, stepID, event)
	}
}

// reconcileExecution runs when the workflow finishes (still in the execution
// goroutine): write the final status/variables back into the in-memory
// record, fold the result tree into the pre-populated step tree, send the
// final WebSocket event, and persist a snapshot. Holds execMu throughout so
// readers never observe a half-updated record.
func (h *Handler) reconcileExecution(run *workflowRun, result *workflow.WorkflowResult) {
	h.execMu.Lock()
	defer h.execMu.Unlock()
	if e, ok := h.executions[run.executionID]; ok {
		e.Status = result.Status
		e.EndTime = result.EndTime
		e.Error = result.Error

		// Keep the resolved variables + sensitive patterns on the detail
		// so GET responses can serve them masked at serialization time.
		// Real values stay in memory and in the store snapshot (replay
		// needs them); only the response is masked.
		e.Variables = result.Variables
		e.SensitivePatterns = result.SensitivePatterns

		apiSteps := result.Steps

		// Build a map of all results for quick lookup
		resultMap := make(map[string]workflow.StepResult)
		buildResultMap(apiSteps, resultMap)

		// Now recursively update existing tree using the result map
		updateTree(e.Steps, run.wf.Steps, resultMap)

		// Calculate duration
		if start, err := time.Parse(time.RFC3339, e.StartTime); err == nil {
			if end, err := time.Parse(time.RFC3339, e.EndTime); err == nil {
				e.Duration = end.Sub(start).String()
			}
		}
	}

	// Send final event
	h.hub.Broadcast(WSProgressEvent{
		Type:         "workflow_end",
		ExecutionID:  run.executionID,
		WorkflowName: run.wf.Name,
		WorkflowFile: strings.TrimSuffix(strings.TrimSuffix(run.name, ".yaml"), ".yml"),
		Status:       result.Status,
		Error:        result.Error,
		Timestamp:    result.EndTime,
	})

	// Persist the completed execution so it survives restarts and can be
	// replayed later. Done under execMu (still held via defer above).
	if h.store != nil {
		// Reuse the in-memory detail (already duration-computed) when
		// available; otherwise derive from the result.
		dur := ""
		if e, ok := h.executions[run.executionID]; ok {
			dur = e.Duration
		}
		snap := workflow.ExecutionSnapshot{
			ExecutionSummary: workflow.ExecutionSummary{
				ID:               run.executionID,
				WorkflowName:     run.wf.Name,
				WorkflowFile:     strings.TrimSuffix(strings.TrimSuffix(run.name, ".yaml"), ".yml"),
				Status:           result.Status,
				StartTime:        result.StartTime,
				EndTime:          result.EndTime,
				Duration:         dur,
				Error:            result.Error,
				StepsCount:       len(run.wf.Steps),
				Nondeterministic: result.Nondeterministic,
			},
			Steps:     result.Steps,
			Variables: result.Variables,
		}
		// Store the original workflow YAML so replays can rebuild the exact
		// definition even if the file has changed since.
		if raw, rerr := os.ReadFile(run.path); rerr == nil {
			snap.Workflow = string(raw)
		}
		// Persist best-effort: a save failure should not fail the request,
		// which has already returned. Log and move on.
		_ = h.store.Save(snap)
	}
}

// buildSteps pre-builds the step-result tree from the workflow definition
// (status pending), including nested container children, so the UI has the
// full structure before any events arrive.
func buildSteps(steps []workflow.Step, parentId string) []workflow.StepResult {
	result := make([]workflow.StepResult, 0, len(steps))
	for i, s := range steps {
		// Generate ID if not present
		stepID := s.ID
		if stepID == "" {
			if s.Name != "" {
				stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")))
			} else {
				stepID = fmt.Sprintf("%s-child-%d", parentId, i)
			}
		}

		step := workflow.StepResult{
			ID:          stepID,
			Name:        s.Name,
			Description: s.Description,
			Action:      s.Action,
			Status:      "pending",
			Next:        s.Next,
			DependsOn:   s.DependsOn,
			JoinMode:    s.JoinMode,
			Expression:  s.Expression, // condition expression
			// Sleep
			SleepDuration: s.Duration,
			// Shell
			ShellCommand: s.Command,
			// HTTP
			HTTPUrl:    s.URL,
			HTTPMethod: s.Method,
			// Log
			LogMessage: s.Message,
		}
		// Recursively build children for parallel (uses Steps)
		childSteps := s.Steps
		if len(childSteps) > 0 {
			step.Children = buildSteps(childSteps, stepID)
		}
		// For foreach/loop, pre-build template child from Do (to show structure before execution)
		if (s.Action == "foreach" || s.Action == "loop") && len(s.Do) > 0 {
			// Build one template child to show the loop body structure
			templateChild := buildSteps(s.Do, stepID+"-template")
			if len(templateChild) > 0 {
				templateChild[0].ID = stepID + "-template"
				templateChild[0].Name = "__pending_loop__" // Special marker for frontend translation
				templateChild[0].Status = "pending"
				step.Children = templateChild
			}
		}
		// For condition: build then_children and else_children separately
		if s.Action == "condition" {
			if len(s.Then) > 0 {
				step.ThenChildren = buildSteps(s.Then, stepID+"-then")
			}
			if len(s.Else) > 0 {
				step.ElseChildren = buildSteps(s.Else, stepID+"-else")
			}
		}
		result = append(result, step)
	}
	return result
}

// updateStepStatus applies one progress event to the pre-populated step tree
// (recursive; returns true when the event matched a node).
func updateStepStatus(steps []workflow.StepResult, stepID string, event workflow.ProgressEvent) bool {
	for i := range steps {
		// 先检查当前节点是否匹配（父节点优先）
		if steps[i].ID == stepID || steps[i].Name == event.Name {
			if event.Type == "step_start" {
				steps[i].Status = "running"
				steps[i].StartTime = event.Time
			} else if event.Type == "step_complete" {
				steps[i].Status = event.Status
				steps[i].EndTime = event.Time
				steps[i].Duration = event.Duration
			} else if event.Type == "step_output" {
				// 累加输出到 step 的 Output 字段
				if event.Output != "" {
					if steps[i].Output != "" {
						steps[i].Output += "\n" + event.Output
					} else {
						steps[i].Output = event.Output
					}
				}
			}
			return true
		}
		// 然后检查子节点
		if len(steps[i].Children) > 0 {
			if updateStepStatus(steps[i].Children, stepID, event) {
				// Update parent status based on children
				allDone := true
				allSuccess := true
				for _, child := range steps[i].Children {
					if child.Status != "success" && child.Status != "failed" && child.Status != "skipped" {
						allDone = false
					}
					if child.Status != "success" {
						allSuccess = false
					}
				}
				if allDone {
					if allSuccess {
						steps[i].Status = "success"
					} else {
						steps[i].Status = "failed"
					}
				}
				return true
			}
			// For foreach/loop, dynamically add new child for iterations
			// Replace template child with first actual iteration
			if steps[i].Action == "foreach" || steps[i].Action == "loop" {
				if event.Type == "step_start" {
					// Check if template child exists and should be replaced
					hasTemplate := len(steps[i].Children) == 1 &&
						strings.HasSuffix(steps[i].Children[0].ID, "-template")

					newChild := workflow.StepResult{
						ID:        stepID,
						Name:      event.Name,
						Action:    event.Action,
						Status:    "running",
						StartTime: event.Time,
					}

					if hasTemplate {
						// Replace template with first iteration
						steps[i].Children = []workflow.StepResult{newChild}
					} else {
						// Append subsequent iterations
						steps[i].Children = append(steps[i].Children, newChild)
					}
					steps[i].Status = "running"
					return true
				}
			}
			// For parallel: check if this event matches one of its pre-defined children
			if steps[i].Action == "parallel" && len(steps[i].Children) > 0 {
				// Update parent status when any child starts/completes
				if event.Type == "step_start" {
					steps[i].Status = "running"
				}
			}
		}
		// Condition branch children (then_children / else_children)
		if steps[i].Action == "condition" {
			if len(steps[i].ThenChildren) > 0 {
				if updateStepStatus(steps[i].ThenChildren, stepID, event) {
					steps[i].Status = "running"
				}
			}
			if len(steps[i].ElseChildren) > 0 {
				if updateStepStatus(steps[i].ElseChildren, stepID, event) {
					steps[i].Status = "running"
				}
			}
		}
	}
	return false
}

// findStepDef recursively searches the step definition for a node by ID or
// name (any nesting depth).
func findStepDef(steps []workflow.Step, key string) *workflow.Step {
	for i := range steps {
		s := &steps[i]
		sID := s.ID
		if sID == "" && s.Name != "" {
			sID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")))
		}
		if sID == key || s.Name == key {
			return s
		}
		for _, children := range [][]workflow.Step{s.Steps, s.Do, s.Then, s.Else} {
			if d := findStepDef(children, key); d != nil {
				return d
			}
		}
	}
	return nil
}

// updateStep folds one executed step result into its pre-populated tree node
// (preserving fields the definition pre-filled, e.g. action).
func updateStep(ex *workflow.StepResult, sr workflow.StepResult, wfSteps []workflow.Step) {
	// Store original ID for lookup before overwriting
	lookupKey := ex.ID
	if lookupKey == "" {
		lookupKey = ex.Name
	}

	if sr.ID != "" {
		ex.ID = sr.ID // Update ID (important for foreach iterations)
	}
	ex.Status = sr.Status
	ex.StartTime = sr.StartTime
	ex.EndTime = sr.EndTime
	ex.Output = sr.Output
	ex.Error = sr.Error
	ex.Duration = sr.Duration
	ex.Description = sr.Description
	// Preserve DAG fields if present
	if len(sr.Next) > 0 {
		ex.Next = sr.Next
	}
	if len(sr.DependsOn) > 0 {
		ex.DependsOn = sr.DependsOn
	}
	if sr.JoinMode != "" {
		ex.JoinMode = sr.JoinMode
	}
	// Condition fields
	if sr.Expression != "" {
		ex.Expression = sr.Expression
	}
	ex.ConditionResult = sr.ConditionResult
	// For foreach/parallel/loop: replace children, but filter to only include steps defined in the original Do/Steps block
	if ex.Action == "foreach" || ex.Action == "loop" || ex.Action == "parallel" {
		// Recursively search step definition for any nesting depth
		def := findStepDef(wfSteps, lookupKey)
		allowedNames := make(map[string]bool)
		if def != nil {
			if def.Action == "foreach" || def.Action == "loop" {
				for _, doStep := range def.Do {
					allowedNames[doStep.Name] = true
				}
			} else if def.Action == "parallel" {
				for _, pStep := range def.Steps {
					allowedNames[pStep.Name] = true
				}
			}
		}

		if len(allowedNames) > 0 {
			filteredChildren := make([]workflow.StepResult, 0, len(sr.Children))
			for _, child := range sr.Children {
				if allowedNames[child.Name] {
					filteredChildren = append(filteredChildren, child)
				}
			}
			ex.Children = filteredChildren
		} else {
			ex.Children = sr.Children
		}
	} else if ex.Action == "condition" {
		// For condition: update ThenChildren and ElseChildren
		// Clear Children (condition uses then_children and else_children)
		ex.Children = nil
		if sr.ThenChildren != nil {
			ex.ThenChildren = sr.ThenChildren
		}
		if sr.ElseChildren != nil {
			ex.ElseChildren = sr.ElseChildren
		}
	} else {
		// Recursively update children
		for i := range sr.Children {
			if i < len(ex.Children) {
				updateStep(&ex.Children[i], sr.Children[i], wfSteps)
			} else {
				ex.Children = append(ex.Children, sr.Children[i])
			}
		}
	}
}

// buildResultMap flattens the executed result tree (including container and
// condition children) into a lookup map keyed by step ID (name fallback).
func buildResultMap(steps []workflow.StepResult, resultMap map[string]workflow.StepResult) {
	for _, sr := range steps {
		key := sr.ID
		if key == "" {
			key = sr.Name
		}
		if key != "" {
			resultMap[key] = sr
		}
		// Also include children in the map
		if len(sr.Children) > 0 {
			buildResultMap(sr.Children, resultMap)
		}
		// Include condition children
		if len(sr.ThenChildren) > 0 {
			buildResultMap(sr.ThenChildren, resultMap)
		}
		if len(sr.ElseChildren) > 0 {
			buildResultMap(sr.ElseChildren, resultMap)
		}
	}
}

// updateTree folds executed results into the pre-populated step tree, using
// resultMap for lookup (entries are consumed once matched).
func updateTree(existing []workflow.StepResult, wfSteps []workflow.Step, resultMap map[string]workflow.StepResult) {
	for i := range existing {
		ex := &existing[i]
		key := ex.ID
		if key == "" {
			key = ex.Name
		}
		if key != "" {
			if sr, ok := resultMap[key]; ok {
				updateStep(ex, sr, wfSteps)
				// Remove from map to avoid processing again
				delete(resultMap, key)
			} else if ex.Name != "" {
				// Also try to match by name (for container nodes without ID in results)
				if sr, ok := resultMap[ex.Name]; ok {
					updateStep(ex, sr, wfSteps)
					delete(resultMap, ex.Name)
				}
			}
		} else if ex.Name != "" {
			// Try to match by name if ID is empty
			if sr, ok := resultMap[ex.Name]; ok {
				updateStep(ex, sr, wfSteps)
				delete(resultMap, ex.Name)
			}
		}
		// Recursively update children
		if len(existing[i].Children) > 0 {
			updateTree(existing[i].Children, wfSteps, resultMap)
		}
		// Update condition children
		if len(existing[i].ThenChildren) > 0 {
			updateTree(existing[i].ThenChildren, wfSteps, resultMap)
		}
		if len(existing[i].ElseChildren) > 0 {
			updateTree(existing[i].ElseChildren, wfSteps, resultMap)
		}
	}
}
