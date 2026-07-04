package workflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"goworkflow/workflow/ai"
)

// templateVarRe matches {{.name}} template variables to determine which
// workflow variables are referenced by an AI prompt. Used by extractInputs
// for the conservative default of only exposing referenced variables to the
// model (cost / leakage control — see docs/PRODUCT.md "上下文注入策略").
var templateVarRe = regexp.MustCompile(`\{\{\s*\.([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// extractInputs decides which workflow variables to expose to the AI model.
//
// If step.Inputs is non-empty, exactly those named variables are passed
// (missing ones are silently dropped). Otherwise, only variables referenced
// in promptText via {{.name}} are passed. This is the conservative default.
func extractInputs(promptText string, stepInputs []string, allVars map[string]string) map[string]string {
	inputs := make(map[string]string)

	if len(stepInputs) > 0 {
		// Explicit allow-list: pass exactly the requested vars that exist.
		for _, name := range stepInputs {
			if v, ok := allVars[name]; ok {
				inputs[name] = v
			}
		}
		return inputs
	}

	// Default: scan the prompt for {{.name}} references.
	for _, m := range templateVarRe.FindAllStringSubmatch(promptText, -1) {
		name := m[1]
		if v, ok := allVars[name]; ok {
			inputs[name] = v
		}
	}
	return inputs
}

// execAI runs the "ai" action: a non-streaming (M1) text completion. The
// generated text is stored via step.SaveOutput (if set) and returned as the
// step output.
func (e *Executor) execAI(step Step) (string, error) {
	if e.aiProvider == nil {
		return "", fmt.Errorf("ai step '%s': no AI provider configured (set ai: in the workflow and ANTHROPIC_API_KEY/DEEPSEEK_API_KEY in the environment)", step.Name)
	}

	prompt, err := e.context.ResolveTemplate(step.Prompt)
	if err != nil {
		return "", fmt.Errorf("ai step '%s': resolve prompt template: %w", step.Name, err)
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("ai step '%s': prompt is empty", step.Name)
	}

	system, err := e.context.ResolveTemplate(step.System)
	if err != nil {
		return "", fmt.Errorf("ai step '%s': resolve system template: %w", step.Name, err)
	}

	req := ai.Request{
		System:      system,
		Prompt:      prompt,
		Inputs:      extractInputs(prompt, step.Inputs, e.context.Snapshot()),
		Model:       e.aiModel,
		MaxTokens:   e.aiMaxTokens,
		Temperature: e.aiTemperature,
	}

	// A per-step timeout keeps a runaway model from blocking the workflow.
	// Default 120s; M1 does not expose a per-step override yet.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := e.aiProvider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("ai step '%s': %w", step.Name, err)
	}

	// Persist the generated text into a variable for downstream steps.
	if step.SaveOutput != "" {
		e.context.Set(step.SaveOutput, resp.Text)
	}
	e.context.SetResult(step.Name, resp.Text)

	// Annotate output with token usage when available, so verbose mode shows
	// cost. Keep the raw text as the canonical output for consumers.
	output := resp.Text
	if resp.InputTokens > 0 || resp.OutputTokens > 0 {
		output = fmt.Sprintf("%s\n[tokens: in=%d out=%d]", resp.Text, resp.InputTokens, resp.OutputTokens)
	}

	return output, nil
}

