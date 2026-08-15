package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunbookManager_LoadListGetResolveTrigger(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(workflowsDir, "deploy.yaml"), []byte("name: deploy\nsteps: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rbYAML := `name: deploy-nightly
workflow: deploy.yaml
triggers:
  - type: cron
    cron: "0 2 * * *"
  - type: manual
variables:
  env: prod
`
	if err := os.WriteFile(filepath.Join(dir, "nightly.yaml"), []byte(rbYAML), 0644); err != nil {
		t.Fatal(err)
	}

	var gotRB *RunbookConfig
	var gotVars map[string]string
	m := NewRunbookManager(dir, workflowsDir, func(rb *RunbookConfig, extraVars map[string]string) (string, error) {
		gotRB = rb
		gotVars = extraVars
		return "exec-1", nil
	}, nil)

	if err := m.LoadDir(); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	list := m.List()
	if len(list) != 1 {
		t.Fatalf("List len=%d, want 1", len(list))
	}
	if m.Get("deploy-nightly") == nil {
		t.Fatal("Get(deploy-nightly) returned nil")
	}
	rb := m.Get("deploy-nightly")
	if rb.Variables["env"] != "prod" {
		t.Errorf("variables not loaded: %+v", rb.Variables)
	}

	path, err := m.ResolveWorkflowPath(rb)
	if err != nil {
		t.Fatalf("ResolveWorkflowPath: %v", err)
	}
	if path != filepath.Join(workflowsDir, "deploy.yaml") {
		t.Errorf("resolved path=%q, want workflows dir candidate", path)
	}

	execID, err := m.Trigger("deploy-nightly", map[string]string{"branch": "main"})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if execID != "exec-1" {
		t.Errorf("execID=%q, want exec-1", execID)
	}
	if gotRB == nil || gotRB.Name != "deploy-nightly" {
		t.Errorf("trigger callback did not receive the runbook: %+v", gotRB)
	}
	if gotVars["branch"] != "main" || gotVars[TriggerSourceExtraVar] != string(TriggerManual) {
		t.Errorf("extraVars not merged with trigger source: %+v", gotVars)
	}
}

func TestRunbookManager_TriggerByPath(t *testing.T) {
	dir := t.TempDir()
	workflowsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workflowsDir, "wf.yaml"), []byte("name: wf\nsteps: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rbYAML := `name: webhook-rb
workflow: wf.yaml
triggers:
  - type: webhook
    path: deploy
`
	if err := os.WriteFile(filepath.Join(dir, "webhook.yaml"), []byte(rbYAML), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewRunbookManager(dir, workflowsDir, func(rb *RunbookConfig, extraVars map[string]string) (string, error) {
		return "exec-web", nil
	}, nil)
	if err := m.LoadDir(); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}

	if _, err := m.TriggerByPath("deploy", nil); err != nil {
		t.Fatalf("TriggerByPath(deploy): %v", err)
	}
	if _, err := m.TriggerByPath("nope", nil); err == nil {
		t.Fatal("TriggerByPath(nope) should fail")
	}
}

func TestRunbookManager_CronOnlyNotManuallyTriggerable(t *testing.T) {
	dir := t.TempDir()
	rbYAML := `name: cron-only
workflow: wf.yaml
triggers:
  - type: cron
    cron: "0 2 * * *"
`
	if err := os.WriteFile(filepath.Join(dir, "cron.yaml"), []byte(rbYAML), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewRunbookManager(dir, t.TempDir(), func(rb *RunbookConfig, extraVars map[string]string) (string, error) {
		return "exec-cron", nil
	}, nil)
	if err := m.LoadDir(); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	_, err := m.Trigger("cron-only", nil)
	if err == nil || !strings.Contains(err.Error(), "no manual/webhook trigger") {
		t.Fatalf("expected no manual/webhook trigger error, got %v", err)
	}
}

func TestRunbookManager_TriggerDispatchErrorWraps(t *testing.T) {
	dir := t.TempDir()
	rbYAML := `name: bad
workflow: wf.yaml
`
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(rbYAML), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewRunbookManager(dir, t.TempDir(), func(rb *RunbookConfig, extraVars map[string]string) (string, error) {
		return "", errors.New("broken reference")
	}, nil)
	if err := m.LoadDir(); err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	_, err := m.Trigger("bad", nil)
	if !errors.Is(err, ErrTriggerDispatch) {
		t.Fatalf("expected ErrTriggerDispatch, got %v", err)
	}
}
