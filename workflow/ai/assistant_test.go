package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNewAssistant_Provider(t *testing.T) {
	m := &mockProvider{}
	a := NewAssistant(m)
	if a.Provider() != m {
		t.Error("Provider() should return the underlying provider")
	}
}

func TestSelectWorkflow_Valid(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: `{"workflow":"deploy","variables":{"env":"prod"},"confidence":0.9}`}, nil
	}}
	a := NewAssistant(m)
	sel, err := a.SelectWorkflow(context.Background(), "部署到生产", []CandidateEntry{
		{Name: "deploy", Description: "部署", Steps: 3, Variables: []string{"env"}},
	})
	if err != nil {
		t.Fatalf("SelectWorkflow: %v", err)
	}
	if sel.Workflow != "deploy" || sel.Variables["env"] != "prod" || sel.Confidence != 0.9 {
		t.Errorf("unexpected selection: %+v", sel)
	}
}

func TestSelectWorkflow_ParseError(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "not json"}, nil
	}}
	_, err := NewAssistant(m).SelectWorkflow(context.Background(), "intent", nil)
	if err == nil || !strings.Contains(err.Error(), "parse model response") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestSelectWorkflow_NilProvider(t *testing.T) {
	_, err := NewAssistant(nil).SelectWorkflow(context.Background(), "intent", nil)
	if err == nil || !strings.Contains(err.Error(), "no AI provider") {
		t.Fatalf("expected no provider error, got %v", err)
	}
}

func TestGenerate_ExtractsYAML(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "```yaml\nname: gen\nsteps: []\n```"}, nil
	}}
	got, err := NewAssistant(m).Generate(context.Background(), "make a workflow")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(got, "name: gen") {
		t.Errorf("expected YAML content, got %q", got)
	}
}

func TestGenerate_EmptyYAML(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "```yaml\n```"}, nil
	}}
	_, err := NewAssistant(m).Generate(context.Background(), "make a workflow")
	if err == nil || !strings.Contains(err.Error(), "empty YAML") {
		t.Fatalf("expected empty YAML error, got %v", err)
	}
}

func TestExplain_ReturnsText(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "  这是解释  "}, nil
	}}
	got, err := NewAssistant(m).Explain(context.Background(), "name: x\nsteps: []")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if got != "这是解释" {
		t.Errorf("expected trimmed text, got %q", got)
	}
}

func TestFix_ExtractsYAML(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "```yaml\nname: fixed\nsteps: []\n```"}, nil
	}}
	got, err := NewAssistant(m).Fix(context.Background(), "name: broken", "some error")
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if !strings.Contains(got, "name: fixed") {
		t.Errorf("expected fixed YAML, got %q", got)
	}
}

func TestFix_EmptyYAMLReturnsOriginal(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "```yaml\n```"}, nil
	}}
	orig := "name: broken"
	got, err := NewAssistant(m).Fix(context.Background(), orig, "some error")
	if err == nil || !strings.Contains(err.Error(), "empty YAML") {
		t.Fatalf("expected empty YAML error, got %v", err)
	}
	if got != orig {
		t.Errorf("expected original YAML returned, got %q", got)
	}
}

func TestModify_ExtractsYAML(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "```yaml\nname: changed\nsteps: []\n```"}, nil
	}}
	got, err := NewAssistant(m).Modify(context.Background(), "name: x\nsteps: []", "rename to changed")
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}
	if !strings.Contains(got, "name: changed") {
		t.Errorf("expected modified YAML, got %q", got)
	}
}

func TestExplainExecution(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "执行成功"}, nil
	}}
	got, err := NewAssistant(m).ExplainExecution(context.Background(), ExecutionView{
		WorkflowName: "wf",
		Status:       "success",
		Steps: []ExecutionStepResult{
			{Name: "a", Action: "log", Status: "success", Output: "hi", Duration: "1s"},
		},
	})
	if err != nil {
		t.Fatalf("ExplainExecution: %v", err)
	}
	if got != "执行成功" {
		t.Errorf("unexpected output %q", got)
	}
}

func TestAnswerExecutionQuestion(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "因为变量缺失"}, nil
	}}
	got, err := NewAssistant(m).AnswerExecutionQuestion(context.Background(), ExecutionView{Status: "failed"}, "为什么失败")
	if err != nil {
		t.Fatalf("AnswerExecutionQuestion: %v", err)
	}
	if got != "因为变量缺失" {
		t.Errorf("unexpected output %q", got)
	}
}

func TestRunAgent_NoToolsFallsBackToComplete(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "plain answer"}, nil
	}}
	var events []AgentEvent
	err := NewAssistant(m).RunAgent(context.Background(), "sys", "user", nil, nil, 0, nil, func(e AgentEvent) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if len(events) < 3 || events[0].Type != "thinking" || events[1].Type != "text" || events[2].Type != "done" {
		t.Errorf("unexpected event sequence: %+v", events)
	}
}

