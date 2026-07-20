package workflow

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/whitefirer/seneschal/workflow/ai"
)

// StepCallback is called after each step completes (including nested steps).
type StepCallback func(stepName string, result StepResult)

// ProgressEvent represents a workflow execution event for real-time streaming.
type ProgressEvent struct {
	Type            string `json:"type"`                       // workflow_start, step_start, step_output, step_complete, workflow_end
	Name            string `json:"name,omitempty"`             // step name
	StepId          string `json:"step_id,omitempty"`          // step ID
	Action          string `json:"action,omitempty"`           // step action type
	Status          string `json:"status,omitempty"`           // success, failed, running, skipped
	Output          string `json:"output,omitempty"`           // step output
	Error           string `json:"error,omitempty"`            // error message
	Duration        string `json:"duration,omitempty"`         // step duration
	Time            string `json:"time,omitempty"`             // timestamp
	ConditionResult *bool  `json:"condition_result,omitempty"` // condition evaluation result
	Depth           int    `json:"depth,omitempty"`
	ParentId        string `json:"parent_id,omitempty"`
	StepName        string `json:"step_name,omitempty"`
}

// Executor executes workflow steps.
type Executor struct {
	context         *Context
	verbose         bool
	dryRun          bool
	httpClient      *http.Client
	stepCallback    StepCallback
	OnProgress      func(ProgressEvent) // callback for real-time progress streaming
	printer         *PrettyPrinter      // pretty output printer (legacy)
	richPrinter     *RichPrinter        // rich output printer
	realtimePrinter *RealtimePrinter    // realtime TUI printer
	outputMode      OutputMode          // output mode
	totalSteps      int
	themeName       string       // theme name
	tuiStyle        string       // TUI style: "hermes" or "claude"
	forceColor      bool         // force color output even if not a terminal
	workflowDir     string       // directory of the current workflow file (for sub-workflow relative paths)
	workflowName    string       // current workflow name (for hooks)
	workflowHooks   []HookConfig // workflow-level hooks (stored for step inheritance)
	globalHooks     []HookConfig // server-level hooks (applied to all workflows)
	// AI integration (Phase 2). aiProvider is set via SetAIProvider or built
	// from the workflow's ai: config in Execute(). The ai* fields hold
	// workflow-level defaults; steps may override per-step in M3+.
	aiProvider    ai.Provider
	aiModel       string
	aiMaxTokens   int
	aiTemperature float64
	// mu guards the mutable execution state below (AI token accounting,
	// conversation history, replay counters). Steps run concurrently
	// (DAG waves, parallel, foreach), so all access goes through the small
	// helper methods (addAITokens, aiHistoryCopy, incReplayHit, ...).
	mu sync.Mutex
	// AI conversation history accumulated within one execution. Guarded by mu.
	aiHistory []ai.Message
	// Per-step AI token counts are returned by execAI/execAIDecide and written
	// directly to the StepResult by executeStep — parallel AI steps must not
	// share a single-slot field or tokens get attributed to the wrong step.
	// cumulative AI token usage across all steps in this execution. Guarded by mu.
	cumulativeTokens int
	aiBudget         int    // workflow-level token budget (0 = unlimited)
	aiMemoryWindow   int    // max prior AI turns to keep (0 = unlimited)
	aiOnError        string // workflow-level on_error: "ai" / "ai_auto" / "" (off)
	aiOnErrorMode    string // resolved mode for workflow-level default
	// execCtx is the cancellation context for the current run, set up by
	// Execute. Quitting the TUI (or another abort path) cancels it so
	// in-flight shell commands / AI calls stop instead of leaking goroutines.
	execCtx    context.Context
	execCancel context.CancelFunc
	// Replay cache (Phase 4): maps step ID (or Name fallback) to a stored
	// StepResult. When non-nil, executeStep returns the cached result for
	// deterministic steps instead of re-executing them. AI / nondeterministic
	// steps are never served from cache — they always re-run. Set via
	// SetReplayCache; nil means normal execution (no replay).
	replayCache map[string]*StepResult
	// replayStats tracks how many steps were served from cache vs re-executed,
	// for the replay summary. Reset when SetReplayCache is called. Guarded by mu.
	replayedHits   int
	replayedMisses int
}

// SetAIProvider configures the LLM provider used by ai/ai_decide steps.
// If not called, Execute() builds one from the workflow's ai: config (if any).
func (e *Executor) SetAIProvider(p ai.Provider) { e.aiProvider = p }

