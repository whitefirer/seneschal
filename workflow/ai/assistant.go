package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// actionSchemaDoc is embedded into the assistant system prompt so the model
// knows the exact goworkflow action vocabulary and YAML shape. Keeping it in
// one place lets Explain/Fix/Generate/SelectWorkflow share the same knowledge
// and stay in sync as actions evolve.
const actionSchemaDoc = `你是一个 goworkflow 工作流专家。goworkflow 是一个 YAML 驱动的工作流引擎。

## Workflow YAML 结构
name: <工作流名>              # 必填
description: <描述>           # 可选
variables:                    # 可选,工作流级变量
  <key>: <value>
ai:                           # 可选,AI 配置(密钥从环境变量读,绝不写进 YAML)
  provider: anthropic
  model: deepseek-v4-flash
steps:                        # 必填,步骤列表
  - name: <步骤名>            # 必填,唯一
    action: <类型>            # 必填
    description: <描述>       # 可选

## 支持的 action 类型
- shell: 执行命令。字段: command, dir, shell(sh/bash/cmd/powershell), env(map), continue_on_error, output_var, output_vars
- http: HTTP 请求。字段: url, method(GET/POST/PUT/DELETE), headers(map), body, timeout, save_output
- condition: 条件分支。字段: expression(支持 ==,!=,contains,>,<,>=,<=), then(步骤列表), else(步骤列表)
- parallel: 并行执行。字段: steps(步骤列表)
- foreach: 循环。字段: items(列表或变量名), item_var(默认 item), do(步骤列表)
- set: 设置变量。字段: value
- sleep: 等待。字段: duration(如 "5s", "1m")
- log: 打印消息。字段: message, level(info/warn/error)
- template: 渲染模板文件。字段: source, output
- ai: AI 文本生成。字段: prompt, system, inputs, save_output
- ai_decide: AI 布尔判断。字段: question, save_output(存 true/false)

## 变量替换
任意字符串字段可用 {{.var}} 引用变量。变量来源: variables 段、上游 step 的 output_var/save_output。

## DAG
默认顺序执行;用 next/depends_on 声明依赖即可并发:
  - name: build
    next: [test]
  - name: test
    depends_on: [build]`

// exampleWorkflow is a compact valid workflow used as a few-shot example so
// the model internalizes idiomatic structure.
const exampleWorkflow = `name: example
description: "示例工作流"
variables:
  app: demo
  env: dev
steps:
  - name: greet
    action: log
    message: "部署 {{.app}} 到 {{.env}}"
  - name: build
    action: shell
    command: "make build"
  - name: check
    action: condition
    expression: "{{.env}} == prod"
    then:
      - name: deploy-prod
        action: shell
        command: "make deploy-prod"
    else:
      - name: deploy-dev
        action: log
        message: "开发环境,跳过部署"`

// Assistant is the channel-agnostic goworkflow AI helper. It wraps a Provider
// with workflow-domain knowledge (action schema, examples) and exposes
// high-level operations: Explain, Fix, Generate, SelectWorkflow.
//
// The same Assistant backs the CLI (Phase 3), the future TUI app, and IM
// channel adapters (Phase 6) — none of them know about prompts or providers,
// only these methods.
type Assistant struct {
	provider Provider
}

// NewAssistant creates an Assistant over the given provider.
func NewAssistant(p Provider) *Assistant {
	return &Assistant{provider: p}
}

// Provider returns the underlying provider (for type assertions).
func (a *Assistant) Provider() Provider { return a.provider }

