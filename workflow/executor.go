package workflow

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"net/http"

	"goworkflow/workflow/ai"
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
	Depth      int    `json:"depth,omitempty"`
	ParentId   string `json:"parent_id,omitempty"`
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
	tuiStyle       string              // TUI style: "hermes" or "claude"
	forceColor     bool                // force color output even if not a terminal
	// AI integration (Phase 2). aiProvider is set via SetAIProvider or built
	// from the workflow's ai: config in Execute(). The ai* fields hold
	// workflow-level defaults; steps may override per-step in M3+.
	aiProvider    ai.Provider
	aiModel       string
	aiMaxTokens   int
	aiTemperature float64
}

// SetAIProvider configures the LLM provider used by ai/ai_decide steps.
// If not called, Execute() builds one from the workflow's ai: config (if any).
func (e *Executor) SetAIProvider(p ai.Provider) { e.aiProvider = p }

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
	if mode == OutputModeTUI {
		theme := GetTheme(e.themeName)
		if e.tuiStyle == "" {
			e.tuiStyle = "hermes"
		}
		e.realtimePrinter = NewRealtimePrinter(theme, e.tuiStyle)
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
	theme := GetTheme(themeName)
	// Reinitialize printers if already created
	if e.richPrinter != nil {
		e.richPrinter = NewRichPrinter(e.outputMode, e.colorEnabled(), themeName)
	}
	if e.realtimePrinter != nil {
		e.realtimePrinter = NewRealtimePrinter(theme, e.tuiStyle)
	}
}

// SetTuiStyle sets the TUI visual style ("hermes" or "claude").
func (e *Executor) SetTuiStyle(style string) {
	e.tuiStyle = style
	if e.tuiStyle == "" {
		e.tuiStyle = "hermes"
	}
	if e.realtimePrinter != nil {
		e.realtimePrinter = NewRealtimePrinter(GetTheme(e.themeName), e.tuiStyle)
	}
}

// SetForceColor forces color output even if stdout is not a terminal.
func (e *Executor) SetForceColor(v bool) {
	e.forceColor = v
	// Recreate printers with updated color setting
	if e.richPrinter != nil {
		e.richPrinter = NewRichPrinter(e.outputMode, e.colorEnabled(), e.themeName)
	}
	if e.realtimePrinter != nil {
		e.realtimePrinter = NewRealtimePrinter(GetTheme(e.themeName), e.tuiStyle)
	}
}