// SetGlobalHooks sets server-level hooks applied to all workflows.
func (e *Executor) SetGlobalHooks(hooks []HookConfig) { e.globalHooks = hooks }

// SetReplayCache enables replay mode: deterministic steps present in the cache
func (e *Executor) SetReplayCache(cache map[string]*StepResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.replayCache = cache
	e.replayedHits = 0
	e.replayedMisses = 0
}

// ReplayStats returns (hits, misses) — how many steps were served from the
// replay cache vs actually executed. Meaningful only after a run with
// SetReplayCache enabled.
func (e *Executor) ReplayStats() (hits, misses int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.replayedHits, e.replayedMisses
}

// addAITokens accumulates one AI call's token usage into the workflow budget
// tracker. Safe for concurrent AI steps.
func (e *Executor) addAITokens(in, out int) {
	e.mu.Lock()
	e.cumulativeTokens += in + out
	e.mu.Unlock()
}

// cumulativeAITokens returns the current cumulative token usage (for budget
// checks). Safe for concurrent AI steps.
func (e *Executor) cumulativeAITokens() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cumulativeTokens
}

// appendAIHistory records one AI turn so downstream AI steps (without explicit
// memory) see it as conversation context. Safe for concurrent AI steps.
func (e *Executor) appendAIHistory(msgs ...ai.Message) {
	e.mu.Lock()
	e.aiHistory = append(e.aiHistory, msgs...)
	e.mu.Unlock()
}

// aiHistoryCopy returns a copy of the accumulated AI conversation history.
func (e *Executor) aiHistoryCopy() []ai.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ai.Message, len(e.aiHistory))
	copy(out, e.aiHistory)
	return out
}

// incReplayHit / incReplayMiss count replay cache outcomes from concurrent
// step goroutines.
func (e *Executor) incReplayHit() {
	e.mu.Lock()
	e.replayedHits++
	e.mu.Unlock()
}

func (e *Executor) incReplayMiss() {
	e.mu.Lock()
	e.replayedMisses++
	e.mu.Unlock()
}

