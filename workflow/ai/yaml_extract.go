package ai

import "strings"

// extractYAML pulls a YAML document out of an LLM response that may wrap it
// in a markdown fenced block (```yaml ... ``` or ``` ... ```) and may add
// leading/trailing prose. The model is instructed to emit raw YAML, but in
// practice it frequently adds fences — this makes parsing robust regardless.
//
// Strategy:
//  1. If the text contains a ``` fenced block, return the contents of the
//     first fence (after stripping an optional "yaml"/"yml" language tag).
//  2. Otherwise return the text trimmed of surrounding whitespace.
func extractYAML(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return t
	}

	// Find the first fence.
	fence := "```"
	startIdx := strings.Index(t, fence)
	if startIdx < 0 {
		return t
	}
	// Content begins after the fence and an optional language tag on the same
	// line.
	after := t[startIdx+len(fence):]
	// Drop the rest of the opening fence line (e.g. "yaml\n").
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		after = after[nl+1:]
	} else {
		// One-liner fence with no newline; nothing useful inside.
		return strings.TrimSpace(after)
	}

	// Find the closing fence.
	endIdx := strings.Index(after, fence)
	if endIdx < 0 {
		// Unclosed fence — return what we have.
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(after[:endIdx])
}
