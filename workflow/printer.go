package workflow

import (
	"fmt"
	"strings"
	"time"
)

// Printer is the unified terminal-output interface for workflow execution.
// It covers the whole event surface the Executor emits while running:
// workflow header/footer, step lifecycle, and per-action detail lines.
//
// Implementations:
//   - PrettyPrinter — legacy ANSI output (default when no mode is selected)
//   - RichPrinter   — lipgloss-styled output (plain/rich/dag/timeline/compact)
//   - RealtimePrinter — Bubble Tea TUI; its Print* methods are no-ops because
//     it renders from the event channel instead (see Runner/EventStreamer)
//   - NoopPrinter   — discards everything (export modes, tests)
//
// Printers with a blocking UI lifecycle (the TUI) additionally implement
// Runner and EventStreamer; the Executor drives those via type assertion
// instead of widening this interface.
type Printer interface {
	// PrintHeader prints the workflow banner before execution starts.
	PrintHeader(wf *Workflow)
	// PrintStep prints a step-start line.
	PrintStep(step Step, depth int)
	// PrintStepResult prints a completed step's outcome. status is one of the
	// Status* constants; for StatusFailed, output carries the error message.
	PrintStepResult(name, status, output, duration string, depth int)
	// PrintFooter prints the end-of-run summary.
	PrintFooter(result *WorkflowResult, startTime, endTime string)

	// Per-action detail lines.
	PrintShell(name, command string, depth int)
	PrintLog(name, level, message string, depth int)
	PrintHTTPRequest(method, url string)
	PrintHTTPCall(method, url string, status int, duration time.Duration)
	PrintCondition(expr string, result bool)
	PrintSleep(duration string)
	PrintForeach(count int, varName string)
}

// Runner is implemented by printers with a blocking UI lifecycle (the TUI).
// Run blocks on the UI; Stop hands it the final result and signals shutdown.
// Kept separate from Printer so plain printers stay lifecycle-free.
type Runner interface {
	Run()
	Stop(result *WorkflowResult, startTime, endTime string)
}

// EventStreamer is implemented by printers that consume ProgressEvents from a
// channel (the TUI). The Executor pushes events non-blockingly.
type EventStreamer interface {
	SetEventChannel(ch chan ProgressEvent)
	EventChannel() chan ProgressEvent
}

// Compile-time conformance checks.
var (
	_ Printer       = (*PrettyPrinter)(nil)
	_ Printer       = (*RichPrinter)(nil)
	_ Printer       = (*RealtimePrinter)(nil)
	_ Printer       = NoopPrinter{}
	_ Runner        = (*RealtimePrinter)(nil)
	_ EventStreamer = (*RealtimePrinter)(nil)
)

// NoopPrinter discards all output. Used for export modes (json/html, which
// print a structured document instead), headless runs, and tests.
type NoopPrinter struct{}

func (NoopPrinter) PrintHeader(*Workflow)                               {}
func (NoopPrinter) PrintStep(Step, int)                                 {}
func (NoopPrinter) PrintStepResult(string, string, string, string, int) {}
func (NoopPrinter) PrintFooter(*WorkflowResult, string, string)         {}
func (NoopPrinter) PrintShell(string, string, int)                      {}
func (NoopPrinter) PrintLog(string, string, string, int)                {}
func (NoopPrinter) PrintHTTPRequest(string, string)                     {}
func (NoopPrinter) PrintHTTPCall(string, string, int, time.Duration)    {}
func (NoopPrinter) PrintCondition(string, bool)                         {}
func (NoopPrinter) PrintSleep(string)                                   {}
func (NoopPrinter) PrintForeach(int, string)                            {}

// ── Shared icon maps ─────────────────────────────────────────────────────────

// actionIcons is the canonical action→glyph map (geometric set) shared by
// RichPrinter and RealtimePrinter — previously two near-identical maps that
// had drifted (the TUI one also covered "loop"). PrettyPrinter keeps its own
// legacy emoji set in pretty.go to preserve its long-standing output.
var actionIcons = map[string]string{
	"shell":     "◇",
	"log":       "◆",
	"http":      "○",
	"condition": "◇",
	"parallel":  "◎",
	"foreach":   "◈",
	"loop":      "◈",
	"set":       "◦",
	"sleep":     "◌",
	"template":  "◉",
}

// actionIcon returns the canonical glyph for an action type.
func actionIcon(action string) string {
	if icon, ok := actionIcons[action]; ok {
		return icon
	}
	return "◦"
}

// statusIcon returns an emoji icon for a step/workflow status. Shared by
// DAGVisualizer and TimelineAnimator (previously two identical copies).
func statusIcon(status string) string {
	switch {
	case isSuccessStatus(status):
		return "✅"
	case status == StatusFailed:
		return "❌"
	case status == StatusRunning:
		return "🔄"
	case status == StatusSkipped:
		return "⏭️"
	default:
		return "⏳"
	}
}

// ── Shared final-result rendering ────────────────────────────────────────────

// renderFinalResult renders the end-of-run summary (status banner, step
// counts, duration) used when the TUI exits — both as its quitting view and
// as the fallback print when the bubbletea program errors out. Previously two
// copies of this logic lived in realtime_printer.go.
func renderFinalResult(sty *ThemeStyle, tuiStyle string, result *WorkflowResult, startTime, endTime string) string {
	ok, bad := 0, 0
	for _, s := range result.Steps {
		if s.Status == StatusFailed {
			bad++
		} else {
			ok++
		}
	}
	st, _ := time.Parse(time.RFC3339, startTime)
	et, _ := time.Parse(time.RFC3339, endTime)
	dur := et.Sub(st).Round(time.Millisecond).String()

	statusStyle := sty.Success()
	icon := "✓"
	label := "SUCCESS"
	if result.Status == StatusFailed {
		statusStyle = sty.Error()
		icon = "✗"
		label = "FAILED"
	}

	var lines []string
	lines = append(lines, statusStyle.Bold(true).Render(fmt.Sprintf("  %s %s", icon, label)))
	lines = append(lines, fmt.Sprintf("  %s/%s steps · %s",
		sty.Success().Render(fmt.Sprintf("%d", ok)),
		sty.Gray().Render(fmt.Sprintf("%d", len(result.Steps))),
		sty.Info().Render(dur)))
	if bad > 0 {
		lines = append(lines, sty.Error().Render(fmt.Sprintf("  %d failed", bad)))
	}

	if tuiStyle == "claude" {
		return strings.Join(lines, "\n") + "\n\n"
	}
	return sty.BoxStyle().Render(strings.Join(lines, "\n")) + "\n\n"
}
