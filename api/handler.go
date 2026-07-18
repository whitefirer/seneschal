package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

var defaultUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Handler handles HTTP requests
type Handler struct {
	hub          *WSHub
	workflowsDir string
	store        workflow.ExecutionStore
	upgrader     websocket.Upgrader
	executions   map[string]*ExecutionDetail
	execMu       sync.RWMutex
	aiConfig     workflow.AIConfig    // server-level AI config
	globalHooks  []workflow.HookConfig // server-level hooks (applied to all workflows)
}

// maxInMemoryExecutions caps the in-memory execution cache. Older entries are
// evicted (but remain on disk) to bound memory use in long-running servers.
const maxInMemoryExecutions = 100

// NewHandler creates a new API handler. store may be nil to disable
// persistence (history lives only in memory, lost on restart). aiCfg carries
// the server-level AI config (model/provider/base_url) for chat/explain/fix.
func NewHandler(hub *WSHub, workflowsDir string, store workflow.ExecutionStore, aiCfg workflow.AIConfig, globalHooks []workflow.HookConfig, checkOrigin func(r *http.Request) bool) *Handler {
	upgrader := defaultUpgrader
	upgrader.CheckOrigin = checkOrigin
	h := &Handler{
		hub:          hub,
		workflowsDir: workflowsDir,
		store:        store,
		aiConfig:     aiCfg,
		globalHooks:  globalHooks,
		upgrader:     upgrader,
		executions:   make(map[string]*ExecutionDetail),
	}
	// Warm the in-memory cache from the store (most recent first), so history
	// is visible immediately after a restart.
	if store != nil {
		h.warmCache()
	}
	return h
}

// warmCache loads the most recent executions from the store into memory.
func (h *Handler) warmCache() {
	summaries, err := h.store.List()
	if err != nil {
		return
	}
	for _, s := range summaries {
		if len(h.executions) >= maxInMemoryExecutions {
			break
		}
		snap, err := h.store.Get(s.ID)
		if err != nil {
			continue
		}
		h.executions[s.ID] = snapshotToDetail(snap)
	}
}

// snapshotToDetail converts a stored ExecutionSnapshot into the in-memory
// ExecutionDetail used by the API. For warm-cache we only need the record +
// steps; logs are not persisted.
func snapshotToDetail(snap workflow.ExecutionSnapshot) *ExecutionDetail {
	return &ExecutionDetail{
		ExecutionRecord: ExecutionRecord{
			ID:           snap.ID,
			WorkflowName: snap.WorkflowName,
			WorkflowFile: snap.WorkflowFile,
			Status:       snap.Status,
			StartTime:    snap.StartTime,
			EndTime:      snap.EndTime,
			Duration:     snap.Duration,
			Error:        snap.Error,
			StepsCount:   snap.StepsCount,
		},
		Logs:     []LogEntry{},
		Steps:    snap.Steps,
		Workflow: snap.WorkflowName,
	}
}

// evictOldest removes the oldest in-memory execution if the cache is full.
// Caller must hold h.execMu.
func (h *Handler) evictOldest() {
	if len(h.executions) < maxInMemoryExecutions {
		return
	}
	var oldestID string
	var oldestStart string
	for id, e := range h.executions {
		if oldestID == "" || e.StartTime < oldestStart {
			oldestID = id
			oldestStart = e.StartTime
		}
	}
	delete(h.executions, oldestID)
}

// success returns a success response
func success(data interface{}) APIResponse {
	return APIResponse{
		Success: true,
		Data:    data,
	}
}

// errorResp returns an error response
func errorResp(msg string) APIResponse {
	return APIResponse{
		Success: false,
		Error:   msg,
	}
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, resp APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// safePath resolves a workflow name to an absolute path inside the workflows
// directory. It rejects path-traversal attempts (e.g. ".." or absolute paths)
// that would escape the directory, and normalizes the .yaml/.yml suffix.
//
// On success it returns the safe absolute path and the normalized file name
// (e.g. "deploy.yaml"). On failure it returns a non-nil error.
func (h *Handler) safePath(name string) (absPath, fileName string, err error) {
	if name == "" {
		return "", "", fmt.Errorf("workflow name required")
	}

	// Normalize the suffix early so the containment check sees the final name.
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		name += ".yaml"
	}

	// Prefix with "/" so filepath.Clean interprets the name as rooted; this
	// collapses any embedded ".." segments before we join it onto the dir.
	cleaned := filepath.Clean("/" + name)
	abs := filepath.Join(h.workflowsDir, cleaned)

	// Containment check: the resolved path must stay within workflowsDir.
	dir := h.workflowsDir
	if !strings.HasSuffix(dir, string(os.PathSeparator)) {
		dir += string(os.PathSeparator)
	}
	if !strings.HasPrefix(abs+string(os.PathSeparator), dir) {
		return "", "", fmt.Errorf("invalid workflow name")
	}

	return abs, name, nil
}

