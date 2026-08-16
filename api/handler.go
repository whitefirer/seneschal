package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/whitefirer/seneschal/workflow"
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
