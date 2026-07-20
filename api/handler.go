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
	aiConfig     workflow.AIConfig     // server-level AI config
	globalHooks  []workflow.HookConfig // server-level hooks (applied to all workflows)
}

// maxInMemoryExecutions caps the in-memory execution cache. Older entries are
// evicted (but remain on disk) to bound memory use in long-running servers.
const maxInMemoryExecutions = 100

// maxRequestBodyBytes caps request bodies on write endpoints (save/run/chat/
// runbook save) to 1 MiB so a rogue client cannot exhaust server memory.
const maxRequestBodyBytes = 1 << 20

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
// ExecutionDetail used by the API. Variables keep their real values here —
// they are masked at response serialization (maskForResponse), never in the
// store, so replay can still restore them. Logs are not persisted.
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
		Logs:              []LogEntry{},
		Steps:             snap.Steps,
		Workflow:          snap.WorkflowName,
		Variables:         snap.Variables,
		SensitivePatterns: sensitivePatternsFromYAML(snap.Workflow),
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

// safeJoin resolves a user-supplied file name to an absolute path inside
// base. It rejects path-traversal attempts (e.g. ".." segments that would
// escape base) and normalizes the .yaml/.yml suffix.
//
// On success it returns the safe absolute path and the normalized file name
// (e.g. "deploy.yaml"). On failure it returns a non-nil error.
func safeJoin(base, name string) (absPath, fileName string, err error) {
	if name == "" {
		return "", "", fmt.Errorf("name required")
	}
	if filepath.IsAbs(name) {
		return "", "", fmt.Errorf("invalid name: absolute paths are not allowed")
	}
	// Reject parent-dir segments outright. The Clean+prefix check below would
	// collapse them into a harmless in-base name, but silently rewriting a
	// traversal attempt into a valid file is worse than failing loudly.
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", "", fmt.Errorf("invalid name: parent directory references are not allowed")
		}
	}

	// Normalize the suffix early so the containment check sees the final name.
	if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
		name += ".yaml"
	}

	// Prefix with "/" so filepath.Clean interprets the name as rooted; this
	// collapses any embedded ".." segments before we join it onto the dir.
	cleaned := filepath.Clean("/" + name)
	abs := filepath.Join(base, cleaned)

	// Containment check (defense in depth): the resolved path must stay
	// within base.
	dir := base
	if !strings.HasSuffix(dir, string(os.PathSeparator)) {
		dir += string(os.PathSeparator)
	}
	if !strings.HasPrefix(abs+string(os.PathSeparator), dir) {
		return "", "", fmt.Errorf("invalid name: path escapes its base directory")
	}

	return abs, name, nil
}

// safePath resolves a workflow name to an absolute path inside the workflows
// directory. See safeJoin.
func (h *Handler) safePath(name string) (absPath, fileName string, err error) {
	return safeJoin(h.workflowsDir, name)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("Invalid request body (too large?)"))
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
