package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirRegistry implements WorkflowRegistry over a filesystem directory.
// Each .yaml/.yml file in the directory is treated as one workflow.
type DirRegistry struct {
	dir string
}

// NewDirRegistry creates a registry rooted at dir.
func NewDirRegistry(dir string) *DirRegistry {
	return &DirRegistry{dir: dir}
}

// Dir returns the registry's root directory.
func (r *DirRegistry) Dir() string { return r.dir }

// List returns metadata for every parseable workflow file in the directory,
// in lexical order by file name. Files that fail to parse are skipped (a
// malformed file should not break listing the rest).
func (r *DirRegistry) List() ([]WorkflowEntry, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, fmt.Errorf("read registry dir %q: %w", r.dir, err)
	}

	var out []WorkflowEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !hasYamlSuffix(name) {
			continue
		}
		path := filepath.Join(r.dir, name)
		wf, err := ParseFile(path)
		if err != nil {
			// Skip unparseable files rather than failing the whole listing.
			continue
		}
		out = append(out, WorkflowEntry{
			Name:        wf.Name,
			FileName:    name,
			Path:        path,
			Description: wf.Description,
			Steps:       len(wf.Steps),
			Variables:   len(wf.Variables),
		})
	}
	return out, nil
}

// Get fetches a workflow by name. The name may be:
//   - the YAML `name:` field (e.g. "deploy-staging")
//   - the file name with suffix (e.g. "deploy.yaml")
//   - the file name without suffix (e.g. "deploy")
//
// The first match wins. Returns the parsed workflow and the raw YAML bytes.
func (r *DirRegistry) Get(name string) (*Workflow, []byte, error) {
	// Fast path: if the name already looks like an existing file, read it
	// directly.
	if hasYamlSuffix(name) {
		path := filepath.Join(r.dir, name)
		if raw, err := os.ReadFile(path); err == nil {
			wf, perr := Parse(raw)
			if perr != nil {
				return nil, raw, fmt.Errorf("parse %s: %w", name, perr)
			}
			return wf, raw, nil
		}
	}

	// Otherwise scan the directory and match by YAML `name:` field or by
	// file stem (name without suffix).
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read registry dir %q: %w", r.dir, err)
	}

	stemTarget := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

	for _, e := range entries {
		if e.IsDir() || !hasYamlSuffix(e.Name()) {
			continue
		}
		fileStem := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".yaml"), ".yml")
		// Cheap pre-filter: skip files whose stem clearly cannot match before
		// reading/parsing.
		if name != fileStem && stemTarget != fileStem {
			// Still need to read+parse to check the YAML `name:` field, which
			// may differ from the file stem. So fall through.
		}
		path := filepath.Join(r.dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		wf, perr := Parse(raw)
		if perr != nil {
			continue
		}
		if wf.Name == name || fileStem == stemTarget || fileStem == name {
			return wf, raw, nil
		}
	}

	return nil, nil, fmt.Errorf("workflow %q not found in %s", name, r.dir)
}

func hasYamlSuffix(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}
