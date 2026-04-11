package openai

import (
	"encoding/json"
	"testing"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

func TestTranslateRequestDropsHistoricalThinkingBlocks(t *testing.T) {
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
	if got := countItemsByType(items, "reasoning"); got != 0 {
		t.Fatalf("expected historical thinking blocks to be dropped, got %d reasoning items", got)
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
	if out.Reasoning.Effort != "high" {
		t.Fatalf("expected budget-driven reasoning effort high, got %q", out.Reasoning.Effort)
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

func TestTranslateRequestOutputConfigEffortTakesPriority(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:        "gpt-5.2",
		Messages:     []domain.Message{},
		Thinking:     json.RawMessage(`{"type":"enabled","budget_tokens":31999}`),
		OutputConfig: json.RawMessage(`{"effort":"max"}`),
	}

	out, err := translateRequest(req, map[string]any{
		"thinking_effort": "low",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.Reasoning == nil {
		t.Fatal("expected reasoning to be populated")
	}
	if out.Reasoning.Effort != "xhigh" {
		t.Fatalf("expected output_config.effort=max to map to xhigh, got %q", out.Reasoning.Effort)
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
	if out.Tools[0].Name != "mcp__Read" || out.Tools[1].Name != "Read" {
		t.Fatalf("unexpected tool order/names: %+v", out.Tools)
	}
	if out.Tools[0].Parameters == nil || out.Tools[1].Parameters == nil {
		t.Fatalf("expected flat Responses tool parameters, got %+v", out.Tools)
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
	if out.Tools[0].Name != "Read" {
		t.Fatalf("expected regular tool to remain, got %+v", out.Tools[0])
	}
}

func TestTranslateRequestInjectsPromptCacheKey(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
	}

	out, err := translateRequest(req, map[string]any{
		"prompt_cache_key": "account-123",
	})
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if out.PromptCacheKey != "account-123" {
		t.Fatalf("expected prompt_cache_key account-123, got %q", out.PromptCacheKey)
	}
}

func TestTranslateRequestToolChoiceToolUsesResponsesShape(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:      "gpt-5.2",
		Messages:   []domain.Message{},
		ToolChoice: json.RawMessage(`{"type":"tool","name":"Read"}`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	choice := mustMap(t, out.ToolChoice)
	if got := stringValue(choice["type"]); got != "function" {
		t.Fatalf("expected tool_choice type function, got %q", got)
	}
	if got := stringValue(choice["name"]); got != "Read" {
		t.Fatalf("expected tool_choice name Read, got %q", got)
	}
	if _, ok := choice["function"]; ok {
		t.Fatalf("expected Responses tool_choice shape without nested function, got %+v", choice)
	}
}

func TestTranslateRequestDropsStopSequencesForResponses(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:         "gpt-5.2",
		Messages:      []domain.Message{},
		StopSequences: []string{"END"},
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if _, ok := decoded["stop"]; ok {
		t.Fatalf("expected Responses payload to drop stop sequences, got %+v", decoded)
	}
}

func TestTranslateRequestCleansToolSchemaURIFormat(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Tools: json.RawMessage(`[
			{"name":"Fetch","description":"fetch","input_schema":{
				"type":"object",
				"properties":{
					"url":{"type":"string","format":"uri"},
					"nested":{"type":"array","items":{"type":"string","format":"uri"}}
				}
			}}
		]`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(out.Tools))
	}
	params := out.Tools[0].Parameters
	props := mustMap(t, params["properties"])
	urlProp := mustMap(t, props["url"])
	if _, ok := urlProp["format"]; ok {
		t.Fatalf("expected root uri format to be removed, got %+v", urlProp)
	}
	nested := mustMap(t, props["nested"])
	items := mustMap(t, nested["items"])
	if _, ok := items["format"]; ok {
		t.Fatalf("expected nested uri format to be removed, got %+v", items)
	}
}

func TestTranslateRequestUsesResponsesToolShape(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model:    "gpt-5.2",
		Messages: []domain.Message{},
		Tools: json.RawMessage(`[
			{"name":"Read","description":"read file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}
		]`),
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(out.Tools))
	}
	if got := out.Tools[0].Type; got != "function" {
		t.Fatalf("expected tool type function, got %q", got)
	}
	if got := out.Tools[0].Name; got != "Read" {
		t.Fatalf("expected tool name Read, got %q", got)
	}

	raw, err := json.Marshal(out.Tools[0])
	if err != nil {
		t.Fatalf("marshal tool: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal tool: %v", err)
	}
	if _, ok := decoded["function"]; ok {
		t.Fatalf("expected Responses tool shape without nested function, got %+v", decoded)
	}
}

func TestTranslateRequestUsesResponsesFunctionCallItemShape(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		Messages: []domain.Message{
			{
				Role: "assistant",
				Content: json.RawMessage(`[
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
	call := findFirstItemByType(t, items, "function_call")
	if got := stringValue(call["call_id"]); got != "toolu_1" {
		t.Fatalf("expected call_id toolu_1, got %q", got)
	}
	if _, ok := call["id"]; ok {
		t.Fatalf("expected Responses function_call item without id, got %+v", call)
	}
}

func TestTranslateRequestUsesResponsesFunctionCallOutputItemShape(t *testing.T) {
	req := &domain.CanonicalRequest{
		Model: "gpt-5.2",
		Messages: []domain.Message{
			{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}
				]`),
			},
		},
	}

	out, err := translateRequest(req, nil)
	if err != nil {
		t.Fatalf("translateRequest returned error: %v", err)
	}

	items := decodeJSONItems(t, out.Input)
	callOutput := findFirstItemByType(t, items, "function_call_output")
	if got := stringValue(callOutput["call_id"]); got != "toolu_1" {
		t.Fatalf("expected call_id toolu_1, got %q", got)
	}
	if _, ok := callOutput["id"]; ok {
		t.Fatalf("expected Responses function_call_output item without id, got %+v", callOutput)
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
