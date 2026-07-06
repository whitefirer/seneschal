package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore_SaveGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)

	snap := ExecutionSnapshot{
		ExecutionSummary: ExecutionSummary{
			ID:            "exec-test-1",
			WorkflowName:  "demo",
			Status:        "success",
			StartTime:     "2026-07-07T10:00:00+08:00",
			EndTime:       "2026-07-07T10:00:05+08:00",
			StepsCount:    2,
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
	if _, err := os.Stat("/etc/passwd.goworkflow-test"); err == nil {
		t.Error("path traversal created a file outside the store dir")
	}
}
