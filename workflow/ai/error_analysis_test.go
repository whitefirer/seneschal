package ai

import (
	"context"
	"strings"
	"testing"
)

func TestAnalyzeError_Retry(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: `{"action":"retry","reason":"网络超时"}`}, nil
	}}
	d, err := NewAssistant(m).AnalyzeError(context.Background(), ErrorAnalysisParams{
		StepName: "deploy",
		Action:   "shell",
		Command:  "curl x",
		Error:    "timeout",
	})
	if err != nil {
		t.Fatalf("AnalyzeError: %v", err)
	}
	if d.Action != "retry" || d.Reason != "网络超时" {
		t.Errorf("unexpected decision: %+v", d)
	}
}

func TestAnalyzeError_InvalidActionFallsBackToSuggest(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: `{"action":"explode","reason":"boom"}`}, nil
	}}
	d, err := NewAssistant(m).AnalyzeError(context.Background(), ErrorAnalysisParams{})
	if err != nil {
		t.Fatalf("AnalyzeError: %v", err)
	}
	if d.Action != "suggest" || d.Reason != "boom" {
		t.Errorf("unexpected decision: %+v", d)
	}
}

func TestAnalyzeError_UnparseableTreatedAsSuggestion(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{Text: "  建议手动检查  "}, nil
	}}
	d, err := NewAssistant(m).AnalyzeError(context.Background(), ErrorAnalysisParams{})
	if err != nil {
		t.Fatalf("AnalyzeError: %v", err)
	}
	if d.Action != "suggest" || d.Reason != "建议手动检查" {
		t.Errorf("unexpected decision: %+v", d)
	}
}

func TestAnalyzeError_ProviderErrorIsSuggestion(t *testing.T) {
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{}, errProviderBoom
	}}
	d, err := NewAssistant(m).AnalyzeError(context.Background(), ErrorAnalysisParams{})
	if err != nil {
		t.Fatalf("AnalyzeError should swallow provider error, got %v", err)
	}
	if d.Action != "suggest" || !strings.Contains(d.Reason, "AI 分析失败") {
		t.Errorf("unexpected decision: %+v", d)
	}
}

func TestAnalyzeError_CustomPromptPrepended(t *testing.T) {
	var gotPrompt string
	m := &mockProvider{completeFn: func(ctx context.Context, req Request) (Response, error) {
		gotPrompt = req.Prompt
		return Response{Text: `{"action":"skip","reason":"non-critical"}`}, nil
	}}
	_, err := NewAssistant(m).AnalyzeError(context.Background(), ErrorAnalysisParams{CustomPrompt: "自定义提示"})
	if err != nil {
		t.Fatalf("AnalyzeError: %v", err)
	}
	if !strings.Contains(gotPrompt, "自定义提示") || !strings.Contains(gotPrompt, "步骤名称") {
		t.Errorf("custom prompt not prepended: %q", gotPrompt)
	}
}
