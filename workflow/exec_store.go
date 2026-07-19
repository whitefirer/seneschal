package workflow

// ExecutionSummary is the metadata for a past execution, used in listings
// without the full step tree.
type ExecutionSummary struct {
	ID               string `json:"id"`
	WorkflowName     string `json:"workflowName"`
	WorkflowFile     string `json:"workflowFile,omitempty"`
	Status           string `json:"status"`
	StartTime        string `json:"startTime"`
	EndTime          string `json:"endTime,omitempty"`
	Duration         string `json:"duration,omitempty"`
	Error            string `json:"error,omitempty"`
	StepsCount       int    `json:"stepsCount"`
	Nondeterministic bool   `json:"nondeterministic,omitempty"`
}

// ExecutionSnapshot is the complete, persistable record of one workflow run:
// metadata, the full step-result tree, the resolved variables, and the
// workflow definition as it was at execution time. Storing the YAML lets a
// replay rebuild the exact workflow even if the file has since changed.
//
// Snapshots may contain sensitive data (variable values, command outputs),
// so they are kept out of version control (see .gitignore) and should be
// stored in a directory that is not committed.
type ExecutionSnapshot struct {
	ExecutionSummary
	Steps     []StepResult      `json:"steps"`
	Variables map[string]string `json:"variables,omitempty"`
	Workflow  string            `json:"workflow"` // original YAML source
}

// ExecutionStore persists execution history. Implementations must be safe for
// concurrent use: the server may Save from a goroutine while List runs on the
// request path.
//
// The Phase 4 implementation is file-based (FileStore); a later phase may add
// a database- or object-backed store behind the same interface, mirroring the
// WorkflowRegistry pattern.
type ExecutionStore interface {
	// Save persists a snapshot. Overwrites any existing snapshot with the
	// same ID.
	Save(snap ExecutionSnapshot) error

	// Get loads a single snapshot by execution ID.
	Get(id string) (ExecutionSnapshot, error)

	// List returns metadata for all stored executions, newest-first by
	// StartTime. Entries that fail to parse are skipped.
	List() ([]ExecutionSummary, error)

	// Delete removes a single snapshot. Removing a non-existent ID is not an
	// error.
	Delete(id string) error
}
