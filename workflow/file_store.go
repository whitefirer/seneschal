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

// indexFileName is the summary index maintained alongside the execution
// files. It lets List answer from one small read instead of decoding every
// snapshot in the directory.
const indexFileName = "_index.json"

// defaultKeepExecutions is the default rotation bound: after each Save the
// store retains at most this many execution files (newest by StartTime). It
// mirrors the API layer's in-memory cap (maxInMemoryExecutions = 100).
const defaultKeepExecutions = 100

// FileStore implements ExecutionStore as one JSON file per execution in a
// directory. Each file is named "<id>.json". The store is concurrent-safe via
// a mutex around file operations; for higher concurrency a later
// implementation could use per-ID locking, but the single-mutex approach is
// correct and sufficient for typical workloads.
//
// Two on-disk invariants back the fast paths:
//   - Writes are atomic: snapshot and index files are written to a "<name>.tmp"
//     sibling and renamed into place, so a crash leaves either the old or the
//     new file, never a truncated one.
//   - List is served from _index.json (rebuilt from a full scan when missing
//     or corrupt). The index is treated as authoritative for List: snapshots
//     deleted or corrupted behind the store's back only surface again on the
//     next index rebuild. Get always reads the real snapshot file.
type FileStore struct {
	dir  string
	keep int // rotation bound after Save; 0 disables rotation
	mu   sync.Mutex
}

// NewFileStore creates a file-backed store rooted at dir. The directory is
// created on first Save if it does not exist. The optional keep argument sets
// the rotation bound (default defaultKeepExecutions; <= 0 disables rotation).
func NewFileStore(dir string, keep ...int) *FileStore {
	k := defaultKeepExecutions
	if len(keep) > 0 {
		k = keep[0]
	}
	if k < 0 {
		k = 0
	}
	return &FileStore{dir: dir, keep: k}
}

// Dir returns the store's root directory.
func (s *FileStore) Dir() string { return s.dir }

func (s *FileStore) path(id string) string {
	// Sanitize: only allow the id to be a filename, not a path. This prevents
	// "../" from escaping the directory.
	clean := strings.NewReplacer("/", "_", "\\", "_", string(filepath.Separator), "_").Replace(id)
	return filepath.Join(s.dir, clean+".json")
}

func (s *FileStore) indexPath() string {
	return filepath.Join(s.dir, indexFileName)
}

// writeFileAtomic writes data to path via a tmp sibling + rename, so readers
// never observe a partially written file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Save writes the snapshot to <dir>/<id>.json, creating the directory if
// needed, then updates the summary index and applies the keep=N rotation.
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
	if err := writeFileAtomic(s.path(snap.ID), data, 0644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	summaries, err := s.loadOrScanLocked()
	if err != nil {
		return err
	}
	summaries = upsertSummary(summaries, snap.ExecutionSummary)
	sortSummaries(summaries)
	summaries = s.rotateLocked(summaries)
	if err := s.writeIndexLocked(summaries); err != nil {
		return fmt.Errorf("write index: %w", err)
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
// It answers from the summary index; a missing or corrupt index falls back to
// a full directory scan (corrupt snapshot files are skipped) and rebuilds it.
func (s *FileStore) List() ([]ExecutionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if summaries, err := s.readIndexLocked(); err == nil {
		return append([]ExecutionSummary(nil), summaries...), nil
	}

	summaries, err := s.scanLocked()
	if err != nil {
		return nil, err
	}
	if summaries == nil {
		return nil, nil // no store dir yet — nothing to index
	}
	// Rebuild the index for next time. Best-effort: a listing must not fail
	// just because the index could not be written.
	_ = s.writeIndexLocked(summaries)
	return summaries, nil
}

// Delete removes the snapshot file for the given ID and drops it from the
// index. Missing files are not an error.
func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.path(id))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete execution %s: %w", id, err)
	}

	summaries, err := s.loadOrScanLocked()
	if err != nil {
		return err
	}
	for i, sum := range summaries {
		if sum.ID == id {
			summaries = append(summaries[:i], summaries[i+1:]...)
			break
		}
	}
	if err := s.writeIndexLocked(summaries); err != nil {
		return fmt.Errorf("write index: %w", err)
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

// ── Index maintenance (caller must hold s.mu) ────────────────────────────────

// readIndexLocked loads the summary index. Any read/parse failure is an error;
// callers fall back to a full scan in that case.
func (s *FileStore) readIndexLocked() ([]ExecutionSummary, error) {
	data, err := os.ReadFile(s.indexPath())
	if err != nil {
		return nil, err
	}
	var summaries []ExecutionSummary
	if err := json.Unmarshal(data, &summaries); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return summaries, nil
}

// writeIndexLocked persists the summary index atomically.
func (s *FileStore) writeIndexLocked(summaries []ExecutionSummary) error {
	data, err := json.Marshal(summaries)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.indexPath(), data, 0644)
}

// loadOrScanLocked returns the indexed summaries, falling back to a full
// directory scan when the index is missing or corrupt.
func (s *FileStore) loadOrScanLocked() ([]ExecutionSummary, error) {
	if summaries, err := s.readIndexLocked(); err == nil {
		return summaries, nil
	}
	return s.scanLocked()
}

// scanLocked reads every snapshot file in the directory and returns the
// summaries newest-first. Corrupt files are skipped. It returns (nil, nil)
// when the directory does not exist yet.
func (s *FileStore) scanLocked() ([]ExecutionSummary, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read executions dir: %w", err)
	}

	var out []ExecutionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == indexFileName {
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

	sortSummaries(out)
	return out, nil
}

// rotateLocked drops the oldest executions beyond the keep bound: their files
// are removed and the returned slice is truncated to at most keep entries.
// The input must already be sorted newest-first.
func (s *FileStore) rotateLocked(summaries []ExecutionSummary) []ExecutionSummary {
	if s.keep <= 0 || len(summaries) <= s.keep {
		return summaries
	}
	for _, sum := range summaries[s.keep:] {
		if err := os.Remove(s.path(sum.ID)); err != nil && !os.IsNotExist(err) {
			// Rotation is housekeeping: a file that refuses to go must not
			// fail the Save that triggered it. It drops out of the index here
			// and is retried on the next full-scan index rebuild or Purge.
			continue
		}
	}
	return summaries[:s.keep]
}

// upsertSummary inserts or replaces the summary with the same ID.
func upsertSummary(summaries []ExecutionSummary, sum ExecutionSummary) []ExecutionSummary {
	for i, existing := range summaries {
		if existing.ID == sum.ID {
			summaries[i] = sum
			return summaries
		}
	}
	return append(summaries, sum)
}

// sortSummaries sorts newest-first by StartTime (RFC3339 sorts lexically by
// time).
func sortSummaries(summaries []ExecutionSummary) {
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StartTime > summaries[j].StartTime
	})
}
