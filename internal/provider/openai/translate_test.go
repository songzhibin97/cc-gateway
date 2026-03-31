package openai

import (
	"encoding/json"
	"testing"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

func TestTranslateRequestPreservesThinkingAndRedactedThinking(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		Messages: []domain.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"thinking","text":"plan A","signature":"sig_123"},
					{"type":"redacted_thinking","encrypted_content":"enc_456"},
					{"type":"text","text":"final"},
					{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"a.txt"}}
				]`),
			},
		},
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}

	items := decodeJSONItems(t, out.Input)
	reasoning := findFirstItemByType(t, items, "reasoning")
	summary := mustArrayOfMaps(t, reasoning["summary"])
	if len(summary) != 2 {
		t.Fatalf("expected 2 reasoning summaries, got %+v", summary)
	}
	if got := stringValue(summary[0]["text"]); got != "plan A" {
		t.Fatalf("expected first reasoning summary plan A, got %q", got)
	}
	if got := stringValue(summary[1]["text"]); got == "" {
		t.Fatal("expected redacted thinking summary to be preserved")
	}
	if got := stringValue(reasoning["encrypted_content"]); got == "" {
		t.Fatal("expected encrypted_content to be preserved for redacted thinking")
	}

	call := findFirstItemByType(t, items, "function_call")
	if got := stringValue(call["name"]); got != "Read" {
		t.Fatalf("expected tool call name Read, got %q", got)
	}

	assistant := findFirstItemWithRole(t, items, "assistant")
	content := mustArrayOfMaps(t, assistant["content"])
	if got := countContentType(content, "output_text"); got == 0 {
		t.Fatal("expected assistant text to be preserved")
	}
}

func TestTranslateRequestAcceptsExtendedBlockSurface(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		Messages: []domain.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"server_tool_use","id":"srv_1","name":"advisor","input":{"query":"status"}},
					{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"text","text":"search hit"}]},
					{"type":"mcp_tool_use","id":"mcp_1","name":"mcp__Read","input":{"path":"foo"}},
					{"type":"container_upload","content":"opaque"},
					{"type":"connector_text","connector_text":"connector note"}
				]`),
			},
			{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},
					{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"cGRm"},"filename":"doc.pdf"}
				]`),
			},
		},
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}

	items := decodeJSONItems(t, out.Input)
	if got := countItemsByType(items, "function_call"); got != 2 {
		t.Fatalf("expected 2 function_call items, got %d", got)
	}
	assistant := findFirstItemWithRole(t, items, "assistant")
	content := mustArrayOfMaps(t, assistant["content"])
	if got := countContentType(content, "output_text"); got == 0 {
		t.Fatal("expected connector_text to be preserved as assistant text")
	}

	user := findFirstItemWithRole(t, items, "user")
	userContent := mustArrayOfMaps(t, user["content"])
	if got := countContentType(userContent, "input_image"); got != 1 {
		t.Fatalf("expected 1 input_image block, got %d", got)
	}
	if got := countContentType(userContent, "input_file"); got != 1 {
		t.Fatalf("expected 1 input_file block, got %d", got)
	}
}

func TestTranslateRequestPreservesStructuredToolResultContent(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		Messages: []domain.Message{
			{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"tool_result","tool_use_id":"toolu_1","content":[
						{"type":"text","text":"hello"},
						{"type":"tool_reference","tool_name":"Read"},
						{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}},
						{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"cGRm"}}
					]}
				]`),
			},
		},
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}

	items := decodeJSONItems(t, out.Input)
	output := findFirstItemByType(t, items, "function_call_output")
	got := stringValue(output["output"])
	var preserved []map[string]any
	if err := json.Unmarshal([]byte(got), &preserved); err != nil {
		t.Fatalf("expected structured tool_result content to remain JSON, got %q: %v", got, err)
	}
	if got := countContentType(preserved, "tool_reference"); got != 1 {
		t.Fatalf("expected tool_reference to be preserved, got %+v", preserved)
	}
	if got := countContentType(preserved, "image"); got != 1 {
		t.Fatalf("expected image to be preserved, got %+v", preserved)
	}
	if got := countContentType(preserved, "document"); got != 1 {
		t.Fatalf("expected document to be preserved, got %+v", preserved)
	}
}

func TestTranslateRequestMapsOutputConfigToTextFormat(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		OutputConfig: json.RawMessage(`{
			"format": {
				"type": "json_schema",
				"json_schema": {
					"name": "task",
					"schema": {
						"type": "object",
						"properties": {
							"answer": {"type": "string"}
						},
						"required": ["answer"]
					}
				}
			}
		}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Text == nil {
		t.Fatal("expected text config to be populated")
	}

	textCfg := mustMap(t, out.Text)
	format := mustMap(t, textCfg["format"])
	if got := stringValue(format["type"]); got != "json_schema" {
		t.Fatalf("expected json_schema format, got %q", got)
	}
	if got := stringValue(format["name"]); got != "task" {
		t.Fatalf("expected schema name task, got %q", got)
	}
	schema := mustMap(t, format["schema"])
	props := mustMap(t, schema["properties"])
	if _, ok := props["answer"]; !ok {
		t.Fatalf("expected answer property to be preserved, got %+v", schema)
	}
}

func TestTranslateRequestMapsTaskBudgetToMaxOutputTokens(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:     "gpt-5.2",
		MaxTokens: 2048,
		OutputConfig: json.RawMessage(`{
			"task_budget": {
				"total": 1024,
				"remaining": 512
			}
		}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.MaxOutputTokens != 512 {
		t.Fatalf("expected task_budget remaining to cap max_output_tokens at 512, got %d", out.MaxOutputTokens)
	}
}

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

func decodeJSONItems(t *testing.T, input []any) []map[string]any {
	t.Helper()

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	return items
}

func findFirstItemByType(t *testing.T, items []map[string]any, typ string) map[string]any {
	t.Helper()

	for _, item := range items {
		if stringValue(item["type"]) == typ {
			return item
		}
	}
	t.Fatalf("missing item type %q in %+v", typ, items)
	return nil
}

func findFirstItemWithRole(t *testing.T, items []map[string]any, role string) map[string]any {
	t.Helper()

	for _, item := range items {
		if stringValue(item["role"]) == role {
			return item
		}
	}
	t.Fatalf("missing item role %q in %+v", role, items)
	return nil
}

func mustArrayOfMaps(t *testing.T, v any) []map[string]any {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal array: %v", err)
	}

	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal array: %v", err)
	}
	return out
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal map: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal map: %v", err)
	}
	return out
}

func countItemsByType(items []map[string]any, typ string) int {
	count := 0
	for _, item := range items {
		if stringValue(item["type"]) == typ {
			count++
		}
	}
	return count
}

func countContentType(items []map[string]any, typ string) int {
	count := 0
	for _, item := range items {
		if stringValue(item["type"]) == typ {
			count++
		}
	}
	return count
}
