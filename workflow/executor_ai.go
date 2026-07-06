package workflow

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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

// execAI runs the "ai" action. In TUI mode (realtimePrinter set) it streams
// tokens and emits ai_token events for incremental display; otherwise it
// completes non-streaming and returns the full text. The generated text is
// stored via step.SaveOutput (if set) and returned as the step output.
func (e *Executor) execAI(step Step, stepID string, depth int, parentID string) (string, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// TUI mode: stream token-by-token, emitting ai_token events so the detail
	// view can render incrementally. Non-TUI: a single Complete call (avoids
	// per-token logging noise in CI/plain output).
	var resp ai.Response
	if e.realtimePrinter != nil {
		resp, err = e.aiProvider.Stream(ctx, req, func(token string) {
			e.sendAIToken(step.Name, stepID, step.Action, depth, parentID, token)
		})
	} else {
		resp, err = e.aiProvider.Complete(ctx, req)
	}
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

// execAIDecide runs the "ai_decide" action: a semantic yes/no judgment. It
// asks the model the step's Question (with a forced "answer only true or
// false" instruction), parses the boolean, stores it as "true"/"false" string
// via step.SaveOutput (so it can flow into a condition's expression), and
// returns a human-readable line as the step output. Marks the step
// Nondeterministic via the executeStep dispatch.
func (e *Executor) execAIDecide(step Step, stepID string, depth int, parentID string) (bool, error) {
	if e.aiProvider == nil {
		return false, fmt.Errorf("ai_decide step '%s': no AI provider configured (set ai: in the workflow and ANTHROPIC_API_KEY/DEEPSEEK_API_KEY in the environment)", step.Name)
	}

	question, err := e.context.ResolveTemplate(step.Question)
	if err != nil {
		return false, fmt.Errorf("ai_decide step '%s': resolve question template: %w", step.Name, err)
	}
	if strings.TrimSpace(question) == "" {
		return false, fmt.Errorf("ai_decide step '%s': question is empty", step.Name)
	}

	// Force the model to answer only true/false. We keep the prompt minimal so
	// parsing stays robust: take the first token, strip punctuation, accept
	// true/yes/1/是 vs false/no/0/否/否.
	prompt := question + "\n\n请只回答 true 或 false,不要任何其他内容。"

	system, err := e.context.ResolveTemplate(step.System)
	if err != nil {
		return false, fmt.Errorf("ai_decide step '%s': resolve system template: %w", step.Name, err)
	}

	req := ai.Request{
		System:      system,
		Prompt:      prompt,
		Inputs:      extractInputs(question, step.Inputs, e.context.Snapshot()),
		Model:       e.aiModel,
		MaxTokens:   e.aiMaxTokens,
		// Decisions want determinism; force temperature 0 unless the workflow
		// explicitly set one for creative judging.
		Temperature: e.aiTemperature,
	}
	// Cap decide responses tightly — the answer is one token.
	if req.MaxTokens == 0 {
		req.MaxTokens = 16
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Decide uses Complete (no streaming): the answer is a single token and
	// per-token display adds no value.
	resp, err := e.aiProvider.Complete(ctx, req)
	if err != nil {
		return false, fmt.Errorf("ai_decide step '%s': %w", step.Name, err)
	}

	result := parseBoolAnswer(resp.Text)
	// Store "true"/"false" so a downstream condition can compare:
	//   expression: "{{.is_urgent}} == true"
	if step.SaveOutput != "" {
		e.context.Set(step.SaveOutput, strconv.FormatBool(result))
	}
	e.context.SetResult(step.Name, strconv.FormatBool(result))

	return result, nil
}

// parseBoolAnswer tolerantly extracts a boolean from a model's text response.
// Models are instructed to answer only true/false, but occasionally wrap it
// in prose ("The answer is true.") or add punctuation. Strategy:
//  1. Take the first whitespace/punctuation-delimited token; if it is a
//     recognized true/false spelling, use it.
//  2. Otherwise scan the whole lowercased text for any true/false keyword.
func parseBoolAnswer(text string) bool {
	low := strings.ToLower(text)
	t := strings.TrimSpace(low)
	if t == "" {
		return false
	}
	// First-token check.
	first := t
	for _, r := range []string{" ", "\n", "\t", ",", ".", ";", "。", "，", "；", "!", "?"} {
		if i := strings.Index(first, r); i >= 0 {
			first = first[:i]
		}
	}
	if isTrueToken(first) {
		return true
	}
	if isFalseToken(first) {
		return false
	}
	// Fallback: scan for any keyword anywhere. Prefer the earliest match to
	// handle "not false" style answers deterministically.
	for i := 0; i < len(low); i++ {
		rest := low[i:]
		if atKeyword(rest, "true", "yes", "是", "对", "正确", "1") {
			return true
		}
		if atKeyword(rest, "false", "no", "否", "错", "0") {
			return false
		}
	}
	return false
}

func isTrueToken(t string) bool {
	switch t {
	case "true", "yes", "1", "是", "对", "正确":
		return true
	}
	return false
}

func isFalseToken(t string) bool {
	switch t {
	case "false", "no", "0", "否", "错", "错误":
		return true
	}
	return false
}

// atKeyword reports whether s starts with any of the keywords at a token
// boundary (so "true" matches but "truly" does not).
func atKeyword(s string, kws ...string) bool {
	for _, kw := range kws {
		if !strings.HasPrefix(s, kw) {
			continue
		}
		// Boundary check: end of string or a non-alphanumeric follows.
		if len(s) == len(kw) {
			return true
		}
		next := s[len(kw)]
		if !isAlphaNum(next) {
			return true
		}
	}
	return false
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

