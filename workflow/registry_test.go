package workflow

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// writeYAML is a small helper that writes content to name inside dir.
func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDirRegistry_List(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "deploy.yaml", `name: deploy
description: "deploy the app"
variables:
  env: prod
steps:
  - name: build
    action: shell
    command: go build
  - name: ship
    action: shell
    command: ./ship
`)
	writeYAML(t, dir, "notify.yml", `name: notify
steps:
  - name: ping
    action: log
    message: hi
`)

	r := NewDirRegistry(dir)
	entries, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	// Lexical order: deploy.yaml before notify.yml.
	if entries[0].FileName != "deploy.yaml" {
		t.Errorf("entries[0].FileName = %q, want deploy.yaml", entries[0].FileName)
	}
	if entries[0].Name != "deploy" || entries[0].Description != "deploy the app" {
		t.Errorf("deploy entry metadata wrong: %+v", entries[0])
	}
	if entries[0].Steps != 2 || entries[0].Variables != 1 {
		t.Errorf("deploy counts wrong: steps=%d vars=%d", entries[0].Steps, entries[0].Variables)
	}
}

func TestDirRegistry_ListSkipsUnparseable(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "good.yaml", `name: good
steps:
  - name: s
    action: log
    message: ok
`)
	// malformed: not valid YAML mapping (a bare string).
	writeYAML(t, dir, "broken.yaml", `this is not: [valid yaml`)
	writeYAML(t, dir, "ignore.txt", "not yaml at all")

	r := NewDirRegistry(dir)
	entries, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the parseable workflow, got %d entries", len(entries))
	}
	if entries[0].Name != "good" {
		t.Errorf("expected 'good', got %q", entries[0].Name)
	}
}

func TestDirRegistry_Get(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "deploy-staging.yaml", `name: deploy-staging
description: "deploy to staging"
steps:
  - name: build
    action: shell
    command: make build
`)

	r := NewDirRegistry(dir)

	// Get by YAML name field.
	wf, raw, err := r.Get("deploy-staging")
	if err != nil {
		t.Fatalf("Get by name: %v", err)
	}
	if wf.Name != "deploy-staging" {
		t.Errorf("wf.Name = %q", wf.Name)
	}
	if len(raw) == 0 {
		t.Error("expected non-empty raw bytes")
	}

	// Get by file name with suffix.
	if _, _, err := r.Get("deploy-staging.yaml"); err != nil {
		t.Errorf("Get by file name: %v", err)
	}

	// Get by file stem.
	if _, _, err := r.Get("deploy-staging"); err != nil {
		t.Errorf("Get by stem: %v", err)
	}

	// Miss.
	if _, _, err := r.Get("nonexistent"); err == nil {
		t.Error("expected error for missing workflow")
	}
}

func TestDirRegistry_GetFavorsYamlNameField(t *testing.T) {
	// File named "x.yaml" but workflow name is "the-real-name". Get should
	// resolve by the YAML name field too.
	dir := t.TempDir()
	writeYAML(t, dir, "x.yaml", `name: the-real-name
steps:
  - name: s
    action: log
    message: hi
`)
	r := NewDirRegistry(dir)
	if _, _, err := r.Get("the-real-name"); err != nil {
		t.Errorf("Get by YAML name field: %v", err)
	}
	if _, _, err := r.Get("x"); err != nil {
		t.Errorf("Get by file stem: %v", err)
	}
}

func TestDirRegistry_ListMissingDir(t *testing.T) {
	r := NewDirRegistry("/nonexistent/path/that/does/not/exist")
	if _, err := r.List(); err == nil {
		t.Error("expected error for missing directory")
	}
}

// ── Directory-mtime cache ────────────────────────────────────────────────────

