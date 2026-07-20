package api

import (
	"sort"
	"strings"

	"github.com/whitefirer/seneschal/workflow"
)

// Response-time masking for sensitive workflow variables. The engine masks
// step outputs when it finalizes a WorkflowResult (see workflow/mask.go), but
// the resolved variable map keeps real values — stored snapshots need them so
// replay can restore the original environment. The API layer therefore masks
// only at response serialization: cached details and on-disk snapshots keep
// real values, and every response exit scrubs them.

// maskForResponse applies sensitive-variable masking to a detail right before
// it is serialized into a response: the variable map is replaced by its
// masked copy, and any known sensitive value is scrubbed from retained log
// lines (live step_output events are stored raw in Logs — masking them there
// would garble the real-time stream, so they are cleaned here instead).
//
// The receiver must be a private copy (deepCopy or a fresh snapshotToDetail
// result): the cached detail keeps real values.
func (e *ExecutionDetail) maskForResponse() {
	if len(e.SensitivePatterns) == 0 {
		return
	}
	masked := workflow.MaskVariables(e.Variables, e.SensitivePatterns)
	values := diffSensitiveValues(e.Variables, masked)
	e.Variables = masked
	if len(values) == 0 {
		return
	}
	for i := range e.Logs {
		for _, v := range values {
			if strings.Contains(e.Logs[i].Message, v) {
				e.Logs[i].Message = strings.ReplaceAll(e.Logs[i].Message, v, "******")
			}
		}
	}
}

// diffSensitiveValues returns the real values that masking hid — i.e. the
// values of variables whose keys match the sensitive patterns — longest first
// so a longer secret is replaced before any shorter value that happens to be
// a substring of it. Values shorter than 2 chars are skipped: replacing them
// would mangle unrelated text. This mirrors workflow.sensitiveValues (which
// is unexported) using only the exported MaskVariables.
func diffSensitiveValues(real, masked map[string]string) []string {
	var values []string
	for k, v := range real {
		if len(v) < 2 {
			continue
		}
		if masked[k] != v {
			values = append(values, v)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

// sensitivePatternsFromYAML extracts the workflow's sensitive variable
// patterns from stored workflow YAML. It returns nil when the YAML is missing
// or unparseable — in that case no masking is applied, matching the engine's
// behavior for workflows without a sensitive: declaration.
func sensitivePatternsFromYAML(raw string) []string {
	if raw == "" {
		return nil
	}
	wf, err := workflow.Parse([]byte(raw))
	if err != nil {
		return nil
	}
	return wf.Sensitive
}
