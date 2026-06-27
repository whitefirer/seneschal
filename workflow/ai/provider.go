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

import "context"

// Request is a single completion request to a provider.
type Request struct {
	// System is the top-level system prompt (Anthropic-style: sent as a
	// sibling of messages, not as a system message).
	System string

	// Prompt is the user message text.
	Prompt string

	// Inputs are the workflow variables explicitly made available to the
	// model. By convention the executor passes only the variables referenced
	// in Prompt (see executor_ai.go extractInputs) unless the step overrides
	// this with an explicit inputs list. This is a conservative default to
	// limit cost and leakage.
	Inputs map[string]string

	// Model is the provider-specific model id, e.g. "deepseek-chat" or
	// "claude-sonnet-4-5-20250929". Falls back to the provider default if empty.
	Model string

	// MaxTokens caps the response length. Defaults to 1024 when zero.
	MaxTokens int

	// Temperature controls randomness. Defaults to 0 (deterministic-ish)
	// when zero; set explicitly for creative tasks.
	Temperature float64
}

// Response is the result of a completion request.
type Response struct {
	// Text is the full generated text.
	Text string

	// StopReason is the provider's reason for stopping, e.g. "end_turn",
	// "max_tokens", "stop_sequence". Useful for debugging truncation.
	StopReason string

	// InputTokens / OutputTokens are billed token counts when the provider
	// reports them (0 if unknown).
	InputTokens  int
	OutputTokens int
}

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

// Name returns a human-readable identifier for the provider, for logging.
type Namer interface {
	Name() string
}
