package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// RichPrinter provides styled terminal output using themes.
type RichPrinter struct {
	color            bool
	mode             OutputMode
	theme            Theme
	style            *ThemeStyle
	dagVisualizer    *DAGVisualizer
	timelineAnimator *TimelineAnimator
}

// NewRichPrinter creates a RichPrinter.
func NewRichPrinter(mode OutputMode, color bool, themeName string) *RichPrinter {
	theme := GetTheme(themeName)
	return &RichPrinter{
		color:            color,
		mode:             mode,
		theme:            theme,
		style:            NewThemeStyle(theme),
		dagVisualizer:    NewDAGVisualizer(color, theme),
		timelineAnimator: NewTimelineAnimator(color, 80, theme),
	}
}

// ── Header ───────────────────────────────────────────────────────────────────

func (p *RichPrinter) PrintHeader(wf *Workflow) {
	if !p.color {
		p.printPlainHeader(wf)
		return
	}
	switch p.mode {
	case OutputModeRich:
		p.printRichHeader(wf)
	case OutputModeDAG:
		p.printDAGHeader(wf)
	case OutputModeTimeline:
		p.printTimelineHeader(wf)
	case OutputModeCompact:
		p.printCompactHeader(wf)
	default:
		p.printPlainHeader(wf)
	}
}

func (p *RichPrinter) printPlainHeader(wf *Workflow) {
	fmt.Printf("\n  Workflow: %s", wf.Name)
	if wf.Version != "" {
		fmt.Printf(" (v%s)", wf.Version)
	}
	if wf.Description != "" {
		fmt.Printf("\n  %s", wf.Description)
	}
	fmt.Println()
}

func (p *RichPrinter) printRichHeader(wf *Workflow) {
	primary := p.style.Primary().Bold(true)

	// Build content lines
	var lines []string
	lines = append(lines, primary.Render("  "+wf.Name))
	if wf.Version != "" {
		lines = append(lines, p.style.Secondary().Render("  v"+wf.Version))
	}
	if wf.Description != "" {
		lines = append(lines, p.style.Gray().Render("  "+wf.Description))
	}

	// Render box
	box := p.box(lines)
	fmt.Println("\n" + box)
}

func (p *RichPrinter) printDAGHeader(wf *Workflow) {
	fmt.Println()
	fmt.Println(p.style.Primary().Bold(true).Render("  ═══ DAG: " + wf.Name + " ═══"))
	fmt.Println()
}

func (p *RichPrinter) printTimelineHeader(wf *Workflow) {
	fmt.Println()
	fmt.Println(p.style.Primary().Bold(true).Render("  ⚡ " + wf.Name))
	fmt.Println()
}

func (p *RichPrinter) printCompactHeader(wf *Workflow) {
	fmt.Printf("[seneschal] %s", wf.Name)
	if wf.Version != "" {
		fmt.Printf(" (v%s)", wf.Version)
	}
	fmt.Println()
}

// ── Steps ────────────────────────────────────────────────────────────────────

func (p *RichPrinter) PrintStep(step Step, depth int) {
	if !p.color {
		p.printPlainStep(step, depth)
		return
	}
	switch p.mode {
	case OutputModeRich:
		p.printRichStep(step, depth)
	case OutputModeDAG:
		// deferred to footer
	case OutputModeTimeline:
		// deferred to footer
	case OutputModeCompact:
		p.printCompactStep(step, depth)
	default:
		p.printPlainStep(step, depth)
	}
}

func (p *RichPrinter) printPlainStep(step Step, depth int) {
	indent := strings.Repeat("  ", depth)
	icon := p.stepIcon(step.Action)
	fmt.Printf("%s%s %s [%s]\n", indent, icon, step.Name, step.Action)
}

func (p *RichPrinter) printRichStep(step Step, depth int) {
	indent := strings.Repeat("  ", depth)
	icon := p.stepIcon(step.Action)
	name := p.style.Primary().Bold(true).Render(step.Name)
	action := p.style.Gray().Render("[" + step.Action + "]")
	fmt.Printf("%s%s %s %s\n", indent, icon, name, action)
}

func (p *RichPrinter) printCompactStep(step Step, depth int) {
	if depth == 0 {
		fmt.Printf("  → %s (%s)\n", step.Name, step.Action)
	}
}

// ── Action outputs ───────────────────────────────────────────────────────────

