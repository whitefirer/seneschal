package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/workflow"
	"gopkg.in/yaml.v3"
)

// RunbookHandler handles runbook CRUD + trigger endpoints.
type RunbookHandler struct {
	manager      *workflow.RunbookManager
	runbooksDir  string
	workflowsDir string
}

func NewRunbookHandler(manager *workflow.RunbookManager, runbooksDir, workflowsDir string) *RunbookHandler {
	return &RunbookHandler{manager: manager, runbooksDir: runbooksDir, workflowsDir: workflowsDir}
}

// ListRunbooks GET /api/runbooks
func (h *RunbookHandler) ListRunbooks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}
	runbooks := h.manager.List()
	writeJSON(w, http.StatusOK, success(runbooks))
}

// GetRunbook GET /api/runbooks/{name}
func (h *RunbookHandler) GetRunbook(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	rb := h.manager.Get(name)
	if rb == nil {
		writeJSON(w, http.StatusNotFound, errorResp("Runbook not found"))
		return
	}
	writeJSON(w, http.StatusOK, success(rb))
}

// TriggerRunbook POST /api/runbooks/{name}/trigger
func (h *RunbookHandler) TriggerRunbook(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	extraVars, ok := readTriggerExtraVars(w, r)
	if !ok {
		return
	}
	execID, err := h.manager.Trigger(name, extraVars)
	if err != nil {
		// Pre-dispatch failures are client errors (unknown runbook, not
		// manually triggerable); a dispatch failure means the runbook's
		// workflow reference is broken server-side.
		if errors.Is(err, workflow.ErrTriggerDispatch) {
			writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}
	resp := map[string]string{"status": "triggered", "runbook": name}
	if execID != "" {
		resp["executionId"] = execID
	}
	writeJSON(w, http.StatusOK, success(resp))
}

// TriggerByPath POST /api/triggers/{path:.*}
func (h *RunbookHandler) TriggerByPath(w http.ResponseWriter, r *http.Request) {
	path := "/" + mux.Vars(r)["path"]
	extraVars, ok := readTriggerExtraVars(w, r)
	if !ok {
		return
	}
	execID, err := h.manager.TriggerByPath(path, extraVars)
	if err != nil {
		if errors.Is(err, workflow.ErrTriggerDispatch) {
			writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
			return
		}
		writeJSON(w, http.StatusNotFound, errorResp(err.Error()))
		return
	}
	resp := map[string]string{"status": "triggered", "path": path}
	if execID != "" {
		resp["executionId"] = execID
	}
	writeJSON(w, http.StatusOK, success(resp))
}

// readTriggerExtraVars reads the optional JSON body of extra variables for a
// trigger request, capped at maxRequestBodyBytes. It always returns a non-nil
// map. ok is false when the body could not be read; the error response has
// already been written.
func readTriggerExtraVars(w http.ResponseWriter, r *http.Request) (vars map[string]string, ok bool) {
	vars = make(map[string]string)
	if r.Body == nil {
		return vars, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid or too large body"))
		return nil, false
	}
	if len(body) > 0 {
		// Malformed JSON is tolerated: extra vars are optional.
		_ = json.Unmarshal(body, &vars)
		if vars == nil {
			vars = make(map[string]string)
		}
	}
	return vars, true
}

// SaveRunbook PUT /api/runbooks/{name}
func (h *RunbookHandler) SaveRunbook(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	path, _, err := safeJoin(h.runbooksDir, name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid or too large body"))
		return
	}
	// Validate before writing: the runbook loader silently skips files it
	// cannot parse (or that lack the required 'workflow' field), so such a
	// body must be rejected here instead of being stored as a runbook that
	// never shows up. The raw bytes are still written as-is — the parse is
	// validation only, not a re-marshal.
	var rb workflow.RunbookConfig
	if err := yaml.Unmarshal(body, &rb); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid runbook YAML: "+err.Error()))
		return
	}
	if strings.TrimSpace(rb.Workflow) == "" {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid runbook: 'workflow' field is required"))
		return
	}
	os.MkdirAll(h.runbooksDir, 0755)
	if err := os.WriteFile(path, body, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}
	// Hot reload will pick it up within 10s; force reload now.
	h.manager.LoadDir()
	writeJSON(w, http.StatusOK, success(map[string]string{"path": path}))
}

// DeleteRunbook DELETE /api/runbooks/{name}
func (h *RunbookHandler) DeleteRunbook(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	path, _, err := safeJoin(h.runbooksDir, name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}
	h.manager.LoadDir()
	writeJSON(w, http.StatusOK, success(map[string]string{"deleted": name}))
}

// RegisterRunbookRoutes registers all runbook routes on the router. It is a
// sub-unit of RegisterRoutes (routes.go), the single composition root used by
// both cmd/server/main.go and the e2e tests — call RegisterRoutes, not this,
// unless you genuinely need only the runbook subset.
func RegisterRunbookRoutes(r *mux.Router, h *RunbookHandler) {
	r.HandleFunc("/api/runbooks", h.ListRunbooks).Methods("GET")
	r.HandleFunc("/api/runbooks/{name}", h.GetRunbook).Methods("GET")
	r.HandleFunc("/api/runbooks/{name}", h.SaveRunbook).Methods("PUT")
	r.HandleFunc("/api/runbooks/{name}", h.DeleteRunbook).Methods("DELETE")
	r.HandleFunc("/api/runbooks/{name}/trigger", h.TriggerRunbook).Methods("POST")
	r.HandleFunc("/api/triggers/{path:.*}", h.TriggerByPath).Methods("POST")
}