func TestRunAgent_ToolLoop(t *testing.T) {
	rawCalls := 0
	m := &mockProvider{
		completeRaw: func(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error) {
			rawCalls++
			if rawCalls == 1 {
				return Response{ToolCalls: []ToolCall{{ID: "call-1", Name: "list_workflows", Input: json.RawMessage(`{}`)}}}, nil
			}
			return Response{Text: "done answer"}, nil
		},
	}
	exec := &recordingExecutor{out: "a,b"}
	var events []AgentEvent
	err := NewAssistant(m).RunAgent(context.Background(), "sys", "user", AgentTools(), exec, 5, nil, func(e AgentEvent) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if rawCalls != 2 {
		t.Errorf("expected 2 raw calls, got %d", rawCalls)
	}
	if len(exec.calls) != 1 || exec.calls[0] != "list_workflows" {
		t.Errorf("unexpected tool calls: %v", exec.calls)
	}
	var sawToolCall, sawToolResult, sawDone bool
	for _, e := range events {
		switch e.Type {
		case "tool_call":
			sawToolCall = true
		case "tool_result":
			sawToolResult = true
		case "done":
			sawDone = true
		}
	}
	if !sawToolCall || !sawToolResult || !sawDone {
		t.Errorf("missing events: tool_call=%v tool_result=%v done=%v (%+v)", sawToolCall, sawToolResult, sawDone, events)
	}
}

func TestRunAgent_ToolErrorStillLoops(t *testing.T) {
	rawCalls := 0
	m := &mockProvider{
		completeRaw: func(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error) {
			rawCalls++
			if rawCalls == 1 {
				return Response{ToolCalls: []ToolCall{{ID: "call-1", Name: "bad_tool", Input: json.RawMessage(`{}`)}}}, nil
			}
			return Response{Text: "recovered"}, nil
		},
	}
	exec := &recordingExecutor{err: errors.New("tool failed")}
	err := NewAssistant(m).RunAgent(context.Background(), "sys", "user", AgentTools(), exec, 5, nil, nil)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if rawCalls != 2 {
		t.Errorf("expected 2 raw calls after tool error, got %d", rawCalls)
	}
}

func TestRunAgent_MaxRoundsExceeded(t *testing.T) {
	m := &mockProvider{
		completeRaw: func(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error) {
			return Response{ToolCalls: []ToolCall{{ID: "call-x", Name: "list_workflows", Input: json.RawMessage(`{}`)}}}, nil
		},
	}
	exec := &recordingExecutor{out: "x"}
	err := NewAssistant(m).RunAgent(context.Background(), "sys", "user", AgentTools(), exec, 2, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeded 2 rounds") {
		t.Fatalf("expected max rounds error, got %v", err)
	}
}

func TestRunAgent_NilProvider(t *testing.T) {
	err := NewAssistant(nil).RunAgent(context.Background(), "sys", "user", nil, nil, 0, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "no AI provider") {
		t.Fatalf("expected no provider error, got %v", err)
	}
}

func TestFormatVars(t *testing.T) {
	got := formatVars(nil)
	if got != "(无)" {
		t.Errorf("formatVars(nil)=%q", got)
	}
	got = formatVars(map[string]string{"a": "b"})
	if !strings.Contains(got, "a = b") {
		t.Errorf("formatVars should contain a = b, got %q", got)
	}
}

func TestFormatStepTree(t *testing.T) {
	got := formatStepTree([]ExecutionStepResult{
		{Name: "a", Action: "log", Status: "success", Output: "ok", Children: []ExecutionStepResult{
			{Name: "b", Action: "shell", Status: "failed", Error: "boom", Nondeterministic: true},
		}},
	}, 0)
	if !strings.Contains(got, "[success] a (log)") || !strings.Contains(got, "[failed] b (shell) [AI]") || !strings.Contains(got, "错误: boom") {
		t.Errorf("unexpected step tree:\n%s", got)
	}
}

func TestTruncateForEvent(t *testing.T) {
	long := strings.Repeat("x", 2500)
	if got := truncateForEvent(long); len(got) != 2000+len("...(截断)") {
		t.Errorf("unexpected truncate length %d", len(got))
	}
	if got := truncateForEvent("short"); got != "short" {
		t.Errorf("short string changed: %q", got)
	}
}

func TestPriorMessagesToMessages(t *testing.T) {
	if got := priorMessagesToMessages(nil); got != nil {
		t.Errorf("nil should map to nil, got %v", got)
	}
	msgs := priorMessagesToMessages([]AnthropicRawMessage{
		{Role: "user", Content: []AnthropicRawContent{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []AnthropicRawContent{{Type: "tool_use", ID: "x", Name: "t", Input: json.RawMessage(`{}`)}}},
	})
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("unexpected conversion: %+v", msgs)
	}
}
