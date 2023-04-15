package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// StepCallback is called after each step completes (including nested steps).
type StepCallback func(stepName string, result StepResult)

// ProgressEvent represents a workflow execution event for real-time streaming.
type ProgressEvent struct {
	Type            string `json:"type"`             // workflow_start, step_start, step_output, step_complete, workflow_end
	Name            string `json:"name,omitempty"`   // step name
	StepId          string `json:"step_id,omitempty"` // step ID
	Action          string `json:"action,omitempty"` // step action type
	Status          string `json:"status,omitempty"` // success, failed, running, skipped
	Output          string `json:"output,omitempty"` // step output
	Error           string `json:"error,omitempty"`  // error message
	Duration        string `json:"duration,omitempty"` // step duration
	Time            string `json:"time,omitempty"`   // timestamp
	ConditionResult *bool  `json:"condition_result,omitempty"` // condition evaluation result
	StepName   string `json:"step_name,omitempty"`
}

// Executor executes workflow steps.
type Executor struct {
	context        *Context
	verbose        bool
	dryRun         bool
	httpClient     *http.Client
	stepCallback   StepCallback
	OnProgress     func(ProgressEvent) // callback for real-time progress streaming
	printer        *PrettyPrinter      // pretty output printer (legacy)
	richPrinter    *RichPrinter        // rich output printer
	realtimePrinter *RealtimePrinter   // realtime TUI printer
	outputMode     OutputMode          // output mode
	totalSteps     int
	themeName      string              // theme name
}

// SetStepCallback sets a callback invoked after each step completes.
func (e *Executor) SetStepCallback(cb StepCallback) {
	e.stepCallback = cb
}

// NewExecutor creates a new workflow executor.
func NewExecutor(variables map[string]string) *Executor {
	return &Executor{
		context: NewContext(variables),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		printer: NewPrettyPrinter(false, false),
	}
}

// SetPrinter sets the pretty printer.
func (e *Executor) SetPrinter(p *PrettyPrinter) {
	e.printer = p
}

// SetOutputMode sets the output mode.
func (e *Executor) SetOutputMode(mode OutputMode) {
	e.outputMode = mode
	if e.themeName == "" {
		e.themeName = "default"
	}
	if mode == OutputModeRealtime {
		theme := GetTheme(e.themeName)
		e.realtimePrinter = NewRealtimePrinter(theme)
		return
	}
	if e.colorEnabled() {
		e.richPrinter = NewRichPrinter(mode, true, e.themeName)
	} else {
		e.richPrinter = NewRichPrinter(mode, false, e.themeName)
	}
}

// SetTheme sets the color theme.
func (e *Executor) SetTheme(themeName string) {
	e.themeName = themeName
	// Reinitialize richPrinter if already created
	if e.richPrinter != nil {
		color := e.colorEnabled()
		e.richPrinter = NewRichPrinter(e.outputMode, color, themeName)
	}
}

// colorEnabled checks if color output is enabled
func (e *Executor) colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal()
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// SetVerbose enables verbose output.
func (e *Executor) SetVerbose(v bool) {
	e.verbose = v
}

// SetDryRun sets dry-run mode (print actions without executing).
func (e *Executor) SetDryRun(dry bool) {
	e.dryRun = dry
}

// GetContext returns the current execution context.
func (e *Executor) GetContext() *Context {
	return e.context
}

// sendEvent sends a progress event if OnProgress is set.
func (e *Executor) sendEvent(typ, name, stepId, action, status, output, duration string, conditionResult *bool) {
	event := ProgressEvent{
		Type:            typ,
		Name:            name,
		StepId:          stepId,
		Action:          action,
		Status:          status,
		Output:          output,
		Duration:        duration,
		Time:            Now(),
		ConditionResult: conditionResult,
	}
	
	if e.OnProgress != nil {
		e.OnProgress(event)
	}
	
	// 实时进度模式：更新 TUI
	if e.realtimePrinter != nil {
		e.realtimePrinter.Update(ProgressEvent{
			StepName: name,
			Action:   action,
			Status:   status,
			Duration: duration,
		})
	}
}

