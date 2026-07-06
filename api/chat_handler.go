package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"goworkflow/workflow"
	"goworkflow/workflow/ai"
)

// ChatHandler handles POST /api/chat — a natural-language workflow trigger
// that streams server-sent events back to the browser.
//
// Unlike RunWorkflow (which delegates to the executor and pushes progress
// over WebSocket), chat is a request-scoped SSE stream: the client POSTs an
// intent and reads a stream of events (thinking -> selection -> done) until
// the assistant has chosen a workflow and filled variables. Confirming and
// running the selected workflow is a separate step (the frontend calls the
// existing /run endpoint after the user confirms).
func (h *Handler) ChatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}

	var req ChatRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResp("invalid request body"))
			return
		}
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, errorResp("message is required"))
		return
	}

	// Build the assistant from the environment. If no key is configured, the
	// assistant cannot call a model — fail early with a clear message.
	provider, err := ai.BuildProvider(ai.Config{})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResp("AI unavailable: "+err.Error()))
		return
	}
	assistant := ai.NewAssistant(provider)

	// Resolve the workflow directory. Fall back to the handler's workflows dir.
	dir := req.Dir
	if dir == "" {
		dir = h.workflowsDir
	}
	registry := workflow.NewDirRegistry(dir)
	entries, err := registry.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResp("list workflows: "+err.Error()))
		return
	}

	// Convert to assistant candidates, exposing declared variable names.
	candidates := make([]ai.CandidateEntry, 0, len(entries))
	for _, e := range entries {
		var vars []string
		if wf, _, gerr := registry.Get(e.Name); gerr == nil {
			for k := range wf.Variables {
				vars = append(vars, k)
			}
		}
		candidates = append(candidates, ai.CandidateEntry{
			Name: e.Name, FileName: e.FileName,
			Description: e.Description, Steps: e.Steps, Variables: vars,
		})
	}

	// SSE setup. Disable proxy buffering and compression so tokens flush.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResp("streaming not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx

	// Helper to send one SSE event.
	sendEvent := func(eventType string, data interface{}) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
		flusher.Flush()
	}

	// Emit a 'thinking' event so the UI can show a spinner immediately.
	sendEvent("thinking", map[string]interface{}{
		"message":  req.Message,
		"workflowCount": len(candidates),
	})

	// Run the selection with a generous timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	sel, err := assistant.SelectWorkflow(ctx, req.Message, candidates)
	if err != nil {
		sendEvent("error", map[string]string{"error": err.Error()})
		return
	}

	// If nothing matched, tell the client and suggest creating one.
	if sel.Workflow == "" {
		sendEvent("selection", map[string]interface{}{
			"workflow":    "",
			"variables":   map[string]string{},
			"confidence":  0,
			"available":   candidateNames(candidates),
			"suggestCreate": true, // frontend offers a "generate" option
		})
		sendEvent("done", map[string]bool{"ok": true})
		return
	}

	// Load the chosen workflow to include its step summary + structure in the
	// response so the UI can show a preview / DAG without a second round-trip.
	var stepSummary []map[string]interface{}
	var fileName string
	if wf, _, gerr := registry.Get(sel.Workflow); gerr == nil {
		// Find the file name for this workflow (needed by /run which matches
		// by file name, not YAML name field).
		for _, e := range entries {
			if e.Name == sel.Workflow {
				fileName = e.FileName
				break
			}
		}
		stepSummary = buildStepSummary(wf.Steps)
	}

	sendEvent("selection", map[string]interface{}{
		"workflow":   sel.Workflow,
		"fileName":   fileName, // for the /run API which matches by file name
		"variables":  sel.Variables,
		"confidence": sel.Confidence,
		"steps":      stepSummary,
	})
	sendEvent("done", map[string]bool{"ok": true})
}

// buildStepSummary flattens the step tree into a list of step descriptors
// including structural fields (next, depends_on, then/else branch presence,
// foreach items, parallel children) so the frontend can render a DAG preview
// or mermaid graph without a second API call.
func buildStepSummary(steps []workflow.Step) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(steps))
	for _, s := range steps {
		entry := map[string]interface{}{
			"name":   s.Name,
			"action": s.Action,
		}
		if len(s.Next) > 0 {
			entry["next"] = s.Next
		}
		if len(s.DependsOn) > 0 {
			entry["depends_on"] = s.DependsOn
		}
		if s.Action == "condition" {
			if len(s.Then) > 0 {
				entry["then"] = buildStepSummary(s.Then)
			}
			if len(s.Else) > 0 {
				entry["else"] = buildStepSummary(s.Else)
			}
		}
		if (s.Action == "foreach" || s.Action == "loop") && len(s.Do) > 0 {
			entry["do"] = buildStepSummary(s.Do)
		}
		if s.Action == "parallel" && len(s.Steps) > 0 {
			entry["steps"] = buildStepSummary(s.Steps)
		}
		out = append(out, entry)
	}
	return out
}

func candidateNames(cs []ai.CandidateEntry) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}
