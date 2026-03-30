package openai

import (
	"encoding/json"
	"strings"
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

func TestTranslateRequestWithThinkingEnabledUsesReasoning(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":31999}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning == nil {
		t.Fatal("expected reasoning to be populated")
	}
	if out.Reasoning.Effort != "medium" {
		t.Fatalf("expected default reasoning effort medium, got %q", out.Reasoning.Effort)
	}
	if out.Reasoning.Summary != "auto" {
		t.Fatalf("expected reasoning summary auto, got %q", out.Reasoning.Summary)
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

func TestTranslateRequestDisabledThinkingDoesNotEnableReasoning(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"disabled"}`),
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "medium",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning != nil {
		t.Fatalf("expected reasoning to stay disabled, got %+v", out.Reasoning)
	}
}

func TestTranslateRequestAdaptiveThinkingEnablesReasoning(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"adaptive"}`),
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "xhigh",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning == nil {
		t.Fatal("expected adaptive thinking to enable reasoning")
	}
	if out.Reasoning.Effort != "xhigh" {
		t.Fatalf("expected adaptive thinking to use configured effort xhigh, got %q", out.Reasoning.Effort)
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

func TestTranslateRequestDefaultsToPassthroughToolFiltering(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Tools: json.RawMessage(`[
			{"name":"mcp__Read","description":"mcp tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}},
			{"name":"Read","description":"regular tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}
		]`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if len(out.Tools) != 2 {
		t.Fatalf("expected both tools to pass through by default, got %d", len(out.Tools))
	}
	if out.Tools[0].Function.Name != "mcp__Read" || out.Tools[1].Function.Name != "Read" {
		t.Fatalf("unexpected tool order/names: %+v", out.Tools)
	}
}

func TestTranslateRequestExplicitStripMCPStillWorks(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Tools: json.RawMessage(`[
			{"name":"mcp__Read","description":"mcp tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}},
			{"name":"Read","description":"regular tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}
		]`),
	}

	out, err := translateRequest(req, map[string]any{
		"tool_filter": "strip_mcp",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("expected only one tool after strip_mcp, got %d", len(out.Tools))
	}
	if out.Tools[0].Function.Name != "Read" {
		t.Fatalf("expected regular tool to remain, got %+v", out.Tools[0])
	}
}

func TestTranslateRequestRejectsUnsupportedContentBlock(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		Messages: []domain.Message{
			{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}
				]`),
			},
		},
	}

	_, err := translateRequest(req, nil)
	if err == nil {
		t.Fatal("expected unsupported block error")
	}
	if got := err.Error(); !strings.Contains(got, `unsupported content block type "image"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