// RunAgent executes a multi-turn tool-use loop: sends the user message with
// available tools, and when the model requests a tool, executes it via the
// ToolExecutor, sends the result back, and repeats until the model returns a
// final text response (end_turn). Each step is reported via onEvent.
//
// maxRounds caps the number of tool-use rounds to prevent infinite loops
// (default 10 if zero).
// RunAgent executes a multi-turn tool-use loop. priorMessages allows the
// caller to pass conversation history so the agent has context from
// previous turns. If nil, the conversation starts fresh.
func (a *Assistant) RunAgent(ctx context.Context, system, userMessage string, tools []ToolDef, executor ToolExecutor, maxRounds int, priorMessages []AnthropicRawMessage, onEvent func(AgentEvent)) error {
	if a.provider == nil {
		return fmt.Errorf("assistant: no AI provider configured")
	}

	// Try tool-capable path; fall back to plain Complete if provider doesn't
	// support CompleteRaw (e.g. a mock).
	tcp, toolCapable := a.provider.(ToolCapableProvider)

	if !toolCapable || executor == nil || len(tools) == 0 {
		// No tools: plain completion.
		onEvent(AgentEvent{Type: "thinking"})
		resp, err := a.provider.Complete(ctx, Request{
			System:   system,
			Prompt:   userMessage,
			Messages: priorMessagesToMessages(priorMessages),
		})
		if err != nil {
			return err
		}
		onEvent(AgentEvent{Type: "text", Output: resp.Text})
		onEvent(AgentEvent{Type: "done"})
		return nil
	}

	if maxRounds <= 0 {
		maxRounds = 10
	}

	// Build initial messages: [user: userMessage]
	model := ""
	if ap, ok := a.provider.(interface{ GetModel() string }); ok {
		model = ap.GetModel()
	}
	// Build messages: prior conversation + current user message.
	messages := make([]AnthropicRawMessage, 0, len(priorMessages)+1)
	messages = append(messages, priorMessages...)
	messages = append(messages, AnthropicRawMessage{
		Role:    "user",
		Content: []AnthropicRawContent{{Type: "text", Text: userMessage}},
	})

	onEvent(AgentEvent{Type: "thinking"})

	for round := 0; round < maxRounds; round++ {
		resp, err := tcp.CompleteRaw(ctx, model, system, 4096, 0, tools, messages)
		if err != nil {
			onEvent(AgentEvent{Type: "error", Error: err.Error()})
			return err
		}

		if !resp.HasToolCalls() {
			// Final response — model is done.
			if resp.Text != "" {
				onEvent(AgentEvent{Type: "text", Output: resp.Text})
			}
			onEvent(AgentEvent{Type: "done"})
			return nil
		}

		// Model wants tools: echo the assistant's tool_use turn back.
		messages = append(messages, RawMessageFromResponse(resp))

		// Execute each tool call and collect results.
		var results []ToolResult
		for _, tc := range resp.ToolCalls {
			onEvent(AgentEvent{Type: "tool_call", Tool: tc.Name, Input: string(tc.Input)})

			output, execErr := executor.ExecuteTool(tc.Name, tc.Input)
			if execErr != nil {
				output = execErr.Error()
				results = append(results, ToolResult{ToolUseID: tc.ID, Output: output, IsError: true})
			} else {
				results = append(results, ToolResult{ToolUseID: tc.ID, Output: output})
			}
			onEvent(AgentEvent{Type: "tool_result", Tool: tc.Name, Output: truncateForEvent(output)})
		}

		// Send tool results back as a user turn.
		messages = append(messages, RawMessageWithToolResults(results))
	}

	onEvent(AgentEvent{Type: "error", Error: fmt.Sprintf("agent loop exceeded %d rounds", maxRounds)})
	return fmt.Errorf("agent loop exceeded %d rounds", maxRounds)
}

func truncateForEvent(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "...(截断)"
	}
	return s
}

// priorMessagesToMessages converts AnthropicRawMessage history to the plain
// Message type used by Complete (non-tool path).
func priorMessagesToMessages(raw []AnthropicRawMessage) []Message {
	if len(raw) == 0 {
		return nil
	}
	msgs := make([]Message, 0, len(raw))
	for _, m := range raw {
		// Flatten content blocks into a single string (best-effort for text).
		var sb strings.Builder
		for _, c := range m.Content {
			if c.Type == "text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
		if sb.Len() > 0 {
			msgs = append(msgs, Message{Role: m.Role, Content: sb.String()})
		}
	}
	return msgs
}

// CandidateEntry is the assistant's view of a selectable workflow. It mirrors
// the fields the model needs to choose and fill variables. Callers convert
// from their own registry type (e.g. workflow.WorkflowEntry) since the ai
// package cannot import workflow (import cycle).
type CandidateEntry struct {
	Name        string
	FileName    string
	Description string
	Steps       int
	// Variables is the set of workflow-level variable names declared in the
	// YAML; the model is asked to fill these.
	Variables []string
}

// Selection is the result of SelectWorkflow: which workflow to run plus the
// variable values the model inferred from the user's intent.
type Selection struct {
	Workflow   string            // matched CandidateEntry.Name
	Variables  map[string]string // filled variable values
	Confidence float64           // model's self-reported confidence, 0..1
}

// SelectWorkflow asks the model to pick the best workflow for a natural-language
// intent and to fill in its variables. entries is the candidate set (typically
// from a WorkflowRegistry). The returned Selection.Workflow matches a
// CandidateEntry.Name; callers should verify it actually exists in entries
// (the model can hallucinate) before executing.
func (a *Assistant) SelectWorkflow(ctx context.Context, intent string, entries []CandidateEntry) (Selection, error) {
	if a.provider == nil {
		return Selection{}, fmt.Errorf("assistant: no AI provider configured")
	}

	// Build a compact candidate listing. Variable names matter: they tell the
	// model what it can/should fill.
	var sb strings.Builder
	sb.WriteString("可选工作流列表:\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "- name: %s", e.Name)
		if e.Description != "" {
			fmt.Fprintf(&sb, " | %s", e.Description)
		}
		if e.Steps > 0 {
			fmt.Fprintf(&sb, " | %d steps", e.Steps)
		}
		if len(e.Variables) > 0 {
			fmt.Fprintf(&sb, " | 变量: %s", strings.Join(e.Variables, ", "))
		}
		sb.WriteString("\n")
	}

	system := actionSchemaDoc + "\n\n## 输出规则\n只输出一个 JSON 对象,不要 markdown 围栏,不要解释。格式:\n" +
		`{"workflow": "<选中的工作流 name>", "variables": {"<var>": "<value>"}, "confidence": 0.0-1.0}` +
		"\n如果没有任何工作流匹配,workflow 设为空字符串,confidence 设为 0。\n\n" +
		"## 判断创建 vs 执行\n" +
		"仅当用户**明确使用创建类动词**(创建/生成/新建/写一个/做一个)且描述的是一个**尚不存在的**工作流时,workflow 设为空。\n" +
		"其他所有情况(运行/执行/部署/跑/或直接描述目标),都从列表中选最匹配的工作流。即使匹配不完全精确,只要语义相关就选,并在 confidence 里反映确信度。"

	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"用户意图: %s\n\n%s\n选出最匹配的工作流,并根据意图填好它声明的变量。仅当用户明确要创建新工作流时才返回空 workflow。只输出 JSON。",
			intent, sb.String()),
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return Selection{}, fmt.Errorf("select workflow: %w", err)
	}

	var sel parseSelection
	if err := json.Unmarshal([]byte(extractJSON(resp.Text)), &sel); err != nil {
		return Selection{}, fmt.Errorf("select workflow: parse model response: %w (raw: %s)", err, truncate(resp.Text, 200))
	}
	return Selection{
		Workflow:   sel.Workflow,
		Variables:  sel.Variables,
		Confidence: sel.Confidence,
	}, nil
}

