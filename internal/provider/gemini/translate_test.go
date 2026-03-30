package gemini

import (
	"encoding/json"
	"testing"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

func TestTranslateRequestDoesNotEnableThinkingWithoutRequest(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gemini-2.5-pro",
		Messages: []domain.Message{},
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_enabled": true,
		"thinking_budget":  8192,
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.GenerationConfig != nil && out.GenerationConfig.ThinkingConfig != nil {
		t.Fatalf("expected no thinking config without request thinking, got %+v", out.GenerationConfig.ThinkingConfig)
	}
}

func TestTranslateRequestEnabledThinkingOnGemini25UsesBudgetAndIncludeThoughts(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gemini-2.5-pro",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"enabled","budget_tokens":2048}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.GenerationConfig == nil || out.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected thinking config to be populated")
	}
	if out.GenerationConfig.ThinkingConfig.ThinkingBudget != 2048 {
		t.Fatalf("expected thinking budget 2048, got %+v", out.GenerationConfig.ThinkingConfig)
	}
	if !out.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Fatal("expected includeThoughts to be enabled")
	}
}

func TestTranslateRequestAdaptiveThinkingStillIncludesThoughts(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gemini-2.5-pro",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"adaptive"}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.GenerationConfig == nil || out.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected adaptive thinking to produce thinking config")
	}
	if !out.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Fatal("expected includeThoughts for adaptive thinking")
	}
	if out.GenerationConfig.ThinkingConfig.ThinkingBudget != 0 {
		t.Fatalf("expected adaptive thinking not to force a budget, got %+v", out.GenerationConfig.ThinkingConfig)
	}
}

func TestTranslateRequestGemini3UsesThinkingLevel(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gemini-3.0-pro",
		Messages: []domain.Message{},
		Thinking: json.RawMessage(`{"type":"enabled"}`),
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "low",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.GenerationConfig == nil || out.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected thinking config to be populated")
	}
	if out.GenerationConfig.ThinkingConfig.ThinkingLevel != "low" {
		t.Fatalf("expected thinking level low, got %+v", out.GenerationConfig.ThinkingConfig)
	}
	if !out.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Fatal("expected includeThoughts to be enabled")
	}
}

func TestTranslateRequestDefaultsToPassthroughToolFiltering(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gemini-2.5-pro",
		Tools: json.RawMessage(`[
			{"name":"mcp__Read","description":"mcp tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}},
			{"name":"Read","description":"regular tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}
		]`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if len(out.Tools) != 1 || len(out.Tools[0].FunctionDeclarations) != 2 {
		t.Fatalf("expected both tools to pass through by default, got %+v", out.Tools)
	}
}

func TestTranslateRawContentPropagatesThinkingSignatureToNextToolCall(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"thinking","text":"plan","signature":"sig_123"},
		{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"a.txt"}}
	]`)

	parts, err := translateRawContent(raw, map[string]string{"toolu_1": "Read"})
	if err != nil {
		t.Fatalf("translateRawContent returned error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if !parts[0].Thought || parts[0].ThoughtSignature != "sig_123" {
		t.Fatalf("expected thinking part with signature, got %+v", parts[0])
	}
	if parts[1].FunctionCall == nil || parts[1].ThoughtSignature != "sig_123" {
		t.Fatalf("expected next tool call to carry thought signature, got %+v", parts[1])
	}
}

func TestTranslateRawContentDoesNotAttachThinkingSignatureToInterveningText(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"thinking","text":"plan","signature":"sig_123"},
		{"type":"text","text":"intermediate"},
		{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"a.txt"}}
	]`)

	parts, err := translateRawContent(raw, map[string]string{"toolu_1": "Read"})
	if err != nil {
		t.Fatalf("translateRawContent returned error: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if !parts[0].Thought || parts[0].ThoughtSignature != "sig_123" {
		t.Fatalf("expected thinking part with signature, got %+v", parts[0])
	}
	if parts[1].Text != "intermediate" || parts[1].ThoughtSignature != "" {
		t.Fatalf("expected intervening text to remain unsigned, got %+v", parts[1])
	}
	if parts[2].FunctionCall == nil || parts[2].ThoughtSignature != "sig_123" {
		t.Fatalf("expected tool call to carry thinking signature, got %+v", parts[2])
	}
}

func TestTranslateRawContentPreservesStructuredToolResultObject(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"tool_result","tool_use_id":"toolu_1","content":{"result":{"ok":true}}}
	]`)

	parts, err := translateRawContent(raw, map[string]string{"toolu_1": "Read"})
	if err != nil {
		t.Fatalf("translateRawContent returned error: %v", err)
	}
	if len(parts) != 1 || parts[0].FunctionResponse == nil {
		t.Fatalf("expected a single function response part, got %+v", parts)
	}
	result, ok := parts[0].FunctionResponse.Response["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured object response to be preserved, got %+v", parts[0].FunctionResponse.Response)
	}
	if okValue, ok := result["ok"].(bool); !ok || !okValue {
		t.Fatalf("expected nested object content to be preserved, got %+v", result)
	}
}
