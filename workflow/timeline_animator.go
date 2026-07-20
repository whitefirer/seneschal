package workflow

import (
	"fmt"
	"strings"
	"time"
)

// TimelineAnimator 时间线动画器
type TimelineAnimator struct {
	color bool
	width int
	style *ThemeStyle
}

// NewTimelineAnimator 创建时间线动画器
func NewTimelineAnimator(color bool, width int, theme Theme) *TimelineAnimator {
	return &TimelineAnimator{
		color: color,
		width: width,
		style: NewThemeStyle(theme),
	}
}

// RenderTimeline 渲染时间线
func (t *TimelineAnimator) RenderTimeline(result *WorkflowResult, startTime, endTime string) string {
	var b strings.Builder

	start, _ := time.Parse(time.RFC3339, startTime)
	end, _ := time.Parse(time.RFC3339, endTime)
	totalDuration := end.Sub(start)

	if totalDuration < time.Millisecond {
		totalDuration = time.Millisecond
	}

	// 标题
	b.WriteString(t.style.Primary().Bold(true).Render("⚡ Timeline View"))
	b.WriteString("\n\n")

	// 时间轴
	b.WriteString(t.renderTimeAxis(totalDuration))
	b.WriteString("\n\n")

	// 解析步骤时间并渲染
	barWidth := t.width - 30
	if barWidth < 40 {
		barWidth = 40
	}

	for _, step := range result.Steps {
		b.WriteString(t.renderStepBar(step, totalDuration, barWidth))
	}

	b.WriteString("\n")
	b.WriteString(t.renderStats(result, totalDuration))

	return b.String()
}

func (t *TimelineAnimator) renderTimeAxis(total time.Duration) string {
	barWidth := t.width - 30
	if barWidth < 40 {
		barWidth = 40
	}

	axis := t.style.Gray().Render("┌" + strings.Repeat("─", barWidth) + "┐")
	axis += "\n" + t.style.Gray().Render("│")

	// 只在关键位置标注：0s, 中间, 总时长
	markers := []struct {
		label string
		pos   int
	}{
		{"0s", 0},
		{"", barWidth * 1 / 3},
		{"", barWidth * 2 / 3},
		{formatDuration(total), barWidth},
	}

	lastPos := 0
	for _, m := range markers {
		if m.label == "" {
			continue
		}
		gap := m.pos - lastPos - len(m.label)
		if gap < 0 {
			gap = 0
		}
		if m.pos > 0 {
			axis += strings.Repeat(" ", gap)
		}
		axis += m.label
		lastPos = m.pos
	}

	axis += t.style.Gray().Render("│")
	axis += "\n" + t.style.Gray().Render("└"+strings.Repeat("─", barWidth)+"┘")

	return axis
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.String()
}

func (t *TimelineAnimator) renderStepBar(step StepResult, total time.Duration, barWidth int) string {
	// 解析步骤时长
	var duration time.Duration
	if step.Duration != "" {
		duration, _ = time.ParseDuration(step.Duration)
	}

	// 最小显示 1ms
	if duration <= 0 {
		duration = 1 * time.Millisecond
	}

	// 确保 total 不小于各步骤时长之和
	if total < duration {
		total = duration
	}

	// 计算进度条比例
	ratio := float64(duration) / float64(total)
	if ratio > 1.0 {
		ratio = 1.0
	}

	filled := int(ratio * float64(barWidth))
	if filled < 1 {
		filled = 1
	}
	if filled > barWidth {
		filled = barWidth
	}

	// 图标和样式
	icon := t.getStatusIcon(step.Status)
	nameColor := t.style.Primary()
	barColor := t.style.Success()

	if step.Status == "failed" {
		nameColor = t.style.Error().Bold(true)
		barColor = t.style.Error()
	}

	// 容器节点特殊标注
	actionLabel := ""
	if step.Action == "parallel" {
		actionLabel = t.style.Secondary().Render(fmt.Sprintf("[∥×%d]", len(step.Children)))
	} else if step.Action == "foreach" || step.Action == "loop" {
		actionLabel = t.style.Secondary().Render("[↻]")
	} else if step.Action == "condition" {
		actionLabel = t.style.Secondary().Render("[?]")
	}

	// 构建进度条
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	bar = barColor.Render(bar)

	// 名称截断
	name := step.Name
	if len(name) > 18 {
		name = name[:15] + "..."
	}

	durationStr := t.style.Secondary().Render(duration.String())
	if duration < time.Second {
		durationStr = t.style.Secondary().Render(fmt.Sprintf("%dms", duration.Milliseconds()))
	}

	return fmt.Sprintf("%s %-18s %s %s %s %s\n",
		icon,
		nameColor.Render(name),
		actionLabel,
		bar,
		durationStr,
		t.style.Gray().Render(fmt.Sprintf("%.0f%%", ratio*100)),
	)
}

func (t *TimelineAnimator) renderStats(result *WorkflowResult, total time.Duration) string {
	var b strings.Builder

	b.WriteString(t.style.Primary().Bold(true).Render("📊 Statistics:"))
	b.WriteString("\n")

	// 成功/失败统计
	success := 0
	failed := 0
	skipped := 0
	containerCount := 0

	for _, step := range result.Steps {
		if step.Action == "parallel" || step.Action == "foreach" || step.Action == "loop" {
			containerCount++
		}
		switch step.Status {
		case "completed", "success", "done":
			success++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
	}

	b.WriteString(fmt.Sprintf("  Steps:  %d total", len(result.Steps)))
	if containerCount > 0 {
		b.WriteString(t.style.Gray().Render(fmt.Sprintf(" (%d containers)", containerCount)))
	}
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("  %s Passed:  %d\n", t.style.Success().Render("✅"), success-containerCount))
	if failed > 0 {
		b.WriteString(fmt.Sprintf("  %s Failed:  %d\n", t.style.Error().Render("❌"), failed))
	}
	if skipped > 0 {
		b.WriteString(fmt.Sprintf("  %s Skipped: %d\n", t.style.Gray().Render("⏭️"), skipped))
	}

	b.WriteString(fmt.Sprintf("\n  %s Duration: %s\n",
		t.style.Primary().Render("⏱️"),
		t.style.Success().Bold(true).Render(total.String()),
	))

	return b.String()
}

func (t *TimelineAnimator) getStatusIcon(status string) string {
	return statusIcon(status)
}