// Generate produces a complete workflow YAML from a natural-language
// requirement. Returns raw YAML text (no markdown fences). Callers should
// validate the result before saving.
func (a *Assistant) Generate(ctx context.Context, requirement string) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("assistant: no AI provider configured")
	}
	system := actionSchemaDoc + "\n\n## 示例工作流\n" + exampleWorkflow +
		"\n\n## 重要输出规则\n你只能输出完整的 workflow YAML,不要 markdown 围栏,不要任何解释文字。"
	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"请根据下面的需求生成一个 goworkflow 工作流 YAML。只输出 YAML,不要其他内容。\n\n需求: %s",
			requirement),
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}
	yaml := extractYAML(resp.Text)
	if strings.TrimSpace(yaml) == "" {
		return "", fmt.Errorf("generate: model returned empty YAML")
	}
	return yaml, nil
}

// truncate keeps error messages readable.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Explain produces a Chinese, human-readable explanation of what the given
// workflow YAML does: overall purpose, each step's role, control flow.
func (a *Assistant) Explain(ctx context.Context, yamlContent string) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("assistant: no AI provider configured")
	}
	system := actionSchemaDoc + "\n\n## 示例工作流\n" + exampleWorkflow
	req := Request{
		System: system,
		Prompt: "请用中文解释下面这个 goworkflow 工作流在做什么。" +
			"先一句话概括整体目的,然后逐个步骤说明(动作、作用、关键变量)。" +
			"如果有条件分支或循环,说明它们的判断/迭代逻辑。用 markdown 列表,简洁。\n\n" +
			"```yaml\n" + yamlContent + "\n```",
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("explain: %w", err)
	}
	return strings.TrimSpace(resp.Text), nil
}

// Fix attempts to repair a workflow YAML given its content and the validation
// errors produced by workflow.Validate. Returns the corrected YAML as raw
// text (no markdown fences). If the model cannot fix it, the original is
// returned alongside an error.
func (a *Assistant) Fix(ctx context.Context, yamlContent, validationError string) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("assistant: no AI provider configured")
	}
	system := actionSchemaDoc + "\n\n## 示例工作流\n" + exampleWorkflow +
		"\n\n## 重要输出规则\n你只能输出修复后的完整 YAML,不要 markdown 围栏,不要解释,不要任何其他文字。"
	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"下面这个 goworkflow 工作流有错误,请修复并输出完整的修复版 YAML。\n\n"+
				"校验错误:\n%s\n\n"+
				"原 YAML:\n```yaml\n%s\n```",
			validationError, yamlContent),
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("fix: %w", err)
	}
	fixed := extractYAML(resp.Text)
	if strings.TrimSpace(fixed) == "" {
		return yamlContent, fmt.Errorf("fix: model returned empty YAML")
	}
	return fixed, nil
}

