package workflow

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ANSI color codes
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorGray    = "\033[90m"
	ColorBold    = "\033[1m"
)

// checkColorSupport checks if the terminal supports colors.
func checkColorSupport() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("COLORTERM") != "" {
		return true
	}
	term := os.Getenv("TERM")
	return strings.Contains(term, "color") || strings.Contains(term, "ansi")
}

// PrettyPrinter handles beautiful workflow output.
type PrettyPrinter struct {
	verbose bool
	dryRun  bool
	width   int
	color   bool
}

// NewPrettyPrinter creates a new pretty printer.
func NewPrettyPrinter(verbose, dryRun bool) *PrettyPrinter {
	return &PrettyPrinter{
		verbose: verbose,
		dryRun:  dryRun,
		width:   80,
		color:   checkColorSupport(),
	}
}

// PrintHeader prints the workflow header.
func (p *PrettyPrinter) PrintHeader(wf *Workflow) {
	fmt.Println()
	if p.color {
		fmt.Println(ColorCyan + "  ┌─────────────────────────────────────────────────────────────┐" + ColorReset)
		fmt.Println(ColorCyan + "  │" + ColorReset + "  " + ColorBold + "📦 " + wf.Name + ColorReset)
	} else {
		fmt.Println("  ┌─────────────────────────────────────────────────────────────┐")
		fmt.Println("  │  📦 " + wf.Name)
	}
	if wf.Version != "" {
		if p.color {
			fmt.Println(ColorCyan + "  │" + ColorReset + "     " + ColorGray + "Version:" + ColorReset + " " + ColorMagenta + wf.Version + ColorReset)
		} else {
			fmt.Println("  │     Version: " + wf.Version)
		}
	}
	if wf.Description != "" {
		desc := p.truncate(wf.Description, 50)
		if p.color {
			fmt.Println(ColorCyan + "  │" + ColorReset + "     " + ColorGray + desc + ColorReset)
		} else {
			fmt.Println("  │     " + desc)
		}
	}
	if p.color {
		fmt.Println(ColorCyan + "  └─────────────────────────────────────────────────────────────┘" + ColorReset)
	} else {
		fmt.Println("  └─────────────────────────────────────────────────────────────┘")
	}
	fmt.Println()
}

// PrintStepStart prints a step start message.
func (p *PrettyPrinter) PrintStepStart(stepName, action string, depth int) {
	indent := strings.Repeat("  ", depth)
	icon := p.getActionIcon(action)
	if p.color {
		fmt.Printf("%s%s %s[%s]%s %s%s%s\n", indent, icon, ColorGray, stepName, ColorReset, ColorCyan, action, ColorReset)
	} else {
		fmt.Printf("%s%s [%s] %s\n", indent, icon, stepName, action)
	}
}

// PrintStepSuccess prints a step success message.
func (p *PrettyPrinter) PrintStepSuccess(stepName, output string, duration time.Duration, depth int) {
	indent := strings.Repeat("  ", depth)
	preview := p.truncate(output, 60)
	if preview != "" {
		if p.color {
			fmt.Printf("%s  %s %s%s%s\n", indent, ColorGreen+"✓"+ColorReset, ColorGray, preview, ColorReset)
		} else {
			fmt.Printf("%s  ✓ %s\n", indent, preview)
		}
	}
}

// PrintStepFailed prints a step failure message.
func (p *PrettyPrinter) PrintStepFailed(stepName, errMsg string, depth int) {
	indent := strings.Repeat("  ", depth)
	if p.color {
		fmt.Printf("%s  %s %s%s\n", indent, ColorRed+"✗"+ColorReset, ColorRed, errMsg)
	} else {
		fmt.Printf("%s  ✗ %s\n", indent, errMsg)
	}
}

// PrintShellCommand prints a shell command.
func (p *PrettyPrinter) PrintShellCommand(cmd string) {
	if p.color {
		fmt.Printf("    %s$%s %s%s\n", ColorGray, ColorReset, ColorYellow, cmd)
	} else {
		fmt.Printf("    $ %s\n", cmd)
	}
}

// PrintHTTPCall prints an HTTP call.
func (p *PrettyPrinter) PrintHTTPCall(method, url string, status int, duration time.Duration) {
	if p.color {
		statusColor := ColorGreen
		if status >= 400 {
			statusColor = ColorRed
		}
		fmt.Printf("    %s%s%s %s%s←%s%s%d%s (%s%s%s)\n", ColorMagenta, method, ColorReset, url, ColorReset, ColorGray, statusColor, status, ColorReset, ColorGray, duration.String(), ColorReset)
	} else {
		fmt.Printf("    %s %s ← %d (%s)\n", method, url, status, duration.String())
	}
}

// PrintCondition prints a condition check.
func (p *PrettyPrinter) PrintCondition(expr string, result bool) {
	resultStr := "false"
	if result {
		resultStr = "true"
	}
	if p.color {
		fmt.Printf("    %s?%s %s%s%s  %s→%s %s%s%s\n", ColorCyan, ColorReset, ColorYellow, expr, ColorReset, ColorGray, ColorReset, ColorMagenta, resultStr, ColorReset)
	} else {
		fmt.Printf("    ? %s → %s\n", expr, resultStr)
	}
}