// ListWorkflows returns a list of all workflows
func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	entries, err := os.ReadDir(h.workflowsDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}

	var workflows []WorkflowInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(h.workflowsDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Parse workflow to get metadata
		wf, err := workflow.ParseFile(path)
		version := ""
		description := ""
		steps := 0
		variables := 0
		if err == nil {
			version = wf.Version
			description = wf.Description
			steps = len(wf.Steps)
			variables = len(wf.Variables)
		}

		workflows = append(workflows, WorkflowInfo{
			Name:        strings.TrimSuffix(name, filepath.Ext(name)),
			FileName:    name,
			Version:     version,
			Description: description,
			Steps:       steps,
			Variables:   variables,
			ModifiedAt:  info.ModTime(),
			Size:        info.Size(),
		})
	}

	writeJSON(w, http.StatusOK, success(workflows))
}

// GetWorkflow returns a workflow's YAML content
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	name := mux.Vars(r)["name"]
	path, normName, err := h.safePath(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}

	content, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResp("Workflow not found"))
		return
	}

	writeJSON(w, http.StatusOK, success(WorkflowContent{
		Name:     strings.TrimSuffix(normName, filepath.Ext(normName)),
		FileName: normName,
		Content:  string(content),
	}))
}

// SaveWorkflow creates or updates a workflow
func (h *Handler) SaveWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	name := mux.Vars(r)["name"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("Invalid request body"))
		return
	}
	defer r.Body.Close()

	// Normalize line endings to Unix style
	content := strings.ReplaceAll(string(body), "\r\n", "\n")

	path, _, err := h.safePath(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, success(map[string]string{
		"path": path,
	}))
}

// DeleteWorkflow deletes a workflow
func (h *Handler) DeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	name := mux.Vars(r)["name"]
	path, _, err := h.safePath(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}

	if err := os.Remove(path); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, success(nil))
}

// ValidateWorkflow validates a workflow YAML
func (h *Handler) ValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	name := mux.Vars(r)["name"]
	path, _, err := h.safePath(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}

	wf, err := workflow.ParseFile(path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}

	// Validate workflow
	if errs := wf.Validate(); len(errs) > 0 {
		var errMsgs []string
		for _, e := range errs {
			errMsgs = append(errMsgs, e.Error())
		}
		writeJSON(w, http.StatusBadRequest, errorResp(strings.Join(errMsgs, "; ")))
		return
	}

	writeJSON(w, http.StatusOK, success(map[string]interface{}{
		"valid":     true,
		"steps":     len(wf.Steps),
		"variables": len(wf.Variables),
	}))
}

