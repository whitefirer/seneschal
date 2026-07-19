package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ErrorDecision is the AI's recommendation for a failed step.
type ErrorDecision struct {
	Action string `json:"action"`        // "retry" | "skip" | "abort" | "suggest"
	Reason string `json:"reason"`        // why this decision
	Fix    string `json:"fix,omitempty"` // optional fix suggestion (informational)
}

// AnalyzeError asks the AI to analyze a failed step and decide what to do next.
//
// The AI sees the step name, action, the command/prompt that was run, the
// output (if any), and the error message. It returns a structured decision:
//
//   - retry: transient error, worth trying again
//   - skip: this step is non-critical, workflow can continue without it
//   - abort: critical failure, stop the workflow
//   - suggest: AI can't auto-decide, provides a text recommendation for the user
//
// The caller (executor) acts on the decision. In "ai" mode (not "ai_auto"),
// only "suggest" is acted upon (the reason is appended to the error). In
// "ai_auto" mode, retry/skip/abort are executed automatically.
func (a *Assistant) AnalyzeError(ctx context.Context, params ErrorAnalysisParams) (ErrorDecision, error) {
	if a.provider == nil {
		return ErrorDecision{}, fmt.Errorf("assistant: no AI provider configured")
	}

	system := `你是 seneschal 工作流错误分析助手。一个步骤失败了,你需要分析失败原因并决定下一步。

## 输出规则
只输出一个 JSON 对象,不要 markdown 围栏,不要解释。
格式: {"action": "<action>", "reason": "<中文原因>", "fix": "<可选的修正建议>"}

## action 可选值
- "retry": 瞬时错误(网络超时、临时不可用),值得重试
- "skip": 非关键步骤,workflow 可以跳过它继续
- "abort": 关键失败,应该中止整个 workflow
- "suggest": 无法自动判断,给出文字建议让用户决定

## 判断原则
- exit code 1 但没有明确错误信息 → 可能是正常退出,suggest 或 skip
- 网络超时/连接拒绝 → retry
- 文件不存在/路径错误 → abort(需要用户修正)
- 权限拒绝 → abort
- 编译/构建错误 → abort 或 suggest(需要用户修代码)
- 测试失败 → abort(需要用户修代码)`

	prompt := fmt.Sprintf(
		"步骤名称: %s\n动作类型: %s\n执行的命令/内容: %s\n输出: %s\n错误信息: %s\n\n请分析失败原因并决定下一步。只输出 JSON。",
		params.StepName, params.Action, truncate(params.Command, 500),
		truncate(params.Output, 1000), truncate(params.Error, 500),
	)

	if params.CustomPrompt != "" {
		prompt = params.CustomPrompt + "\n\n" + prompt
	}

	req := Request{
		System: system,
		Prompt: prompt,
	}

	resp, err := a.provider.Complete(ctx, req)
	if err != nil {
		return ErrorDecision{Action: "suggest", Reason: fmt.Sprintf("AI 分析失败: %v", err)}, nil
	}

	var decision ErrorDecision
	raw := extractJSON(resp.Text)
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		// Can't parse JSON — treat as a text suggestion.
		return ErrorDecision{
			Action: "suggest",
			Reason: strings.TrimSpace(resp.Text),
		}, nil
	}

	// Validate action.
	switch decision.Action {
	case "retry", "skip", "abort", "suggest":
		// valid
	default:
		decision.Action = "suggest"
	}

	if decision.Reason == "" {
		decision.Reason = "AI 未提供原因"
	}

	return decision, nil
}

// ErrorAnalysisParams contains the context the AI needs to analyze a failure.
type ErrorAnalysisParams struct {
	StepName     string
	Action       string
	Command      string // shell command, http url, ai prompt, etc.
	Output       string // stdout/output before failure
	Error        string // error message
	CustomPrompt string // optional user-provided analysis instruction
}