// PrintParallel prints parallel execution info.
func (p *PrettyPrinter) PrintParallel(count int) {
	if p.color {
		fmt.Printf("    %s⚡ parallel (%d steps)%s\n", ColorCyan, count, ColorReset)
	} else {
		fmt.Printf("    ⚡ parallel (%d steps)\n", count)
	}
}

// PrintForeach prints foreach iteration info.
func (p *PrettyPrinter) PrintForeach(count int, varName string) {
	if p.color {
		fmt.Printf("    %s🔄 foreach (%d items, var=%s)%s\n", ColorCyan, count, varName, ColorReset)
	} else {
		fmt.Printf("    🔄 foreach (%d items, var=%s)\n", count, varName)
	}
}

// PrintSleep prints sleep info.
func (p *PrettyPrinter) PrintSleep(duration string) {
	if p.color {
		fmt.Printf("    %s💤 sleeping %s...%s\n", ColorGray, duration, ColorReset)
	} else {
		fmt.Printf("    💤 sleeping %s...\n", duration)
	}
}

// PrintLog prints a log message.
func (p *PrettyPrinter) PrintLog(level, message string) {
	icon := "ℹ"
	if p.color {
		color := ColorCyan
		switch level {
		case "warn":
			icon = "⚠"
			color = ColorYellow
		case "error":
			icon = "✖"
			color = ColorRed
		}
		fmt.Printf("%s %s [%s] %s%s\n", color+icon+ColorReset, color, level, message, ColorReset)
	} else {
		switch level {
		case "warn":
			icon = "⚠"
		case "error":
			icon = "✖"
		}
		fmt.Printf("%s [%s] %s\n", icon, level, message)
	}
}

// PrintFooter prints the workflow footer.
func (p *PrettyPrinter) PrintFooter(result *WorkflowResult, startTime, endTime string) {
	fmt.Println()

	statusIcon := "✅"
	if result.Status == "failed" {
		statusIcon = "❌"
	}

	if p.color {
		statusColor := ColorGreen
		if result.Status == "failed" {
			statusColor = ColorRed
		}
		fmt.Printf("  %s%s Result: %s%s\n", ColorBold, statusIcon, statusColor, strings.ToUpper(result.Status))
		if result.Error != "" {
			fmt.Printf("  %sError: %s%s\n", ColorRed, result.Error, ColorReset)
		}
		fmt.Println(ColorReset)
		fmt.Printf("  %sSteps:%s\n", ColorBold, ColorReset)
		for _, step := range result.Steps {
			icon := "✓"
			color := ColorGreen
			if step.Status == "failed" {
				icon = "✗"
				color = ColorRed
			} else if step.Status == "skipped" {
				icon = "○"
				color = ColorGray
			}
			fmt.Printf("    %s%s %s%s\n", color+icon+ColorReset, color, step.Name, ColorReset)
		}
		fmt.Println()
		fmt.Printf("  %s⏱️  Started:%s  %s\n", ColorGray, ColorReset, startTime)
		fmt.Printf("  %s⏱️  Finished:%s %s\n", ColorGray, ColorReset, endTime)
		start, _ := time.Parse(time.RFC3339, startTime)
		end, _ := time.Parse(time.RFC3339, endTime)
		duration := end.Sub(start)
		fmt.Printf("  %s⏱️  Duration:%s %s%s\n", ColorGray, ColorReset, ColorMagenta, duration.String())
		fmt.Println(ColorReset)
	} else {
		fmt.Printf("  %s Result: %s\n", statusIcon, strings.ToUpper(result.Status))
		fmt.Println()
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
			fmt.Printf("    %s %s\n", icon, step.Name)
		}
		fmt.Println()
		fmt.Printf("  ⏱️  Started:  %s\n", startTime)
		fmt.Printf("  ⏱️  Finished: %s\n", endTime)
		start, _ := time.Parse(time.RFC3339, startTime)
		end, _ := time.Parse(time.RFC3339, endTime)
		duration := end.Sub(start)
		fmt.Printf("  ⏱️  Duration: %s\n", duration.String())
		fmt.Println()
	}
}

// PrintDryRun prints dry-run info.
func (p *PrettyPrinter) PrintDryRun() {
	if p.color {
		fmt.Printf("\n  %s⚠️  DRY RUN MODE - No actions executed%s\n\n", ColorYellow, ColorReset)
	} else {
		fmt.Print("\n  ⚠️  DRY RUN MODE - No actions executed\n\n")
	}
}

// getActionIcon returns an icon for an action type.
func (p *PrettyPrinter) getActionIcon(action string) string {
	icons := map[string]string{
		"shell":     "💻",
		"http":      "🌐",
		"condition": "🔀",
		"set":       "📝",
		"sleep":     "💤",
		"log":       "📢",
		"parallel":  "⚡",
		"template":  "📄",
		"foreach":   "🔄",
	}
	if icon, ok := icons[action]; ok {
		return icon
	}
	return "▶"
}

// truncate truncates a string to maxLen characters.
func (p *PrettyPrinter) truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
