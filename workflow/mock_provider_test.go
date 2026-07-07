package workflow

import (
	"context"
	"sync"

	"goworkflow/workflow/ai"
)

// MockProvider is a test double for ai.Provider + ai.ToolCapableProvider.
// It returns pre-configured responses, enabling unit tests for execAI,
// execAIDecide, and the agent loop without a real LLM API.
type MockProvider struct {
	mu           sync.Mutex
	Responses    []ai.Response // queued responses (FIFO); last one repeats
	Errors       []error       // queued errors (FIFO); if non-nil, returned before response
	callCount    int
	InputTokens  int // default token counts if Response doesn't specify
	OutputTokens int
}

// NewMockProvider creates a MockProvider with one default response.
func NewMockProvider(text string) *MockProvider {
	return &MockProvider{
		Responses: []ai.Response{{Text: text}},
	}
}

func (m *MockProvider) next() (ai.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.callCount
	m.callCount++

	var err error
	if idx < len(m.Errors) {
		err = m.Errors[idx]
	}
	if err != nil {
		return ai.Response{}, err
	}
	if idx < len(m.Responses) {
		r := m.Responses[idx]
		if r.InputTokens == 0 {
			r.InputTokens = m.InputTokens
		}
		if r.OutputTokens == 0 {
			r.OutputTokens = m.OutputTokens
		}
		return r, nil
	}
	// Repeat last response.
	if len(m.Responses) > 0 {
		r := m.Responses[len(m.Responses)-1]
		if r.InputTokens == 0 {
			r.InputTokens = m.InputTokens
		}
		if r.OutputTokens == 0 {
			r.OutputTokens = m.OutputTokens
		}
		return r, nil
	}
	return ai.Response{Text: "mock"}, nil
}

// CallCount returns the number of times the provider was called.
func (m *MockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// Name implements the optional Namer interface.
func (m *MockProvider) Name() string { return "mock" }

// GetModel returns a fixed model name for ToolCapableProvider.
func (m *MockProvider) GetModel() string { return "mock-model" }

// Complete returns the next queued response (or error).
func (m *MockProvider) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	return m.next()
}

// Stream falls back to Complete, invoking onToken once with the full text.
func (m *MockProvider) Stream(ctx context.Context, req ai.Request, onToken func(string)) (ai.Response, error) {
	resp, err := m.next()
	if err == nil && onToken != nil && resp.Text != "" {
		onToken(resp.Text)
	}
	return resp, err
}

// CompleteRaw satisfies ai.ToolCapableProvider. Ignores tools/messages,
// returns the next queued response.
func (m *MockProvider) CompleteRaw(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ai.ToolDef, messages []ai.AnthropicRawMessage) (ai.Response, error) {
	return m.next()
}