// Execute runs a workflow and returns the result.
// Execute runs the workflow.
func (e *Executor) Execute(wf *Workflow) *WorkflowResult {
	result := &WorkflowResult{
		Name:      wf.Name,
		Status:    "success",
		StartTime: Now(),
		Variables: make(map[string]string),
	}

	// Copy initial variables
	for k, v := range wf.Variables {
		e.context.Set(k, v)
		result.Variables[k] = v
	}

	e.sendEvent("workflow_start", wf.Name, "", "", "running", "", "", nil)

	// Validate workflow

	// 实时进度模式：真实终端 → TUI，管道 → 降级 rich
	if e.outputMode == OutputModeRealtime {
		if isTerminal() {
			e.realtimePrinter.Start()
			e.verbose = false
			e.printer = nil
		} else {
			e.realtimePrinter = nil
			e.outputMode = OutputModeRich
			if e.colorEnabled() {
				e.richPrinter = NewRichPrinter(e.outputMode, true, e.themeName)
			} else {
				e.richPrinter = NewRichPrinter(e.outputMode, false, e.themeName)
			}
		}
	}

	// Print header (fallback to legacy printer if richPrinter not set)
	if e.richPrinter != nil {
		e.richPrinter.PrintHeader(wf)
	} else if e.printer != nil {
		e.printer.PrintHeader(wf)
	}
	if errs := wf.Validate(); len(errs) > 0 {
		result.Status = "failed"
		var errMsgs []string
		for _, err := range errs {
			errMsgs = append(errMsgs, err.Error())
		}
		result.Error = strings.Join(errMsgs, "; ")
		result.EndTime = Now()
		return result
	}

	// 自动推断依赖关系（所有流程都转成 DAG）
	if err := wf.InferDependencies(); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("infer dependencies: %v", err)
		return result
	}

	// 统一使用 DAG 模式执行（线性流程是链式 DAG 的特例）
	e.executeDAG(wf, result)

	e.sendEvent("workflow_end", wf.Name, "", "", result.Status, result.Error, "", nil)

	result.EndTime = Now()

	// Print footer
	if e.realtimePrinter != nil {
		e.realtimePrinter.Stop(result, result.StartTime, result.EndTime)
	} else if e.richPrinter != nil {
		e.richPrinter.PrintFooter(result, result.StartTime, result.EndTime)
	}

	return result
}

// ============================================================================
// DAG Execution Support
// ============================================================================

// DAGNode represents a node in the DAG graph
type DAGNode struct {
	Step      Step
	ID        string
	DependsOn []string
	Next      []string
	JoinMode  string // "all" (全部完成) 或 "any" (任意完成)
}

// hasDAGStructure checks if the workflow has DAG structure (next/depends_on fields)
func (e *Executor) hasDAGStructure(steps []Step) bool {
	for _, step := range steps {
		if len(step.Next) > 0 || len(step.DependsOn) > 0 {
			return true
		}
	}
	return false
}

// buildDAGGraph builds a DAG graph from workflow steps
// 注意：只处理主流程节点，容器子节点由容器的 executeStep 内部处理
func (e *Executor) buildDAGGraph(steps []Step) (map[string]*DAGNode, error) {
	graph := make(map[string]*DAGNode)
	
	// 创建 ID 映射：原始 name → 生成的 ID
	nameToId := make(map[string]string)

	// First pass: create all nodes (只处理顶层步骤，不递归处理容器子节点)
	for _, step := range steps {
		id := step.ID
		if id == "" {
			id = step.Name  // 直接使用 name 作为 ID
		}
		
		nameToId[step.Name] = id

		graph[id] = &DAGNode{
			Step:      step,
			ID:        id,
			DependsOn: step.DependsOn,
			Next:      step.Next,
			JoinMode:  step.JoinMode,
		}
	}

	// Second pass: normalize DependsOn and Next to use actual IDs
	// 同时过滤掉对容器子节点的引用（子节点不在 graph 中）
	for id, node := range graph {
		// Normalize DependsOn (只保留主流程节点的引用)
		normalizedDeps := []string{}
		for _, dep := range node.DependsOn {
			if actualId, ok := nameToId[dep]; ok {
				// 检查这个依赖是否是主流程节点（在 graph 中）
				if _, exists := graph[actualId]; exists {
					normalizedDeps = append(normalizedDeps, actualId)
				}
			}
		}
		node.DependsOn = normalizedDeps
		
		// Normalize Next (只保留主流程节点的引用)
		normalizedNext := []string{}
		for _, next := range node.Next {
			if actualId, ok := nameToId[next]; ok {
				// 检查这个 next 是否是主流程节点（在 graph 中）
				if _, exists := graph[actualId]; exists {
					normalizedNext = append(normalizedNext, actualId)
				}
			}
		}
		node.Next = normalizedNext
		
		graph[id] = node
	}

	// Third pass: infer dependencies from next relationships
	// If A.next = [B, C], then B.dependsOn should include A
	for id, node := range graph {
		for _, nextID := range node.Next {
			if nextNode, ok := graph[nextID]; ok {
				// Add implicit dependency
				if !containsString(nextNode.DependsOn, id) {
					nextNode.DependsOn = append(nextNode.DependsOn, id)
				}
			}
		}
	}

	return graph, nil
}

