package workflow

// Step / workflow status values. These constants centralize the string
// literals that were previously scattered as bare strings. Existing call sites
// still use the literal forms; new code (ai_decide, determinism propagation)
// should use these constants, and the literals can be migrated incrementally
// (see ARCHITECTURE.md tech-debt #6).
const (
	StatusSuccess   = "success"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"
	StatusRunning   = "running"
	StatusCompleted = "completed" // legacy alias of success (foreach); printers treat both as success
	StatusDone      = "done"      // legacy alias of success
	StatusPartial   = "partial"
)

// isSuccessStatus reports whether a status string indicates a successful
// outcome, tolerating the legacy aliases ("completed", "done").
func isSuccessStatus(s string) bool {
	switch s {
	case StatusSuccess, StatusCompleted, StatusDone:
		return true
	}
	return false
}
