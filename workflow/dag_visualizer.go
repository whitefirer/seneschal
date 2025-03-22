package workflow

import (
	"fmt"
	"strings"
)

// DAGVisualizer DAG 图可视化器
type DAGVisualizer struct {
	color   bool
	style   *ThemeStyle
	rendered map[string]bool // 已渲染节点集合
}

// NewDAGVisualizer 创建 DAG 可视化器
func NewDAGVisualizer(color bool, theme Theme) *DAGVisualizer {
	return &DAGVisualizer{
		color:    color,
		style:    NewThemeStyle(theme),
		rendered: make(map[string]bool),
	}
}

// RenderDAGFromResult 从结果渲染 DAG 图（带嵌套，自动去重）
func (v *DAGVisualizer) RenderDAGFromResult(result *WorkflowResult) string {
	var b strings.Builder
	v.rendered = make(map[string]bool) // 重置

	titleStyle := v.style.Primary().Bold(true)
	b.WriteString(titleStyle.Render("📊 Execution Graph:"))
	b.WriteString("\n\n")

	// 先扫描所有容器，标记它们的 Children 为"已渲染"
	for i := range result.Steps {
		step := &result.Steps[i]
		if v.isContainer(*step) {
			v.markChildrenRendered(*step)
		}
	}

	// 渲染所有步骤
	b.WriteString(v.renderResultSteps(result.Steps, 0))

	return b.String()
}

// markChildrenRendered 递归标记子节点为已渲染
func (v *DAGVisualizer) markChildrenRendered(step StepResult) {
	for _, child := range step.Children {
		// 用 Name+Action 作为唯一键
		key := child.Name + "|" + child.Action
		v.rendered[key] = true
		// 递归处理嵌套容器
		if v.isContainer(child) {
			v.markChildrenRendered(child)
		}
	}
}

// isRendered 检查节点是否已被容器渲染过
func (v *DAGVisualizer) isRendered(step StepResult) bool {
	key := step.Name + "|" + step.Action
	return v.rendered[key]
}

// renderResultSteps 递归渲染步骤
func (v *DAGVisualizer) renderResultSteps(steps []StepResult, depth int) string {
	var b strings.Builder
	indent := strings.Repeat("│   ", depth)

	// 过滤掉已经被父容器渲染过的步骤
	visible := make([]StepResult, 0)
	for _, s := range steps {
		if !v.isRendered(s) {
			visible = append(visible, s)
		}
	}

	if depth > 0 {
		indent = strings.Repeat("    ", depth-1) + "│   "
	}

	for i, step := range visible {
		isLast := i == len(visible)-1
		connector := "├──"
		if isLast {
			connector = "└──"
		}

		if v.isContainer(step) {
			b.WriteString(v.renderContainerNode(step, indent, connector, depth))
		} else {
			b.WriteString(v.renderLeafNode(step, indent, connector))
		}
	}

	return b.String()
}

func (v *DAGVisualizer) isContainer(step StepResult) bool {
	return len(step.Children) > 0 &&
		(step.Action == "parallel" || step.Action == "foreach" || step.Action == "loop" || step.Action == "condition")
}

// renderContainerNode 渲染容器节点
func (v *DAGVisualizer) renderContainerNode(step StepResult, indent, connector string, depth int) string {
	var b strings.Builder

	icon := v.getStatusIcon(step.Status)
	nameStyle := v.style.Primary().Bold(true)

	b.WriteString(fmt.Sprintf("%s%s %s %s %s\n",
		indent,
		connector,
		icon,
		nameStyle.Render(step.Name),
		v.style.Secondary().Render(v.getContainerLabel(step)),
	))

	// 渲染子节点
	childIndent := indent + "    "

	for j, child := range step.Children {
		isLastChild := j == len(step.Children)-1
		childConn := "├──"
		if isLastChild {
			childConn = "└──"
		}

		childIcon := v.getStatusIcon(child.Status)
		childName := v.style.Primary().Render(child.Name)
		childAction := v.style.Secondary().Render(" (" + child.Action + ")")
		duration := ""
		if child.Duration != "" && child.Duration != "0s" {
			duration = " " + v.style.Success().Render(child.Duration)
		}

		b.WriteString(fmt.Sprintf("%s%s %s %s%s%s\n",
			childIndent,
			childConn,
			childIcon,
			childName,
			childAction,
			duration,
		))
	}

	return b.String()
}

// renderLeafNode 渲染叶子节点
func (v *DAGVisualizer) renderLeafNode(step StepResult, indent, connector string) string {
	icon := v.getStatusIcon(step.Status)
	nameStyle := v.style.Primary().Render(step.Name)
	actionStyle := v.style.Secondary().Render(" (" + step.Action + ")")

	duration := ""
	if step.Duration != "" && step.Duration != "0s" {
		duration = " " + v.style.Success().Render(step.Duration)
	}

	return fmt.Sprintf("%s%s %s %s%s%s\n",
		indent,
		connector,
		icon,
		nameStyle,
		actionStyle,
		duration,
	)
}

func (v *DAGVisualizer) getStatusIcon(status string) string {
	switch status {
	case "completed", "success", "done":
		return "✅"
	case "failed":
		return "❌"
	case "running":
		return "🔄"
	case "skipped":
		return "⏭️"
	default:
		return "⏳"
	}
}

func (v *DAGVisualizer) getContainerLabel(step StepResult) string {
	switch step.Action {
	case "parallel":
		return fmt.Sprintf("(parallel ×%d tasks)", len(step.Children))
	case "foreach", "loop":
		return "(foreach loop)"
	case "condition":
		if step.ConditionResult != nil {
			if *step.ConditionResult {
				return "(condition → then)"
			}
			return "(condition → else)"
		}
		return "(condition)"
	default:
		return "(" + step.Action + ")"
	}
}
