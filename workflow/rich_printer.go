package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// RichPrinter 提供美化的终端输出
type RichPrinter struct {
	color             bool
	mode              OutputMode
	theme             Theme
	style             *ThemeStyle
	dagVisualizer     *DAGVisualizer
	timelineAnimator  *TimelineAnimator
}

// 样式定义
var (
	// 颜色样式
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	
	// 边框样式
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")).
			Padding(0, 1).
			MarginTop(1)
	
	// 标题样式
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))
	
	// 进度条样式
	progressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("6"))
)

// NewRichPrinter 创建 RichPrinter
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

// PrintHeader 打印工作流头部
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
	fmt.Printf("\n┌─────────────────────────────────────────────────────────────┐\n")
	fmt.Printf("  │  📦 %s\n", wf.Name)
	fmt.Printf("  │     Version: %s\n", wf.Version)
	if wf.Description != "" {
		desc := wf.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Printf("  │     %s\n", desc)
	}
	fmt.Printf("  └─────────────────────────────────────────────────────────────┘\n\n")
}

func (p *RichPrinter) printRichHeader(wf *Workflow) {
	header := fmt.Sprintf("📦 %s", wf.Name)
	if wf.Version != "" {
		header += fmt.Sprintf(" · v%s", wf.Version)
	}
	
	content := titleStyle.Render(header)
	if wf.Description != "" {
		content += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(wf.Description)
	}
	
	fmt.Println(boxStyle.Render(content))
	fmt.Println()
}

