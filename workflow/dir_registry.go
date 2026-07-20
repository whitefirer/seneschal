package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DirRegistry implements WorkflowRegistry over a filesystem directory.
// Each .yaml/.yml file in the directory is treated as one workflow.
//
// Listing/parsing results are cached and keyed to the directory's mtime:
// as long as os.Stat(dir).ModTime() is unchanged, List and the name→file
// resolution in Get reuse the previous parse. The cache is rebuilt when the
// directory mtime changes.
//
// Trade-off to be aware of: a directory's mtime reflects file additions,
// removals and renames — NOT content edits of existing files. So after an
// in-place edit, List metadata (description, step counts) can be stale until
// some file is added or removed. Get is not affected: it resolves the target
// file through the cache but always re-reads and re-parses that file, so the
// executed content is always current. The registry's main use case (adding
// and removing workflow files) fits the coarser granularity.
type DirRegistry struct {
	dir string

	mu      sync.RWMutex
	dirMod  time.Time         // directory mtime the cache was built from
	entries []WorkflowEntry   // cached List result, lexical by file name
	byName  map[string]string // YAML `name:` field → file name
	byStem  map[string]string // file stem (no suffix) → file name
	fresh   bool              // whether the cache has been built
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
	if err := r.ensureFresh(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]WorkflowEntry(nil), r.entries...), nil
}

// Get fetches a workflow by name. The name may be:
//   - the YAML `name:` field (e.g. "deploy-staging")
//   - the file name with suffix (e.g. "deploy.yaml")
//   - the file name without suffix (e.g. "deploy")
//
// The first match wins. Returns the parsed workflow and the raw YAML bytes.
// The target file is always read fresh, so content edits are picked up
// immediately even though the directory-mtime cache may still be serving
// stale List metadata (see the type comment).
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

	// Otherwise resolve through the cached name maps (YAML `name:` field or
	// file stem), then read the resolved file.
	if err := r.ensureFresh(); err != nil {
		return nil, nil, err
	}
	stemTarget := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

	r.mu.RLock()
	fileName, ok := r.byName[name]
	if !ok {
		fileName, ok = r.byStem[stemTarget]
	}
	if !ok {
		fileName, ok = r.byStem[name]
	}
	r.mu.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("workflow %q not found in %s", name, r.dir)
	}

	path := filepath.Join(r.dir, fileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("workflow %q not found in %s", name, r.dir)
	}
	wf, perr := Parse(raw)
	if perr != nil {
		return nil, raw, fmt.Errorf("parse %s: %w", name, perr)
	}
	return wf, raw, nil
}

// ensureFresh rebuilds the cache when the directory mtime has changed since
// the last build (or when the cache has never been built).
func (r *DirRegistry) ensureFresh() error {
	info, err := os.Stat(r.dir)
	if err != nil {
		return fmt.Errorf("read registry dir %q: %w", r.dir, err)
	}
	mod := info.ModTime()

	r.mu.RLock()
	fresh := r.fresh && mod.Equal(r.dirMod)
	r.mu.RUnlock()
	if fresh {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fresh && mod.Equal(r.dirMod) {
		return nil // another goroutine rebuilt while we waited for the lock
	}
	return r.rebuildLocked(mod)
}

// rebuildLocked re-scans the directory and re-parses every workflow file.
// Caller must hold the write lock.
func (r *DirRegistry) rebuildLocked(mod time.Time) error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return fmt.Errorf("read registry dir %q: %w", r.dir, err)
	}

	r.entries = nil
	r.byName = make(map[string]string)
	r.byStem = make(map[string]string)
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
		r.entries = append(r.entries, WorkflowEntry{
			Name:        wf.Name,
			FileName:    name,
			Path:        path,
			Description: wf.Description,
			Steps:       len(wf.Steps),
			Variables:   len(wf.Variables),
		})
		if wf.Name != "" {
			if _, taken := r.byName[wf.Name]; !taken {
				r.byName[wf.Name] = name
			}
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		if _, taken := r.byStem[stem]; !taken {
			r.byStem[stem] = name
		}
	}
	r.dirMod = mod
	r.fresh = true
	return nil
}

func hasYamlSuffix(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}