func (p *RichPrinter) PrintShell(name, command string, depth int) {
	indent := strings.Repeat("  ", depth)
	if !p.color {
		fmt.Printf("%s  $ %s\n", indent, command)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	prompt := p.style.Secondary().Render("$")
	cmd := p.style.Gray().Render(command)
	fmt.Printf("%s  %s %s\n", indent, prompt, cmd)
}

func (p *RichPrinter) PrintLog(name, level, message string, depth int) {
	indent := strings.Repeat("  ", depth)
	if !p.color {
		fmt.Printf("%s  [%s] %s\n", indent, level, message)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	var lvl lipgloss.Style
	switch level {
	case "info":
		lvl = p.style.Info()
	case "warn":
		lvl = p.style.Warning()
	case "error":
		lvl = p.style.Error()
	default:
		lvl = p.style.Info()
	}
	fmt.Printf("%s  %s %s\n", indent, lvl.Render("["+level+"]"), message)
}

func (p *RichPrinter) PrintHTTPRequest(method, url string) {
	if !p.color {
		fmt.Printf("    %s %s\n", method, url)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	fmt.Printf("    %s %s\n",
		p.style.Info().Bold(true).Render(method),
		p.style.Gray().Render(url))
}

func (p *RichPrinter) PrintHTTPCall(method, url string, status int, duration time.Duration) {
	if !p.color {
		icon := "✓"
		if status >= 400 {
			icon = "✗"
		}
		fmt.Printf("    %s %s %s ← %d (%s)\n", icon, method, url, status, duration.String())
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	methodStyle := p.style.Info().Bold(true).Render(method)
	urlStyle := p.style.Gray().Render(url)
	var statusStyle lipgloss.Style
	icon := "✓"
	if status >= 400 {
		statusStyle = p.style.Error()
		icon = "✗"
	} else {
		statusStyle = p.style.Success()
	}
	durStyle := p.style.Info().Render(duration.String())
	fmt.Printf("    %s %s %s ← %s %s\n", icon, methodStyle, urlStyle, statusStyle.Render(fmt.Sprintf("%d", status)), durStyle)
}

func (p *RichPrinter) PrintCondition(expr string, result bool) {
	if !p.color {
		res := "false"
		if result {
			res = "true"
		}
		fmt.Printf("    ? %s → %s\n", expr, res)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	resultStr := "false"
	resultStyle := p.style.Error()
	if result {
		resultStr = "true"
		resultStyle = p.style.Success()
	}
	exprStyle := p.style.Warning().Render(expr)
	fmt.Printf("    ? %s → %s\n", exprStyle, resultStyle.Bold(true).Render(resultStr))
}

func (p *RichPrinter) PrintParallel(count int) {
	if !p.color {
		fmt.Printf("    ⚡ %d branches\n", count)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	fmt.Printf("    %s %s\n",
		p.style.Info().Render("⚡"),
		p.style.Gray().Render(fmt.Sprintf("%d parallel branches", count)))
}

func (p *RichPrinter) PrintSleep(duration string) {
	if !p.color {
		fmt.Printf("    💤 %s\n", duration)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	fmt.Printf("    %s %s\n",
		p.style.Gray().Render("💤"),
		p.style.Info().Render(duration))
}

func (p *RichPrinter) PrintForeach(count int, varName string) {
	if !p.color {
		fmt.Printf("    ↻ %d items (var: %s)\n", count, varName)
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	fmt.Printf("    %s %s\n",
		p.style.Info().Render("↻"),
		p.style.Gray().Render(fmt.Sprintf("%d items (var: %s)", count, varName)))
}

func (p *RichPrinter) PrintStepResult(name string, status string, output string, duration string, depth int) {
	indent := strings.Repeat("  ", depth)
	if !p.color {
		if status == "failed" {
			fmt.Printf("%s  ✗ %s\n", indent, name)
		} else if output != "" {
			preview := output
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			if duration != "" {
				fmt.Printf("%s  ✓ %s: %s (%s)\n", indent, name, preview, duration)
			} else {
				fmt.Printf("%s  ✓ %s: %s\n", indent, name, preview)
			}
		}
		return
	}
	switch p.mode {
	case OutputModeDAG, OutputModeTimeline:
		return
	}
	if status == "failed" {
		line := fmt.Sprintf("%s  %s %s", indent,
			p.style.Error().Render("✗"),
			p.style.Error().Render(name))
		if duration != "" {
			line += "  " + p.style.Info().Render(duration)
		}
		fmt.Println(line)
	} else {
		preview := output
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		line := fmt.Sprintf("%s  %s %s  %s", indent,
			p.style.Success().Render("✓"),
			p.style.Primary().Render(name),
			p.style.Gray().Render(preview))
		if duration != "" {
			line += "  " + p.style.Info().Render(duration)
		}
		fmt.Println(line)
	}
}

// ── Footer ───────────────────────────────────────────────────────────────────

func (p *RichPrinter) PrintFooter(result *WorkflowResult, startTime, endTime string) {
	switch p.mode {
	case OutputModeDAG:
		p.printDAGFooter(result, startTime, endTime)
		return
	case OutputModeTimeline:
		p.printTimelineFooter(result, startTime, endTime)
		return
	}
	if !p.color {
		p.printPlainFooter(result, startTime, endTime)
		return
	}
	switch p.mode {
	case OutputModeRich:
		p.printRichFooter(result, startTime, endTime)
	case OutputModeCompact:
		p.printCompactFooter(result, startTime, endTime)
	default:
		p.printPlainFooter(result, startTime, endTime)
	}
}

func (p *RichPrinter) printPlainFooter(result *WorkflowResult, startTime, endTime string) {
	fmt.Println()
	statusIcon := "OK"
	if result.Status == "failed" {
		statusIcon = "FAILED"
	}
	fmt.Printf("  Result: %s\n", statusIcon)
	if result.Error != "" {
		fmt.Printf("  Error: %s\n", result.Error)
	}
	fmt.Println("  Steps:")
	for _, step := range result.Steps {
		icon := "✓"
		if step.Status == "failed" {
			icon = "✗"
		} else if step.Status == "skipped" {
			icon = "○"
		}
		dur := ""
		if step.Duration != "" {
			dur = " " + step.Duration
		}
		fmt.Printf("    %s %s%s\n", icon, step.Name, dur)
	}
	fmt.Println()
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	fmt.Printf("  Duration: %s\n", end.Sub(start).Round(time.Millisecond))
	fmt.Println()
}

func (p *RichPrinter) printRichFooter(result *WorkflowResult, startTime, endTime string) {
	fmt.Println()

	// Status banner
	statusIcon := "✓"
	statusStyle := p.style.Success()
	statusLabel := "SUCCESS"
	if result.Status == "failed" {
		statusIcon = "✗"
		statusStyle = p.style.Error()
		statusLabel = "FAILED"
	}

	// Count steps
	ok, bad, skipped := 0, 0, 0
	for _, s := range result.Steps {
		switch s.Status {
		case "failed":
			bad++
		case "skipped":
			skipped++
		default:
			ok++
		}
	}

	// Build stats line
	stats := fmt.Sprintf("%s/%s steps", p.style.Success().Render(fmt.Sprintf("%d", ok)), p.style.Gray().Render(fmt.Sprintf("%d", len(result.Steps))))
	if bad > 0 {
		stats += " · " + p.style.Error().Render(fmt.Sprintf("%d failed", bad))
	}
	if skipped > 0 {
		stats += " · " + p.style.Gray().Render(fmt.Sprintf("%d skipped", skipped))
	}
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	dur := end.Sub(start).Round(time.Millisecond).String()
	stats += " · " + p.style.Info().Render(dur)

	// Build box lines
	var lines []string
	lines = append(lines, statusStyle.Bold(true).Render(fmt.Sprintf("  %s %s", statusIcon, statusLabel)))
	lines = append(lines, "  "+stats)

	if result.Error != "" {
		lines = append(lines, p.style.Error().Render("  "+result.Error))
	}

	box := p.box(lines)
	fmt.Println(box)
	fmt.Println()
}

func (p *RichPrinter) printDAGFooter(result *WorkflowResult, startTime, endTime string) {
	fmt.Println()
	fmt.Println(p.dagVisualizer.RenderDAGFromResult(result))
}

func (p *RichPrinter) printTimelineFooter(result *WorkflowResult, startTime, endTime string) {
	fmt.Println()
	fmt.Println(p.timelineAnimator.RenderTimeline(result, startTime, endTime))
}

func (p *RichPrinter) printCompactFooter(result *WorkflowResult, startTime, endTime string) {
	status := "✓"
	if result.Status == "failed" {
		status = "✗"
	}
	ok := 0
	for _, step := range result.Steps {
		if step.Status == "completed" || step.Status == "success" {
			ok++
		}
	}
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	fmt.Printf("  %s %d/%d steps · %s\n", status, ok, len(result.Steps), end.Sub(start).Round(time.Millisecond))
	fmt.Println()
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// box draws a rounded box around lines of content.
func (p *RichPrinter) box(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return p.style.BoxStyle().Render(strings.Join(lines, "\n"))
}

// stepIcon returns an icon for the action type.
func (p *RichPrinter) stepIcon(action string) string {
	icons := map[string]string{
		"shell":     "◇",
		"log":       "◆",
		"http":      "○",
		"condition": "◇",
		"parallel":  "◎",
		"foreach":   "◈",
		"set":       "◦",
		"sleep":     "◌",
		"template":  "◉",
	}
	if icon, ok := icons[action]; ok {
		return icon
	}
	return "◦"
}