// containsString checks if a string is in a slice
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// topologicalSort returns nodes in execution order (respecting dependencies)
func (e *Executor) topologicalSort(graph map[string]*DAGNode) ([]string, error) {
	// Kahn's algorithm for topological sort
	inDegree := make(map[string]int)

	// Initialize in-degree
	for id := range graph {
		inDegree[id] = 0
	}

	// Calculate in-degree based on dependencies
	// depends_on 表示当前节点依赖于其他节点
	for _, node := range graph {
		for _, dep := range node.DependsOn {
			if _, ok := graph[dep]; ok {
				inDegree[node.ID]++
			}
		}
	}

	// Find all nodes with in-degree 0 (entry nodes)
	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Process queue
	result := []string{}
	for len(queue) > 0 {
		// Take first node
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)

		// Reduce in-degree of dependent nodes
		// next 指向的节点依赖于当前节点，所以要减少它们的入度
		node := graph[id]
		for _, nextID := range node.Next {
			if inDegree[nextID] > 0 {
				inDegree[nextID]--
				if inDegree[nextID] == 0 {
					queue = append(queue, nextID)
				}
			}
		}
	}

	// Check for cycles
	if len(result) != len(graph) {
		return nil, fmt.Errorf("DAG contains a cycle, cannot execute")
	}

	return result, nil
}

