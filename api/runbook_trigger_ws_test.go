package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whitefirer/seneschal/workflow"
)

// Hub observability in these tests: WSHub has no exported tap, but the tests
// are white-box (package api), so they read the unexported broadcast channel
// directly — a hub created without Run() queues events instead of fanning
// them out. No production test hook needed. Broadcasts happen synchronously
// inside the trigger callback, so once a trigger call returns, its event is
// already queued.

// nextWSEvent drains one event from the hub, failing if none arrives.
func nextWSEvent(t *testing.T, hub *WSHub) WSProgressEvent {
	t.Helper()
	select {
	case ev := <-hub.broadcast:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("expected a WS event, none broadcast")
		return WSProgressEvent{}
	}
}

func writeTriggerTestWorkflow(t *testing.T, dir string) {
	t.Helper()
	yaml := "name: simple\nsteps:\n  - name: greet\n    action: log\n    message: hello\n"
	if err := os.WriteFile(filepath.Join(dir, "simple.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTriggerCallback_BroadcastsRunbookTrigger(t *testing.T) {
	workflowsDir := t.TempDir()
	writeTriggerTestWorkflow(t, workflowsDir)
	store := workflow.NewFileStore(filepath.Join(t.TempDir(), "execs"))
	hub := NewWSHub() // no Run(): events queue in hub.broadcast

	cb := MakeTriggerCallback(store, hub, workflowsDir, workflow.AIConfig{})
	rb := &workflow.RunbookConfig{Name: "nightly", Workflow: "simple.yaml"}

	execID, err := cb(rb, map[string]string{
		workflow.TriggerSourceExtraVar: "manual",
		"env":                          "prod",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !strings.HasPrefix(execID, "runbook-nightly-") {
		t.Errorf("execID=%q want runbook-nightly- prefix", execID)
	}

	ev := nextWSEvent(t, hub)
	if ev.Type != "runbook_trigger" {
		t.Errorf("Type=%q want runbook_trigger", ev.Type)
	}
	if ev.RunbookName != "nightly" {
		t.Errorf("RunbookName=%q want nightly", ev.RunbookName)
	}
	if ev.Source != "manual" {
		t.Errorf("Source=%q want manual", ev.Source)
	}
	if ev.ExecutionID != execID {
		t.Errorf("ExecutionID=%q want %q", ev.ExecutionID, execID)
	}
	if ev.WorkflowName != "simple" {
		t.Errorf("WorkflowName=%q want simple", ev.WorkflowName)
	}
	if ev.WorkflowFile != "simple.yaml" {
		t.Errorf("WorkflowFile=%q want simple.yaml", ev.WorkflowFile)
	}
	if ev.Status != "triggered" {
		t.Errorf("Status=%q want triggered", ev.Status)
	}
	if ev.Timestamp == "" {
		t.Error("Timestamp must be set")
	}
	if ev.LogLevel != "INFO" || !strings.Contains(ev.LogMessage, "nightly") || !strings.Contains(ev.LogMessage, execID) {
		t.Errorf("log line should mention runbook + execution: level=%q msg=%q", ev.LogLevel, ev.LogMessage)
	}

	// The reserved source key is metadata: it must be stripped before
	// extraVars merge into the execution environment. The run is async —
	// poll the store until its snapshot lands.
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap, err := store.Get(execID)
		if err == nil {
			if _, leaked := snap.Variables[workflow.TriggerSourceExtraVar]; leaked {
				t.Error("reserved trigger-source key leaked into execution variables")
			}
			if snap.Variables["env"] != "prod" {
				t.Errorf("env=%q want prod (user extra vars must survive)", snap.Variables["env"])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runbook execution snapshot not persisted")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTriggerCallback_BroadcastsDispatchFailure(t *testing.T) {
	workflowsDir := t.TempDir() // no workflow files — dispatch must fail
	hub := NewWSHub()

	cb := MakeTriggerCallback(nil, hub, workflowsDir, workflow.AIConfig{})
	rb := &workflow.RunbookConfig{Name: "broken", Workflow: "missing.yaml"}

	execID, err := cb(rb, map[string]string{workflow.TriggerSourceExtraVar: "cron"})
	if err == nil {
		t.Fatal("expected dispatch error")
	}
	if execID != "" {
		t.Errorf("execID=%q want empty on dispatch failure", execID)
	}

	ev := nextWSEvent(t, hub)
	if ev.Type != "runbook_trigger" {
		t.Errorf("Type=%q want runbook_trigger", ev.Type)
	}
	if ev.RunbookName != "broken" {
		t.Errorf("RunbookName=%q want broken", ev.RunbookName)
	}
	if ev.Source != "cron" {
		t.Errorf("Source=%q want cron", ev.Source)
	}
	if ev.Status != "failed" {
		t.Errorf("Status=%q want failed", ev.Status)
	}
	if !strings.Contains(ev.Error, "not found") {
		t.Errorf("Error=%q should describe the missing workflow", ev.Error)
	}
	if ev.ExecutionID != "" {
		t.Errorf("ExecutionID=%q want empty (nothing started)", ev.ExecutionID)
	}
	if ev.LogLevel != "ERROR" {
		t.Errorf("LogLevel=%q want ERROR", ev.LogLevel)
	}
	if ev.Timestamp == "" {
		t.Error("Timestamp must be set")
	}
}

// Source labeling end-to-end through the manager: Trigger → manual,
// TriggerByPath → webhook, the cron scheduler → cron. The cron runbook uses
// the "1s" Go-duration shortcut so the test doesn't wait minutes.
func TestRunbookTrigger_SourceLabeling(t *testing.T) {
	workflowsDir := t.TempDir()
	writeTriggerTestWorkflow(t, workflowsDir)
	runbooksDir := t.TempDir()

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(runbooksDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("manual-rb.yaml", "name: manual-rb\nworkflow: simple.yaml\ntriggers:\n  - type: manual\n")
	write("hook-rb.yaml", "name: hook-rb\nworkflow: simple.yaml\ntriggers:\n  - type: webhook\n    path: /hook-x\n")
	cronFile := filepath.Join(runbooksDir, "cron-rb.yaml")
	if err := os.WriteFile(cronFile, []byte("name: cron-rb\nworkflow: simple.yaml\ntriggers:\n  - type: cron\n    cron: 1s\n"), 0644); err != nil {
		t.Fatal(err)
	}

	hub := NewWSHub()
	mgr := workflow.NewRunbookManager(runbooksDir, workflowsDir,
		MakeTriggerCallback(nil, hub, workflowsDir, workflow.AIConfig{}), nil)
	if err := mgr.LoadDir(); err != nil {
		t.Fatal(err)
	}
	// Stop the cron ticker at test end: remove the file and reload.
	t.Cleanup(func() {
		os.Remove(cronFile)
		mgr.LoadDir()
	})

	if _, err := mgr.Trigger("manual-rb", nil); err != nil {
		t.Fatalf("manual trigger: %v", err)
	}
	if _, err := mgr.TriggerByPath("/hook-x", nil); err != nil {
		t.Fatalf("webhook trigger: %v", err)
	}

	// Collect runbook_trigger events until all three sources have reported.
	seen := make(map[string]WSProgressEvent)
	deadline := time.Now().Add(5 * time.Second)
	for len(seen) < 3 && time.Now().Before(deadline) {
		select {
		case ev := <-hub.broadcast:
			if ev.Type == "runbook_trigger" {
				seen[ev.RunbookName] = ev
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	for name, wantSource := range map[string]string{
		"manual-rb": "manual",
		"hook-rb":   "webhook",
		"cron-rb":   "cron",
	} {
		ev, ok := seen[name]
		if !ok {
			t.Errorf("no runbook_trigger event for %s", name)
			continue
		}
		if ev.Source != wantSource {
			t.Errorf("%s: Source=%q want %q", name, ev.Source, wantSource)
		}
		if ev.ExecutionID == "" {
			t.Errorf("%s: expected an execution ID in the event", name)
		}
	}
}

// The event payload must serialize the new fields for runbook_trigger and —
// just as important — leave existing event shapes byte-identical (omitempty)
// so current frontend parsing is unaffected.
func TestWSProgressEvent_RunbookTriggerJSON(t *testing.T) {
	data, err := json.Marshal(WSProgressEvent{
		Type:         "runbook_trigger",
		RunbookName:  "nightly",
		Source:       "cron",
		ExecutionID:  "runbook-nightly-ab12",
		WorkflowName: "simple",
		Status:       "triggered",
		Timestamp:    "2026-01-02T03:04:05Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, frag := range []string{
		`"type":"runbook_trigger"`,
		`"runbookName":"nightly"`,
		`"source":"cron"`,
		`"executionId":"runbook-nightly-ab12"`,
		`"status":"triggered"`,
		`"timestamp":"2026-01-02T03:04:05Z"`,
	} {
		if !strings.Contains(s, frag) {
			t.Errorf("payload missing %s: %s", frag, s)
		}
	}

	// Existing events must not grow the new keys.
	end, err := json.Marshal(WSProgressEvent{
		Type:        "workflow_end",
		ExecutionID: "exec-1",
		Status:      "success",
		Timestamp:   "2026-01-02T03:04:05Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(end), "runbookName") || strings.Contains(string(end), "source") {
		t.Errorf("workflow_end payload must not contain runbook fields: %s", end)
	}
}
