package workflow

import (
	"fmt"
	"strings"
	"sync"
)

func (e *Executor) executeForeach(container Step, depth int, result *WorkflowResult) StepResult {
	// 解析 items
	items, err := e.parseItems(container.Items)
	if err != nil {
		return StepResult{
			Name:     container.Name,
			Action:   container.Action,
			Status:   "failed",
			Error:    fmt.Sprintf("parse items: %v", err),
		}
	}
	
	if len(items) == 0 {
		return StepResult{
			Name:   container.Name,
			Status: "completed",
		}
	}
	
	// 获取迭代变量名
	itemVar := container.ItemVar
	if itemVar == "" {
		itemVar = "item"
	}
	
	allChildren := make([]StepResult, 0)
	
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
				Error:  fmt.Sprintf("iteration %d: build DAG graph: %v", i, err),
			}
		}
		
		// 拓扑排序
		order, err := e.topologicalSort(graph)
		if err != nil {
			return StepResult{
				Name:     container.Name,
				Action:   container.Action,
				Status:   "failed",
				Error:  fmt.Sprintf("iteration %d: topological sort: %v", i, err),
			}
		}
		
		// 执行迭代内的步骤
		completed := make(map[string]*StepResult)
		completedMutex := sync.Mutex{}
		waiting := make(map[string][]string)
		
		// Initialize waiting list
		for id, node := range graph {
			pending := []string{}
			for _, dep := range node.DependsOn {
				if _, ok := graph[dep]; ok {
					pending = append(pending, dep)
				}
			}
			if len(pending) > 0 {
				waiting[id] = pending
			}
		}
		
		// Find entry nodes
		ready := []string{}
		for _, id := range order {
			if len(waiting[id]) == 0 {
				ready = append(ready, id)
			}
		}
		
		hasError := false
		firstErr := ""
		
		for len(ready) > 0 && !hasError {
			var wg sync.WaitGroup
			waveResults := make(map[string]*StepResult)
			resultsMutex := sync.Mutex{}
			
			for _, id := range ready {
				wg.Add(1)
				go func(nodeID string) {
					defer wg.Done()
					
					node := graph[nodeID]
					var stepResult StepResult
					
					// 递归处理嵌套容器
					if node.Step.Action == "condition" || node.Step.Action == "parallel" || 
					   node.Step.Action == "foreach" || node.Step.Action == "loop" {
						stepResult = e.executeContainerDAG(node.Step, depth+1, result)
					} else {
						stepResult = e.executeStep(node.Step, depth+1, result)
					}
					
					resultsMutex.Lock()
					waveResults[nodeID] = &stepResult
					resultsMutex.Unlock()
				}(id)
			}
			
			wg.Wait()
			
			// Process wave results
			for _, id := range ready {
				sr := waveResults[id]
				
				// 添加到结果列表
				result.Steps = append(result.Steps, *sr)
				
				// 只有当这个节点是在 Do 块定义中时才添加到 allChildren
				// 检查这个节点是否在容器的 Do 块内
				isInDoBlock := false
				for _, doStep := range container.Do {
					if doStep.Name == sr.Name {
						isInDoBlock = true
						break
					}
				}
				if isInDoBlock {
					allChildren = append(allChildren, *sr)
				}
				
				completedMutex.Lock()
				completed[id] = sr
				completedMutex.Unlock()
				
				if sr.Status == "failed" && !graph[id].Step.ContinueOnError {
					hasError = true
					if firstErr == "" {
						firstErr = fmt.Sprintf("iteration %d, step '%s' failed: %s", i, sr.Name, sr.Error)
					}
				}
			}
			
			if hasError {
				break
			}
			
			// Find next ready nodes
			newReady := []string{}
			for id, pending := range waiting {
				newPending := []string{}
				for _, dep := range pending {
					if _, ok := completed[dep]; !ok {
						newPending = append(newPending, dep)
					}
				}
				
				if len(newPending) == 0 {
					newReady = append(newReady, id)
					delete(waiting, id)
				} else {
					waiting[id] = newPending
				}
			}
			
			ready = newReady
		}
		
		if hasError {
			return StepResult{
				Name:     container.Name,
				Action:   container.Action,
				Status:   "failed",
				Error:  firstErr,
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

func (e *Executor) executeContainerDAG(container Step, depth int, result *WorkflowResult) StepResult {
	// 创建子工作流
	childWf := &Workflow{
		Name:      container.Name,
		Variables: e.context.Variables,
	}
	
	// 根据容器类型收集子节点
	var children []Step
	if container.Action == "condition" {
		// Condition: 根据表达式选择 then 或 else
		evalResult, _ := e.evaluateExpression(container.Expression)
		if evalResult {
			children = container.Then
		} else {
			children = container.Else
		}
	} else if container.Action == "parallel" {
		children = container.Steps
	} else if container.Action == "foreach" || container.Action == "loop" {
		// Foreach: 迭代执行
		return e.executeForeach(container, depth, result)
	}
	
	childWf.Steps = children
	
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
	completed := make(map[string]*StepResult)
	completedMutex := sync.Mutex{}
	waiting := make(map[string][]string)
	
	// Initialize waiting list
	for id, node := range graph {
		pending := []string{}
		for _, dep := range node.DependsOn {
			if _, ok := graph[dep]; ok {
				pending = append(pending, dep)
			}
		}
		if len(pending) > 0 {
			waiting[id] = pending
		}
	}
	
	// Find entry nodes
	ready := []string{}
	for _, id := range order {
		if len(waiting[id]) == 0 {
			ready = append(ready, id)
		}
	}
	
	hasError := false
	firstErr := ""
	containerChildren := make([]StepResult, 0)
	
	for len(ready) > 0 && !hasError {
		var wg sync.WaitGroup
		waveResults := make(map[string]*StepResult)
		resultsMutex := sync.Mutex{}
		
		for _, id := range ready {
			wg.Add(1)
			go func(nodeID string) {
				defer wg.Done()
				
				node := graph[nodeID]
				var stepResult StepResult
				
				// 递归处理嵌套容器
				if node.Step.Action == "condition" || node.Step.Action == "parallel" || 
				   node.Step.Action == "foreach" || node.Step.Action == "loop" {
					stepResult = e.executeContainerDAG(node.Step, depth+1, result)
				} else {
					stepResult = e.executeStep(node.Step, depth+1, result)
				}
				
				resultsMutex.Lock()
				waveResults[nodeID] = &stepResult
				resultsMutex.Unlock()
			}(id)
		}
		
		wg.Wait()
		
		// Process wave results
		for _, id := range ready {
			sr := waveResults[id]
			
			// 添加到结果列表
			result.Steps = append(result.Steps, *sr)
			
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
			
			completedMutex.Lock()
			completed[id] = sr
			completedMutex.Unlock()
			
			if sr.Status == "failed" && !graph[id].Step.ContinueOnError {
				hasError = true
				if firstErr == "" {
					firstErr = fmt.Sprintf("child step '%s' failed: %s", sr.Name, sr.Error)
				}
			}
		}
		
		if hasError {
			break
		}
		
		// Find next ready nodes
		newReady := []string{}
		for id, pending := range waiting {
			newPending := []string{}
			for _, dep := range pending {
				if _, ok := completed[dep]; !ok {
					newPending = append(newPending, dep)
				}
			}
			
			if len(newPending) == 0 {
				newReady = append(newReady, id)
				delete(waiting, id)
			} else {
				waiting[id] = newPending
			}
		}
		
		ready = newReady
	}
	
	// Generate container step ID
	containerStepID := container.ID
	if containerStepID == "" {
		containerStepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(container.Name, " ", "-")))
	}
	
	if hasError {
		containerResult := StepResult{
			ID:       containerStepID,
			Name:     container.Name,
			Action:   container.Action,
			Status:   "failed",
			Error:    firstErr,
			Children: containerChildren,
		}
		// Send WebSocket events for container completion
		e.sendEvent("step_output", container.Name, containerStepID, container.Action, "failed", firstErr, "", nil)
		e.sendEvent("step_complete", container.Name, containerStepID, container.Action, "failed", "", "", nil)
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
	e.sendEvent("step_complete", container.Name, containerStepID, container.Action, "success", "", "", nil)
	return containerResult
}