// executeDAG executes workflow in DAG mode with parallel execution support
func (e *Executor) executeDAG(wf *Workflow, result *WorkflowResult) {
	graph, err := e.buildDAGGraph(wf.Steps)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("build DAG graph: %v", err)
		return
	}

	// Get execution order
	order, err := e.topologicalSort(graph)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("topological sort: %v", err)
		return
	}

	// Track completed nodes and their results
	completed := make(map[string]*StepResult)
	completedMutex := sync.Mutex{}

	// Track nodes waiting for dependencies
	waiting := make(map[string][]string) // nodeID -> pending dependencies

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

	// Find entry nodes (no dependencies)
	ready := []string{}
	for _, id := range order {
		if len(waiting[id]) == 0 {
			ready = append(ready, id)
		}
	}

	// Execute in waves (parallel execution of independent nodes)
	hasError := false
	firstErr := ""

	for len(ready) > 0 && !hasError {
		// Execute all ready nodes concurrently
		var wg sync.WaitGroup
		waveResults := make(map[string]*StepResult)
		resultsMutex := sync.Mutex{}

		for _, id := range ready {
			wg.Add(1)
			go func(nodeID string) {
				defer wg.Done()

				node := graph[nodeID]
				var stepResult StepResult
				
				// 如果是容器节点，递归执行子 DAG
				if node.Step.Action == "condition" || node.Step.Action == "parallel" || 
				   node.Step.Action == "foreach" || node.Step.Action == "loop" {
					stepResult = e.executeContainerDAG(node.Step, 0, result)
				} else {
					stepResult = e.executeStep(node.Step, 0)
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
			result.Steps = append(result.Steps, *sr)

			completedMutex.Lock()
			completed[id] = sr
			completedMutex.Unlock()

			// Update variables
			for k, v := range e.context.Variables {
				result.Variables[k] = v
			}

			if sr.Status == "failed" && !graph[id].Step.ContinueOnError {
				hasError = true
				if firstErr == "" {
					firstErr = fmt.Sprintf("step '%s' failed: %s", sr.Name, sr.Error)
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

			// Check join mode
			if len(newPending) == 0 {
				// All dependencies completed, this node is ready
				newReady = append(newReady, id)
				delete(waiting, id)
			} else if graph[id].JoinMode == "any" {
				// "any" mode: at least one dependency completed
				anyCompleted := false
				for _, dep := range pending {
					if _, ok := completed[dep]; ok {
						anyCompleted = true
						break
					}
				}
				if anyCompleted {
					newReady = append(newReady, id)
					delete(waiting, id)
				}
			} else {
				// "all" mode (default): update waiting list
				waiting[id] = newPending
			}
		}

		ready = newReady
	}

	// Handle remaining waiting nodes (if error occurred)
	if hasError {
		result.Status = "failed"
		result.Error = firstErr

		// Mark remaining nodes as skipped
		for id := range waiting {
			sr := StepResult{
				Name:   graph[id].Step.Name,
				ID:     id,
				Status: "skipped",
				Error:  "skipped due to previous failure",
			}
			result.Steps = append(result.Steps, sr)
		}
	}
}

// executeStep executes a single step.


// executeForeach 执行 foreach/loop 迭代
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
		e.context.Variables[itemVar] = item
		e.context.Variables[itemVar+"_index"] = fmt.Sprintf("%d", i)
		e.context.Variables[itemVar+"_value"] = item
		
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
						stepResult = e.executeStep(node.Step, depth+1)
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
	
	// Debug: log what's being returned
	fmt.Fprintf(os.Stderr, "DEBUG executeForeach: container.Name=%s, container.Action=%s, len(allChildren)=%d\n", 
		container.Name, container.Action, len(allChildren))
	for i, child := range allChildren {
		fmt.Fprintf(os.Stderr, "  Child[%d]: name=%s, action=%s\n", i, child.Name, child.Action)
	}
	
	return StepResult{
		Name:     container.Name,
		Action:   container.Action,
		Status:   "success",
		Children: allChildren,
	}
}

// parseItems 解析 foreach 的 items 字段
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
			if val, ok := e.context.Variables[v]; ok {
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

// executeContainerDAG 递归执行容器子节点的 DAG
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
					stepResult = e.executeStep(node.Step, depth+1)
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
	
	// Debug: log what's being returned
	fmt.Fprintf(os.Stderr, "DEBUG executeContainerDAG: container.Name=%s, container.Action=%s, len(containerChildren)=%d\n", 
		container.Name, container.Action, len(containerChildren))
	for i, child := range containerChildren {
		fmt.Fprintf(os.Stderr, "  Child[%d]: name=%s, action=%s\n", i, child.Name, child.Action)
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
		fmt.Fprintf(os.Stderr, "DEBUG Condition container %s: evalResult=%v, len(container.Then)=%d, len(container.Else)=%d\n", 
			container.Name, evalResult, len(container.Then), len(container.Else))
		if evalResult {
			containerResult.ThenChildren = containerChildren // Executed branch
			// Create skipped results for else branch
			skippedElse := make([]StepResult, 0, len(container.Else))
			for _, s := range container.Else {
				skippedElse = append(skippedElse, createSkippedStepResult(s))
			}
			containerResult.ElseChildren = skippedElse
			fmt.Fprintf(os.Stderr, "  Set ThenChildren=%d, ElseChildren=%d\n", len(containerResult.ThenChildren), len(containerResult.ElseChildren))
		} else {
			containerResult.ElseChildren = containerChildren // Executed branch
			// Create skipped results for then branch
			skippedThen := make([]StepResult, 0, len(container.Then))
			for _, s := range container.Then {
				skippedThen = append(skippedThen, createSkippedStepResult(s))
			}
			containerResult.ThenChildren = skippedThen
			fmt.Fprintf(os.Stderr, "  Set ElseChildren=%d, ThenChildren=%d\n", len(containerResult.ElseChildren), len(containerResult.ThenChildren))
		}
	} else {
		// For parallel/foreach/loop, set Children
		containerResult.Children = containerChildren
	}
	
	// Send WebSocket events for container completion
	e.sendEvent("step_complete", container.Name, containerStepID, container.Action, "success", "", "", nil)
	return containerResult
}

func (e *Executor) executeStep(step Step, depth int) StepResult {
	indent := strings.Repeat("  ", depth)
	startTime := Now()

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		if step.Name != "" {
			stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
		} else {
			stepID = fmt.Sprintf("step-%d", depth)
		}
	}

	result := StepResult{
		Name:        step.Name,
		ID:          stepID,
		Action:      step.Action,
		Description: step.Description,
		StartTime:   startTime,
		Next:        step.Next,
		DependsOn:   step.DependsOn,
		JoinMode:    step.JoinMode,
	}

	// Print step start with pretty output
	if e.richPrinter != nil {
		e.richPrinter.PrintStep(step, depth)
	} else if e.printer != nil {
		e.printer.PrintStepStart(step.Name, step.Action, depth)
	}

	e.sendEvent("step_start", step.Name, stepID, step.Action, "running", "", "", nil)

	if e.dryRun {
		result.Status = "skipped"
		result.Output = "(dry run)"
		result.EndTime = Now()
		return result
	}

	var err error
	var output string
	var children []StepResult
	var condResult bool

	switch step.Action {
	case "shell":
		output, err = e.execShell(step)
		// Set ShellCommand to whichever field was used
		if step.Command != "" {
			result.ShellCommand = step.Command
		} else {
			result.ShellCommand = step.Shell
		}
	case "http":
		output, err = e.execHTTP(step)
		result.HTTPUrl = step.URL
		result.HTTPMethod = step.Method
	case "condition":
		var execChildren, skippedChildren []StepResult
		output, execChildren, skippedChildren, condResult, err = e.execCondition(step, depth)
		result.ConditionResult = &condResult
		result.Expression = step.Expression
		// Condition: 设置 ThenChildren 和 ElseChildren
		if condResult {
			result.ThenChildren = execChildren // 执行了 then 分支
			result.ElseChildren = skippedChildren // else 分支未执行
		} else {
			result.ElseChildren = execChildren // 执行了 else 分支
			result.ThenChildren = skippedChildren // then 分支未执行
		}
		// Condition 不设置 Children（使用 then_children 和 else_children）
		children = nil // 清空 children，避免被下面设置到 result.Children
	case "set":
		output, err = e.execSet(step)
	case "sleep":
		output, err = e.execSleep(step)
		result.SleepDuration = step.Duration
	case "log":
		output = e.execLog(step)
		result.LogMessage = step.Message
	case "parallel":
		output, children, err = e.execParallel(step, depth)
	case "template":
		output, err = e.execTemplate(step)
	case "foreach":
		output, children, err = e.execForeach(step, depth)
	default:
		err = fmt.Errorf("unknown action: %s", step.Action)
	}

	// Set children for parallel/foreach/condition
	result.Children = children

	result.EndTime = Now()
	
	// Calculate duration
	if startTime != "" && result.EndTime != "" {
		if start, err := time.Parse(time.RFC3339, startTime); err == nil {
			if end, err := time.Parse(time.RFC3339, result.EndTime); err == nil {
				duration := end.Sub(start)
				result.Duration = duration.String()
			}
		}
	}

	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		if e.verbose {
			fmt.Printf("%s  ✗ %s: %s\n", indent, step.Name, err.Error())
		}
	} else {
		result.Status = "success"
		result.Output = output
		if output != "" && e.verbose {
			preview := output
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			fmt.Printf("%s  ✓ %s: %s\n", indent, step.Name, preview)
		}
	}

	// Fire step callback for streaming (after status is set)
	if e.stepCallback != nil {
		e.stepCallback(step.Name, result)
	}

	// Send step_output event if there's output
	if result.Output != "" {
		e.sendEvent("step_output", step.Name, stepID, step.Action, result.Status, result.Output, result.Duration, nil)
	}

	// Send step_complete event
	e.sendEvent("step_complete", step.Name, stepID, step.Action, result.Status, "", result.Duration, result.ConditionResult)

	return result
}

