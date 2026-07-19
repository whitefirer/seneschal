package workflow

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse parses a YAML byte slice into a Workflow.
func Parse(data []byte) (*Workflow, error) {
	var wf Workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}

	// Normalize If field to Expression (for backward compatibility)
	wf.normalizeIfToExpression(wf.Steps)

	return &wf, nil
}

// normalizeIfToExpression recursively copies If field to Expression
func (wf *Workflow) normalizeIfToExpression(steps []Step) {
	for i := range steps {
		step := &steps[i]
		if step.If != "" && step.Expression == "" {
			step.Expression = step.If
		}
		// Recursively process child steps
		if step.Action == "condition" {
			wf.normalizeIfToExpression(step.Then)
			wf.normalizeIfToExpression(step.Else)
		} else if step.Action == "parallel" {
			wf.normalizeIfToExpression(step.Steps)
		} else if step.Action == "foreach" || step.Action == "loop" {
			wf.normalizeIfToExpression(step.Do)
		}
	}
}

// ParseFile reads and parses a workflow YAML file.
func ParseFile(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow file %s: %w", path, err)
	}
	return Parse(data)
}

// ToYAML serializes a workflow to YAML bytes.
func (wf *Workflow) ToYAML() ([]byte, error) {
	return yaml.Marshal(wf)
}

// Save writes the workflow YAML to a file.
func (wf *Workflow) Save(path string) error {
	data, err := wf.ToYAML()
	if err != nil {
		return fmt.Errorf("serialize workflow: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write workflow file %s: %w", path, err)
	}
	return nil
}

// Load reads a workflow from a file, updating in place.
func (wf *Workflow) Load(path string) error {
	loaded, err := ParseFile(path)
	if err != nil {
		return err
	}
	*wf = *loaded
	return nil
}

// ToYAMLString returns the YAML representation as a string.
func (wf *Workflow) ToYAMLString() (string, error) {
	data, err := wf.ToYAML()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Validate checks the workflow for structural issues.
func (wf *Workflow) Validate() []error {
	var errs []error
	if wf.Name == "" {
		errs = append(errs, fmt.Errorf("workflow name is required"))
	}
	for i, step := range wf.Steps {
		stepErrs := ValidateStep(step, i)
		errs = append(errs, stepErrs...)
	}
	errs = append(errs, validateUniqueStepIDs(wf.Steps)...)
	return errs
}

// validateUniqueStepIDs walks the full step tree (including nested container
// children) and reports duplicate effective step IDs. The effective ID is
// step.ID, falling back to step.Name — the same derivation buildDAGGraph and
// buildStepMapRecursive use. Duplicates previously silently overwrote each
// other in those maps, corrupting dependency inference and step lookup.
func validateUniqueStepIDs(steps []Step) []error {
	var errs []error
	seen := make(map[string]string) // effective ID -> first location
	var walk func(steps []Step, path string)
	walk = func(steps []Step, path string) {
		for i, step := range steps {
			loc := fmt.Sprintf("%s[%d] (%s)", path, i, step.Name)
			id := step.ID
			if id == "" {
				id = step.Name
			}
			if id != "" {
				if first, dup := seen[id]; dup {
					errs = append(errs, fmt.Errorf("duplicate step id %q: %s and %s — step names/ids must be unique across the workflow", id, first, loc))
				} else {
					seen[id] = loc
				}
			}
			switch step.Action {
			case "condition":
				walk(step.Then, loc+".then")
				walk(step.Else, loc+".else")
			case "parallel":
				walk(step.Steps, loc+".steps")
			case "foreach", "loop":
				walk(step.Do, loc+".do")
			}
		}
	}
	walk(steps, "steps")
	return errs
}

// ValidateStep checks a single step for structural issues.
func ValidateStep(step Step, index int) []error {
	var errs []error
	if step.Name == "" {
		errs = append(errs, fmt.Errorf("step[%d]: name is required", index))
	}
	switch step.Action {
	case "":
		errs = append(errs, fmt.Errorf("step[%d] (%s): action is required", index, step.Name))
	case "shell":
		if step.Command == "" && step.Shell == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): shell action requires 'command' or 'shell'", index, step.Name))
		}
	case "http":
		if step.URL == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): http action requires 'url'", index, step.Name))
		}
	case "condition":
		if step.Expression == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): condition action requires 'expression'", index, step.Name))
		}
	case "set":
		// value can reference other vars, so empty is ok for pure deletion
	case "sleep":
		if step.Duration == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): sleep action requires 'duration'", index, step.Name))
		}
	case "log":
		// message is optional, level defaults to info
	case "parallel":
		for j, sub := range step.Steps {
			subErrs := ValidateStep(sub, j)
			for _, e := range subErrs {
				errs = append(errs, fmt.Errorf("step[%d] (%s) parallel[%d]: %v", index, step.Name, j, e))
			}
		}
	case "template":
		if step.Source == "" || step.Output == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): template action requires 'source' and 'output'", index, step.Name))
		}
	case "foreach":
		if step.Do == nil || len(step.Do) == 0 {
			errs = append(errs, fmt.Errorf("step[%d] (%s): foreach action requires 'do' steps", index, step.Name))
		}
	case "ai":
		if step.Prompt == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): ai action requires 'prompt'", index, step.Name))
		}
	case "script":
		if step.Lang == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): script action requires 'lang' (e.g. python, node)", index, step.Name))
		}
		if step.Code == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): script action requires 'code'", index, step.Name))
		}
	case "workflow":
		if step.Source == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): workflow action requires 'source' (path to sub-workflow YAML)", index, step.Name))
		}
	case "ai_decide":
		if step.Question == "" {
			errs = append(errs, fmt.Errorf("step[%d] (%s): ai_decide action requires 'question'", index, step.Name))
		}
	default:
		errs = append(errs, fmt.Errorf("step[%d] (%s): unknown action '%s'", index, step.Name, step.Action))
	}
	return errs
}

