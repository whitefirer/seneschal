package ai

import (
	"context"
	"encoding/json"
	"errors"
)

// errProviderBoom is a shared error for provider-failure tests.
var errProviderBoom = errors.New("provider boom")

// mockProvider is a configurable Provider + ToolCapableProvider test double.
type mockProvider struct {
	name        string
	model       string
	completeFn  func(ctx context.Context, req Request) (Response, error)
	streamFn    func(ctx context.Context, req Request, onToken func(string)) (Response, error)
	completeRaw func(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error)
	callCount   int
}

func (m *mockProvider) Complete(ctx context.Context, req Request) (Response, error) {
	m.callCount++
	if m.completeFn == nil {
		return Response{Text: "mock response"}, nil
	}
	return m.completeFn(ctx, req)
}

func (m *mockProvider) Stream(ctx context.Context, req Request, onToken func(string)) (Response, error) {
	if m.streamFn == nil {
		resp, err := m.Complete(ctx, req)
		if err == nil && onToken != nil {
			onToken(resp.Text)
		}
		return resp, err
	}
	return m.streamFn(ctx, req, onToken)
}

func (m *mockProvider) Name() string {
	if m.name == "" {
		return "mock"
	}
	return m.name
}

func (m *mockProvider) GetModel() string {
	if m.model == "" {
		return "mock-model"
	}
	return m.model
}

func (m *mockProvider) CompleteRaw(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error) {
	if m.completeRaw == nil {
		return Response{Text: "mock raw response"}, nil
	}
	return m.completeRaw(ctx, model, system, maxTokens, temperature, tools, messages)
}

type recordingExecutor struct {
	calls  []string
	inputs []json.RawMessage
	out    string
	err    error
}

func (e *recordingExecutor) ExecuteTool(name string, input json.RawMessage) (string, error) {
	e.calls = append(e.calls, name)
	e.inputs = append(e.inputs, input)
	return e.out, e.err
}