// execShell executes a shell command.
func (e *Executor) execShell(step Step) (string, error) {
	// Support both 'command' and 'shell' fields
	command := step.Command
	if command == "" {
		command = step.Shell
	}
	
	command, err := e.context.ResolveTemplate(command)
	if err != nil {
		return "", fmt.Errorf("resolve command template: %w", err)
	}

	// Resolve working directory
	dir := step.Dir
	if dir != "" {
		dir, err = e.context.ResolveTemplate(dir)
		if err != nil {
			return "", fmt.Errorf("resolve dir template: %w", err)
		}
	}

	// Determine shell
	shell, args := e.getShell(step.Shell)
	args = append(args, command)

	// Print command with pretty output
	if e.richPrinter != nil {
		e.richPrinter.PrintShell(step.Name, command, 0)
	} else if e.printer != nil {
		e.printer.PrintShellCommand(command)
	}

	cmd := exec.CommandContext(context.Background(), shell, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Merge environment
	env, err := e.context.MergeEnv(step.Env)
	if err != nil {
		return "", err
	}

	// Inherit system environment, then overlay workflow variables
	// This ensures HOME, PATH, etc. are available while still allowing overrides
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		return output, fmt.Errorf("command failed (exit %v, %s): %s", err, duration.Truncate(time.Millisecond), stderr.String())
	}

	e.context.Results[step.Name] = output

	// Save output to variable if specified (HTTP action compatibility)
	if step.SaveOutput != "" {
		e.context.Set(step.SaveOutput, strings.TrimSpace(output))
	}

	// Shell action: save entire output to single variable
	if step.OutputVar != "" {
		e.context.Set(step.OutputVar, strings.TrimSpace(output))
	}

	// Shell action: parse KEY=VALUE lines and save each as variable
	if len(step.OutputVars) > 0 {
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					// Only save if key is in the output_vars list
					for _, expectedKey := range step.OutputVars {
						if key == expectedKey {
							e.context.Set(key, value)
							break
						}
					}
				}
			}
		}
	}

	return output, nil
}

// getShell returns the shell command and initial arguments.
func (e *Executor) getShell(shell string) (string, []string) {
	if shell != "" {
		switch shell {
		case "bash":
			if runtime.GOOS == "windows" {
				// Try Git Bash
				gitBash := "C:\\Program Files\\Git\\bin\\bash.exe"
				if _, err := os.Stat(gitBash); err == nil {
					return gitBash, []string{"-c"}
				}
			}
			return "bash", []string{"-c"}
		case "sh":
			return "sh", []string{"-c"}
		case "powershell", "pwsh":
			return "powershell", []string{"-NoProfile", "-Command"}
		case "cmd":
			return "cmd", []string{"/C"}
		}
	}

	// Default shell by OS
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C"}
	}
	return "sh", []string{"-c"}
}

