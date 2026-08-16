package workflow

import (
	"fmt"
	"sort"
	"sync"
)

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
	// canceled. All three call sites set it, so a TUI quit (or any other
	// abort) stops container and foreach wave scheduling within one wave
	// too, not just the top-level DAG.
	checkCancel bool
	// joinAny enables join_mode: any. All three call sites set it, so a
	// sub-DAG inside a container/foreach honors join_mode: any exactly like
	// the top-level DAG (previously containers silently treated it as all).
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

	// Precompute declaration-order positions so every wave's ready slice is
	// deterministic regardless of map iteration order in the waiting map.
	orderIndex := make(map[string]int, len(cfg.order))
	for i, id := range cfg.order {
		orderIndex[id] = i
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

		sort.Slice(newReady, func(i, j int) bool {
			return orderIndex[newReady[i]] < orderIndex[newReady[j]]
		})
		ready = newReady
	}

	// Mark remaining waiting nodes as skipped (if the call site wants that).
	// The waiting map's iteration order is random, so sort the survivors by
	// topological order first — otherwise two failing runs of the same
	// workflow produce differently ordered skipped results.
	if failed && cfg.markSkipped != nil {
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
