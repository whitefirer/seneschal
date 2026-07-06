package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// exportResult writes the WorkflowResult to stdout in the requested format
// (JSON or HTML). Used by the --output json / --output html modes. The export
// is the final output of a run, replacing the normal terminal progress view.
func exportResult(mode OutputMode, result *WorkflowResult) {
	switch mode {
	case OutputModeJSON:
		exportJSON(result)
	case OutputModeHTML:
		exportHTML(result)
	}
}

// exportJSON writes the WorkflowResult as indented JSON to stdout.
func exportJSON(result *WorkflowResult) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error exporting JSON: %v\n", err)
		return
	}
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}

// exportHTML writes a self-contained HTML execution report to stdout. The
// report includes the workflow status, a step tree with outputs, and basic
// styling — shareable as a standalone .html file.
func exportHTML(result *WorkflowResult) {
	fmt.Print(renderHTMLReport(result))
}

// renderHTMLReport builds the HTML report string.
func renderHTMLReport(r *WorkflowResult) string {
	var b strings.Builder

	statusColor := "#16a34a" // green
	if r.Status == "failed" {
		statusColor = "#dc2626" // red
	} else if r.Status == "partial" {
		statusColor = "#ea580c" // orange
	}

	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>`)
	b.WriteString(htmlEscape(r.Name))
	b.WriteString(` — goworkflow 报告</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 960px; margin: 0 auto; padding: 2rem; line-height: 1.6; background: #fafafa; color: #1a1a1a; }
  @media (prefers-color-scheme: dark) { body { background: #0a0a0a; color: #e0e0e0; } .step { background: #161616; } .step-output { background: #1a1a1a; } }
  h1 { margin-bottom: 0.25rem; }
  .status-badge { display: inline-block; padding: 2px 12px; border-radius: 999px; font-size: 0.85rem; font-weight: 600; color: white; }
  .meta { color: #666; font-size: 0.9rem; margin: 0.5rem 0 1.5rem; }
  @media (prefers-color-scheme: dark) { .meta { color: #999; } }
  .step { background: white; border-radius: 8px; padding: 12px 16px; margin-bottom: 8px; border-left: 3px solid #ccc; }
  @media (prefers-color-scheme: dark) { .step { background: #161616; border-left-color: #333; } }
  .step.success { border-left-color: #16a34a; }
  .step.failed { border-left-color: #dc2626; }
  .step.skipped { border-left-color: #999; opacity: 0.6; }
  .step-header { display: flex; align-items: center; gap: 8px; }
  .step-name { font-weight: 600; }
  .step-action { font-size: 0.8rem; background: #e5e7eb; padding: 1px 6px; border-radius: 4px; color: #555; }
  @media (prefers-color-scheme: dark) { .step-action { background: #2a2a2a; color: #aaa; } }
  .step-duration { margin-left: auto; font-size: 0.8rem; color: #888; }
  .step-output { font-family: "SF Mono", Menlo, monospace; font-size: 0.82rem; background: #f4f4f5; padding: 8px 12px; border-radius: 4px; margin-top: 6px; white-space: pre-wrap; word-break: break-all; max-height: 300px; overflow-y: auto; }
  @media (prefers-color-scheme: dark) { .step-output { background: #1a1a1a; } }
  .step-error { color: #dc2626; }
  .ai-badge { font-size: 0.7rem; background: #7c3aed; color: white; padding: 1px 5px; border-radius: 3px; }
  .child { margin-left: 20px; }
  .footer { margin-top: 2rem; color: #999; font-size: 0.8rem; border-top: 1px solid #e5e7eb; padding-top: 1rem; }
  @media (prefers-color-scheme: dark) { .footer { border-top-color: #333; } }
</style>
</head>
<body>
<h1>`)
	b.WriteString(htmlEscape(r.Name))
	b.WriteString(`</h1>
<div><span class="status-badge" style="background:`)
	b.WriteString(statusColor)
	b.WriteString(`">`)
	b.WriteString(htmlEscape(r.Status))
	b.WriteString(`</span>`)
	if r.Nondeterministic {
		b.WriteString(` <span class="ai-badge">AI</span>`)
	}
	b.WriteString(`</div>
<div class="meta">`)

	if r.StartTime != "" {
		b.WriteString("开始: " + htmlEscape(r.StartTime))
	}
	if r.EndTime != "" {
		b.WriteString(" · 结束: " + htmlEscape(r.EndTime))
	}
	if r.Error != "" {
		b.WriteString(`<br><span class="step-error">` + htmlEscape(r.Error) + `</span>`)
	}
	b.WriteString(`</div>
`)

	// Variables
	if len(r.Variables) > 0 {
		b.WriteString("<h3>变量</h3><ul>")
		for _, k := range sortedStringKeys(r.Variables) {
			b.WriteString("<li><code>" + htmlEscape(k) + "</code> = " + htmlEscape(truncateStr(r.Variables[k], 500)) + "</li>")
		}
		b.WriteString("</ul>")
	}

	// Steps
	b.WriteString("<h3>步骤</h3>")
	for _, s := range r.Steps {
		renderStepHTML(&b, &s, "")
	}

	b.WriteString(`<div class="footer">由 goworkflow 生成</div>
</body>
</html>
`)
	return b.String()
}

func renderStepHTML(b *strings.Builder, s *StepResult, indentClass string) {
	b.WriteString(`<div class="step ` + indentClass + s.Status + `">`)
	b.WriteString(`<div class="step-header">`)
	b.WriteString(`<span class="step-name">` + htmlEscape(s.Name) + `</span>`)
	b.WriteString(`<span class="step-action">` + htmlEscape(s.Action) + `</span>`)
	if s.Nondeterministic {
		b.WriteString(`<span class="ai-badge">AI</span>`)
	}
	if s.Duration != "" {
		b.WriteString(`<span class="step-duration">` + htmlEscape(s.Duration) + `</span>`)
	}
	b.WriteString(`</div>`)
	if s.Output != "" {
		b.WriteString(`<div class="step-output">` + htmlEscape(truncateStr(s.Output, 5000)) + `</div>`)
	}
	if s.Error != "" {
		b.WriteString(`<div class="step-output step-error">` + htmlEscape(s.Error) + `</div>`)
	}
	b.WriteString(`</div>`)

	// Children
	for _, c := range s.Children {
		renderStepHTML(b, &c, "child "+indentClass)
	}
	for _, c := range s.ThenChildren {
		renderStepHTML(b, &c, "child "+indentClass)
	}
	for _, c := range s.ElseChildren {
		renderStepHTML(b, &c, "child "+indentClass)
	}
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(s)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(截断)"
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