// MakeTriggerCallback creates a TriggerFunc that executes the runbook's
// workflow. The execution ID is generated synchronously and returned to the
// caller (the HTTP trigger handler reports it to the client, the cron
// scheduler logs it); the run itself stays async and its snapshot is
// persisted on completion.
//
// The hub carries "runbook_trigger" WS events: one when a trigger is
// dispatched (Status "triggered", with the new execution ID) and one when
// dispatch fails (Status "failed", with the error) — the latter is what makes
// a cron-fired runbook with a broken workflow reference visible in the UI
// instead of only in server logs. The trigger source (manual/webhook/cron)
// arrives via the reserved workflow.TriggerSourceExtraVar extraVars key,
// which is stripped before variables are merged so it never leaks into the
// execution environment. A nil hub disables broadcasting.
//
// aiCfg is still reserved (AI-assisted trigger handling) and intentionally
// unused — kept so the integration seam stays stable; do not remove it.
func MakeTriggerCallback(store workflow.ExecutionStore, hub *WSHub, workflowsDir string, aiCfg workflow.AIConfig) workflow.TriggerFunc {
	return func(rb *workflow.RunbookConfig, extraVars map[string]string) (string, error) {
		// Trigger source metadata, defaulting to manual for direct callback
		// invocations that bypass the manager.
		source := extraVars[workflow.TriggerSourceExtraVar]
		if source == "" {
			source = string(workflow.TriggerManual)
		}
		broadcast := func(execID, wfName, status, errMsg string) {
			if hub == nil {
				return
			}
			ev := WSProgressEvent{
				Type:         "runbook_trigger",
				RunbookName:  rb.Name,
				Source:       source,
				ExecutionID:  execID,
				WorkflowName: wfName,
				WorkflowFile: rb.Workflow,
				Status:       status,
				Error:        errMsg,
				Timestamp:    workflow.Now(),
			}
			if status == "triggered" {
				ev.LogLevel = "INFO"
				ev.LogMessage = fmt.Sprintf("📋 runbook %s triggered (%s) → execution %s", rb.Name, source, execID)
			} else {
				ev.LogLevel = "ERROR"
				ev.LogMessage = fmt.Sprintf("⚠️ runbook %s trigger failed (%s): %s", rb.Name, source, errMsg)
			}
			hub.Broadcast(ev)
		}

		// Resolve workflow path.
		wfPath, err := resolveWorkflowPath(rb, workflowsDir)
		if err != nil {
			broadcast("", "", "failed", err.Error())
			return "", fmt.Errorf("runbook %s: %w", rb.Name, err)
		}
		wf, err := workflow.ParseFile(wfPath)
		if err != nil {
			broadcast("", "", "failed", err.Error())
			return "", fmt.Errorf("runbook %s: parse workflow: %w", rb.Name, err)
		}

		// Merge variables: runbook defaults + trigger extra vars. The
		// reserved source key is metadata, not a workflow variable.
		vars := make(map[string]string)
		for k, v := range rb.Variables {
			vars[k] = v
		}
		for k, v := range extraVars {
			if k == workflow.TriggerSourceExtraVar {
				continue
			}
			vars[k] = v
		}

		// Generate the execution ID up front. Unique per trigger — the old
		// runbook-<name>-<pid> scheme collided when a runbook fired more than
		// once per process.
		execID := fmt.Sprintf("runbook-%s-%s", rb.Name, randomHex(4))

		// Execute in background.
		go func() {
			executor := workflow.NewExecutor(vars)
			result := executor.Execute(wf)
			fmt.Printf("📋 runbook %s → %s (%s)\n", rb.Name, result.Status, wf.Name)

			// Persist if store is available.
			if store != nil {
				_ = store.Save(workflow.ExecutionSnapshot{
					ExecutionSummary: workflow.ExecutionSummary{
						ID:           execID,
						WorkflowName: wf.Name,
						Status:       result.Status,
						StartTime:    result.StartTime,
						EndTime:      result.EndTime,
						StepsCount:   len(wf.Steps),
					},
					Steps:     result.Steps,
					Variables: result.Variables,
					Workflow:  string(workflowYAML(wfPath)),
				})
			}
		}()

		broadcast(execID, wf.Name, "triggered", "")
		return execID, nil
	}
}

// resolveWorkflowPath resolves a runbook's workflow reference. The path must
// be relative and must stay inside the workflows directory — absolute paths
// and ".." escapes are rejected so a runbook cannot make the server execute
// arbitrary YAML files from anywhere on disk.
func resolveWorkflowPath(rb *workflow.RunbookConfig, workflowsDir string) (string, error) {
	if rb.Workflow == "" {
		return "", fmt.Errorf("workflow path is empty")
	}
	if filepath.IsAbs(rb.Workflow) {
		return "", fmt.Errorf("absolute workflow paths are not allowed: %s", rb.Workflow)
	}
	// Root the path at "/" before cleaning so ".." segments collapse instead
	// of escaping, then join onto the workflows dir and verify containment.
	cleaned := filepath.Clean("/" + rb.Workflow)
	candidate := filepath.Join(workflowsDir, cleaned)
	dir := workflowsDir
	if !strings.HasSuffix(dir, string(os.PathSeparator)) {
		dir += string(os.PathSeparator)
	}
	if !strings.HasPrefix(candidate+string(os.PathSeparator), dir) {
		return "", fmt.Errorf("workflow path escapes the workflows directory: %s", rb.Workflow)
	}
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("workflow file not found: %s", rb.Workflow)
	}
	return candidate, nil
}

func workflowYAML(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
