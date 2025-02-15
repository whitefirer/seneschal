package workflow

// OutputMode 定义输出模式
type OutputMode string

const (
	// OutputModePlain 朴素文本模式（默认）
	OutputModePlain OutputMode = "plain"
	// OutputModeRich Rich 样式模式（lipgloss 美化）
	OutputModeRich OutputMode = "rich"
	// OutputModeDAG DAG 图可视化模式
	OutputModeDAG OutputMode = "dag"
	// OutputModeTimeline 时间线模式
	OutputModeTimeline OutputMode = "timeline"
	// OutputModeCompact 紧凑模式（适合 CI/CD）
	OutputModeCompact OutputMode = "compact"
	// OutputModeRealtime 实时进度 TUI 模式
	OutputModeRealtime OutputMode = "realtime"
)

// ParseOutputMode 解析输出模式字符串
func ParseOutputMode(s string) OutputMode {
	switch s {
	case "plain", "text":
		return OutputModePlain
	case "rich", "fancy":
		return OutputModeRich
	case "dag", "graph":
		return OutputModeDAG
	case "timeline", "time":
		return OutputModeTimeline
	case "compact", "ci":
		return OutputModeCompact
	case "realtime", "tui", "progress":
		return OutputModeRealtime
	default:
		return OutputModePlain
	}
}

// String 返回模式的字符串表示
func (m OutputMode) String() string {
	return string(m)
}
