package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/whitefirer/seneschal/workflow/ai"
)

// HookPhase identifies when in the lifecycle a hook fires.
type HookPhase string

const (
	HookAfterStep   HookPhase = "after_step"
	HookWorkflowEnd HookPhase = "workflow_end"
)

// HookConfig is a single hook declaration from YAML.
type HookConfig struct {
	On      HookPhase `yaml:"on"`                // after_step | workflow_end
	When    string    `yaml:"when,omitempty"`    // success | failed | always (default)
	Type    string    `yaml:"type"`              // webhook | shell | ai
	URL     string    `yaml:"url,omitempty"`     // webhook: target URL
	Message string    `yaml:"message,omitempty"` // webhook: optional message template
	Command string    `yaml:"command,omitempty"` // shell: command to run
	Mode    string    `yaml:"mode,omitempty"`    // ai: "ai" (suggest) or "ai_auto" (auto)
	Prompt  string    `yaml:"prompt,omitempty"`  // ai: custom analysis prompt
}

// HookEvent is the runtime context passed to hooks.
type HookEvent struct {
	Phase        HookPhase
	StepName     string
	Action       string
	Status       string // success | failed | skipped
	Output       string
	Error        string
	Duration     string
	WorkflowName string
	Variables    map[string]string
}

// shouldFire reports whether a hook should fire for this event.
func (h HookConfig) shouldFire(event HookEvent) bool {
	if h.On != event.Phase {
		return false
	}
	when := h.When
	if when == "" || when == "always" {
		return true
	}
	if when == event.Status {
		return true
	}
	// "failed" also fires on "skipped" for error-handling hooks.
	if when == "failed" && event.Status == "skipped" {
		return true
	}
	return false
}

// HookResult describes what a hook decided (for ai hooks that affect control flow).
type HookResult struct {
	Action string // "" (no effect) | "skip" | "abort" | "retry"
	Reason string
}

// executeHook dispatches to the appropriate handler based on hook type.
// For "ai" hooks, returns a HookResult that may affect control flow.
// For "webhook" and "shell", always returns HookResult{} (no control flow effect).
func executeHook(hook HookConfig, event HookEvent, e *Executor) HookResult {
	if !hook.shouldFire(event) {
		return HookResult{}
	}

	switch hook.Type {
	case "webhook":
		fireWebhook(hook, event)
		return HookResult{}
	case "shell":
		fireShellHook(hook, event)
		return HookResult{}
	case "ai":
		return fireAIHook(hook, event, e)
	default:
		if e.verbose {
			fmt.Printf("  ⚠️ unknown hook type: %s\n", hook.Type)
		}
		return HookResult{}
	}
}

// fireWebhook sends a JSON POST to the hook URL. Non-blocking: errors are
// logged but don't fail the workflow. 5s timeout.
func fireWebhook(hook HookConfig, event HookEvent) {
	payload := map[string]interface{}{
		"workflow":  event.WorkflowName,
		"step":      event.StepName,
		"action":    event.Action,
		"status":    event.Status,
		"output":    truncateOutput(event.Output, 1000),
		"error":     event.Error,
		"duration":  event.Duration,
		"timestamp": Now(),
	}
	if hook.Message != "" {
		payload["message"] = resolveHookTemplate(hook.Message, event)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(hook.URL, "application/json", bytes.NewReader(data))
	if err != nil {
		// Webhook failures are non-fatal — log and move on.
		return
	}
	resp.Body.Close()
}

// fireShellHook runs a shell command with template substitution. Non-blocking.
func fireShellHook(hook HookConfig, event HookEvent) {
	cmd := resolveHookTemplate(hook.Command, event)
	if cmd == "" {
		return
	}
	// Run via sh -c for flexibility (pipes, redirects, etc.)
	c := exec.CommandContext(context.Background(), "sh", "-c", cmd)
	c.Env = buildHookEnv(event)
	// Discard output — hook side effects matter, not stdout.
	c.Run()
}

// fireAIHook calls the AI to analyze the error and returns a decision.
// This reuses the existing AnalyzeError logic from error_analysis.go.
func fireAIHook(hook HookConfig, event HookEvent, e *Executor) HookResult {
	if e.aiProvider == nil {
		return HookResult{Action: "suggest", Reason: "no AI provider configured"}
	}

	// Derived from the execution context so a canceled run aborts the call.
	ctx, cancel := context.WithTimeout(e.executionContext(), 120*time.Second)
	defer cancel()

	mode := hook.Mode
	if mode == "" {
		mode = "ai" // suggest-only by default
	}

	asst := ai.NewAssistant(e.aiProvider)
	decision, err := asst.AnalyzeError(ctx, ai.ErrorAnalysisParams{
		StepName:     event.StepName,
		Action:       event.Action,
		Command:      event.Output, // best available context
		Output:       event.Output,
		Error:        event.Error,
		CustomPrompt: hook.Prompt,
	})
	if err != nil {
		return HookResult{Action: "suggest", Reason: err.Error()}
	}

	if e.verbose {
		fmt.Printf("  🤖 hook[ai]: %s (%s)\n", decision.Action, decision.Reason)
	}

	// Only "auto" mode acts on retry/skip/abort. Otherwise just suggest.
	if mode != "ai_auto" {
		return HookResult{Action: "suggest", Reason: decision.Reason}
	}

	return HookResult{Action: decision.Action, Reason: decision.Reason}
}

// resolveHookTemplate replaces {{.step_name}}, {{.status}}, etc. in a template string.
func resolveHookTemplate(tmpl string, event HookEvent) string {
	r := strings.NewReplacer(
		"{{.step_name}}", event.StepName,
		"{{.workflow_name}}", event.WorkflowName,
		"{{.status}}", event.Status,
		"{{.action}}", event.Action,
		"{{.duration}}", event.Duration,
		"{{.error}}", event.Error,
		"{{.output}}", truncateOutput(event.Output, 500),
	)
	return r.Replace(tmpl)
}

// buildHookEnv creates env vars from the HookEvent for shell hooks.
func buildHookEnv(event HookEvent) []string {
	return []string{
		fmt.Sprintf("HOOK_STEP=%s", event.StepName),
		fmt.Sprintf("HOOK_STATUS=%s", event.Status),
		fmt.Sprintf("HOOK_WORKFLOW=%s", event.WorkflowName),
		fmt.Sprintf("HOOK_ACTION=%s", event.Action),
		fmt.Sprintf("HOOK_DURATION=%s", event.Duration),
		fmt.Sprintf("HOOK_ERROR=%s", event.Error),
	}
}

func truncateOutput(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
