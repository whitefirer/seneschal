package workflow

import (
	"os"
	"path/filepath"
	"testing"
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
