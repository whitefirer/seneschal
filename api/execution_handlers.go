package api

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

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

	// Deep-copy under the read lock: the executor goroutine keeps mutating the
	// cached ExecutionDetail (logs, status, step tree) while we serialize it,
	// so handing the shared pointer to the JSON encoder would be a data race.
	h.execMu.RLock()
	exec, ok := h.executions[id]
	if ok {
		exec = exec.deepCopy()
	}
	h.execMu.RUnlock()
	if !ok {
		// Fall back to the store for evicted / pre-restart history.
		if h.store != nil {
			if snap, serr := h.store.Get(id); serr == nil {
				detail := snapshotToDetail(snap)
				detail.maskForResponse()
				writeJSON(w, http.StatusOK, success(detail))
				return
			}
		}
		writeJSON(w, http.StatusNotFound, errorResp("Execution not found"))
		return
	}

	exec.maskForResponse()
	writeJSON(w, http.StatusOK, success(exec))
}

// deepCopy returns an independent copy of the ExecutionDetail that shares no
// mutable memory with the original, safe to use after the lock is released.
func (e *ExecutionDetail) deepCopy() *ExecutionDetail {
	cp := &ExecutionDetail{
		ExecutionRecord:   e.ExecutionRecord,
		Logs:              make([]LogEntry, len(e.Logs)),
		Steps:             deepCopySteps(e.Steps),
		Workflow:          e.Workflow,
		SensitivePatterns: append([]string(nil), e.SensitivePatterns...),
	}
	copy(cp.Logs, e.Logs)
	if e.Variables != nil {
		cp.Variables = make(map[string]string, len(e.Variables))
		for k, v := range e.Variables {
			cp.Variables[k] = v
		}
	}
	return cp
}

// deepCopySteps recursively copies a step-result tree — children, condition
// branches, and the slice/pointer fields — so the result shares no mutable
// memory with the original.
func deepCopySteps(steps []workflow.StepResult) []workflow.StepResult {
	if steps == nil {
		return nil
	}
	out := make([]workflow.StepResult, len(steps))
	for i, s := range steps {
		out[i] = s
		out[i].Children = deepCopySteps(s.Children)
		out[i].ThenChildren = deepCopySteps(s.ThenChildren)
		out[i].ElseChildren = deepCopySteps(s.ElseChildren)
		if s.Next != nil {
			out[i].Next = append([]string(nil), s.Next...)
		}
		if s.DependsOn != nil {
			out[i].DependsOn = append([]string(nil), s.DependsOn...)
		}
		if s.ConditionResult != nil {
			v := *s.ConditionResult
			out[i].ConditionResult = &v
		}
	}
	return out
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

	// Register the replay run in memory so it is visible while running
	// (GET /api/executions/{id}) and receives the same WebSocket progress
	// pipeline as normal runs.
	wfFile := strings.TrimSuffix(strings.TrimSuffix(snap.WorkflowFile, ".yaml"), ".yml")
	run := &workflowRun{
		name:         snap.WorkflowFile,
		path:         "",
		wf:           wf,
		executionID:  replayID,
		vars:         snap.Variables,
		workflowYAML: snap.Workflow,
	}
	h.registerExecution(run)

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
	executor.OnProgress = func(event workflow.ProgressEvent) {
		h.onRunProgress(run, event)
	}

	// Broadcast start.
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

	// Run in background, reusing the same reconcile/persist path as normal
	// runs (which also emits the final workflow_end event).
	go func() {
		result := executor.Execute(wf)
		h.reconcileExecution(run, result)
		hits, misses := executor.ReplayStats()
		// Log the reuse/re-exec summary via a WS event for visibility.
		h.hub.Broadcast(WSProgressEvent{
			Type: "step_output", ExecutionID: replayID,
			Output:    fmt.Sprintf("replay: %d reused, %d re-executed", hits, misses),
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
