package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStore_SaveGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)

	snap := ExecutionSnapshot{
		ExecutionSummary: ExecutionSummary{
			ID:           "exec-test-1",
			WorkflowName: "demo",
			Status:       "success",
			StartTime:    "2026-07-07T10:00:00+08:00",
			EndTime:      "2026-07-07T10:00:05+08:00",
			StepsCount:   2,
		},
		Steps: []StepResult{
			{Name: "build", ID: "build", Status: "success", Output: "ok"},
			{Name: "ai", ID: "ai", Status: "success", Output: "summary", Nondeterministic: true},
		},
		Variables: map[string]string{"env": "prod"},
		Workflow:  "name: demo\n",
	}
	if err := s.Save(snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File exists on disk.
	if _, err := os.Stat(filepath.Join(dir, "exec-test-1.json")); err != nil {
		t.Fatalf("expected file on disk: %v", err)
	}

	got, err := s.Get("exec-test-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "exec-test-1" || got.WorkflowName != "demo" {
		t.Errorf("got %+v", got)
	}
	if len(got.Steps) != 2 || got.Steps[1].Nondeterministic != true {
		t.Errorf("steps not round-tripped: %+v", got.Steps)
	}
	if got.Variables["env"] != "prod" {
		t.Errorf("variables not round-tripped: %v", got.Variables)
	}
}

func TestFileStore_ListNewestFirst(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)

	// Save out of order; List must return newest-first by StartTime.
	for _, snap := range []ExecutionSnapshot{
		{ExecutionSummary: ExecutionSummary{ID: "old", StartTime: "2026-01-01T00:00:00+08:00"}},
		{ExecutionSummary: ExecutionSummary{ID: "new", StartTime: "2026-12-01T00:00:00+08:00"}},
		{ExecutionSummary: ExecutionSummary{ID: "mid", StartTime: "2026-06-01T00:00:00+08:00"}},
	} {
		if err := s.Save(snap); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	if list[0].ID != "new" || list[1].ID != "mid" || list[2].ID != "old" {
		t.Errorf("order wrong: %s %s %s", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestFileStore_ListSkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: "good", StartTime: "2026-01-01T00:00:00+08:00"}}); err != nil {
		t.Fatal(err)
	}
	// Write a corrupt JSON file alongside it.
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not valid"), 0644); err != nil {
		t.Fatal(err)
	}
	// And a non-json file.
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "good" {
		t.Errorf("expected only 'good', got %+v", list)
	}
}

func TestFileStore_Delete(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("x"); err == nil {
		t.Error("expected error after delete")
	}
	// Deleting again is not an error.
	if err := s.Delete("x"); err != nil {
		t.Errorf("re-delete should be no-op, got: %v", err)
	}
}

func TestFileStore_Purge(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	for i, ts := range []string{
		"2026-01-01T00:00:00+08:00",
		"2026-02-01T00:00:00+08:00",
		"2026-03-01T00:00:00+08:00",
	} {
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: "e" + string(rune('a'+i)), StartTime: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.Purge(1)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2", deleted)
	}
	list, _ := s.List()
	if len(list) != 1 || list[0].ID != "ec" {
		t.Errorf("expected only newest kept, got %+v", list)
	}
}

func TestFileStore_PathSanitization(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	// An ID with path separators must not escape the directory.
	snap := ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: "../../etc/passwd", StartTime: "2026-01-01T00:00:00+08:00"}}
	if err := s.Save(snap); err != nil {
		t.Fatal(err)
	}
	// File should be inside dir, sanitized.
	list, _ := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].ID != "../../etc/passwd" {
		t.Errorf("ID should round-trip: %q", list[0].ID)
	}
	// Confirm /etc/passwd was not touched.
	if _, err := os.Stat("/etc/passwd.seneschal-test"); err == nil {
		t.Error("path traversal created a file outside the store dir")
	}
}

// ── Summary index ────────────────────────────────────────────────────────────

// countExecutionFiles returns the number of execution snapshot files in dir
// (excludes the index file and any tmp leftovers).
func countExecutionFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && e.Name() != indexFileName {
			n++
		}
	}
	return n
}