// RunWorkflow executes a workflow
func (h *Handler) RunWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	name := mux.Vars(r)["name"]

	// Parse request body
	var req RunRequest
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			json.Unmarshal(body, &req)
		}
	}

	path, _, err := h.safePath(name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}

	wf, err := workflow.ParseFile(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResp("Workflow not found: "+err.Error()))
		return
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

	// Create execution record with pre-populated steps (including nested children)
	
	// Helper function to update step status recursively
	var updateStepStatus func(steps []workflow.StepResult, stepID string, event workflow.ProgressEvent) bool
	updateStepStatus = func(steps []workflow.StepResult, stepID string, event workflow.ProgressEvent) bool {
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
	
	var buildSteps func(steps []workflow.Step, parentId string) []workflow.StepResult
	buildSteps = func(steps []workflow.Step, parentId string) []workflow.StepResult {
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
	
	steps := buildSteps(wf.Steps, "")
	
	exec := &ExecutionDetail{
		ExecutionRecord: ExecutionRecord{
			ID:           executionID,
			WorkflowName: wf.Name,
			WorkflowFile: strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"),
			Status:       "running",
			StartTime:    time.Now().Format(time.RFC3339),
			StepsCount:   len(wf.Steps),
		},
		Logs:     []LogEntry{},
		Steps:    steps,
		Workflow: wf.Name,
	}

	h.execMu.Lock()
	h.evictOldest()
	h.executions[executionID] = exec
	h.execMu.Unlock()

	// Create executor
	executor := workflow.NewExecutor(vars)
	executor.SetDryRun(req.DryRun)
	if len(h.globalHooks) > 0 {
		executor.SetGlobalHooks(h.globalHooks)
	}

	// Setup progress callback
	executor.OnProgress = func(event workflow.ProgressEvent) {
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
			logMessage = fmt.Sprintf("🚀 Starting workflow: %s", wf.Name)
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
			ExecutionID:     executionID,
			WorkflowName:    wf.Name,
			WorkflowFile:    strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"),
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
		if e, ok := h.executions[executionID]; ok {
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
					Message:   fmt.Sprintf("🚀 Starting workflow: %s", wf.Name),
				})
			}

			// Update step status in real-time
			updateStepStatus(e.Steps, stepID, event)
		}
	}

	// Execute in goroutine
	go func() {
		result := executor.Execute(wf)

		h.execMu.Lock()
		defer h.execMu.Unlock()
		if e, ok := h.executions[executionID]; ok {
			e.Status = result.Status
			e.EndTime = result.EndTime
			e.Error = result.Error

			apiSteps := result.Steps

			// Update steps with results (preserve action from pre-populated steps)
			var updateStep func(existing *workflow.StepResult, result workflow.StepResult, wfSteps []workflow.Step)
			updateStep = func(ex *workflow.StepResult, sr workflow.StepResult, wfSteps []workflow.Step) {
				// Store original ID for lookup before overwriting
				lookupKey := ex.ID
				if lookupKey == "" {
					lookupKey = ex.Name
				}

				if sr.ID != "" {
					ex.ID = sr.ID  // Update ID (important for foreach iterations)
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
					var findStepDef func(steps []workflow.Step, key string) *workflow.Step
					findStepDef = func(steps []workflow.Step, key string) *workflow.Step {
						for i := range steps {
							s := &steps[i]
							sID := s.ID
							if sID == "" && s.Name != "" {
								sID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")))
							}
							if sID == key || s.Name == key { return s }
							for _, children := range [][]workflow.Step{s.Steps, s.Do, s.Then, s.Else} {
								if d := findStepDef(children, key); d != nil { return d }
							}
						}
						return nil
					}
					def := findStepDef(wfSteps, lookupKey)
					allowedNames := make(map[string]bool)
					if def != nil {
						if def.Action == "foreach" || def.Action == "loop" {
							for _, doStep := range def.Do { allowedNames[doStep.Name] = true }
						} else if def.Action == "parallel" {
							for _, pStep := range def.Steps { allowedNames[pStep.Name] = true }
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
			
			// Build a map of all results for quick lookup
			resultMap := make(map[string]workflow.StepResult)
			var buildResultMap func(steps []workflow.StepResult)
			buildResultMap = func(steps []workflow.StepResult) {
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
						buildResultMap(sr.Children)
					}
					// Include condition children
					if len(sr.ThenChildren) > 0 {
						buildResultMap(sr.ThenChildren)
					}
					if len(sr.ElseChildren) > 0 {
						buildResultMap(sr.ElseChildren)
					}
				}
			}
			buildResultMap(apiSteps)
			
			// Now recursively update existing tree using the result map
			var updateTree func(existing []workflow.StepResult, wfSteps []workflow.Step)
			updateTree = func(existing []workflow.StepResult, wfSteps []workflow.Step) {
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
						updateTree(existing[i].Children, wfSteps)
					}
					// Update condition children
					if len(existing[i].ThenChildren) > 0 {
						updateTree(existing[i].ThenChildren, wfSteps)
					}
					if len(existing[i].ElseChildren) > 0 {
						updateTree(existing[i].ElseChildren, wfSteps)
					}
				}
			}
			
			updateTree(e.Steps, wf.Steps)

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
			ExecutionID:  executionID,
			WorkflowName: wf.Name,
			WorkflowFile: strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"),
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
			if e, ok := h.executions[executionID]; ok {
				dur = e.Duration
			}
			snap := workflow.ExecutionSnapshot{
				ExecutionSummary: workflow.ExecutionSummary{
					ID:               executionID,
					WorkflowName:     wf.Name,
					WorkflowFile:     strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"),
					Status:           result.Status,
					StartTime:        result.StartTime,
					EndTime:          result.EndTime,
					Duration:         dur,
					Error:            result.Error,
					StepsCount:       len(wf.Steps),
					Nondeterministic: result.Nondeterministic,
				},
				Steps:     result.Steps,
				Variables: result.Variables,
			}
			// Store the original workflow YAML so replays can rebuild the exact
			// definition even if the file has changed since.
			if raw, rerr := os.ReadFile(path); rerr == nil {
				snap.Workflow = string(raw)
			}
			// Persist best-effort: a save failure should not fail the request,
			// which has already returned. Log and move on.
			_ = h.store.Save(snap)
		}
	}()

	writeJSON(w, http.StatusOK, success(map[string]string{
		"executionId": executionID,
		"status":      "started",
	}))
}