// executionContext returns the cancellation context for the current run.
// Falls back to Background when Execute hasn't set one up (e.g. tests calling
// internals directly).
func (e *Executor) executionContext() context.Context {
	if e.execCtx != nil {
		return e.execCtx
	}
	return context.Background()
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
	if e.forceColor || os.Getenv("SENESCHAL_FORCE_COLOR") != "" {
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

// sendAIToken emits an incremental AI token event. Unlike sendEvent, the
// token text is carried through to the TUI channel (so the detail view can
// append incrementally), not dropped. Used by the "ai" action in streaming
// (TUI) mode.
func (e *Executor) sendAIToken(name, stepID, action string, depth int, parentID, token string) {
	if e.OnProgress != nil {
		e.OnProgress(ProgressEvent{
			Type:     "ai_token",
			Name:     name,
			StepId:   stepID,
			Action:   action,
			Status:   "running",
			Output:   token,
			Depth:    depth,
			ParentId: parentID,
			Time:     Now(),
		})
	}
	// TUI channel: include Output (the token) so the detail view can append.
	if e.realtimePrinter != nil {
		ev := ProgressEvent{
			Type:     "ai_token",
			StepId:   stepID,
			Depth:    depth,
			ParentId: parentID,
			StepName: name,
			Action:   action,
			Status:   "running",
			Output:   token,
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
	// Set up the cancellation context for this run. Quitting the TUI (or any
	// other abort path) cancels it so in-flight steps stop promptly. Cleared
	// on return so a reused Executor starts the next run with a fresh context.
	if e.execCtx == nil {
		e.execCtx, e.execCancel = context.WithCancel(context.Background())
		defer func() {
			e.execCancel()
			e.execCtx = nil
			e.execCancel = nil
		}()
	}

	result := &WorkflowResult{
		Name:      wf.Name,
		Status:    "success",
		StartTime: Now(),
		Variables: make(map[string]string),
	}

	// Initialize variables: workflow YAML defaults first, then the executor's
	// preset variables (from NewExecutor / --var / chat) override them. This
	// gives explicit user/AI-supplied values priority over YAML defaults —
	// previously Execute() clobbered NewExecutor's vars with wf.Variables.
	for k, v := range wf.Variables {
		if _, preset := e.context.GetOK(k); !preset {
			e.context.Set(k, v)
		}
	}
	for k, v := range e.context.Snapshot() {
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
	e.aiBudget = wf.AI.Budget
	e.aiMemoryWindow = wf.AI.MemoryWindow
	e.aiOnError = wf.AI.OnError
	if wf.AI.OnError == "ai_auto" {
		e.aiOnErrorMode = "auto"
	} else if wf.AI.OnError == "ai" {
		e.aiOnErrorMode = "suggest"
	}

	e.sendEvent("workflow_start", wf.Name, "", "", "running", "", "", 0, "", nil)

	// Store workflow context for hooks.
	e.workflowName = wf.Name
	e.workflowHooks = wf.Hooks
	result.SensitivePatterns = wf.Sensitive

	// Validate workflow

	// TUI 模式：真实终端或强制颜色 → run TUI on current goroutine, workflow in background
	if e.outputMode == OutputModeTUI {
		if isTerminal() || e.forceColor || os.Getenv("SENESCHAL_FORCE_COLOR") != "" {
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

	// Export modes (json/html): suppress terminal printers during execution,
	// run quietly, then emit the structured export afterward.
	if IsExportMode(e.outputMode) {
		e.verbose = false
		e.printer = nil
		e.richPrinter = nil
	}

	result = e.runWorkflow(wf, result)

	// Mask sensitive values in the finalized result before it is persisted,
	// exported, or served via the API.
	e.finalizeResult(result)

	// Emit the structured export to stdout if requested.
	if IsExportMode(e.outputMode) {
		exportResult(e.outputMode, result)
	}

	return result
}

// runTUI runs the workflow inside a bubbletea TUI on the current goroutine.
func (e *Executor) runTUI(wf *Workflow, result *WorkflowResult) *WorkflowResult {
	e.verbose = false
	e.printer = nil

	ch := make(chan ProgressEvent, 256)
	e.realtimePrinter.SetEventChannel(ch)

	// Run workflow in background goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runWorkflow(wf, result)
		close(ch)
	}()

	// Block on TUI (main goroutine). When the user quits early, Run returns
	// while the workflow is still running: cancel in-flight work and wait for
	// the background goroutine — otherwise reading result below would race
	// its writes and the goroutine would leak.
	e.realtimePrinter.Run()
	if e.execCancel != nil {
		e.execCancel()
	}
	wg.Wait()

	// Mask sensitive values in the finalized result (see finalizeResult).
	e.finalizeResult(result)
	return result
}

// finalizeResult masks sensitive values in the finalized result tree: any
// occurrence of a sensitive variable's value in step outputs (recursively,
// including container children) is replaced with "******". This runs once at
// the end of Execute so stored snapshots, exports, and API responses are
// masked by construction.
//
// Real-time output during execution (terminal stream, progress events) is
// intentionally NOT masked — masking there would garble live logs. Result
// variables are also left untouched here: they are masked at display time by
// MaskWorkflowResult (export/API), and stored snapshots must keep real values
// so replay can restore them.
func (e *Executor) finalizeResult(result *WorkflowResult) {
	if result == nil || len(result.SensitivePatterns) == 0 {
		return
	}
	MaskStepResultVariables(result.Steps, result.SensitivePatterns, e.context.Snapshot())
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

	// Fire workflow_end hooks. An ai_auto hook may veto the outcome (abort).
	if hr := e.fireWorkflowHooks(wf, result); hr.Action == "abort" {
		result.Status = "failed"
		if result.Error != "" {
			result.Error += "\n"
		}
		result.Error += fmt.Sprintf("aborted by hook: %s", hr.Reason)
	}

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
	Order     int    // 在 YAML 中的声明顺序，用于确定性拓扑排序
}

// buildDAGGraph builds a DAG graph from workflow steps
// 注意：只处理主流程节点，容器子节点由容器的 executeStep 内部处理
func (e *Executor) buildDAGGraph(steps []Step) (map[string]*DAGNode, error) {
	graph := make(map[string]*DAGNode)

	// 创建 ID 映射：原始 name → 生成的 ID
	nameToId := make(map[string]string)

	// First pass: create all nodes (只处理顶层步骤，不递归处理容器子节点)
	for i, step := range steps {
		id := step.ID
		if id == "" {
			id = step.Name // 直接使用 name 作为 ID
		}

		nameToId[step.Name] = id

		graph[id] = &DAGNode{
			Step:      step,
			ID:        id,
			DependsOn: step.DependsOn,
			Next:      step.Next,
			JoinMode:  step.JoinMode,
			Order:     i,
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

	// Calculate in-degree based on dependencies, and build the reverse
	// adjacency (dependents) used to decrement in-degrees. Both must derive
	// from DependsOn: decrementing along Next under-counts nodes whose
	// dependencies exist only as explicit depends_on entries (no matching
	// next edge), which used to produce a false "DAG contains a cycle" error.
	// (buildDAGGraph already folds next edges into DependsOn, so DependsOn is
	// the complete edge set.)
	dependents := make(map[string][]string)
	for _, node := range graph {
		for _, dep := range node.DependsOn {
			if _, ok := graph[dep]; ok {
				inDegree[node.ID]++
				dependents[dep] = append(dependents[dep], node.ID)
			}
		}
	}

	// Sort dependents by declaration order so Kahn's traversal is
	// deterministic — map iteration order is random across runs, and the
	// resulting order feeds wave scheduling, result collection, and logs.
	for id := range dependents {
		sort.SliceStable(dependents[id], func(a, b int) bool {
			return graph[dependents[id][a]].Order < graph[dependents[id][b]].Order
		})
	}

	// Find all nodes with in-degree 0 (entry nodes), in declaration order.
	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	sort.SliceStable(queue, func(a, b int) bool {
		return graph[queue[a]].Order < graph[queue[b]].Order
	})

	// Process queue
	result := []string{}
	for len(queue) > 0 {
		// Take first node
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)

		// Reduce in-degree of nodes that depend on the current node
		// (reverse DependsOn adjacency — see above).
		for _, depID := range dependents[id] {
			if inDegree[depID] > 0 {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					queue = append(queue, depID)
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

// isContainerAction reports whether the action is a container (condition,
// parallel, foreach, loop) whose children are scheduled as a sub-DAG via
// executeContainerDAG rather than dispatched through executeStep.
func isContainerAction(action string) bool {
	switch action {
	case "condition", "parallel", "foreach", "loop":
		return true
	}
	return false
}

// waveConfig parameterizes one DAG wave schedule (runWaves). The three call
// sites — top-level executeDAG, executeContainerDAG (condition/parallel
// children), and executeForeach (per-iteration do-steps) — share the same
// scheduling skeleton and differ only in the knobs below.
type waveConfig struct {
	graph map[string]*DAGNode
	order []string // topological order (also determines first-wave ordering)
	// exec runs one node; it owns the container-vs-plain dispatch and the
	// depth/parentID (and, for foreach, the per-iteration step ID rewrite).
	exec func(node *DAGNode) StepResult
	// collect is called once per finished node, in ready order, after its
	// wave completes (append to result.Steps / container children, ...).
	collect func(id string, sr *StepResult)
	// failError builds the first-failure message for a failed node
	// (each call site has its own wording).
	failError func(sr *StepResult) string
	// checkCancel makes runWaves stop scheduling new waves once the run is
	// canceled (top-level DAG only).
	checkCancel bool
	// joinAny enables join_mode: any (top-level DAG only; container and
	// foreach sub-DAGs historically only support join_mode: all).
	joinAny bool
	// markSkipped, if set, is called for every still-waiting node when the
	// schedule ends in failure (top-level DAG synthesizes skipped results).
	markSkipped func(id string)
}

// runWaves executes a DAG in waves: all dependency-free nodes run
// concurrently, then the next wave is computed from what completed. It
// returns (failed, firstErr); failure stops scheduling (unless every failure
// so far had continue_on_error).
func (e *Executor) runWaves(cfg waveConfig) (failed bool, firstErr string) {
	// Track completed nodes and their results
	completed := make(map[string]*StepResult)

	// Track nodes waiting for dependencies
	waiting := make(map[string][]string) // nodeID -> pending dependencies

	// Initialize waiting list
	for id, node := range cfg.graph {
		pending := []string{}
		for _, dep := range node.DependsOn {
			if _, ok := cfg.graph[dep]; ok {
				pending = append(pending, dep)
			}
		}
		if len(pending) > 0 {
			waiting[id] = pending
		}
	}

	// Find entry nodes (no dependencies)
	ready := []string{}
	for _, id := range cfg.order {
		if len(waiting[id]) == 0 {
			ready = append(ready, id)
		}
	}

	// Execute in waves (parallel execution of independent nodes)
	for len(ready) > 0 && !failed {
		// Stop scheduling new waves once the run is canceled (e.g. TUI quit).
		if cfg.checkCancel {
			if err := e.executionContext().Err(); err != nil {
				failed = true
				firstErr = "workflow canceled"
				break
			}
		}

		// Execute all ready nodes concurrently
		var wg sync.WaitGroup
		waveResults := make(map[string]*StepResult)
		resultsMutex := sync.Mutex{}

		for _, id := range ready {
			wg.Add(1)
			go func(nodeID string) {
				defer wg.Done()

				stepResult := cfg.exec(cfg.graph[nodeID])

				resultsMutex.Lock()
				waveResults[nodeID] = &stepResult
				resultsMutex.Unlock()
			}(id)
		}

		wg.Wait()

		// Process wave results
		for _, id := range ready {
			sr := waveResults[id]

			cfg.collect(id, sr)

			completed[id] = sr

			if sr.Status == "failed" && !cfg.graph[id].Step.ContinueOnError {
				failed = true
				if firstErr == "" {
					firstErr = cfg.failError(sr)
				}
			}
		}

		if failed {
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
			} else if cfg.joinAny && cfg.graph[id].JoinMode == "any" {
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

	// Mark remaining waiting nodes as skipped (if the call site wants that).
	// The waiting map's iteration order is random, so sort the survivors by
	// topological order first — otherwise two failing runs of the same
	// workflow produce differently ordered skipped results.
	if failed && cfg.markSkipped != nil {
		orderIndex := make(map[string]int, len(cfg.order))
		for i, id := range cfg.order {
			orderIndex[id] = i
		}
		remaining := make([]string, 0, len(waiting))
		for id := range waiting {
			remaining = append(remaining, id)
		}
		sort.Slice(remaining, func(i, j int) bool {
			return orderIndex[remaining[i]] < orderIndex[remaining[j]]
		})
		for _, id := range remaining {
			cfg.markSkipped(id)
		}
	}

	return failed, firstErr
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

	failed, firstErr := e.runWaves(waveConfig{
		graph: graph,
		order: order,
		exec: func(node *DAGNode) StepResult {
			// 如果是容器节点，递归执行子 DAG
			if isContainerAction(node.Step.Action) {
				return e.executeContainerDAG(node.Step, 0, result, "")
			}
			return e.executeStep(node.Step, 0, result, "")
		},
		collect: func(id string, sr *StepResult) {
			result.Steps = append(result.Steps, *sr)
			// Update variables (use Snapshot to avoid racing concurrent Set calls)
			for k, v := range e.context.Snapshot() {
				result.Variables[k] = v
			}
		},
		failError: func(sr *StepResult) string {
			return fmt.Sprintf("step '%s' failed: %s", sr.Name, sr.Error)
		},
		checkCancel: true,
		joinAny:     true,
		markSkipped: func(id string) {
			result.Steps = append(result.Steps, StepResult{
				Name:   graph[id].Step.Name,
				ID:     id,
				Status: "skipped",
				Error:  "skipped due to previous failure",
			})
		},
	})

	// Handle failure (remaining waiting nodes were already marked as skipped)
	if failed {
		result.Status = "failed"
		result.Error = firstErr
	}

	// Propagate nondeterminism: any step that consumes the output of an
	// ai/ai_decide step becomes Nondeterministic itself (taint along
	// DependsOn). Also derives the workflow-level flag.
	result.Nondeterministic = propagateDeterminism(result.Steps)

	// Sum token usage across all steps (including nested children).
	result.TotalInputTokens, result.TotalOutputTokens = sumTokenUsage(result.Steps)
}

// executeStep executes a single step.

// executeForeach 执行 foreach/loop 迭代

// executeStep runs one step and applies control-flow decisions returned by
// ai_auto after_step hooks: "retry" re-runs the step (capped at maxHookRetries
// to prevent infinite AI-retry loops); "skip"/"abort" are applied inside
// executeStepOnce before completion events are emitted.
func (e *Executor) executeStep(step Step, depth int, wfResult *WorkflowResult, parentID string) StepResult {
	const maxHookRetries = 3
	for hookRetries := 0; ; hookRetries++ {
		result, hookDecision := e.executeStepOnce(step, depth, wfResult, parentID)
		if hookDecision.Action == "retry" && hookRetries < maxHookRetries {
			if e.verbose {
				fmt.Printf("  ↻ hook 要求重试 %s (%d/%d): %s\n", step.Name, hookRetries+1, maxHookRetries, hookDecision.Reason)
			}
			continue
		}
		return result
	}
}

// executeStepOnce performs a single dispatch of the step and fires its
// after_step hooks, returning the (possibly ai_auto-influenced) hook decision
// so executeStep can act on "retry".
func (e *Executor) executeStepOnce(step Step, depth int, wfResult *WorkflowResult, parentID string) (StepResult, HookResult) {
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

	// Fail fast when the run was canceled (e.g. TUI quit) — don't start new
	// steps, don't emit start events.
	if cerr := e.executionContext().Err(); cerr != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("canceled: %v", cerr)
		result.EndTime = Now()
		return result, HookResult{}
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
		return result, HookResult{}
	}

	// Replay: if a cache is configured and this deterministic step has a stored
	// result, serve it directly. Nondeterministic steps (AI and their tainted
	// downstream) always re-run — that is the whole point of smart replay.
	// The cached result keeps its original output/error/status so downstream
	// variable substitution behaves identically.
	if e.replayCache != nil {
		var cached *StepResult
		if c, ok := e.replayCache[stepID]; ok {
			cached = c
		} else if c, ok := e.replayCache[step.Name]; ok {
			cached = c
		}
		if cached != nil && !cached.Nondeterministic && cached.Status != "" {
			// Hit: restore the stored result, preserving its determinism flags.
			e.incReplayHit()
			c := *cached
			c.StartTime = startTime
			c.EndTime = Now()
			// Re-emit events so UIs/WS see the step "run" (start + complete).
			e.sendEvent("step_start", step.Name, stepID, step.Action, "running", "", "", depth, parentID, nil)
			e.sendEvent("step_complete", step.Name, stepID, step.Action, c.Status, c.Output, c.Duration, depth, parentID, c.ConditionResult)
			return c, HookResult{}
		}
		e.incReplayMiss()
	}

	var err error
	var output string
	var children []StepResult

	// Step-level retry: repeat the dispatch up to step.Retry+1 times on
	// failure. Only the dispatch is retried — save_output / context.Set happen
	// inside the dispatch functions and are idempotent for most actions (shell
	// re-runs, AI re-calls). The final attempt's result is what sticks.
	maxAttempts := 1
	if step.Retry > 0 {
		maxAttempts = step.Retry + 1
	}
	var retryDelay time.Duration
	if step.RetryDelay != "" {
		if d, e := ParseDuration(step.RetryDelay); e == nil {
			retryDelay = d
		}
	}

	// on_error: ai — outer error-recovery loop. Derived from the execution
	// context so a canceled run aborts the analysis call too.
	ctx2, cancel2 := context.WithTimeout(e.executionContext(), 120*time.Second)
	defer cancel2()

	// on_error: ai — outer error-recovery loop. When the step (including its
	// Phase 6 blind retries) fails AND on_error is configured, the AI analyzes
	// the failure and may decide to retry/skip/abort/suggest. This outer loop
	// caps at maxErrorRetries to prevent infinite AI-retry cycles.
	errorRetries := 0
	maxErrorRetries := 3

errorRecovery:
	for {
		// Inner: Phase 6 blind retry loop.
	attemptLoop:
		for attempt := 0; attempt < maxAttempts; attempt++ {
			// Reset per-attempt state.
			output = ""
			children = nil
			err = nil

			switch step.Action {
			case "shell":
				output, err = e.execShell(step)
				if step.Command != "" {
					result.ShellCommand = step.Command
				} else {
					result.ShellCommand = step.Shell
				}
			case "http":
				output, err = e.execHTTP(step)
				result.HTTPUrl = step.URL
				result.HTTPMethod = step.Method
			case "set":
				output, err = e.execSet(step)
			case "sleep":
				output, err = e.execSleep(step)
				result.SleepDuration = step.Duration
			case "log":
				output = e.execLog(step)
				result.LogMessage = step.Message
			case "template":
				output, err = e.execTemplate(step)
			case "ai":
				var inTok, outTok int
				output, inTok, outTok, err = e.execAI(step, stepID, depth, parentID)
				// Token counts travel with the return value — parallel AI
				// steps each get their own counts, not a shared slot's.
				result.InputTokens = inTok
				result.OutputTokens = outTok
				result.Nondeterministic = true
			case "ai_decide":
				decided, inTok, outTok, derr := e.execAIDecide(step, stepID, depth, parentID)
				result.InputTokens = inTok
				result.OutputTokens = outTok
				if derr != nil {
					err = derr
				} else {
					result.ConditionResult = &decided
					output = fmt.Sprintf("decided: %v", decided)
				}
				result.Nondeterministic = true
			case "script":
				output, err = e.execScript(step)
				result.SideEffecting = true
			case "workflow":
				output, children, err = e.execWorkflow(step)
				result.SideEffecting = true
			default:
				err = fmt.Errorf("unknown action: %s", step.Action)
			}

			if err == nil {
				result.Retries = attempt
				break attemptLoop // success
			}
			if attempt < maxAttempts-1 {
				if e.verbose {
					fmt.Printf("%s  ↻ retrying %s (attempt %d/%d) after %s\n", indent, step.Name, attempt+2, maxAttempts, retryDelay)
				}
				if retryDelay > 0 {
					time.Sleep(retryDelay)
				}
			} else {
				result.Retries = attempt
			}
		} // end attemptLoop

		// on_error: ai — if the step still failed after all retries, let AI
		// analyze and decide what to do.
		if err != nil && e.shouldAnalyzeError(step) && e.aiProvider != nil && errorRetries < maxErrorRetries {
			errorRetries++
			mode := e.errorAnalysisMode(step)

			params := ai.ErrorAnalysisParams{
				StepName:     step.Name,
				Action:       step.Action,
				Command:      e.stepCommandForError(step),
				Output:       output,
				Error:        err.Error(),
				CustomPrompt: step.OnErrorPrompt,
			}
			asst := ai.NewAssistant(e.aiProvider)
			decision, derr := asst.AnalyzeError(ctx2, params)
			if derr != nil {
				decision = ai.ErrorDecision{Action: "suggest", Reason: derr.Error()}
			}

			if e.verbose {
				fmt.Printf("%s  🤖 on_error: AI 分析 → %s (%s)\n", indent, decision.Action, decision.Reason)
			}

			switch {
			case decision.Action == "retry" && mode == "auto":
				if e.verbose {
					fmt.Printf("%s  ↻ AI 建议重试 (error recovery %d/%d)\n", indent, errorRetries, maxErrorRetries)
				}
				// Reset and re-enter the attempt loop.
				result.Retries = 0
				continue errorRecovery
			case decision.Action == "skip" && mode == "auto":
				result.Status = "skipped"
				result.Error = fmt.Sprintf("skipped (AI: %s)", decision.Reason)
				result.Children = children
				result.EndTime = Now()
				e.sendEvent("step_complete", step.Name, stepID, step.Action, "skipped", "", "", depth, parentID, nil)
				return result, HookResult{}
			case decision.Action == "abort" && mode == "auto":
				// Fall through to normal failure handling, but augment error.
				err = fmt.Errorf("%s\nAI 建议中止: %s", err, decision.Reason)
			case decision.Action == "suggest" || mode == "suggest":
				// Most conservative: append AI's suggestion to the error.
				err = fmt.Errorf("%s\nAI 建议: %s", err, decision.Reason)
			}
		}

		break errorRecovery
	} // end errorRecovery

	// Set children (currently only the workflow action produces child results;
	// container actions never reach this dispatch — the wave schedulers route
	// them to executeContainerDAG).
	result.Children = children

	// 填充确定性标记初值(见 docs/PRODUCT.md 的"三种确定性层级")
	// (AI token counts were already recorded on the result by the dispatch
	// above — they travel with the execAI/execAIDecide return values.)
	switch step.Action {
	case "shell", "http", "template":
		result.SideEffecting = true
	case "ai", "ai_decide":
		result.Nondeterministic = true
	}

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
		// http 失败(非 2xx)时响应体已在 output 中,保留到结果里便于排查
		if step.Action == "http" && output != "" {
			result.Output = output
		}
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

	// Fire after_step hooks (step-level + inherited workflow-level). An
	// ai_auto hook may return a control-flow decision: skip/abort are applied
	// here, before completion events, so the events carry the final status;
	// retry is handled by the executeStep wrapper re-dispatching the step.
	hookDecision := e.fireStepHooks(step, result)
	switch hookDecision.Action {
	case "skip":
		result.Status = "skipped"
		result.Error = fmt.Sprintf("skipped (hook: %s)", hookDecision.Reason)
		result.EndTime = Now()
	case "abort":
		// Fail the step so the DAG stops (unless continue_on_error).
		result.Status = "failed"
		if result.Error != "" {
			result.Error += "\n"
		}
		result.Error += fmt.Sprintf("aborted by hook: %s", hookDecision.Reason)
		result.EndTime = Now()
	}

	// Send step_output event if there's output
	if result.Output != "" {
		e.sendEvent("step_output", step.Name, stepID, step.Action, result.Status, result.Output, result.Duration, depth, parentID, nil)
	}

	// Send step_complete event
	e.sendEvent("step_complete", step.Name, stepID, step.Action, result.Status, "", result.Duration, depth, parentID, result.ConditionResult)

	return result, hookDecision
}

// shouldAnalyzeError reports whether on_error: ai should fire for this step.
func (e *Executor) shouldAnalyzeError(step Step) bool {
	if step.OnError != "" {
		return step.OnError == "ai" || step.OnError == "ai_auto"
	}
	return e.aiOnError == "ai" || e.aiOnError == "ai_auto"
}

// errorAnalysisMode returns "auto" or "suggest" based on the on_error config.
func (e *Executor) errorAnalysisMode(step Step) string {
	mode := step.OnError
	if mode == "" {
		mode = e.aiOnErrorMode
	}
	if mode == "ai_auto" {
		return "auto"
	}
	return "suggest"
}

// stepCommandForError extracts the relevant command for the error prompt.
func (e *Executor) stepCommandForError(step Step) string {
	switch step.Action {
	case "shell":
		if step.Command != "" {
			return step.Command
		}
		return step.Shell
	case "http":
		return step.URL
	case "ai":
		return step.Prompt
	case "ai_decide":
		return step.Question
	case "script":
		return step.Code
	default:
		return ""
	}
}

// fireStepHooks fires after_step hooks for a completed step. Both step-level
// and workflow-level hooks are checked. The executor must have access to the
// workflow's hooks (stored at Execute time). Returns the merged control-flow
// decision of all fired hooks (ai_auto hooks only; other hook types return no
// decision). When several hooks decide, the highest-priority action wins:
// abort > retry > skip.
func (e *Executor) fireStepHooks(step Step, result StepResult) HookResult {
	event := HookEvent{
		Phase:        HookAfterStep,
		StepName:     step.Name,
		Action:       step.Action,
		Status:       result.Status,
		Output:       result.Output,
		Error:        result.Error,
		Duration:     result.Duration,
		WorkflowName: e.workflowName,
		Variables:    e.context.Snapshot(),
	}
	decision := HookResult{}
	// Step-level hooks.
	for _, hook := range step.Hooks {
		decision = mergeHookDecision(decision, executeHook(hook, event, e))
	}
	// Workflow-level hooks (inherited).
	for _, hook := range e.workflowHooks {
		if hook.On == HookAfterStep {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	// Server-level global hooks.
	for _, hook := range e.globalHooks {
		if hook.On == HookAfterStep {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	return decision
}

// mergeHookDecision picks the higher-priority of two hook decisions.
func mergeHookDecision(cur, next HookResult) HookResult {
	if hookActionPriority(next.Action) > hookActionPriority(cur.Action) {
		return next
	}
	return cur
}

// hookActionPriority ranks control-flow actions: abort > retry > skip; ""
// (no effect) and "suggest" (suggest-only mode) never affect control flow.
func hookActionPriority(action string) int {
	switch action {
	case "abort":
		return 3
	case "retry":
		return 2
	case "skip":
		return 1
	}
	return 0
}

// fireWorkflowHooks fires workflow_end hooks after the entire workflow
// completes. Returns the merged control-flow decision; only "abort" is acted
// upon by the caller.
func (e *Executor) fireWorkflowHooks(wf *Workflow, result *WorkflowResult) HookResult {
	event := HookEvent{
		Phase:        HookWorkflowEnd,
		WorkflowName: wf.Name,
		Status:       result.Status,
		Error:        result.Error,
		Output:       result.Error, // best context for end hooks
		Variables:    result.Variables,
	}
	decision := HookResult{}
	for _, hook := range wf.Hooks {
		if hook.On == HookWorkflowEnd {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	// Server-level global hooks.
	for _, hook := range e.globalHooks {
		if hook.On == HookWorkflowEnd {
			decision = mergeHookDecision(decision, executeHook(hook, event, e))
		}
	}
	return decision
}
