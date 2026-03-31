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

func TestTranslateRequestAppliesOutputConfigStructuredOutputsAndBudget(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:     "gemini-3.0-pro",
		MaxTokens: 4096,
		Messages:  []domain.Message{},
		Thinking:  json.RawMessage(`{"type":"enabled"}`),
		OutputConfig: json.RawMessage(`{
			"effort":"low",
			"task_budget":{"type":"tokens","total":2048},
			"format":{"type":"json_schema","json_schema":{"name":"recipe","schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}}}
		}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.GenerationConfig == nil {
		t.Fatal("expected generation config to be populated")
	}
	if out.GenerationConfig.ResponseMimeType != "application/json" {
		t.Fatalf("expected responseMimeType application/json, got %q", out.GenerationConfig.ResponseMimeType)
	}
	schema, ok := out.GenerationConfig.ResponseJsonSchema.(map[string]any)
	if !ok {
		t.Fatalf("expected responseJsonSchema map, got %#v", out.GenerationConfig.ResponseJsonSchema)
	}
	if schema["type"] != "object" {
		t.Fatalf("expected object schema, got %#v", schema["type"])
	}
	if out.GenerationConfig.MaxOutputTokens != 2048 {
		t.Fatalf("expected task_budget to cap maxOutputTokens at 2048, got %d", out.GenerationConfig.MaxOutputTokens)
	}
	if out.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected thinking config to be populated")
	}
	if out.GenerationConfig.ThinkingConfig.ThinkingLevel != "low" {
		t.Fatalf("expected output_config effort to drive thinking level low, got %+v", out.GenerationConfig.ThinkingConfig)
	}
	if !out.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Fatal("expected includeThoughts to stay enabled")
	}
}

func TestTranslateRequestOutputConfigEffortEnablesThinkingWithoutThinkingRequest(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gemini-3.0-pro",
		Messages: []domain.Message{},
		OutputConfig: json.RawMessage(`{
			"effort":"high"
		}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.GenerationConfig == nil || out.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected output_config effort to enable thinking config")
	}
	if out.GenerationConfig.ThinkingConfig.ThinkingLevel != "high" {
		t.Fatalf("expected output_config effort to drive thinking level high, got %+v", out.GenerationConfig.ThinkingConfig)
	}
	if !out.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Fatal("expected includeThoughts to stay enabled")
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

func TestTranslateRawContentSupportsWideBlockSurface(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"redacted_thinking","data":"ciphertext","signature":"sig_redacted"},
		{"type":"server_tool_use","id":"srv_1","name":"advisor","input":{"query":"status"}},
		{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"text","text":"summary"},{"type":"tool_reference","tool_name":"Read"}]},
		{"type":"mcp_tool_use","id":"mcp_1","name":"Read","input":{"path":"a.txt"}},
		{"type":"container_upload","content":"archive.zip"},
		{"type":"connector_text","connector_text":"connector output","signature":"sig_connector"}
	]`)

	parts, err := translateRawContent(raw, map[string]string{})
	if err != nil {
		t.Fatalf("translateRawContent returned error: %v", err)
	}
	if len(parts) != 6 {
		t.Fatalf("expected 6 parts, got %d", len(parts))
	}
	if !parts[0].Thought || parts[0].Text != "" || parts[0].ThoughtSignature != "sig_redacted" {
		t.Fatalf("expected redacted thinking to preserve position without visible text, got %+v", parts[0])
	}
	if parts[1].FunctionCall == nil || parts[1].FunctionCall.Name != "advisor" {
		t.Fatalf("expected server_tool_use to map to function call, got %+v", parts[1])
	}
	if parts[2].FunctionResponse == nil || parts[2].FunctionResponse.Name != "advisor" {
		t.Fatalf("expected web_search_tool_result to map to function response, got %+v", parts[2])
	}
	if parts[3].FunctionCall == nil || parts[3].FunctionCall.Name != "Read" {
		t.Fatalf("expected mcp_tool_use to map to function call, got %+v", parts[3])
	}
	if parts[4].FunctionResponse == nil || parts[4].FunctionResponse.Name != "container_upload" {
		t.Fatalf("expected container_upload to preserve a structured response, got %+v", parts[4])
	}
	if parts[5].Text != "connector output" {
		t.Fatalf("expected connector_text to downgrade to text, got %+v", parts[5])
	}
}

func TestTranslateRawContentPreservesStructuredToolResultContent(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"tool_use","id":"toolu_1","name":"ToolSearch","input":{"query":"read"}},
		{"type":"tool_result","tool_use_id":"toolu_1","content":[
			{"type":"text","text":"summary"},
			{"type":"tool_reference","tool_name":"Read"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}
		]}
	]`)

	parts, err := translateRawContent(raw, map[string]string{})
	if err != nil {
		t.Fatalf("translateRawContent returned error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[1].FunctionResponse == nil {
		t.Fatalf("expected tool_result to map to function response, got %+v", parts[1])
	}
	content, ok := parts[1].FunctionResponse.Response["content"].([]any)
	if !ok {
		t.Fatalf("expected structured content array to be preserved, got %+v", parts[1].FunctionResponse.Response)
	}
	if len(content) != 3 {
		t.Fatalf("expected 3 content items, got %d", len(content))
	}
	first, ok := content[0].(map[string]any)
	if !ok || first["type"] != "text" {
		t.Fatalf("expected first item to remain text, got %+v", content[0])
	}
	second, ok := content[1].(map[string]any)
	if !ok || second["type"] != "tool_reference" || second["tool_name"] != "Read" {
		t.Fatalf("expected tool_reference to be preserved, got %+v", content[1])
	}
	third, ok := content[2].(map[string]any)
	if !ok || third["type"] != "image" {
		t.Fatalf("expected image payload to be preserved, got %+v", content[2])
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
