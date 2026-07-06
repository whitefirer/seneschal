package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileStore implements ExecutionStore as one JSON file per execution in a
// directory. Each file is named "<id>.json". The store is concurrent-safe via
// a mutex around file operations; for higher concurrency a later
// implementation could use per-ID locking, but the single-mutex approach is
// correct and sufficient for typical workloads.
type FileStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileStore creates a file-backed store rooted at dir. The directory is
// created on first Save if it does not exist.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Dir returns the store's root directory.
func (s *FileStore) Dir() string { return s.dir }

func (s *FileStore) path(id string) string {
	// Sanitize: only allow the id to be a filename, not a path. This prevents
	// "../" from escaping the directory.
	clean := strings.NewReplacer("/", "_", "\\", "_", string(filepath.Separator), "_").Replace(id)
	return filepath.Join(s.dir, clean+".json")
}

// Save writes the snapshot to <dir>/<id>.json, creating the directory if
// needed.
func (s *FileStore) Save(snap ExecutionSnapshot) error {
	if snap.ID == "" {
		return fmt.Errorf("execution snapshot has no ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("create executions dir: %w", err)
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(s.path(snap.ID), data, 0644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// Get loads a snapshot by ID.
func (s *FileStore) Get(id string) (ExecutionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return ExecutionSnapshot{}, fmt.Errorf("get execution %s: %w", id, err)
	}
	var snap ExecutionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return ExecutionSnapshot{}, fmt.Errorf("parse execution %s: %w", id, err)
	}
	return snap, nil
}

// List returns summaries for all stored executions, newest-first by StartTime.
// Corrupt files are skipped rather than failing the whole listing.
func (s *FileStore) List() ([]ExecutionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read executions dir: %w", err)
	}

	var out []ExecutionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var snap ExecutionSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			// Skip corrupt files rather than aborting.
			continue
		}
		out = append(out, snap.ExecutionSummary)
	}

	// Sort newest-first by StartTime (RFC3339 sorts lexically by time).
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartTime > out[j].StartTime
	})
	return out, nil
}

// Delete removes the snapshot file for the given ID. Missing files are not an
// error.
func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete execution %s: %w", id, err)
	}
	return nil
}

// Purge deletes all but the most recent `keep` executions (by StartTime).
// Returns the number of executions deleted. This is a convenience method on
// FileStore (not part of the interface) used by the `history purge` command.
func (s *FileStore) Purge(keep int) (int, error) {
	summaries, err := s.List()
	if err != nil {
		return 0, err
	}
	if keep < 0 {
		keep = 0
	}
	if len(summaries) <= keep {
		return 0, nil
	}
	deleted := 0
	for _, sum := range summaries[keep:] {
		if err := s.Delete(sum.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