// execHTTP executes an HTTP request.
func (e *Executor) execHTTP(step Step) (string, error) {
	url, err := e.context.ResolveTemplate(step.URL)
	if err != nil {
		return "", fmt.Errorf("resolve URL template: %w", err)
	}

	method := step.Method
	if method == "" {
		method = "GET"
	}

	// Print HTTP call with pretty output (will update with status after)
	if e.verbose {
		fmt.Printf("    %s%s %s%s\n", ColorMagenta, method, ColorReset, url)
	}

	var bodyReader io.Reader
	if step.Body != "" {
		bodyStr, err := e.context.ResolveTemplate(step.Body)
		if err != nil {
			return "", fmt.Errorf("resolve body template: %w", err)
		}
		bodyReader = strings.NewReader(bodyStr)
	}

	// Parse timeout
	timeout := 60 * time.Second
	if step.Timeout != "" {
		if parsed, err := ParseDuration(step.Timeout); err == nil {
			timeout = parsed
		}
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	// Set headers
	for k, v := range step.Headers {
		resolved, err := e.context.ResolveTemplate(v)
		if err != nil {
			return "", fmt.Errorf("resolve header template: %w", err)
		}
		req.Header.Set(k, resolved)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	output := fmt.Sprintf("Status: %d (%s)\n%s", resp.StatusCode, duration.Truncate(time.Millisecond), string(body))

	// Print HTTP response with pretty output
	if e.printer != nil {
		e.printer.PrintHTTPCall(method, url, resp.StatusCode, duration.Truncate(time.Millisecond))
	}

	e.context.Results[step.Name] = output

	// Save output to variable if specified
	if step.SaveOutput != "" {
		// Store as structured data
		resultData := map[string]interface{}{
			"status":  resp.StatusCode,
			"body":    string(body),
			"headers": resp.Header,
		}
		if jsonData, err := json.Marshal(resultData); err == nil {
			e.context.Set(step.SaveOutput, string(jsonData))
		}
	}

	return output, nil
}

// createSkippedStepResult 递归创建 skipped 步骤及其子步骤
func createSkippedStepResult(s Step) StepResult {
	sr := StepResult{
		Name:        s.Name,
		ID:          s.ID,
		Action:      s.Action,
		Description: s.Description,
		Status:      "skipped",
	}
	if sr.ID == "" && sr.Name != "" {
		sr.ID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(sr.Name, " ", "-")))
	}

	// 如果是 condition，递归处理 then/else 子步骤
	if s.Action == "condition" {
		if len(s.Then) > 0 {
			sr.ThenChildren = make([]StepResult, len(s.Then))
			for i, subStep := range s.Then {
				sr.ThenChildren[i] = createSkippedStepResult(subStep)
			}
		}
		if len(s.Else) > 0 {
			sr.ElseChildren = make([]StepResult, len(s.Else))
			for i, subStep := range s.Else {
				sr.ElseChildren[i] = createSkippedStepResult(subStep)
			}
		}
	}

	return sr
}

// execCondition evaluates a condition and runs then/else branches.
func (e *Executor) execCondition(step Step, depth int) (string, []StepResult, []StepResult, bool, error) {
	expr, err := e.context.ResolveTemplate(step.Expression)
	if err != nil {
		return "", nil, nil, false, fmt.Errorf("resolve expression: %w", err)
	}

	// Simple expression evaluation
	// Supports: variable comparisons, string contains, empty checks
	// Syntax: "{{.var}} == value", "{{.var}} != value", "{{.var}} contains value"
	// Also supports: "var1 == var2" (resolves both sides)
	result, err := e.evaluateExpression(expr)
	if err != nil {
		return "", nil, nil, false, fmt.Errorf("evaluate condition: %w", err)
	}

	// Print condition with pretty output
	if e.printer != nil {
		e.printer.PrintCondition(expr, result)
	}

	// Determine which branch to execute
	var execSteps []Step
	var skippedSteps []Step
	if result {
		execSteps = step.Then
		skippedSteps = step.Else
	} else {
		execSteps = step.Else
		skippedSteps = step.Then
	}

	// Execute the selected branch
	var outputs []string
	var execChildren []StepResult
	for _, s := range execSteps {
		sr := e.executeStep(s, depth+1)
		if sr.Output != "" {
			outputs = append(outputs, sr.Output)
		}
		execChildren = append(execChildren, sr)
		if sr.Status == "failed" && !s.ContinueOnError {
			return strings.Join(outputs, "\n"), execChildren, nil, result, fmt.Errorf("sub-step '%s' failed: %s", s.Name, sr.Error)
		}
	}

	// Create skipped branch results (pending status, not executed)
	// 使用递归函数处理嵌套 condition
	var skippedChildren []StepResult
	for _, s := range skippedSteps {
		skippedChildren = append(skippedChildren, createSkippedStepResult(s))
	}

	return strings.Join(outputs, "\n"), execChildren, skippedChildren, result, nil
}

