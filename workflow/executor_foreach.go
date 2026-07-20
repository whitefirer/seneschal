package workflow

import (
	"fmt"
	"strings"
)

func (e *Executor) executeForeach(container Step, depth int, result *WorkflowResult, parentID string) StepResult {
	// 解析 items
	items, err := e.parseItems(container.Items)
	if err != nil {
		return StepResult{
			Name:   container.Name,
			Action: container.Action,
			Status: "failed",
			Error:  fmt.Sprintf("parse items: %v", err),
		}
	}

	if len(items) == 0 {
		return StepResult{
			Name:   container.Name,
			Action: container.Action,
			Status: "success",
		}
	}

	// 获取迭代变量名
	itemVar := container.ItemVar
	if itemVar == "" {
		itemVar = "item"
	}

	if e.richPrinter != nil {
		e.richPrinter.PrintForeach(len(items), itemVar)
	} else if e.printer != nil {
		e.printer.PrintForeach(len(items), itemVar)
	}

	allChildren := make([]StepResult, 0)

	// Container's own step ID becomes the parent for its iteration children.
	containerStepID := container.ID
	if containerStepID == "" {
		containerStepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(container.Name, " ", "-")))
	}

	// 逐个迭代执行
	for i, item := range items {
		// 设置迭代变量
		e.context.Set(itemVar, item)
		e.context.Set(itemVar+"_index", fmt.Sprintf("%d", i))
		e.context.Set(itemVar+"_value", item)

		// 为每次迭代创建子 DAG
		graph, err := e.buildDAGGraph(container.Do)
		if err != nil {
			return StepResult{
				Name:     container.Name,
				Action:   container.Action,
				Status:   "failed",
				Error:    fmt.Sprintf("iteration %d: build DAG graph: %v", i, err),
				Children: allChildren,
			}
		}

		// 拓扑排序
		order, err := e.topologicalSort(graph)
		if err != nil {
			return StepResult{
				Name:     container.Name,
				Action:   container.Action,
				Status:   "failed",
				Error:    fmt.Sprintf("iteration %d: topological sort: %v", i, err),
				Children: allChildren,
			}
		}

		// 执行迭代内的步骤
		failed, firstErr := e.runWaves(waveConfig{
			graph: graph,
			order: order,
			exec: func(node *DAGNode) StepResult {
				// 为迭代中的步骤生成唯一 ID
				iterStep := node.Step
				if iterStep.ID == "" {
					iterStep.ID = fmt.Sprintf("step-%s-%d", strings.ToLower(strings.ReplaceAll(iterStep.Name, " ", "-")), i)
				}
				// 递归处理嵌套容器
				if isContainerAction(iterStep.Action) {
					return e.executeContainerDAG(iterStep, depth+1, result, containerStepID)
				}
				return e.executeStep(iterStep, depth+1, result, containerStepID)
			},
			collect: func(id string, sr *StepResult) {
				// child result tracked in container.Children only
				// 只有当这个节点是在容器的 Do 块定义中时才添加到 allChildren
				for _, doStep := range container.Do {
					if doStep.Name == sr.Name {
						allChildren = append(allChildren, *sr)
						break
					}
				}
			},
			failError: func(sr *StepResult) string {
				return fmt.Sprintf("iteration %d, step '%s' failed: %s", i, sr.Name, sr.Error)
			},
		})

		if failed {
			// 迭代失败:已收集的 children(含前序迭代成果和失败步骤本身)保留在结果中
			return StepResult{
				Name:     container.Name,
				Action:   container.Action,
				Status:   "failed",
				Error:    firstErr,
				Children: allChildren,
			}
		}
	}

	return StepResult{
		Name:     container.Name,
		Action:   container.Action,
		Status:   "success",
		Children: allChildren,
	}
}

func (e *Executor) parseItems(items interface{}) ([]string, error) {
	result := []string{}

	switch v := items.(type) {
	case string:
		// 字符串：可能是变量名或逗号分隔的列表
		if strings.Contains(v, ",") {
			// 逗号分隔的列表
			parts := strings.Split(v, ",")
			for _, p := range parts {
				result = append(result, strings.TrimSpace(p))
			}
		} else {
			// 变量名：从上下文中获取
			if val := e.context.Get(v); val != "" {
				// 如果值包含逗号，分割
				if strings.Contains(val, ",") {
					parts := strings.Split(val, ",")
					for _, p := range parts {
						result = append(result, strings.TrimSpace(p))
					}
				} else {
					result = append(result, val)
				}
			} else {
				// 直接作为单个值
				result = append(result, v)
			}
		}
	case []interface{}:
		// 数组：转换为字符串
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
	case int, float64, bool:
		// 单个值：转换为字符串
		result = append(result, fmt.Sprintf("%v", v))
	default:
		return nil, fmt.Errorf("unsupported items type: %T", items)
	}

	return result, nil
}

