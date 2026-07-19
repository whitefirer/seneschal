package workflow

import "github.com/charmbracelet/lipgloss"

// Theme 定义颜色主题
type Theme struct {
	Name      string
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Success   lipgloss.Color
	Error     lipgloss.Color
	Warning   lipgloss.Color
	Info      lipgloss.Color
	Gray      lipgloss.Color
	Border    lipgloss.Color
}

// 预定义主题
var (
	// ThemeDefault Claude Code style — warm amber palette
	ThemeDefault = Theme{
		Name:      "Default",
		Primary:   lipgloss.Color("214"), // warm amber
		Secondary: lipgloss.Color("243"), // muted gray
		Success:   lipgloss.Color("78"),  // soft green
		Error:     lipgloss.Color("203"), // warm red
		Warning:   lipgloss.Color("221"), // soft yellow
		Info:      lipgloss.Color("117"), // soft blue
		Gray:      lipgloss.Color("243"), // muted gray
		Border:    lipgloss.Color("214"), // amber border
	}

	// ThemeDark 暗黑主题（紫色系）
	ThemeDark = Theme{
		Name:      "Dark",
		Primary:   lipgloss.Color("129"),
		Secondary: lipgloss.Color("241"),
		Success:   lipgloss.Color("118"),
		Error:     lipgloss.Color("196"),
		Warning:   lipgloss.Color("214"),
		Info:      lipgloss.Color("81"),
		Gray:      lipgloss.Color("241"),
		Border:    lipgloss.Color("129"),
	}

	// ThemeLight 明亮主题（绿色系）
	ThemeLight = Theme{
		Name:      "Light",
		Primary:   lipgloss.Color("22"),
		Secondary: lipgloss.Color("236"),
		Success:   lipgloss.Color("34"),
		Error:     lipgloss.Color("160"),
		Warning:   lipgloss.Color("172"),
		Info:      lipgloss.Color("25"),
		Gray:      lipgloss.Color("236"),
		Border:    lipgloss.Color("22"),
	}

	// ThemeMonokai Monokai 主题
	ThemeMonokai = Theme{
		Name:      "Monokai",
		Primary:   lipgloss.Color("197"),
		Secondary: lipgloss.Color("242"),
		Success:   lipgloss.Color("118"),
		Error:     lipgloss.Color("197"),
		Warning:   lipgloss.Color("227"),
		Info:      lipgloss.Color("81"),
		Gray:      lipgloss.Color("242"),
		Border:    lipgloss.Color("197"),
	}

	// ThemeOcean 海洋主题
	ThemeOcean = Theme{
		Name:      "Ocean",
		Primary:   lipgloss.Color("39"),
		Secondary: lipgloss.Color("111"),
		Success:   lipgloss.Color("48"),
		Error:     lipgloss.Color("196"),
		Warning:   lipgloss.Color("214"),
		Info:      lipgloss.Color("39"),
		Gray:      lipgloss.Color("111"),
		Border:    lipgloss.Color("39"),
	}
)

// GetTheme 根据名称获取主题
func GetTheme(name string) Theme {
	switch name {
	case "dark", "Dark":
		return ThemeDark
	case "light", "Light":
		return ThemeLight
	case "monokai", "Monokai":
		return ThemeMonokai
	case "ocean", "Ocean":
		return ThemeOcean
	case "claude", "Claude":
		return ThemeDefault
	default:
		return ThemeDefault
	}
}

// ListThemes 列出所有可用主题
func ListThemes() []string {
	return []string{
		"default",
		"dark",
		"light",
		"monokai",
		"ocean",
		"claude",
	}
}

// ThemeStyle 主题化样式生成器
type ThemeStyle struct {
	theme Theme
}

// NewThemeStyle 创建主题样式生成器
func NewThemeStyle(theme Theme) *ThemeStyle {
	return &ThemeStyle{theme: theme}
}

// Primary 主样式
func (ts *ThemeStyle) Primary() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Primary)
}

// Secondary 次要样式
func (ts *ThemeStyle) Secondary() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Secondary)
}

// Success 成功样式
func (ts *ThemeStyle) Success() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Success)
}

// Error 错误样式
func (ts *ThemeStyle) Error() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Error)
}

// Warning 警告样式
func (ts *ThemeStyle) Warning() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Warning)
}

// Info 信息样式
func (ts *ThemeStyle) Info() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Info)
}

// Gray 灰色样式
func (ts *ThemeStyle) Gray() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Gray)
}

// Border 边框样式
func (ts *ThemeStyle) Border() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ts.theme.Border)
}

// BoxStyle returns a rounded-box with theme border color.
func (ts *ThemeStyle) BoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ts.theme.Border).
		Padding(0, 1)
}