// evaluateExpression evaluates a simple boolean expression.
func (e *Executor) evaluateExpression(expr string) (bool, error) {
	expr = strings.TrimSpace(expr)

	// Resolve any remaining template variables in the expression
	resolved, err := e.context.ResolveTemplate(expr)
	if err != nil {
		return false, err
	}
	expr = strings.TrimSpace(resolved)

	// Handle "contains" operator
	if strings.Contains(expr, " contains ") {
		parts := strings.SplitN(expr, " contains ", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		return strings.Contains(left, right), nil
	}

	// Handle "==" operator
	if strings.Contains(expr, " == ") {
		parts := strings.SplitN(expr, " == ", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		return left == right, nil
	}

	// Handle "!=" operator
	if strings.Contains(expr, " != ") {
		parts := strings.SplitN(expr, " != ", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		return left != right, nil
	}

	// Handle ">" operator
	if strings.Contains(expr, " > ") && !strings.Contains(expr, " >= ") {
		parts := strings.SplitN(expr, " > ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), ">")
	}

	// Handle "<" operator
	if strings.Contains(expr, " < ") && !strings.Contains(expr, " <= ") {
		parts := strings.SplitN(expr, " < ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), "<")
	}

	// Handle ">=" operator
	if strings.Contains(expr, " >= ") {
		parts := strings.SplitN(expr, " >= ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), ">=")
	}

	// Handle "<=" operator
	if strings.Contains(expr, " <= ") {
		parts := strings.SplitN(expr, " <= ", 2)
		return compareValues(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), "<=")
	}

	// Handle "empty" keyword
	if strings.HasSuffix(expr, " == empty") || strings.HasSuffix(expr, " == \"\"") {
		parts := strings.SplitN(expr, " ==", 2)
		left := strings.TrimSpace(parts[0])
		return left == "" || left == "\"\"", nil
	}
	if strings.HasSuffix(expr, " != empty") || strings.HasSuffix(expr, " != \"\"") {
		parts := strings.SplitN(expr, " !=", 2)
		left := strings.TrimSpace(parts[0])
		return left != "" && left != "\"\"", nil
	}

	// Boolean-like values
	lower := strings.ToLower(expr)
	if lower == "true" || lower == "1" || lower == "yes" {
		return true, nil
	}
	if lower == "false" || lower == "0" || lower == "no" || lower == "" {
		return false, nil
	}

	return false, fmt.Errorf("unsupported expression: %s", expr)
}

// execSet sets a variable in the context and returns the output.
func (e *Executor) execSet(step Step) (string, error) {
	value, err := e.context.ResolveTemplate(step.Value)
	if err != nil {
		return "", fmt.Errorf("resolve value template: %w", err)
	}

	e.context.Set(step.Name, value)
	
	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	output := fmt.Sprintf("Set %s = %s", step.Name, value)
	
	// Note: step_output is sent by executeStep() to avoid duplication
	
	if e.verbose {
		fmt.Printf("    = %s → %s\n", step.Name, value)
	}
	return output, nil
}

// execSleep pauses execution and returns the output.
func (e *Executor) execSleep(step Step) (string, error) {
	duration, err := ParseDuration(step.Duration)
	if err != nil {
		return "", fmt.Errorf("parse duration: %w", err)
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	// Print sleep with pretty output
	if e.printer != nil {
		e.printer.PrintSleep(duration.String())
	}
	
	// Send sleep start event (progress indicator)
	e.sendEvent("step_output", step.Name, stepID, "sleep", "running", fmt.Sprintf("Sleeping for %s...", duration.String()), "", nil)
	
	time.Sleep(duration)
	
	// Note: final output is sent by executeStep() to avoid duplication
	output := fmt.Sprintf("Slept for %s", duration.String())
	
	return output, nil
}

// execLog prints a log message and returns the formatted output.
func (e *Executor) execLog(step Step) string {
	level := step.Level
	if level == "" {
		level = "info"
	}

	msg, err := e.context.ResolveTemplate(step.Message)
	if err != nil {
		msg = step.Message
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	// Print log with pretty output
	if e.richPrinter != nil {
		e.richPrinter.PrintLog(step.Name, level, msg, 0)
	} else if e.printer != nil {
		e.printer.PrintLog(level, msg)
	}
	
	// Format output (note: step_output is sent by executeStep() to avoid duplication)
	output := fmt.Sprintf("[%s] %s", strings.ToUpper(level), msg)
	
	return output
}

// execParallel executes steps concurrently.
func (e *Executor) execParallel(step Step, depth int) (string, []StepResult, error) {
	// Print parallel with pretty output
	if e.printer != nil {
		e.printer.PrintParallel(len(step.Steps))
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		outputs  []string
		hasError bool
		firstErr error
		children []StepResult
		successCount int
		failedCount  int
	)

	for i, s := range step.Steps {
		// Generate ID for child step if not present
		childID := s.ID
		if childID == "" {
			if s.Name != "" {
				childID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")))
			} else {
				childID = fmt.Sprintf("%s-child-%d", stepID, i)
			}
		}
		
		wg.Add(1)
		go func(s Step, childID string) {
			defer wg.Done()
			
			// Note: step_start, step_output, step_complete are all sent by executeStep()
			// Don't send again to avoid duplicate output
			
			result := e.executeStep(s, depth+1)
			
			mu.Lock()
			defer mu.Unlock()
			if result.Output != "" {
				outputs = append(outputs, fmt.Sprintf("[%s] %s", s.Name, result.Output))
			}
			if result.Status == "success" {
				successCount++
			} else if result.Status == "failed" {
				failedCount++
				hasError = true
				if firstErr == nil {
					firstErr = fmt.Errorf("parallel step '%s' failed: %s", s.Name, result.Error)
				}
			}
			children = append(children, result)
		}(s, childID)
	}

	wg.Wait()

	// Add summary output for parallel
	summaryOutput := fmt.Sprintf("并行完成: %d个任务, %d成功, %d失败", len(step.Steps), successCount, failedCount)
	
	if hasError && firstErr != nil {
		if len(outputs) > 0 {
			return summaryOutput + "\n" + strings.Join(outputs, "\n"), children, firstErr
		}
		return summaryOutput, children, firstErr
	}
	if len(outputs) > 0 {
		return summaryOutput + "\n" + strings.Join(outputs, "\n"), children, nil
	}
	return summaryOutput, children, nil
}

// execTemplate renders a template file and writes the output.
func (e *Executor) execTemplate(step Step) (string, error) {
	result, err := e.context.ResolveTemplateFromFile(step.Source)
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}

	// Resolve output path
	outputPath, err := e.context.ResolveTemplate(step.Output)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}

	if e.verbose {
		fmt.Printf("    %s → %s\n", step.Source, outputPath)
	}

	if err := os.WriteFile(outputPath, []byte(result), 0644); err != nil {
		return "", fmt.Errorf("write output: %w", err)
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	output := fmt.Sprintf("Rendered %s → %s (%d bytes)", step.Source, outputPath, len(result))
	
	// Note: step_output is sent by executeStep() to avoid duplication

	e.context.Results[step.Name] = fmt.Sprintf("wrote %d bytes to %s", len(result), outputPath)
	return output, nil
}

// execForeach iterates over items and executes steps for each.
func (e *Executor) execForeach(step Step, depth int) (string, []StepResult, error) {
	// Resolve items
	var items []string

	switch v := step.Items.(type) {
	case string:
		// Reference to a variable
		value := e.context.Get(v)
		if value != "" {
			items = strings.Split(value, "\n")
			// Trim empty items
			var filtered []string
			for _, item := range items {
				if strings.TrimSpace(item) != "" {
					filtered = append(filtered, strings.TrimSpace(item))
				}
			}
			items = filtered
		}
	case []interface{}:
		for _, item := range v {
			items = append(items, fmt.Sprintf("%v", item))
		}
	default:
		// Try to handle []string from YAML
		if strSlice, ok := v.([]string); ok {
			items = strSlice
		}
	}

	itemVar := step.ItemVar
	if itemVar == "" {
		itemVar = "item"
	}

	// Generate ID if not present
	stepID := step.ID
	if stepID == "" {
		stepID = fmt.Sprintf("step-%s", strings.ToLower(strings.ReplaceAll(step.Name, " ", "-")))
	}

	// Print foreach with pretty output
	if e.printer != nil {
		e.printer.PrintForeach(len(items), itemVar)
	}

	var outputs []string
	var children []StepResult
	successCount := 0
	failedCount := 0
	
	for i, item := range items {
		e.context.Set(itemVar, item)
		e.context.Set(itemVar+"_index", fmt.Sprintf("%d", i))

		for j, s := range step.Do {
			// Generate unique ID for each iteration
			childID := s.ID
			if childID == "" {
				if s.Name != "" {
					childID = fmt.Sprintf("step-%s-iter-%d", strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")), i)
				} else {
					childID = fmt.Sprintf("%s-item-%d-step-%d", stepID, i, j)
				}
			} else {
				// Append iteration index to existing ID
				childID = fmt.Sprintf("%s-iter-%d", childID, i)
			}
			
			// Note: step_start, step_output, step_complete are all sent by executeStep()
			// Don't send again to avoid duplicate output
			
			sr := e.executeStep(s, depth+1)
			
			// Override the ID with the unique iteration ID
			sr.ID = childID
			
			if sr.Output != "" {
				outputs = append(outputs, sr.Output)
			}
			children = append(children, sr)
			if sr.Status == "success" {
				successCount++
			} else if sr.Status == "failed" {
				failedCount++
			}
			if sr.Status == "failed" && !s.ContinueOnError {
				return strings.Join(outputs, "\n"), children, fmt.Errorf("foreach iteration %d step '%s' failed: %s", i, s.Name, sr.Error)
			}
		}
	}

	// Add summary output for foreach
	summaryOutput := fmt.Sprintf("循环完成: %d次迭代, %d成功, %d失败", len(items), successCount, failedCount)
	if len(outputs) > 0 {
		return summaryOutput + "\n" + strings.Join(outputs, "\n"), children, nil
	}
	return summaryOutput, children, nil
}
