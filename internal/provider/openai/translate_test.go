package openai

import (
	"encoding/json"
	"testing"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

func TestTranslateRequestWithoutThinkingDoesNotEnableReasoning(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "xhigh",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning != nil {
		t.Fatalf("expected reasoning to remain unset without thinking, got %+v", out.Reasoning)
	}
}

func TestTranslateRequestWithThinkingUsesConfiguredEffort(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":31999}`),
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "xhigh",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning == nil {
		t.Fatal("expected reasoning to be populated")
	}
	if out.Reasoning.Effort != "xhigh" {
		t.Fatalf("expected reasoning effort xhigh, got %q", out.Reasoning.Effort)
	}
	if out.Reasoning.Summary != "auto" {
		t.Fatalf("expected reasoning summary auto, got %q", out.Reasoning.Summary)
	}
}

func TestTranslateRequestAdaptiveThinkingUsesConfiguredEffort(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"adaptive"}`),
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "medium",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning == nil {
		t.Fatal("expected reasoning to be populated")
	}
	if out.Reasoning.Effort != "medium" {
		t.Fatalf("expected reasoning effort medium, got %q", out.Reasoning.Effort)
	}
	if out.Reasoning.Summary != "auto" {
		t.Fatalf("expected reasoning summary auto, got %q", out.Reasoning.Summary)
	}
}

func TestTranslateRequestWithThinkingFallsBackToRequestHintWhenAccountUnset(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"level":"high"}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning == nil {
		t.Fatal("expected reasoning to be populated")
	}
	if out.Reasoning.Effort != "high" {
		t.Fatalf("expected reasoning effort high, got %q", out.Reasoning.Effort)
	}
}