// GetExecutions returns execution history
func (h *Handler) GetExecutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	h.execMu.RLock()
	var executions []ExecutionRecord
	for _, e := range h.executions {
		executions = append(executions, e.ExecutionRecord)
	}
	h.execMu.RUnlock()

	// Merge in on-disk history not present in memory (so the listing covers
	// evicted and pre-restart entries too).
	if h.store != nil {
		if summaries, err := h.store.List(); err == nil {
			seen := make(map[string]bool, len(executions))
			for _, e := range executions {
				seen[e.ID] = true
			}
			for _, s := range summaries {
				if !seen[s.ID] {
					executions = append(executions, ExecutionRecord{
						ID:           s.ID,
						WorkflowName: s.WorkflowName,
						WorkflowFile: s.WorkflowFile,
						Status:       s.Status,
						StartTime:    s.StartTime,
						EndTime:      s.EndTime,
						Duration:     s.Duration,
						Error:        s.Error,
						StepsCount:   s.StepsCount,
					})
				}
			}
		}
	}

	// Sort by start time (newest first)
	for i := 0; i < len(executions)-1; i++ {
		for j := i + 1; j < len(executions); j++ {
			if executions[i].StartTime < executions[j].StartTime {
				executions[i], executions[j] = executions[j], executions[i]
			}
		}
	}

	writeJSON(w, http.StatusOK, success(executions))
}

// GetExecution returns execution details
func (h *Handler) GetExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResp("Execution ID required"))
		return
	}

	h.execMu.RLock()
	exec, ok := h.executions[id]
	h.execMu.RUnlock()
	if !ok {
		// Fall back to the store for evicted / pre-restart history.
		if h.store != nil {
			if snap, serr := h.store.Get(id); serr == nil {
				writeJSON(w, http.StatusOK, success(snapshotToDetail(snap)))
				return
			}
		}
		writeJSON(w, http.StatusNotFound, errorResp("Execution not found"))
		return
	}

	writeJSON(w, http.StatusOK, success(exec))
}

