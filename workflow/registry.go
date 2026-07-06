package workflow

// WorkflowEntry is the metadata for a workflow discoverable through a
// registry, without the full parsed content. It is what AI assistants and
// listing UIs need to present choices.
type WorkflowEntry struct {
	Name        string // workflow name (from YAML `name:`)
	FileName    string // file name, e.g. "deploy.yaml"
	Path        string // full path or storage key
	Description string // human-readable description
	Steps       int    // top-level step count
	Variables   int    // workflow-level variable count
}

// WorkflowRegistry abstracts how workflows are discovered and fetched.
//
// The CLI chat assistant, the future TUI app, and the HTTP server all share
// this abstraction. The Phase 3 implementation is directory-based
// (DirRegistry); Phase 4 may add a database- or object-backed implementation
// without changing call sites.
type WorkflowRegistry interface {
	// List returns metadata for all discoverable workflows. Entries are
	// returned in directory (lexical) order. Files that fail to parse are
	// skipped rather than aborting the whole listing.
	List() ([]WorkflowEntry, error)

	// Get fetches a single workflow by name. The name may match either the
	// workflow's YAML `name:` field or its file name (with or without the
	// .yaml/.yml suffix). Returns the parsed workflow plus the raw YAML bytes
	// (the latter is needed by assistants that send the content to an LLM).
	Get(name string) (*Workflow, []byte, error)
}