// TestDirRegistry_CacheStaleMetadataOnContentEdit pins the documented
// trade-off: editing a file in place does NOT bump the directory mtime, so
// List keeps serving the cached (stale) metadata until a file is added or
// removed. Get is unaffected — it always re-reads the target file.
func TestDirRegistry_CacheStaleMetadataOnContentEdit(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "app.yaml", `name: app
description: "v1"
steps:
  - name: s1
    action: log
    message: one
`)

	r := NewDirRegistry(dir)
	entries, err := r.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Description != "v1" || entries[0].Steps != 1 {
		t.Fatalf("initial List: %+v", entries)
	}

	// Edit the file in place: no add/remove, so the dir mtime is unchanged
	// and the cache is (deliberately) not invalidated.
	writeYAML(t, dir, "app.yaml", `name: app
description: "v2"
steps:
  - name: s1
    action: log
    message: one
  - name: s2
    action: log
    message: two
`)

	entries, err = r.List()
	if err != nil {
		t.Fatalf("List after edit: %v", err)
	}
	if len(entries) != 1 || entries[0].Description != "v1" || entries[0].Steps != 1 {
		t.Errorf("List should serve stale cached metadata after content edit, got %+v", entries)
	}

	// Get always re-reads the file: it sees the new content immediately.
	wf, _, err := r.Get("app")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if wf.Description != "v2" || len(wf.Steps) != 2 {
		t.Errorf("Get should bypass the stale cache: desc=%q steps=%d", wf.Description, len(wf.Steps))
	}
}

// TestDirRegistry_CacheInvalidatedOnAdd: adding a file bumps the directory
// mtime, so the next List rebuilds the cache and sees it.
func TestDirRegistry_CacheInvalidatedOnAdd(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "one.yaml", "name: one\nsteps:\n  - name: s\n    action: log\n    message: hi\n")

	r := NewDirRegistry(dir)
	if entries, _ := r.List(); len(entries) != 1 {
		t.Fatalf("initial List: %+v", entries)
	}

	writeYAML(t, dir, "two.yaml", "name: two\nsteps:\n  - name: s\n    action: log\n    message: hi\n")
	// Force a distinct dir mtime so the test does not depend on filesystem
	// timestamp granularity (creation already bumps it; this makes it certain).
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(dir, future, future); err != nil {
		t.Fatal(err)
	}

	entries, err := r.List()
	if err != nil {
		t.Fatalf("List after add: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected cache rebuild after add, got %+v", entries)
	}
	if entries[0].FileName != "one.yaml" || entries[1].FileName != "two.yaml" {
		t.Errorf("unexpected entries: %+v", entries)
	}

	// The new file is resolvable by Get through the rebuilt maps.
	if _, _, err := r.Get("two"); err != nil {
		t.Errorf("Get new workflow: %v", err)
	}
}

// TestDirRegistry_ConcurrentAccess hammers List/Get from many goroutines
// while a writer adds and removes files to force cache rebuilds. Run with
// -race: the cache maps must be guarded by the RWMutex.
func TestDirRegistry_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yaml", "name: base\nsteps:\n  - name: s\n    action: log\n    message: hi\n")

	r := NewDirRegistry(dir)
	stop := make(chan struct{})
	writerDone := make(chan struct{})

	// Writer: churn the directory so the cache is repeatedly invalidated and
	// rebuilt under the readers.
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			name := filepath.Join(dir, "churn.yaml")
			_ = os.WriteFile(name, []byte("name: churn\nsteps:\n  - name: s\n    action: log\n    message: hi\n"), 0644)
			_ = os.Remove(name)
		}
	}()

	// Readers.
	var wg sync.WaitGroup
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if _, err := r.List(); err != nil {
					t.Errorf("List: %v", err)
					return
				}
				if _, _, err := r.Get("base"); err != nil {
					t.Errorf("Get by name: %v", err)
					return
				}
				if _, _, err := r.Get("base.yaml"); err != nil {
					t.Errorf("Get by file: %v", err)
					return
				}
				_, _, _ = r.Get("missing")
			}
		}()
	}

	wg.Wait() // readers finish first, then stop the writer
	close(stop)
	<-writerDone
}