// CreateWorkflow creates a new workflow with the given name.
func CreateWorkflow(name, description string) *Workflow {
	return &Workflow{
		Name:        name,
		Version:     "1.0",
		Description: description,
		Variables:   make(map[string]string),
		Steps:       []Step{},
	}
}

// AddStep adds a step to the workflow.
func (wf *Workflow) AddStep(step Step) {
	wf.Steps = append(wf.Steps, step)
}

// SetVariable sets a workflow-level variable.
func (wf *Workflow) SetVariable(key, value string) {
	if wf.Variables == nil {
		wf.Variables = make(map[string]string)
	}
	wf.Variables[key] = value
}

// PrintWorkflow prints the workflow YAML to the given writer.
func (wf *Workflow) PrintWorkflow(w io.Writer) error {
	data, err := wf.ToYAML()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// InferDependencies 自动推断依赖关系（支持分层 DAG）
// 规则：
// 1. 递归填充运行时元数据（ParentId/BranchType/BranchIndex）
// 2. 主流程相邻节点自动添加 next/depends_on
// 3. 容器内子节点按顺序添加依赖（支持子节点间的 next/depends_on）
// 4. next → depends_on 反向推断
// 5. 显式依赖优先（如果已有则不覆盖）
// 6. 验证依赖关系合法性
func (wf *Workflow) InferDependencies() error {
	// 1. 递归填充运行时元数据
	wf.fillRuntimeMetadata(wf.Steps, "", "", 0)

	// 2. 主流程相邻节点推断（线性流程）
	wf.inferLinearDependencies(wf.Steps)

	// 3. next → depends_on 反向推断（全局）
	wf.inferNextToDependsOnGlobal(wf.Steps)

	// 4. 容器内子节点推断（递归）
	wf.inferContainerDependenciesRecursive(wf.Steps)

	// 5. 验证依赖关系合法性
	if err := wf.ValidateDependencies(); err != nil {
		return err
	}

	return nil
}

// fillRuntimeMetadata 递归填充运行时元数据
func (wf *Workflow) fillRuntimeMetadata(steps []Step, parentId, branchType string, startIndex int) {
	for i := range steps {
		step := &steps[i]
		step.ParentId = parentId
		step.BranchType = branchType
		step.BranchIndex = startIndex + i

		// 递归处理子节点
		if step.Action == "condition" {
			wf.fillRuntimeMetadata(step.Then, step.Name, "then", 0)
			wf.fillRuntimeMetadata(step.Else, step.Name, "else", 0)
		} else if step.Action == "parallel" {
			wf.fillRuntimeMetadata(step.Steps, step.Name, "parallel", 0)
		} else if step.Action == "foreach" || step.Action == "loop" {
			wf.fillRuntimeMetadata(step.Do, step.Name, "do", 0)
		}
	}
}

// inferLinearDependencies 推断线性流程的相邻节点依赖
func (wf *Workflow) inferLinearDependencies(steps []Step) {
	if len(steps) > 1 {
		for i := 0; i < len(steps)-1; i++ {
			current := &steps[i]
			next := &steps[i+1]

			// 如果没有显式 next，添加后继
			if len(current.Next) == 0 {
				currentId := getStepId(current)
				nextId := getStepId(next)
				if currentId != "" && nextId != "" {
					current.Next = []string{nextId}
				}
			}

			// 如果没有显式 depends_on，添加前驱
			if len(next.DependsOn) == 0 {
				currentId := getStepId(current)
				if currentId != "" {
					next.DependsOn = append(next.DependsOn, currentId)
				}
			}
		}
	}

	// 递归处理容器子节点
	for i := range steps {
		step := &steps[i]
		if step.Action == "condition" {
			wf.inferLinearDependencies(step.Then)
			wf.inferLinearDependencies(step.Else)
		} else if step.Action == "parallel" {
			wf.inferLinearDependencies(step.Steps)
		} else if step.Action == "foreach" || step.Action == "loop" {
			wf.inferLinearDependencies(step.Do)
		}
	}
}

// getStepId 获取步骤的唯一标识（优先 ID，其次 Name）
func getStepId(step *Step) string {
	if step.ID != "" {
		return step.ID
	}
	return step.Name
}

// inferNextToDependsOnGlobal 全局 next → depends_on 反向推断（递归所有层级）
func (wf *Workflow) inferNextToDependsOnGlobal(steps []Step) {
	// 构建全局映射
	stepMap := make(map[string]*Step)
	wf.buildStepMapRecursive(steps, stepMap)

	// 遍历所有 next，添加反向 depends_on
	for _, step := range steps {
		for _, nextID := range step.Next {
			if nextStep, ok := stepMap[nextID]; ok {
				// 检查是否已存在
				exists := false
				currentId := getStepId(&step)
				for _, dep := range nextStep.DependsOn {
					if dep == currentId {
						exists = true
						break
					}
				}
				if !exists && currentId != "" {
					nextStep.DependsOn = append(nextStep.DependsOn, currentId)
				}
			}
		}
	}
}

// buildStepMapRecursive 递归构建步骤映射
func (wf *Workflow) buildStepMapRecursive(steps []Step, stepMap map[string]*Step) {
	for i := range steps {
		step := &steps[i]
		id := getStepId(step)
		if id != "" {
			stepMap[id] = step
		}
		// 递归处理子节点
		if step.Action == "condition" {
			wf.buildStepMapRecursive(step.Then, stepMap)
			wf.buildStepMapRecursive(step.Else, stepMap)
		} else if step.Action == "parallel" {
			wf.buildStepMapRecursive(step.Steps, stepMap)
		} else if step.Action == "foreach" || step.Action == "loop" {
			wf.buildStepMapRecursive(step.Do, stepMap)
		}
	}
}

// inferContainerDependenciesRecursive 递归推断容器内子节点的依赖关系
func (wf *Workflow) inferContainerDependenciesRecursive(steps []Step) {
	for i := range steps {
		step := &steps[i]

		// 处理 condition.then/else
		if step.Action == "condition" {
			// Then 分支：子节点之间链式依赖
			if len(step.Then) > 0 {
				// 第一个 then 节点依赖 condition 父节点
				firstThen := &step.Then[0]
				if len(firstThen.DependsOn) == 0 {
					parentId := getStepId(step)
					if parentId != "" {
						firstThen.DependsOn = append(firstThen.DependsOn, parentId)
					}
				}
				// then 子节点之间链式连接
				for j := 0; j < len(step.Then)-1; j++ {
					current := &step.Then[j]
					next := &step.Then[j+1]
					if len(current.Next) == 0 {
						currentId := getStepId(current)
						nextId := getStepId(next)
						if currentId != "" && nextId != "" {
							current.Next = []string{nextId}
						}
					}
				}
			}
			// 递归处理嵌套容器
			for j := range step.Then {
				thenStep := &step.Then[j]
				if thenStep.Action == "condition" || thenStep.Action == "parallel" || thenStep.Action == "foreach" || thenStep.Action == "loop" {
					wf.inferContainerDependenciesRecursive([]Step{*thenStep})
				}
			}
			for j := range step.Else {
				elseStep := &step.Else[j]
				if elseStep.Action == "condition" || elseStep.Action == "parallel" || elseStep.Action == "foreach" || elseStep.Action == "loop" {
					wf.inferContainerDependenciesRecursive([]Step{*elseStep})
				}
			}
		} else if step.Action == "parallel" {
			// Parallel 分支：子节点默认并行（不添加依赖），只有显式指定 next/depends_on 的才按拓扑序执行
			// 只递归处理嵌套容器，不自动添加依赖
			for j := range step.Steps {
				child := &step.Steps[j]
				// 递归处理嵌套容器
				if child.Action == "condition" || child.Action == "parallel" || child.Action == "foreach" || child.Action == "loop" {
					wf.inferContainerDependenciesRecursive([]Step{*child})
				}
			}
		} else if step.Action == "foreach" || step.Action == "loop" {
			// Foreach/Loop 分支：子节点之间链式依赖
			if len(step.Do) > 0 {
				// 第一个 do 节点依赖父节点
				firstDo := &step.Do[0]
				if len(firstDo.DependsOn) == 0 {
					parentId := getStepId(step)
					if parentId != "" {
						firstDo.DependsOn = append(firstDo.DependsOn, parentId)
					}
				}
				// do 子节点之间链式连接
				for j := 0; j < len(step.Do)-1; j++ {
					current := &step.Do[j]
					next := &step.Do[j+1]
					if len(current.Next) == 0 {
						currentId := getStepId(current)
						nextId := getStepId(next)
						if currentId != "" && nextId != "" {
							current.Next = []string{nextId}
						}
					}
				}
			}
			// 递归处理嵌套容器
			for j := range step.Do {
				doStep := &step.Do[j]
				if doStep.Action == "condition" || doStep.Action == "parallel" || doStep.Action == "foreach" || doStep.Action == "loop" {
					wf.inferContainerDependenciesRecursive([]Step{*doStep})
				}
			}
		}
	}
}

// ValidateDependencies 验证依赖关系的合法性
// 规则：
// 1. 子节点不能依赖外部节点（跨层依赖）
// 2. 外部节点不能依赖子节点
// 3. then 节点不能依赖 else 节点（跨分支依赖）
// 4. parallel 子节点之间不应有依赖（并行语义）
func (wf *Workflow) ValidateDependencies() error {
	stepMap := make(map[string]*Step)
	wf.buildStepMapRecursive(wf.Steps, stepMap)

	for _, step := range stepMap {
		// 验证 depends_on
		for _, depId := range step.DependsOn {
			depStep, ok := stepMap[depId]
			if !ok {
				continue // 依赖的节点不存在，会在执行时报错
			}

			// 规则 1 & 2: 检查跨层依赖（但允许子节点依赖父节点）
			if !wf.isSameLayer(step, depStep) {
				// 检查是否是子节点依赖父节点（合法）
				if step.ParentId == getStepId(depStep) {
					// 合法：子节点依赖父容器
					continue
				}
				return fmt.Errorf("cross-layer dependency detected: %q depends on %q (different layers)",
					getStepId(step), getStepId(depStep))
			}

			// 规则 3: 检查跨分支依赖
			if !wf.isSameBranch(step, depStep) {
				return fmt.Errorf("cross-branch dependency detected: %q depends on %q (different branches)",
					getStepId(step), getStepId(depStep))
			}
		}
	}
	return nil
}

// isSameLayer 检查两个节点是否在同一层级（相同的 ParentId）
func (wf *Workflow) isSameLayer(a, b *Step) bool {
	return a.ParentId == b.ParentId
}

// isSameBranch 检查两个节点是否在同一分支（可以同时执行）
func (wf *Workflow) isSameBranch(a, b *Step) bool {
	// 如果 ParentId 相同，检查 BranchType
	if a.ParentId == b.ParentId {
		return a.BranchType == b.BranchType
	}
	// 如果 ParentId 不同，但都是顶层节点
	if a.ParentId == "" && b.ParentId == "" {
		return true
	}
	// 其他情况需要检查祖先
	return false
}
