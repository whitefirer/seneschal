// Package ai provides provider-agnostic LLM access for the workflow engine.
//
// The Provider interface abstracts over specific LLM protocols so that
// workflow steps (the "ai" and "ai_decide" actions) can call a model without
// knowing whether it is Anthropic, OpenAI-compatible, or local.
//
// See docs/PRODUCT.md "Provider 架构" for the design rationale: keys never
// enter workflow YAML (they are read from environment variables), and the
// first implementation is the Anthropic protocol which covers both Claude
// (api.anthropic.com) and DeepSeek (api.deepseek.com/anthropic) via a
// configurable base URL.
package ai

import (
	"context"
	"encoding/json"
)

// Message is one turn in a conversation history. Role is "user" or
// "assistant". Providers translate this into their native multi-turn format.
type Message struct {
	Role    string
	Content string
}

// ToolDef declares a tool the model can call. InputSchema is a JSON Schema
// object (kept as RawMessage to avoid modeling all of JSON Schema in Go).
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// ToolCall is the model's request to execute a tool. ID must be echoed back
// in the matching tool_result. Input is the parsed JSON arguments object.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Request is a single completion request to a provider.
type Request struct {
	// System is the top-level system prompt (Anthropic-style: sent as a
	// sibling of messages, not as a system message).
	System string

	// Prompt is the current user message text.
	Prompt string

	// Messages is the conversation history (prior turns). When non-empty,
	// providers send these as multi-turn messages followed by Prompt as the
	// final user turn. When empty, Prompt is sent as a single user message
	// (backward compatible).
	Messages []Message

	// Inputs are the workflow variables explicitly made available to the
	// model. By convention the executor passes only the variables referenced
	// in Prompt (see executor_ai.go extractInputs) unless the step overrides
	// this with an explicit inputs list. This is a conservative default to
	// limit cost and leakage.
	Inputs map[string]string

	// Model is the provider-specific model id, e.g. "deepseek-v4-flash" or
	// "claude-sonnet-4-5-20250929". Falls back to the provider default if empty.
	Model string

	// MaxTokens caps the response length. Defaults to 1024 when zero.
	MaxTokens int

	// Temperature controls randomness. Defaults to 0 (deterministic-ish)
	// when zero; set explicitly for creative tasks.
	Temperature float64

	// Tools declares the tools available to the model. When non-empty, the
	// model may respond with ToolCalls (see Response) instead of text. The
	// caller must execute the tools and send results back via a follow-up
	// request (multi-turn tool use loop). Empty = no tools (backward compatible).
	Tools []ToolDef
}

// Response is the result of a completion request.
type Response struct {
	// Text is the full generated text (concatenation of all text blocks).
	Text string

	// ToolCalls are tool invocations the model wants the caller to execute.
	// Non-empty when StopReason == "tool_use". The caller executes each tool,
	// then sends a follow-up request with tool_result messages.
	ToolCalls []ToolCall

	// StopReason is the provider's reason for stopping: "end_turn" (normal),
	// "tool_use" (wants tools called), "max_tokens", "stop_sequence".
	StopReason string

	// InputTokens / OutputTokens are billed token counts when the provider
	// reports them (0 if unknown).
	InputTokens  int
	OutputTokens int

	// Retries is the number of automatic retries that occurred (provider-level
	// exponential backoff on 429/5xx/timeout). 0 = succeeded on first try.
	Retries int
}

// HasToolCalls reports whether the response contains tool invocations that
// need to be executed before the conversation can continue.
func (r Response) HasToolCalls() bool { return len(r.ToolCalls) > 0 }

// Provider is the abstraction over LLM backends.
//
// Implementations must be safe for concurrent use: the workflow executor may
// run multiple ai steps in parallel (see executeDAG wave execution).
type Provider interface {
	// Complete performs a non-streaming completion and returns the full text.
	Complete(ctx context.Context, req Request) (Response, error)

	// Stream performs a streaming completion, invoking onToken for each
	// incremental piece of text as it arrives. The returned Response contains
	// the full concatenated text and token counts. Implementations that do not
	// support streaming may fall back to Complete (calling onToken once with
	// the whole text).
	Stream(ctx context.Context, req Request, onToken func(string)) (Response, error)
}

// ToolCapableProvider is implemented by providers that support multi-turn
// tool use. The agent loop (RunAgent) type-asserts to this interface; if the
// provider doesn't implement it, RunAgent falls back to plain Complete (no
// tools).
type ToolCapableProvider interface {
	// CompleteRaw sends pre-built messages (including tool_use assistant turns
	// and tool_result user turns) and returns the response. Used for multi-turn
	// tool use loops.
	CompleteRaw(ctx context.Context, model, system string, maxTokens int, temperature float64, tools []ToolDef, messages []AnthropicRawMessage) (Response, error)
}

// Name returns a human-readable identifier for the provider, for logging.
type Namer interface {
	Name() string
}
