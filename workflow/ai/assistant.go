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
  model: deepseek-chat
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
		"\n如果没有任何工作流匹配,workflow 设为空字符串,confidence 设为 0。"

	req := Request{
		System: system,
		Prompt: fmt.Sprintf(
			"用户意图: %s\n\n%s\n请选出最匹配的工作流,并根据意图填好它声明的变量。只输出 JSON。",
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
