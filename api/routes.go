package api

import "github.com/gorilla/mux"

// RegisterRoutes registers the complete API route table on r: workflow and
// execution management, chat, the WebSocket endpoint, and (via
// RegisterRunbookRoutes) the runbook trigger/schedule routes.
//
// This is the single source of truth for the API route table: both
// cmd/server/main.go and the e2e tests register their routes through this
// function, so the test server can never drift from production. Middleware
// (CORS, auth) is NOT part of this table — it stays with the callers (the
// config package owns it, and api must not depend on config).
//
// The SPA static-file fallback is intentionally absent: it lives in
// cmd/server/main.go because it serves the embedded web FS, which api does
// not know about.
func RegisterRoutes(r *mux.Router, h *Handler, rh *RunbookHandler) {
	// Workflows.
	r.HandleFunc("/api/workflows", h.ListWorkflows).Methods("GET")
	r.HandleFunc("/api/workflows/{name}", h.GetWorkflow).Methods("GET")
	r.HandleFunc("/api/workflows/{name}", h.SaveWorkflow).Methods("PUT")
	r.HandleFunc("/api/workflows/{name}", h.DeleteWorkflow).Methods("DELETE")
	r.HandleFunc("/api/workflows/{name}/validate", h.ValidateWorkflow).Methods("POST")
	r.HandleFunc("/api/workflows/{name}/run", h.RunWorkflow).Methods("POST")

	// Executions (history, replay, AI ask).
	r.HandleFunc("/api/executions", h.GetExecutions).Methods("GET")
	r.HandleFunc("/api/executions/{id}", h.GetExecution).Methods("GET")
	r.HandleFunc("/api/executions/{id}", h.DeleteExecution).Methods("DELETE")
	r.HandleFunc("/api/executions/{id}/replay", h.ReplayExecution).Methods("POST")
	r.HandleFunc("/api/executions/{id}/ask", h.AskExecution).Methods("POST")

	// Chat (SSE) and the real-time progress WebSocket.
	r.HandleFunc("/api/chat", h.ChatHandler).Methods("POST")
	r.HandleFunc("/api/ws", h.WSHandler)

	// Runbooks: trigger/schedule management.
	RegisterRunbookRoutes(r, rh)
}