func (e *Executor) executeContainerDAG(container Step, depth int, result *WorkflowResult, parentID string) StepResult {
	// Generate container step ID
	containerStepID := container.ID
	if containerStepID == "" {
		containerStepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(container.Name, " ", "-")))
	}
	if e.richPrinter != nil {
		e.richPrinter.PrintStep(container, depth)
	} else if e.printer != nil {
		e.printer.PrintStepStart(container.Name, container.Action, depth)
	}
	e.sendEvent("step_start", container.Name, containerStepID, container.Action, "running", "", "", depth, parentID, nil)

	// 根据容器类型收集子节点
	var children []Step
	if container.Action == "condition" {
		// Condition: 根据表达式选择 then 或 else
		expr, _ := e.context.ResolveTemplate(container.Expression)
		evalResult, _ := e.evaluateExpression(container.Expression)
		if e.richPrinter != nil {
			e.richPrinter.PrintCondition(expr, evalResult)
		} else if e.printer != nil {
			e.printer.PrintCondition(expr, evalResult)
		}
		if evalResult {
			children = container.Then
		} else {
			children = container.Else
		}
	} else if container.Action == "parallel" {
		children = container.Steps
	} else if container.Action == "foreach" || container.Action == "loop" {
		// Foreach: 迭代执行。这里提前返回,但必须像下方常规完成路径一样发出
		// 容器完成事件,否则 TUI/WS 上该容器一直显示 running。
		sr := e.executeForeach(container, depth, result, parentID)
		if sr.Status == "failed" {
			e.sendEvent("step_output", container.Name, containerStepID, container.Action, "failed", sr.Error, "", depth, parentID, nil)
		}
		e.sendEvent("step_complete", container.Name, containerStepID, container.Action, sr.Status, "", "", depth, parentID, nil)
		return sr
	}

	// 子节点的 parentID 是当前容器的 step ID
	// (parentID 参数显式传递,不再存 Executor 字段,避免并行容器竞争)

	// 为子节点构建子 DAG 图
	graph, err := e.buildDAGGraph(children)
	if err != nil {
		return StepResult{
			Name:   container.Name,
			Status: "failed",
			Error:  fmt.Sprintf("build child DAG graph: %v", err),
		}
	}

	// 拓扑排序子 DAG
	order, err := e.topologicalSort(graph)
	if err != nil {
		return StepResult{
			Name:   container.Name,
			Status: "failed",
			Error:  fmt.Sprintf("topological sort child DAG: %v", err),
		}
	}

	// 执行子节点（支持嵌套容器）
	containerChildren := make([]StepResult, 0)

	failed, firstErr := e.runWaves(waveConfig{
		graph: graph,
		order: order,
		exec: func(node *DAGNode) StepResult {
			// 递归处理嵌套容器
			if isContainerAction(node.Step.Action) {
				return e.executeContainerDAG(node.Step, depth+1, result, containerStepID)
			}
			return e.executeStep(node.Step, depth+1, result, containerStepID)
		},
		collect: func(id string, sr *StepResult) {
			// child result tracked in container.Children only
			// 只有当这个节点是在容器子步骤定义中时才添加到 containerChildren
			// 检查这个节点是否在容器的 Steps/Then/Else 块内
			isInContainerBlock := false
			if container.Action == "parallel" {
				for _, pStep := range container.Steps {
					if pStep.Name == sr.Name {
						isInContainerBlock = true
						break
					}
				}
			} else if container.Action == "condition" {
				for _, tStep := range container.Then {
					if tStep.Name == sr.Name {
						isInContainerBlock = true
						break
					}
				}
				if !isInContainerBlock {
					for _, eStep := range container.Else {
						if eStep.Name == sr.Name {
							isInContainerBlock = true
							break
						}
					}
				}
			}

			if isInContainerBlock {
				containerChildren = append(containerChildren, *sr)
			}
		},
		failError: func(sr *StepResult) string {
			return fmt.Sprintf("child step '%s' failed: %s", sr.Name, sr.Error)
		},
	})

	if failed {
		containerResult := StepResult{
			ID:       containerStepID,
			Name:     container.Name,
			Action:   container.Action,
			Status:   "failed",
			Error:    firstErr,
			Children: containerChildren,
		}
		if container.Action == "condition" {
			evalResult, _ := e.evaluateExpression(container.Expression)
			containerResult.ConditionResult = &evalResult
		}
		// Send WebSocket events for container completion
		e.sendEvent("step_output", container.Name, containerStepID, container.Action, "failed", firstErr, "", depth, parentID, nil)
		e.sendEvent("step_complete", container.Name, containerStepID, container.Action, "failed", "", "", depth, parentID, nil)
		return containerResult
	}

	containerResult := StepResult{
		ID:     containerStepID,
		Name:   container.Name,
		Action: container.Action,
		Status: "success",
	}

	// For condition nodes, set ThenChildren/ElseChildren instead of Children
	if container.Action == "condition" {
		containerResult.Children = nil
		// Determine which branch was executed based on condition result
		evalResult, _ := e.evaluateExpression(container.Expression)
		containerResult.ConditionResult = &evalResult
		if evalResult {
			containerResult.ThenChildren = containerChildren // Executed branch
			// Create skipped results for else branch
			skippedElse := make([]StepResult, 0, len(container.Else))
			for _, s := range container.Else {
				skippedElse = append(skippedElse, createSkippedStepResult(s))
			}
			containerResult.ElseChildren = skippedElse
		} else {
			containerResult.ElseChildren = containerChildren // Executed branch
			// Create skipped results for then branch
			skippedThen := make([]StepResult, 0, len(container.Then))
			for _, s := range container.Then {
				skippedThen = append(skippedThen, createSkippedStepResult(s))
			}
			containerResult.ThenChildren = skippedThen
		}
	} else {
		// For parallel/foreach/loop, set Children
		containerResult.Children = containerChildren
	}

	// Send WebSocket events for container completion
	e.sendEvent("step_complete", container.Name, containerStepID, container.Action, "success", "", "", depth, parentID, containerResult.ConditionResult)
	return containerResult
}