func TestFileStore_ListServedFromIndex(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	for _, id := range []string{"idx-a", "idx-b"} {
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: id, StartTime: "2026-01-01T00:00:00+08:00"}}); err != nil {
			t.Fatal(err)
		}
	}
	// The index must exist after Save.
	if _, err := os.Stat(filepath.Join(dir, indexFileName)); err != nil {
		t.Fatalf("index file not created: %v", err)
	}

	// Corrupt one snapshot file behind the store's back. List answers from the
	// index, so the listing is unaffected (Get would still surface the
	// corruption — it reads the real file).
	if err := os.WriteFile(filepath.Join(dir, "idx-a.json"), []byte("{garbage"), 0644); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List should be unaffected by a corrupt snapshot (index path), got %+v", list)
	}
}

func TestFileStore_IndexRebuildWhenMissing(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	for _, id := range []string{"re-a", "re-b"} {
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: id, StartTime: "2026-01-01T00:00:00+08:00"}}); err != nil {
			t.Fatal(err)
		}
	}
	// Drop the index: the next List must fall back to a full scan and rebuild
	// it, returning the same summaries.
	if err := os.Remove(filepath.Join(dir, indexFileName)); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries after rebuild, got %+v", list)
	}
	if _, err := os.Stat(filepath.Join(dir, indexFileName)); err != nil {
		t.Errorf("index not rebuilt: %v", err)
	}
}

func TestFileStore_IndexRebuildWhenCorrupt(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: "rc-a", StartTime: "2026-01-01T00:00:00+08:00"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFileName), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "rc-a" {
		t.Fatalf("expected rebuilt listing, got %+v", list)
	}
	// The rebuilt index must be valid JSON again.
	data, _ := os.ReadFile(filepath.Join(dir, indexFileName))
	var summaries []ExecutionSummary
	if err := json.Unmarshal(data, &summaries); err != nil || len(summaries) != 1 {
		t.Errorf("rebuilt index invalid: %v (%s)", err, data)
	}
}

func TestFileStore_DeleteUpdatesIndex(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	for _, id := range []string{"del-a", "del-b"} {
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: id, StartTime: "2026-01-01T00:00:00+08:00"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete("del-a"); err != nil {
		t.Fatal(err)
	}
	// Sabotage the remaining snapshot file: List must still reflect the
	// deletion, proving the index (not a scan) is the source.
	if err := os.WriteFile(filepath.Join(dir, "del-b.json"), []byte("{garbage"), 0644); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "del-b" {
		t.Errorf("index not updated by Delete: %+v", list)
	}
}

// ── Atomic writes ────────────────────────────────────────────────────────────

func TestFileStore_NoTmpLeftovers(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	for i, id := range []string{"t1", "t2", "t3"} {
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: id, StartTime: "2026-01-0" + string(rune('1'+i)) + "T00:00:00+08:00"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete("t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("tmp file left behind: %s", e.Name())
		}
	}
}

// ── Rotation ─────────────────────────────────────────────────────────────────

func TestFileStore_RotationKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir, 3)
	// Save 8 executions with increasing StartTime, out of order for realism.
	ids := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8"}
	for i, id := range ids {
		ts := "2026-01-0" + string(rune('1'+i)) + "T00:00:00+08:00"
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: id, StartTime: ts}}); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 kept, got %d: %+v", len(list), list)
	}
	for i, want := range []string{"r8", "r7", "r6"} {
		if list[i].ID != want {
			t.Errorf("list[%d]=%s, want %s", i, list[i].ID, want)
		}
	}
	if n := countExecutionFiles(t, dir); n != 3 {
		t.Errorf("expected 3 execution files on disk, got %d", n)
	}
	// The rotated-out files must be gone; the kept ones must still Get.
	for _, id := range []string{"r1", "r2", "r3", "r4", "r5"} {
		if _, err := os.Stat(filepath.Join(dir, id+".json")); !os.IsNotExist(err) {
			t.Errorf("rotated file still on disk: %s", id)
		}
	}
	if _, err := s.Get("r8"); err != nil {
		t.Errorf("kept execution not readable: %v", err)
	}
}

func TestFileStore_RotationDisabled(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir, 0)
	for i, id := range []string{"k1", "k2", "k3", "k4", "k5"} {
		ts := "2026-01-0" + string(rune('1'+i)) + "T00:00:00+08:00"
		if err := s.Save(ExecutionSnapshot{ExecutionSummary: ExecutionSummary{ID: id, StartTime: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.List()
	if len(list) != 5 {
		t.Errorf("rotation disabled: expected 5, got %d", len(list))
	}
	if n := countExecutionFiles(t, dir); n != 5 {
		t.Errorf("rotation disabled: expected 5 files, got %d", n)
	}
}