// ReplayExecution re-runs a historical execution: deterministic steps reuse
// recorded output, AI steps re-execute. POST /api/executions/{id}/replay.
// Returns a new executionId for the replay run.
func (h *Handler) ReplayExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResp("Execution store not configured"))
		return
	}
	id := mux.Vars(r)["id"]
	snap, err := h.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResp("Execution not found"))
		return
	}
	if snap.Workflow == "" {
		writeJSON(w, http.StatusBadRequest, errorResp("Snapshot has no stored workflow YAML"))
		return
	}
	wf, err := workflow.Parse([]byte(snap.Workflow))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp("Rebuild workflow: "+err.Error()))
		return
	}

	// Determine replay options from query string: ?full=true, ?step=name
	opts := workflow.ReplayOptions{
		Full:      r.URL.Query().Get("full") == "true",
		OnlySteps: r.URL.Query()["step"],
	}

	// Generate a new execution ID for the replay run.
	replayID := fmt.Sprintf("exec-%s-%s", time.Now().Format("20060102-150405"), randomHex(4))

	// Build the executor with the replay cache and an AI provider (from env,
	// so AI steps can re-run). The provider is best-effort: a workflow with
	// no AI steps needs none.
	executor := workflow.NewExecutor(snap.Variables)
	if len(h.globalHooks) > 0 {
		executor.SetGlobalHooks(h.globalHooks)
	}
	if !opts.Full {
		cache := buildAPIReplayCache(snap.Steps, opts)
		executor.SetReplayCache(cache)
	}
	if p, perr := ai.BuildProvider(h.aiConfig); perr == nil {
		executor.SetAIProvider(p)
	}

	// Broadcast start.
	wfFile := strings.TrimSuffix(strings.TrimSuffix(snap.WorkflowFile, ".yaml"), ".yml")
	h.hub.Broadcast(WSProgressEvent{
		Type: "workflow_start", ExecutionID: replayID,
		WorkflowName: wf.Name, WorkflowFile: wfFile,
		Timestamp: workflow.Now(),
	})

	writeJSON(w, http.StatusOK, success(map[string]string{
		"executionId": replayID,
		"replayOf":    id,
		"status":      "started",
	}))

	// Run in background, broadcasting progress like RunWorkflow does.
	go func() {
		result := executor.Execute(wf)
		h.hub.Broadcast(WSProgressEvent{
			Type: "workflow_end", ExecutionID: replayID,
			WorkflowName: wf.Name, WorkflowFile: wfFile,
			Status: result.Status, Error: result.Error,
			Timestamp: result.EndTime,
		})
		hits, misses := executor.ReplayStats()
		// Persist the replay run too.
		if h.store != nil {
			_ = h.store.Save(workflow.ExecutionSnapshot{
				ExecutionSummary: workflow.ExecutionSummary{
					ID:               replayID,
					WorkflowName:     wf.Name,
					WorkflowFile:     wfFile,
					Status:           result.Status,
					StartTime:        result.StartTime,
					EndTime:          result.EndTime,
					Error:            result.Error,
					StepsCount:       len(wf.Steps),
					Nondeterministic: result.Nondeterministic,
				},
				Steps:     result.Steps,
				Variables: result.Variables,
				Workflow:  snap.Workflow,
			})
		}
		// Log the reuse/re-exec summary via a WS event for visibility.
		h.hub.Broadcast(WSProgressEvent{
			Type: "step_output", ExecutionID: replayID,
			Output:   fmt.Sprintf("replay: %d reused, %d re-executed", hits, misses),
			Timestamp: workflow.Now(),
		})
	}()
}

// DeleteExecution removes a single execution from the store (and memory).
// DELETE /api/executions/{id}.
func (h *Handler) DeleteExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}
	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResp("Execution store not configured"))
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.store.Delete(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}
	h.execMu.Lock()
	delete(h.executions, id)
	h.execMu.Unlock()
	writeJSON(w, http.StatusOK, success(map[string]string{"deleted": id}))
}

// buildAPIReplayCache mirrors workflow.buildReplayCache but is duplicated in
// the api package because the workflow helper is unexported. It flattens the
// historical step tree into a cache keyed by ID then Name.
func buildAPIReplayCache(steps []workflow.StepResult, opts workflow.ReplayOptions) map[string]*workflow.StepResult {
	cache := make(map[string]*workflow.StepResult)
	onlySet := make(map[string]bool, len(opts.OnlySteps))
	for _, s := range opts.OnlySteps {
		onlySet[s] = true
	}
	var walk func([]workflow.StepResult)
	walk = func(ss []workflow.StepResult) {
		for i := range ss {
			sr := &ss[i]
			if len(opts.OnlySteps) > 0 && (onlySet[sr.Name] || onlySet[sr.ID]) {
				continue
			}
			if sr.ID != "" {
				cache[sr.ID] = sr
			}
			if sr.Name != "" {
				if _, ok := cache[sr.Name]; !ok {
					cache[sr.Name] = sr
				}
			}
			walk(sr.Children)
			walk(sr.ThenChildren)
			walk(sr.ElseChildren)
		}
	}
	walk(steps)
	return cache
}

// WSHandler handles WebSocket connections
func (h *Handler) WSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &WSClient{
		hub:  h.hub,
		conn: conn,
		send: make(chan WSProgressEvent, 512), // Increased buffer for long-running workflows
		sub:  make(map[string]bool),
	}

	client.hub.register <- client

	go client.WritePump()
	go client.ReadPump()
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