func (p *RichPrinter) printDAGHeader(wf *Workflow) {
	fmt.Println()
	fmt.Println(titleStyle.Render("╔═══════════════════════════════════════════════════════════╗"))
	fmt.Printf("║  %-58s ║\n", titleStyle.Render("📦 "+wf.Name))
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func (p *RichPrinter) printTimelineHeader(wf *Workflow) {
	fmt.Println()
	fmt.Println(titleStyle.Render("⚡ Workflow: " + wf.Name))
	fmt.Println()
}

func (p *RichPrinter) printCompactHeader(wf *Workflow) {
	fmt.Printf("[goworkflow] %s", wf.Name)
	if wf.Version != "" {
		fmt.Printf(" (v%s)", wf.Version)
	}
	fmt.Println()
}

// PrintStep 打印步骤开始
func (p *RichPrinter) PrintStep(step Step, depth int) {
	if !p.color {
		p.printPlainStep(step, depth)
		return
	}
	
	switch p.mode {
	case OutputModeRich:
		p.printRichStep(step, depth)
	case OutputModeDAG:
		p.printDAGStep(step, depth)
	case OutputModeTimeline:
		p.printTimelineStep(step, depth)
	case OutputModeCompact:
		p.printCompactStep(step, depth)
	default:
		p.printPlainStep(step, depth)
	}
}

func (p *RichPrinter) printPlainStep(step Step, depth int) {
	indent := strings.Repeat("  ", depth)
	action := strings.ToUpper(step.Action)
	
	switch step.Action {
	case "shell":
		fmt.Printf("%s💻 [%s] %s\n", indent, step.Name, action)
	case "log":
		fmt.Printf("%s📢 [%s] %s\n", indent, step.Name, action)
	case "http":
		fmt.Printf("%s🌐 [%s] %s\n", indent, step.Name, action)
	case "condition":
		fmt.Printf("%s🔀 [%s] %s\n", indent, step.Name, action)
	case "parallel":
		fmt.Printf("%s⚡ [%s] %s\n", indent, step.Name, action)
	case "foreach":
		fmt.Printf("%s🔄 [%s] %s\n", indent, step.Name, action)
	default:
		fmt.Printf("%s[%s] %s\n", indent, step.Name, action)
	}
}

func (p *RichPrinter) printRichStep(step Step, depth int) {
	indent := strings.Repeat("  ", depth)
	icon := p.getIcon(step.Action)
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	
	fmt.Printf("%s%s %s %s\n", indent, icon, nameStyle.Render(step.Name), actionStyle.Render(step.Action))
}

func (p *RichPrinter) printDAGStep(step Step, depth int) {
	// DAG 模式延迟到 PrintFooter 时统一绘制
}

func (p *RichPrinter) printTimelineStep(step Step, depth int) {
	// 时间线模式延迟到 PrintFooter 时统一绘制
}

func (p *RichPrinter) printCompactStep(step Step, depth int) {
	if depth == 0 {
		fmt.Printf("  → %s (%s)\n", step.Name, step.Action)
	}
}

// PrintLog 打印日志输出
func (p *RichPrinter) PrintLog(name, level, message string, depth int) {
	indent := strings.Repeat("  ", depth)
	
	if !p.color {
		fmt.Printf("%s  ℹ [%s] %s\n", indent, level, message)
		return
	}
	
	var levelStyle lipgloss.Style
	switch level {
	case "info":
		levelStyle = infoStyle
	case "warn":
		levelStyle = warnStyle
	case "error":
		levelStyle = errorStyle
	default:
		levelStyle = infoStyle
	}
	
	fmt.Printf("%s  %s %s\n", indent, levelStyle.Render("ℹ"), message)
}

// PrintShell 打印 shell 命令
func (p *RichPrinter) PrintShell(name, command string, depth int) {
	indent := strings.Repeat("  ", depth)
	
	if !p.color {
		fmt.Printf("%s    $ %s\n", indent, command)
		return
	}
	
	cmdStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fmt.Printf("%s    %s %s\n", indent, lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("$"), cmdStyle.Render(command))
}

// PrintFooter 打印工作流尾部
func (p *RichPrinter) PrintFooter(result *WorkflowResult, startTime, endTime string) {
	// DAG 和 Timeline 模式总是使用特殊输出
	switch p.mode {
	case OutputModeDAG:
		p.printDAGFooter(result, startTime, endTime)
		return
	case OutputModeTimeline:
		p.printTimelineFooter(result, startTime, endTime)
		return
	}
	
	// 其他模式检查颜色支持
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
	
	statusIcon := "✅"
	if result.Status == "failed" {
		statusIcon = "❌"
	}
	
	fmt.Printf("  %s Result: %s\n", statusIcon, strings.ToUpper(result.Status))
	if result.Error != "" {
		fmt.Printf("  Error: %s\n", result.Error)
	}
	fmt.Println()
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

func (p *RichPrinter) printRichFooter(result *WorkflowResult, startTime, endTime string) {
	fmt.Println()
	
	// 状态行
	statusIcon := "✅"
	var statusStyle lipgloss.Style
	if result.Status == "failed" {
		statusIcon = "❌"
		statusStyle = errorStyle
	} else {
		statusStyle = successStyle
	}
	
	statusLine := fmt.Sprintf("%s %s", statusIcon, strings.ToUpper(result.Status))
	fmt.Println(statusStyle.Bold(true).Render(statusLine))
	
	if result.Error != "" {
		fmt.Println(errorStyle.Render("  Error: " + result.Error))
	}
	
	fmt.Println()
	fmt.Println(titleStyle.Render("Steps:"))
	
	// 步骤列表
	for _, step := range result.Steps {
		icon := "✓"
		var style lipgloss.Style
		switch step.Status {
		case "failed":
			icon = "✗"
			style = errorStyle
		case "skipped":
			icon = "○"
			style = pendingStyle
		default:
			style = successStyle
		}
		
		duration := ""
		if step.Duration != "" {
			duration = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(step.Duration)
		}
		
		fmt.Printf("  %s %s %s\n", style.Render(icon), step.Name, duration)
	}
	
	// 时间统计
	fmt.Println()
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fmt.Printf("%s ⏱️  Started:  %s\n", timeStyle.Render(""), startTime)
	fmt.Printf("%s ⏱️  Finished: %s\n", timeStyle.Render(""), endTime)
	
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	duration := end.Sub(start)
	fmt.Printf("%s ⏱️  Duration: %s\n", timeStyle.Render(""), lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Render(duration.String()))
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
	
	steps := len(result.Steps)
	success := 0
	for _, step := range result.Steps {
		if step.Status == "completed" || step.Status == "success" {
			success++
		}
	}
	
	fmt.Printf("  %s %d/%d steps completed\n", status, success, steps)
	
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	fmt.Printf("  ⏱️  %s\n", end.Sub(start).String())
	fmt.Println()
}

func (p *RichPrinter) printTimeStats(startTime, endTime string) {
	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	duration := end.Sub(start)
	
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fmt.Printf("%s ⏱️  Started:  %s\n", timeStyle.Render(""), startTime)
	fmt.Printf("%s ⏱️  Finished: %s\n", timeStyle.Render(""), endTime)
	fmt.Printf("%s ⏱️  Duration: %s\n", timeStyle.Render(""), lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Render(duration.String()))
	fmt.Println()
}

// getIcon 获取动作对应的图标
func (p *RichPrinter) getIcon(action string) string {
	icons := map[string]string{
		"shell":     "💻",
		"log":       "📢",
		"http":      "🌐",
		"condition": "🔀",
		"parallel":  "⚡",
		"foreach":   "🔄",
		"set":       "⚙️",
		"sleep":     "💤",
		"template":  "📄",
	}
	
	if icon, ok := icons[action]; ok {
		return icon
	}
	return "📍"
}
