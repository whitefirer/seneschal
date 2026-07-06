package workflow

import "goworkflow/workflow/ai"

// AIConfig is the workflow-level AI configuration (the `ai:` block).
// It is an alias for ai.Config so workflow YAML parsing reuses the same
// tags. See ai.BuildProvider for how a provider is constructed from this
// plus environment variables.
type AIConfig = ai.Config

// Step represents a single unit of work in a workflow.
type Step struct {
	Name           string            `yaml:"name"`
	ID             string            `yaml:"id,omitempty"`
	Action         string            `yaml:"action"` // shell, http, condition, parallel, set, sleep, log, template, foreach
	ContinueOnError bool             `yaml:"continue_on_error"`
	Description    string            `yaml:"description,omitempty"`

	// Shell action
	Command string `yaml:"command,omitempty"`
	Shell   string `yaml:"shell,omitempty"` // default: sh on unix, cmd on windows
	Dir     string `yaml:"dir,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	OutputVar string `yaml:"output_var,omitempty"` // save entire output to single variable
	OutputVars []string `yaml:"output_vars,omitempty"` // parse KEY=VALUE lines, save each as variable

	// HTTP action
	URL         string            `yaml:"url,omitempty"`
	Method      string            `yaml:"method,omitempty"` // GET, POST, PUT, DELETE
	Headers     map[string]string `yaml:"headers,omitempty"`
	Body        string            `yaml:"body,omitempty"`
	Timeout     string            `yaml:"timeout,omitempty"` // e.g. "30s"
	SaveOutput  string            `yaml:"save_output,omitempty"` // save response to variable

	// Condition action
	Expression string `yaml:"expression,omitempty" json:"expression,omitempty"`  // 也支持 if 字段
	If         string `yaml:"if,omitempty"`  // 条件表达式（别名）
	Then       []Step `yaml:"then,omitempty"`
	Else       []Step `yaml:"else,omitempty"`

	// Parallel action
	Steps []Step `yaml:"steps,omitempty"`

	// Set action (set variable)
	Value string `yaml:"value,omitempty"`

	// Sleep action
	Duration string `yaml:"duration,omitempty"` // e.g. "5s", "1m"

	// Log action
	Message string `yaml:"message,omitempty"`
	Level   string `yaml:"level,omitempty"` // info, warn, error

	// Template action
	Source string `yaml:"source,omitempty"` // template file path
	Output string `yaml:"output,omitempty"` // output file path

	// Foreach action
	Items    interface{} `yaml:"items,omitempty"` // string (variable name) or list
	ItemVar  string      `yaml:"item_var,omitempty"` // loop variable name, default: "item"
	Do       []Step      `yaml:"do,omitempty"`

	// AI action (action: "ai")
	// Prompt is the user message; supports {{.var}} templates. By default only
	// the variables referenced here are passed to the model (conservative
	// default — override with Inputs).
	Prompt   string   `yaml:"prompt,omitempty"`
	System   string   `yaml:"system,omitempty"`   // optional system prompt
	Inputs   []string `yaml:"inputs,omitempty"`   // explicit vars to expose to the model
	Question string   `yaml:"question,omitempty"` // ai_decide: the question to answer true/false
	Model    string   `yaml:"model,omitempty"`    // override the workflow-level model for this step
	// Memory explicitly declares which upstream steps' AI output to include as
	// conversation history. Empty = automatic (all prior AI steps' turns).
	Memory   []string `yaml:"memory,omitempty"`

	// DAG support (依赖关系和下一节点)
	Next       []string `yaml:"next,omitempty"`       // 指定下一节点列表（DAG模式）
	DependsOn  []string `yaml:"depends_on,omitempty"` // 依赖的节点列表（DAG模式）
	JoinMode   string   `yaml:"join_mode,omitempty"`  // 汇合模式: "all" (全部完成) 或 "any" (任意完成)

	// Runtime metadata (not serialized to YAML)
	// 运行时元数据，由解析器自动填充，不序列化到 YAML
	ParentId    string `yaml:"-"` // 父容器节点 ID
	BranchType  string `yaml:"-"` // 分支类型："then"/"else"/"parallel"/"do"
	BranchIndex int    `yaml:"-"` // 分支内序号
}

// Workflow represents a complete workflow definition.
type Workflow struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Variables   map[string]string `yaml:"variables,omitempty"`
	Mode        string            `yaml:"mode,omitempty"` // "linear" (默认, 顺序执行) 或 "dag" (DAG拓扑执行)
	Steps       []Step            `yaml:"steps"`
	// AI is the workflow-level AI provider configuration. API keys are NOT
	// stored here (they come from environment variables at runtime) — see
	// docs/PRODUCT.md "Provider 架构" and ai.BuildProvider.
	AI AIConfig `yaml:"ai,omitempty"`
}

// StepResult holds the result of executing a single step.
type StepResult struct {
	Name        string        `json:"name"`
	ID          string        `json:"id,omitempty"`
	Action      string        `json:"action,omitempty"`
	Description string        `json:"description,omitempty"`
	Status      string        `json:"status"` // success, failed, skipped
	Output      string        `json:"output,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    string        `json:"duration,omitempty"`
	StartTime   string        `json:"startTime,omitempty"`
	EndTime     string        `json:"endTime,omitempty"`
	Children    []StepResult  `json:"children,omitempty"`
	// DAG fields
	Next       []string `json:"next,omitempty"`       // 下一节点列表（DAG模式）
	DependsOn  []string `json:"depends_on,omitempty"` // 依赖节点列表（DAG模式）
	JoinMode   string   `json:"join_mode,omitempty"`  // 汇合模式
	// Condition fields
	Expression      string       `json:"expression,omitempty"`        // 条件表达式
	ThenChildren    []StepResult `json:"then_children,omitempty"`    // then 分支子步骤
	ElseChildren    []StepResult `json:"else_children,omitempty"`    // else 分支子步骤
	ConditionResult *bool        `json:"condition_result"` // 条件求值结果（使用指针以支持 false 值）
	// Sleep fields
	SleepDuration string `json:"sleepDuration,omitempty"` // Sleep 休眠时长
	// Shell fields
	ShellCommand string `json:"shellCommand,omitempty"` // Shell 命令
	// HTTP fields
	HTTPUrl    string `json:"httpUrl,omitempty"`    // HTTP URL
	HTTPMethod string `json:"httpMethod,omitempty"` // HTTP Method
	// Log fields
	LogMessage string `json:"logMessage,omitempty"` // Log 消息
	// 确定性标记(AI 集成用,见 docs/PRODUCT.md 的"双确定性模型")
	// Nondeterministic:本步结果每次可能不同(AI step 及其下游,由引擎传播算法推导)
	// SideEffecting:本步有副作用但可复现(shell/http/template 写盘)
	Nondeterministic bool `json:"nondeterministic,omitempty"`
	SideEffecting    bool `json:"sideEffecting,omitempty"`
}

// WorkflowResult holds the result of executing a workflow.
type WorkflowResult struct {
	Name      string        `json:"name"`
	Status    string        `json:"status"` // success, failed, partial
	Steps     []StepResult  `json:"steps"`
	Variables map[string]string `json:"variables,omitempty"`
	Error     string        `json:"error,omitempty"`
	StartTime string        `json:"startTime,omitempty"`
	EndTime   string        `json:"endTime,omitempty"`
	// Nondeterministic:整条 workflow 是否含非确定步骤(AI 及其下游)
	// 由引擎传播算法推导,Phase 2 实现
	Nondeterministic bool `json:"nondeterministic,omitempty"`
}
