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
	// OutputModeTUI 终端进度 TUI 模式
	OutputModeTUI OutputMode = "tui"
	// OutputModeJSON 输出 WorkflowResult 的 JSON（机器可读，脚本/管道友好）
	OutputModeJSON OutputMode = "json"
	// OutputModeHTML 输出可分享的 HTML 执行报告
	OutputModeHTML OutputMode = "html"
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
	case "tui", "realtime", "progress":
		return OutputModeTUI
	case "json":
		return OutputModeJSON
	case "html":
		return OutputModeHTML
	default:
		return OutputModePlain
	}
}

// IsExportMode reports whether the mode produces a structured export
// (JSON/HTML) rather than terminal output. Callers use this to suppress
// terminal printers during execution and emit the export afterward.
func IsExportMode(m OutputMode) bool {
	return m == OutputModeJSON || m == OutputModeHTML
}

// String 返回模式的字符串表示
func (m OutputMode) String() string {
	return string(m)
}