// Modify takes an existing workflow YAML and a natural-language instruction,
// returns the modified YAML. Like Fix but for arbitrary edits rather than
// validation errors.
func (a *Assistant) Modify(ctx context.Context, yamlContent, instruction string) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("assistant: no AI provider configured")
	}
	system := actionSchemaDoc + "\n\n## 示例工作流\n" + exampleWorkflow +
		"\n\n## 重要输出规则\n你只能输出修改后的完整 YAML,不要 markdown 围栏,不要解释,不要任何其他文字。"
	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"请根据指令修改下面这个 goworkflow 工作流,输出修改后的完整 YAML。\n\n"+
				"修改指令:\n%s\n\n"+
				"原 YAML:\n```yaml\n%s\n```",
			instruction, yamlContent),
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("modify: %w", err)
	}
	modified := extractYAML(resp.Text)
	if strings.TrimSpace(modified) == "" {
		return yamlContent, fmt.Errorf("modify: model returned empty YAML")
	}
	return modified, nil
}
// the fields the model needs to reason about an execution (status, output,
// error, determinism). Callers convert from workflow.StepResult since ai
// cannot import workflow.
type ExecutionStepResult struct {
	Name            string
	Action          string
	Status          string
	Output          string
	Error           string
	Duration        string
	Nondeterministic bool
	Children        []ExecutionStepResult
}

// ExecutionView is the assistant's view of a completed or in-progress
// execution, for ExplainExecution / AnswerExecutionQuestion.
type ExecutionView struct {
	WorkflowName   string
	Status         string
	Error          string
	Variables      map[string]string
	Steps          []ExecutionStepResult
	Nondeterministic bool
}

// ExplainExecution produces a Chinese summary of an execution: overall result,
// notable steps, failures, and AI-influenced branches.
func (a *Assistant) ExplainExecution(ctx context.Context, ex ExecutionView) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("assistant: no AI provider configured")
	}
	system := actionSchemaDoc + "\n\n你是一个工作流执行分析助手。用户会给你一个执行结果,你要帮他们理解。"
	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"请用中文分析下面这次工作流执行。先一句话概括结果(成功/失败),然后挑值得注意的步骤说明(失败的、AI 相关的、耗时长)。简洁,markdown 列表。\n\n"+
				"执行状态: %s\n错误: %s\n包含 AI 步骤: %v\n变量: %s\n\n步骤树:\n%s",
			ex.Status, ex.Error, ex.Nondeterministic, formatVars(ex.Variables), formatStepTree(ex.Steps, 0)),
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("explain execution: %w", err)
	}
	return strings.TrimSpace(resp.Text), nil
}

// AnswerExecutionQuestion answers a free-form question about an execution
// (e.g. "why did step X fail?", "what does variable Y contain?").
func (a *Assistant) AnswerExecutionQuestion(ctx context.Context, ex ExecutionView, question string) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("assistant: no AI provider configured")
	}
	system := actionSchemaDoc + "\n\n你是一个工作流执行分析助手。根据执行结果回答用户的问题,用中文,简洁。"
	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"执行状态: %s\n错误: %s\n变量: %s\n\n步骤树:\n%s\n\n用户问题: %s",
			ex.Status, ex.Error, formatVars(ex.Variables), formatStepTree(ex.Steps, 0), question),
	}
	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("answer execution question: %w", err)
	}
	return strings.TrimSpace(resp.Text), nil
}

func formatVars(vars map[string]string) string {
	if len(vars) == 0 {
		return "(无)"
	}
	var sb strings.Builder
	for k, v := range vars {
		val := v
		if len(val) > 200 {
			val = val[:200] + "...(截断)"
		}
		fmt.Fprintf(&sb, "  %s = %s\n", k, val)
	}
	return sb.String()
}

func formatStepTree(steps []ExecutionStepResult, depth int) string {
	var sb strings.Builder
	indent := strings.Repeat("  ", depth)
	for _, s := range steps {
		flag := ""
		if s.Nondeterministic {
			flag = " [AI]"
		}
		fmt.Fprintf(&sb, "%s- [%s] %s (%s)%s", indent, s.Status, s.Name, s.Action, flag)
		if s.Duration != "" {
			fmt.Fprintf(&sb, " %s", s.Duration)
		}
		if s.Error != "" {
			errMsg := s.Error
			if len(errMsg) > 300 {
				errMsg = errMsg[:300] + "...(截断)"
			}
			fmt.Fprintf(&sb, "\n%s  错误: %s", indent, errMsg)
		}
		if s.Output != "" {
			out := s.Output
			if len(out) > 500 {
				out = out[:500] + "...(截断)"
			}
			fmt.Fprintf(&sb, "\n%s  输出: %s", indent, out)
		}
		sb.WriteString("\n")
		if len(s.Children) > 0 {
			sb.WriteString(formatStepTree(s.Children, depth+1))
		}
	}
	return sb.String()
}