// colorEnabled checks if color output is enabled
func (e *Executor) colorEnabled() bool {
	if e.forceColor || os.Getenv("GOWORKFLOW_FORCE_COLOR") != "" {
		return true
	}
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
func (e *Executor) sendEvent(typ, name, stepId, action, status, output, duration string, depth int, parentId string, conditionResult *bool) {
	event := ProgressEvent{
		Type:            typ,
		Name:            name,
		StepId:          stepId,
		Action:          action,
		Status:          status,
		Output:          output,
		Duration:        duration,
		Depth:           depth,
		ParentId:        parentId,
		Time:            Now(),
		ConditionResult: conditionResult,
	}
	
	if e.OnProgress != nil {
		e.OnProgress(event)
	}
	
	// Send to TUI event channel if available
	if e.realtimePrinter != nil {
		ev := ProgressEvent{
			StepId:   stepId,
			Depth:    depth,
			ParentId: parentId,
			StepName: name,
			Action:   action,
			Status:   status,
			Duration: duration,
		}
		if ch := e.realtimePrinter.EventCh; ch != nil {
			select {
			case ch <- ev:
			default:
			}
		}
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

	// Configure AI provider from the workflow's ai: block if not already set
	// explicitly via SetAIProvider. Steps may still run without a provider if
	// no ai/ai_decide step is used; BuildProvider only fails if a config is
	// present but a key is missing — in that case we record an error so the
	// first ai step surfaces a clear message.
	if e.aiProvider == nil && !wf.AI.IsZero() {
		p, err := ai.BuildProvider(wf.AI)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		e.aiProvider = p
	}
	// Apply workflow-level AI defaults for steps to inherit.
	e.aiModel = wf.AI.Model
	if wf.AI.MaxTokens > 0 {
		e.aiMaxTokens = wf.AI.MaxTokens
	}
	e.aiTemperature = wf.AI.Temperature

	e.sendEvent("workflow_start", wf.Name, "", "", "running", "", "", 0, "", nil)

	// Validate workflow

	// TUI 模式：真实终端或强制颜色 → run TUI on current goroutine, workflow in background
	if e.outputMode == OutputModeTUI {
		if isTerminal() || e.forceColor || os.Getenv("GOWORKFLOW_FORCE_COLOR") != "" {
			return e.runTUI(wf, result)
		}
		e.realtimePrinter = nil
		e.outputMode = OutputModeRich
		if e.colorEnabled() {
			e.richPrinter = NewRichPrinter(e.outputMode, true, e.themeName)
		} else {
			e.richPrinter = NewRichPrinter(e.outputMode, false, e.themeName)
		}
	}

	return e.runWorkflow(wf, result)
}

// runTUI runs the workflow inside a bubbletea TUI on the current goroutine.
func (e *Executor) runTUI(wf *Workflow, result *WorkflowResult) *WorkflowResult {
	e.verbose = false
	e.printer = nil

	ch := make(chan ProgressEvent, 256)
	e.realtimePrinter.SetEventChannel(ch)

	// Run workflow in background goroutine
	go func() {
		e.runWorkflow(wf, result)
		close(ch)
	}()

	// Block on TUI (main goroutine)
	e.realtimePrinter.Run()
	return result
}

// runWorkflow executes the core workflow logic without TUI setup.
func (e *Executor) runWorkflow(wf *Workflow, result *WorkflowResult) *WorkflowResult {
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
		result.EndTime = Now()
		return result
	}

	// 统一使用 DAG 模式执行（线性流程是链式 DAG 的特例）
	e.executeDAG(wf, result)

	e.sendEvent("workflow_end", wf.Name, "", "", result.Status, result.Error, "", 0, "", nil)

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
					stepResult = e.executeContainerDAG(node.Step, 0, result, "")
				} else {
					stepResult = e.executeStep(node.Step, 0, result, "")
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

			// Update variables (use Snapshot to avoid racing concurrent Set calls)
			for k, v := range e.context.Snapshot() {
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

func (e *Executor) executeStep(step Step, depth int, wfResult *WorkflowResult, parentID string) StepResult {
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

	e.sendEvent("step_start", step.Name, stepID, step.Action, "running", "", "", depth, parentID, nil)

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
		output, execChildren, skippedChildren, condResult, err = e.execCondition(step, depth, wfResult, stepID)
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
		output, children, err = e.execParallel(step, depth, wfResult, stepID)
	case "template":
		output, err = e.execTemplate(step)
	case "foreach":
			sr := e.executeForeach(step, depth, wfResult, stepID)
			output = sr.Output
			children = sr.Children
			if sr.Status == "failed" {
				err = fmt.Errorf("%s", sr.Error)
			}
	case "ai":
		output, err = e.execAI(step)
		result.Nondeterministic = true
	default:
		err = fmt.Errorf("unknown action: %s", step.Action)
	}

	// Set children for parallel/foreach/condition
	result.Children = children

	// 填充确定性标记初值(见 docs/PRODUCT.md 的"三种确定性层级")
	// 传播算法在 Phase 2 实现,这里只标 SideEffecting
	switch step.Action {
	case "shell", "http", "template":
		result.SideEffecting = true
	}
	// ai / ai_decide 在 Phase 2 加 case 时填 Nondeterministic = true

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
		if e.richPrinter != nil {
			e.richPrinter.PrintStepResult(step.Name, "failed", err.Error(), result.Duration, depth)
		} else if e.verbose {
			fmt.Printf("%s  ✗ %s: %s\n", indent, step.Name, err.Error())
		}
	} else {
		result.Status = "success"
		result.Output = output
		if e.richPrinter != nil {
			e.richPrinter.PrintStepResult(step.Name, "success", output, result.Duration, depth)
		} else if output != "" && e.verbose {
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
		e.sendEvent("step_output", step.Name, stepID, step.Action, result.Status, result.Output, result.Duration, depth, parentID, nil)
	}

	// Send step_complete event
	e.sendEvent("step_complete", step.Name, stepID, step.Action, result.Status, "", result.Duration, depth, parentID, result.ConditionResult)

	return result
}

