package ai

import (
	"encoding/json"
)

// AgentTools returns the tool definitions the chat agent can use. The model
// sees these descriptions and input schemas and autonomously decides which to
// call based on the user's intent — no hard-coded routing.
func AgentTools() []ToolDef {
	return []ToolDef{
		{
			Name:        "list_workflows",
			Description: "列出所有可用的工作流,返回每个工作流的名称、描述、步骤数。当用户想了解有哪些工作流可用时调用。",
			InputSchema: jsonRaw(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "select_workflow",
			Description: "从已有工作流中选择一个来执行。根据用户的自然语言意图匹配最合适的工作流,并填充它声明的变量。当用户想运行/执行/部署某个已有工作流时调用。",
			InputSchema: jsonRaw(`{"type":"object","properties":{"intent":{"type":"string","description":"用户的自然语言意图"}},"required":["intent"]}`),
		},
		{
			Name:        "generate_workflow",
			Description: "根据自然语言需求生成一个全新的工作流 YAML。当用户说'创建'、'生成'、'新建'一个工作流时调用。",
			InputSchema: jsonRaw(`{"type":"object","properties":{"requirement":{"type":"string","description":"工作流需求描述"}},"required":["requirement"]}`),
		},
		{
			Name:        "modify_workflow",
			Description: "基于现有工作流 YAML 进行修改。传入当前 YAML 和修改指令,返回修改后的 YAML。当用户想改一个已有工作流时调用。",
			InputSchema: jsonRaw(`{"type":"object","properties":{"yaml":{"type":"string","description":"当前工作流的 YAML 内容"},"instruction":{"type":"string","description":"修改指令"}},"required":["yaml","instruction"]}`),
		},
		{
			Name:        "explain_workflow",
			Description: "解释一个工作流 YAML 在做什么。返回中文的逐步说明。当用户想理解某个工作流时调用。",
			InputSchema: jsonRaw(`{"type":"object","properties":{"yaml":{"type":"string","description":"要解释的工作流 YAML 内容"}},"required":["yaml"]}`),
		},
		{
			Name:        "validate_workflow",
			Description: "校验工作流 YAML 是否合法。返回校验结果(通过/错误列表)。生成或修改后应调用此工具确认结果正确。",
			InputSchema: jsonRaw(`{"type":"object","properties":{"yaml":{"type":"string","description":"要校验的工作流 YAML 内容"}},"required":["yaml"]}`),
		},
		{
			Name:        "run_workflow",
			Description: "执行一个工作流。传入工作流文件名和变量。返回执行 ID。当用户确认要运行某个工作流时调用。",
			InputSchema: jsonRaw(`{"type":"object","properties":{"fileName":{"type":"string","description":"工作流文件名(如 example.yaml)"},"variables":{"type":"object","description":"工作流变量","additionalProperties":{"type":"string"}}},"required":["fileName"]}`),
		},
	}
}

// ToolExecutor is the interface the agent loop calls to execute a tool.
// The api layer implements this, wiring tools to the registry/executor.
// ai package can't import workflow (cycle), so execution is injected.
type ToolExecutor interface {
	ExecuteTool(name string, input json.RawMessage) (string, error)
}

// AgentEvent is one step in the agent loop, sent to the caller via onEvent.
type AgentEvent struct {
	Type    string `json:"type"`              // thinking, tool_call, tool_result, text, done, error
	Tool    string `json:"tool,omitempty"`    // tool name (for tool_call/tool_result)
	Input   string `json:"input,omitempty"`   // tool input JSON (for tool_call)
	Output  string `json:"output,omitempty"`  // tool output or text content
	Error   string `json:"error,omitempty"`   // error message
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }
