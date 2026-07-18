package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/workflow"
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
	var extraVars map[string]string
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			json.Unmarshal(body, &extraVars)
		}
	}
	if err := h.manager.Trigger(name, extraVars); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, success(map[string]string{"status": "triggered", "runbook": name}))
}

// TriggerByPath POST /api/triggers/{path:.*}
func (h *RunbookHandler) TriggerByPath(w http.ResponseWriter, r *http.Request) {
	path := "/" + mux.Vars(r)["path"]
	var extraVars map[string]string
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			json.Unmarshal(body, &extraVars)
		}
	}
	if err := h.manager.TriggerByPath(path, extraVars); err != nil {
		writeJSON(w, http.StatusNotFound, errorResp(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, success(map[string]string{"status": "triggered", "path": path}))
}

// SaveRunbook PUT /api/runbooks/{name}
func (h *RunbookHandler) SaveRunbook(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid body"))
		return
	}
	path := filepath.Join(h.runbooksDir, name)
	if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
		path += ".yaml"
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
	path := filepath.Join(h.runbooksDir, name)
	if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
		path += ".yaml"
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, errorResp(err.Error()))
		return
	}
	h.manager.LoadDir()
	writeJSON(w, http.StatusOK, success(map[string]string{"deleted": name}))
}

// RegisterRunbookRoutes registers all runbook routes on the router.
func RegisterRunbookRoutes(r *mux.Router, h *RunbookHandler) {
	r.HandleFunc("/api/runbooks", h.ListRunbooks).Methods("GET")
	r.HandleFunc("/api/runbooks/{name}", h.GetRunbook).Methods("GET")
	r.HandleFunc("/api/runbooks/{name}", h.SaveRunbook).Methods("PUT")
	r.HandleFunc("/api/runbooks/{name}", h.DeleteRunbook).Methods("DELETE")
	r.HandleFunc("/api/runbooks/{name}/trigger", h.TriggerRunbook).Methods("POST")
	r.HandleFunc("/api/triggers/{path:.*}", h.TriggerByPath).Methods("POST")
}

// triggerCallback is the function called when a runbook fires.
// It's set by the server to wire up executor execution.
type TriggerCallback func(rb *workflow.RunbookConfig, extraVars map[string]string)

// MakeTriggerCallback creates a TriggerFunc that executes the runbook's workflow.
func MakeTriggerCallback(store workflow.ExecutionStore, hub *WSHub, workflowsDir string, aiCfg workflow.AIConfig) workflow.TriggerFunc {
	return func(rb *workflow.RunbookConfig, extraVars map[string]string) {
		// Resolve workflow path.
		wfPath, err := resolveWorkflowPath(rb, workflowsDir)
		if err != nil {
			fmt.Printf("⚠️ runbook %s: %v\n", rb.Name, err)
			return
		}
		wf, err := workflow.ParseFile(wfPath)
		if err != nil {
			fmt.Printf("⚠️ runbook %s: parse workflow: %v\n", rb.Name, err)
			return
		}

		// Merge variables: runbook defaults + trigger extra vars.
		vars := make(map[string]string)
		for k, v := range rb.Variables {
			vars[k] = v
		}
		for k, v := range extraVars {
			vars[k] = v
		}

		// Execute in background.
		go func() {
			executor := workflow.NewExecutor(vars)
			result := executor.Execute(wf)
			fmt.Printf("📋 runbook %s → %s (%s)\n", rb.Name, result.Status, wf.Name)

			// Persist if store is available.
			if store != nil {
				execID := fmt.Sprintf("runbook-%s-%d", rb.Name, os.Getpid())
				_ = store.Save(workflow.ExecutionSnapshot{
					ExecutionSummary: workflow.ExecutionSummary{
						ID:             execID,
						WorkflowName:   wf.Name,
						Status:         result.Status,
						StartTime:      result.StartTime,
						EndTime:        result.EndTime,
						StepsCount:     len(wf.Steps),
					},
					Steps:     result.Steps,
					Variables: result.Variables,
					Workflow:  string(workflowYAML(wfPath)),
				})
			}
		}()
	}
}

func resolveWorkflowPath(rb *workflow.RunbookConfig, workflowsDir string) (string, error) {
	// Try absolute first.
	if filepath.IsAbs(rb.Workflow) {
		if _, err := os.Stat(rb.Workflow); err == nil {
			return rb.Workflow, nil
		}
	}
	// Try relative to cwd.
	if _, err := os.Stat(rb.Workflow); err == nil {
		return rb.Workflow, nil
	}
	// Try relative to workflows dir.
	candidate := filepath.Join(workflowsDir, rb.Workflow)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("workflow file not found: %s", rb.Workflow)
}

func workflowYAML(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
