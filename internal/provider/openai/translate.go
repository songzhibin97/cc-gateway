package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type responsesRequest struct {
	Model           string              `json:"model"`
	Input           []any               `json:"input"`
	Instructions    string              `json:"instructions,omitempty"`
	Tools           []responsesTool     `json:"tools,omitempty"`
	ToolChoice      any                 `json:"tool_choice,omitempty"`
	Text            any                 `json:"text,omitempty"`
	Stop            []string            `json:"stop,omitempty"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	Stream          bool                `json:"stream,omitempty"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
}

type responsesTool struct {
	Type     string            `json:"type"`
	Function responsesFunction `json:"function"`
}

type responsesFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type responsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

func translateRequest(req *domain.CanonicalRequest, extra map[string]any) (*responsesRequest, error) {
	instructions, err := translateSystem(req.System)
	if err != nil {
		return nil, fmt.Errorf("translate system: %w", err)
	}

	input := make([]any, 0, len(req.Messages))
	for i, msg := range req.Messages {
		items, err := translateMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("translate message %d: %w", i, err)
		}
		input = append(input, items...)
	}

	requestReasoning, requestWantsReasoning := translateReasoning(req.Thinking)

	out := &responsesRequest{
		Model:        req.Model,
		Input:        input,
		Instructions: instructions,
		Temperature:  req.Temperature,
		TopP:         req.TopP,
		Stream:       req.Stream,
		Reasoning:    requestReasoning,
	}
	if req.MaxTokens > 0 {
		out.MaxOutputTokens = req.MaxTokens
	}

	if len(req.Tools) > 0 {
		toolFilter, _ := extra["tool_filter"].(string)
		if toolFilter == "" {
			toolFilter = "strip_mcp" // 默认过滤 MCP 工具
		}
		if toolFilter != "none" {
			tools, err := translateTools(req.Tools, toolFilter)
			if err != nil {
				return nil, fmt.Errorf("translate tools: %w", err)
			}
			out.Tools = tools
		}
	}
	if len(req.ToolChoice) > 0 {
		out.ToolChoice = translateToolChoice(req.ToolChoice)
	}
	// output_config 不翻译 — 多数第三方代理不支持 text/json_schema，
	// 模型可通过 system prompt 指令生成有效 JSON。
	if len(req.StopSequences) > 0 {
		out.Stop = req.StopSequences
	}

	// reasoning 配置：只有请求自带 thinking 时才启用 reasoning。
	// 具体 effort 优先走账号默认配置；若账号未配置，再回退到请求里可翻译的提示。
	if requestWantsReasoning {
		effortKey := stringValue(extra["thinking_effort"])
		if effortKey == "" {
			effortKey = stringValue(extra["reasoning_effort"])
		}
		if effort := strings.TrimSpace(effortKey); effort != "" {
			if out.Reasoning == nil {
				out.Reasoning = &responsesReasoning{}
			}
			out.Reasoning.Effort = effort
		}
	}
	// summary 只在 reasoning 已存在时补充，不凭空创建 reasoning
	if out.Reasoning != nil {
		summaryKey := stringValue(extra["thinking_summary"])
		if summaryKey == "" {
			summaryKey = stringValue(extra["reasoning_summary"])
		}
		if summaryKey == "" {
			summaryKey = "auto"
		}
		if out.Reasoning.Summary == "" {
			out.Reasoning.Summary = summaryKey
		}
	}

	return out, nil
}

func translateSystem(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text), nil
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("unsupported system payload: %w", err)
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" {
			return "", fmt.Errorf("unsupported system block type %q", block.Type)
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n"), nil
}

func translateTools(raw json.RawMessage, toolFilter string) ([]responsesTool, error) {
	var tools []anthropicTool
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}

	out := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		if toolFilter == "strip_mcp" && strings.HasPrefix(tool.Name, "mcp__") {
			continue
		}
		out = append(out, responsesTool{
			Type: "function",
			Function: responsesFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out, nil
}

func translateMessage(msg domain.Message) ([]any, error) {
	if len(bytes.TrimSpace(msg.Content)) == 0 {
		return []any{map[string]any{"role": msg.Role}}, nil
	}

	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		return []any{
			map[string]any{
				"role":    msg.Role,
				"content": text,
			},
		}, nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("unsupported message content: %w", err)
	}
	return translateContentBlocks(msg.Role, blocks)
}

