package ai

import (
	"strings"
)

// extractJSON pulls a JSON object out of an LLM response that may wrap it in
// a markdown fenced block (```json ... ```) or surround it with prose. The
// model is instructed to emit raw JSON, but robustness here avoids whole-flow
// failures when it adds fences or a leading sentence.
//
// Strategy:
//  1. If there is a ``` fence, take the first fenced contents (after an
//     optional "json" language tag).
//  2. Otherwise locate the first '{' and the matching last '}' and return
//     that substring — this tolerates "Sure! Here is the result: {...}".
//  3. Fall back to the trimmed original.
func extractJSON(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return t
	}

	// Try a fenced block first.
	fence := "```"
	if i := strings.Index(t, fence); i >= 0 {
		after := t[i+len(fence):]
		// Skip an optional language tag line (e.g. "json").
		if nl := strings.IndexByte(after, '\n'); nl >= 0 {
			after = after[nl+1:]
		}
		if j := strings.Index(after, fence); j >= 0 {
			return strings.TrimSpace(after[:j])
		}
		return strings.TrimSpace(after)
	}

	// Otherwise carve out the outermost { ... }.
	start := strings.IndexByte(t, '{')
	end := strings.LastIndexByte(t, '}')
	if start >= 0 && end > start {
		return t[start : end+1]
	}
	return t
}

// parseSelection is the JSON shape returned by SelectWorkflow.
type parseSelection struct {
	Workflow   string            `json:"workflow"`
	Variables  map[string]string `json:"variables"`
	Confidence float64           `json:"confidence"`
}
