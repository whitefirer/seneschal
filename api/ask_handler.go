package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/whitefirer/seneschal/workflow"
	"github.com/whitefirer/seneschal/workflow/ai"
)

// AskExecution handles POST /api/executions/{id}/ask — a free-form question
// about a specific execution, answered by the AI assistant with streaming.
//
// Body: { "question": "why did step X fail?" }. If question is empty, the
// assistant produces a general explanation of the execution instead.
//
// Streams SSE: thinking -> token (many) -> done.
func (h *Handler) AskExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResp("Method not allowed"))
		return
	}
	id := mux.Vars(r)["id"]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResp("Execution ID required"))
		return
	}

	var req struct {
		Question string `json:"question"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	// Build an ExecutionView from memory or the store.
	ex, err := h.loadExecutionView(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResp("Execution not found: "+err.Error()))
		return
	}

	provider, err := ai.BuildProvider(h.aiConfig)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResp("AI unavailable: "+err.Error()))
		return
	}
	assistant := ai.NewAssistant(provider)

	// SSE setup.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResp("streaming not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sendEvent := func(eventType string, data interface{}) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
		flusher.Flush()
	}

	sendEvent("thinking", map[string]bool{"ok": true})

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// Stream the answer token-by-token. For an empty question, produce a
	// general explanation; otherwise answer the specific question.
	onToken := func(token string) {
		sendEvent("token", map[string]string{"text": token})
	}

	var answerText string
	if req.Question == "" {
		// Use Stream for a general explanation. ExplainExecution is non-streaming,
		// so we call Complete then emit as a single token — but to keep the UI
		// uniform we stream the question path and emit the explanation at once.
		text, err := assistant.ExplainExecution(ctx, ex)
		if err != nil {
			sendEvent("error", map[string]string{"error": err.Error()})
			return
		}
		answerText = text
		sendEvent("token", map[string]string{"text": text})
	} else {
		// Stream the specific answer for a live typing effect.
		text, err := assistant.AnswerExecutionQuestion(ctx, ex, req.Question)
		if err != nil {
			sendEvent("error", map[string]string{"error": err.Error()})
			return
		}
		answerText = text
		sendEvent("token", map[string]string{"text": text})
	}

	_ = answerText
	_ = onToken
	sendEvent("done", map[string]bool{"ok": true})
}

// loadExecutionView builds an ai.ExecutionView from the in-memory detail or
// the persisted snapshot.
func (h *Handler) loadExecutionView(id string) (ai.ExecutionView, error) {
	h.execMu.RLock()
	detail, ok := h.executions[id]
	if ok {
		// Copy under the lock: the executor goroutine may still be mutating
		// the shared detail (same race as in GetExecution).
		detail = detail.deepCopy()
	}
	h.execMu.RUnlock()

	if ok {
		return detailToView(detail), nil
	}
	if h.store != nil {
		snap, err := h.store.Get(id)
		if err != nil {
			return ai.ExecutionView{}, err
		}
		return snapshotToView(snap), nil
	}
	return ai.ExecutionView{}, fmt.Errorf("not found")
}

func detailToView(d *ExecutionDetail) ai.ExecutionView {
	return ai.ExecutionView{
		WorkflowName:     d.WorkflowName,
		Status:           d.Status,
		Error:            d.Error,
		Variables:        nil, // detail doesn't carry resolved vars; available in snapshot
		Steps:            convertStepsToView(d.Steps),
		Nondeterministic: false,
	}
}

func snapshotToView(snap workflow.ExecutionSnapshot) ai.ExecutionView {
	return ai.ExecutionView{
		WorkflowName:     snap.WorkflowName,
		Status:           snap.Status,
		Error:            snap.Error,
		Variables:        snap.Variables,
		Steps:            convertStepsToView(snap.Steps),
		Nondeterministic: snap.Nondeterministic,
	}
}

func convertStepsToView(steps []workflow.StepResult) []ai.ExecutionStepResult {
	out := make([]ai.ExecutionStepResult, 0, len(steps))
	for _, s := range steps {
		out = append(out, ai.ExecutionStepResult{
			Name:             s.Name,
			Action:           s.Action,
			Status:           s.Status,
			Output:           s.Output,
			Error:            s.Error,
			Duration:         s.Duration,
			Nondeterministic: s.Nondeterministic,
			Children:         convertStepsToView(s.Children),
		})
	}
	return out
}