func translateContentBlocks(role string, blocks []map[string]any) ([]any, error) {
	var out []any
	var textBlocks []map[string]any

	flushText := func() {
		if len(textBlocks) == 0 {
			return
		}
		out = append(out, map[string]any{
			"role":    role,
			"content": append([]map[string]any(nil), textBlocks...),
		})
		textBlocks = nil
	}

	for _, block := range blocks {
		switch stringValue(block["type"]) {
		case "text":
			partType := "input_text"
			if role == "assistant" {
				partType = "output_text"
			}
			textBlocks = append(textBlocks, map[string]any{
				"type": partType,
				"text": stringValue(block["text"]),
			})
		case "tool_use":
			if role != "assistant" {
				return nil, fmt.Errorf("tool_use block requires assistant role, got %q", role)
			}
			flushText()
			inputJSON, err := json.Marshal(block["input"])
			if err != nil {
				return nil, fmt.Errorf("marshal tool_use input: %w", err)
			}
			out = append(out, map[string]any{
				"type":      "function_call",
				"call_id":   stringValue(block["id"]),
				"name":      stringValue(block["name"]),
				"arguments": string(inputJSON),
			})
		case "tool_result":
			flushText()
			output, err := stringifyToolResultContent(block["content"])
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": stringValue(block["tool_use_id"]),
				"output":  output,
			})
		case "thinking":
			continue
		default:
			return nil, fmt.Errorf("unsupported content block type %q", stringValue(block["type"]))
		}
	}

	flushText()
	return out, nil
}

func stringifyToolResultContent(content any) (string, error) {
	switch v := content.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok {
				return "", fmt.Errorf("unsupported tool_result content item %T", item)
			}
			if stringValue(block["type"]) == "text" {
				parts = append(parts, stringValue(block["text"]))
			}
		}
		return strings.Join(parts, "\n"), nil
	default:
		buf, err := json.Marshal(content)
		if err != nil {
			return "", fmt.Errorf("marshal tool_result content: %w", err)
		}
		return string(buf), nil
	}
}

func translateReasoning(raw json.RawMessage) (*responsesReasoning, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, true
	}

	// Anthropic "thinking" settings do not map 1:1. Preserve only effort-like hints.
	for _, key := range []string{"effort", "level"} {
		if effort := strings.TrimSpace(stringValue(payload[key])); effort != "" {
			return &responsesReasoning{Effort: effort}, true
		}
	}

	switch strings.TrimSpace(stringValue(payload["type"])) {
	case "enabled":
		return &responsesReasoning{Effort: "medium"}, true
	case "adaptive", "disabled":
		return nil, true
	}

	if enabled, ok := payload["enabled"].(bool); ok && enabled {
		return &responsesReasoning{Effort: "medium"}, true
	}

	return nil, true
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func isTrue(v any) bool {
	b, _ := v.(bool)
	return b
}

func translateToolChoice(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	switch stringValue(payload["type"]) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		name := stringValue(payload["name"])
		if name == "" {
			return "required"
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		}
	}
	return nil
}

func translateOutputConfig(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	format, ok := cfg["format"].(map[string]any)
	if !ok {
		return nil
	}
	if stringValue(format["type"]) != "json_schema" {
		return nil
	}

	// Extract schema - try format.json_schema.schema first, then format.schema
	name := "response"
	var schema any
	if js, ok := format["json_schema"].(map[string]any); ok {
		if n := stringValue(js["name"]); n != "" {
			name = n
		}
		schema = js["schema"]
	}
	if schema == nil {
		schema = format["schema"]
	}
	if schema == nil {
		return nil
	}

	return map[string]any{
		"type":   "json_schema",
		"name":   name,
		"schema": schema,
	}
}
