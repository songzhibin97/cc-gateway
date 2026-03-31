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

const defaultReasoningEffort = "medium"

type reasoningState struct {
	reasoning         *responsesReasoning
	enabled           bool
	hasExplicitEffort bool
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

	requestReasoning := translateReasoning(req.Thinking)

	out := &responsesRequest{
		Model:        req.Model,
		Input:        input,
		Instructions: instructions,
		Temperature:  req.Temperature,
		TopP:         req.TopP,
		Stream:       req.Stream,
		Reasoning:    requestReasoning.reasoning,
	}
	if req.MaxTokens > 0 {
		out.MaxOutputTokens = req.MaxTokens
	}

	if len(req.Tools) > 0 {
		toolFilter, _ := extra["tool_filter"].(string)
		toolFilter = strings.TrimSpace(toolFilter)
		if toolFilter == "" {
			toolFilter = "passthrough"
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
	if outputConfig := translateOutputConfig(req.OutputConfig); outputConfig != nil {
		if outputConfig.text != nil {
			out.Text = map[string]any{
				"format": outputConfig.text,
			}
		}
		if outputConfig.maxOutputTokens > 0 && (out.MaxOutputTokens == 0 || outputConfig.maxOutputTokens < out.MaxOutputTokens) {
			out.MaxOutputTokens = outputConfig.maxOutputTokens
		}
	}
	if len(req.StopSequences) > 0 {
		out.Stop = req.StopSequences
	}

	// reasoning 仅在请求明确要求 thinking 时启用。
	if requestReasoning.enabled {
		effortKey := strings.TrimSpace(stringValue(extra["thinking_effort"]))
		if effortKey == "" {
			effortKey = strings.TrimSpace(stringValue(extra["reasoning_effort"]))
		}
		if out.Reasoning == nil {
			out.Reasoning = &responsesReasoning{}
		}
		if requestReasoning.hasExplicitEffort {
			out.Reasoning.Effort = requestReasoning.reasoning.Effort
		} else if effortKey != "" {
			out.Reasoning.Effort = effortKey
		} else if out.Reasoning.Effort == "" {
			out.Reasoning.Effort = defaultReasoningEffort
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
	var reasoningBlocks []map[string]any
	reasoningIndex := 0

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

	flushReasoning := func() {
		if len(reasoningBlocks) == 0 {
			return
		}
		out = append(out, buildReasoningItem(reasoningBlocks, reasoningIndex))
		reasoningIndex++
		reasoningBlocks = nil
	}

	for _, block := range blocks {
		switch blockType := stringValue(block["type"]); blockType {
		case "text":
			flushReasoning()
			partType := "input_text"
			if role == "assistant" {
				partType = "output_text"
			}
			textBlocks = append(textBlocks, map[string]any{
				"type": partType,
				"text": stringValue(block["text"]),
			})
		case "connector_text":
			flushReasoning()
			partType := "input_text"
			if role == "assistant" {
				partType = "output_text"
			}
			text := stringValue(block["connector_text"])
			if text == "" {
				text = stringValue(block["text"])
			}
			textBlocks = append(textBlocks, map[string]any{
				"type": partType,
				"text": text,
			})
		case "image":
			flushReasoning()
			if contentBlock, ok := translateImageContentBlock(block); ok {
				textBlocks = append(textBlocks, contentBlock)
				continue
			}
			textBlocks = append(textBlocks, fallbackTextContentBlock(role, block))
		case "document":
			flushReasoning()
			if contentBlock, ok := translateDocumentContentBlock(block); ok {
				textBlocks = append(textBlocks, contentBlock)
				continue
			}
			textBlocks = append(textBlocks, fallbackTextContentBlock(role, block))
		case "tool_use":
			flushText()
			flushReasoning()
			if item, ok := translateFunctionCallItem(block, blockType); ok {
				out = append(out, item)
				continue
			}
			textBlocks = append(textBlocks, fallbackTextContentBlock(role, block))
		case "server_tool_use", "mcp_tool_use":
			flushText()
			flushReasoning()
			if item, ok := translateFunctionCallItem(block, blockType); ok {
				out = append(out, item)
				continue
			}
			textBlocks = append(textBlocks, fallbackTextContentBlock(role, block))
		case "tool_result", "web_search_tool_result", "mcp_tool_result", "code_execution_tool_result", "web_fetch_tool_result", "bash_code_execution_tool_result", "text_editor_code_execution_tool_result", "tool_search_tool_result":
			flushText()
			flushReasoning()
			if item, ok := translateFunctionCallOutputItem(block); ok {
				out = append(out, item)
				continue
			}
			textBlocks = append(textBlocks, fallbackTextContentBlock(role, block))
		case "thinking", "redacted_thinking":
			flushText()
			reasoningBlocks = append(reasoningBlocks, block)
		default:
			flushReasoning()
			textBlocks = append(textBlocks, fallbackTextContentBlock(role, block))
		}
	}

	flushText()
	flushReasoning()
	return out, nil
}

func stringifyToolResultContent(content any) (string, error) {
	switch v := content.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case []any:
		textOnly := true
		parts := make([]string, 0, len(v))
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok || stringValue(block["type"]) != "text" {
				textOnly = false
				break
			}
			parts = append(parts, stringValue(block["text"]))
		}
		if textOnly {
			return strings.Join(parts, "\n"), nil
		}
		buf, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("marshal tool_result content: %w", err)
		}
		return string(buf), nil
	default:
		buf, err := json.Marshal(content)
		if err != nil {
			return "", fmt.Errorf("marshal tool_result content: %w", err)
		}
		return string(buf), nil
	}
}

func partTypeForRole(role string) string {
	if role == "assistant" {
		return "output_text"
	}
	return "input_text"
}

func fallbackTextContentBlock(role string, block map[string]any) map[string]any {
	text := stringifyBlock(block)
	if text == "" {
		text = stringValue(block["text"])
	}
	return map[string]any{
		"type": partTypeForRole(role),
		"text": text,
	}
}

func translateImageContentBlock(block map[string]any) (map[string]any, bool) {
	source, ok := asMap(block["source"])
	if !ok {
		return nil, false
	}

	switch stringValue(source["type"]) {
	case "base64":
		data := stringValue(source["data"])
		if data == "" {
			return nil, false
		}
		mediaType := stringValue(source["media_type"])
		if mediaType == "" {
			mediaType = "image/png"
		}
		return map[string]any{
			"type":      "input_image",
			"detail":    "auto",
			"image_url": "data:" + mediaType + ";base64," + data,
		}, true
	case "url":
		imageURL := stringValue(source["image_url"])
		if imageURL == "" {
			imageURL = stringValue(source["url"])
		}
		if imageURL == "" {
			return nil, false
		}
		return map[string]any{
			"type":      "input_image",
			"detail":    "auto",
			"image_url": imageURL,
		}, true
	}

	return nil, false
}

func translateDocumentContentBlock(block map[string]any) (map[string]any, bool) {
	source, ok := asMap(block["source"])
	if !ok {
		return nil, false
	}

	item := map[string]any{
		"type": "input_file",
	}
	added := false

	if data := stringValue(source["data"]); data != "" {
		item["file_data"] = data
		added = true
	}
	if fileURL := stringValue(source["file_url"]); fileURL != "" {
		item["file_url"] = fileURL
		added = true
	}
	if fileID := stringValue(source["file_id"]); fileID != "" {
		item["file_id"] = fileID
		added = true
	}
	if filename := stringValue(source["filename"]); filename != "" {
		item["filename"] = filename
		added = true
	}
	if !added {
		return nil, false
	}
	return item, true
}

func translateFunctionCallItem(block map[string]any, blockType string) (map[string]any, bool) {
	name := stringValue(block["name"])
	if name == "" {
		return nil, false
	}

	callID := firstNonEmpty(
		stringValue(block["call_id"]),
		stringValue(block["tool_use_id"]),
		stringValue(block["id"]),
		name,
		blockType,
	)
	arguments, err := json.Marshal(firstNonNil(block["input"], block["arguments"], map[string]any{}))
	if err != nil {
		return nil, false
	}

	item := map[string]any{
		"type":      "function_call",
		"id":        callID,
		"call_id":   callID,
		"name":      name,
		"arguments": string(arguments),
	}
	return item, true
}

func translateFunctionCallOutputItem(block map[string]any) (map[string]any, bool) {
	callID := firstNonEmpty(
		stringValue(block["tool_use_id"]),
		stringValue(block["call_id"]),
		stringValue(block["id"]),
	)
	if callID == "" {
		return nil, false
	}

	output, err := stringifyToolResultContent(firstNonNil(block["content"], block["output"], block["result"]))
	if err != nil {
		return nil, false
	}

	return map[string]any{
		"type":    "function_call_output",
		"id":      callID,
		"call_id": callID,
		"output":  output,
	}, true
}

func buildReasoningItem(blocks []map[string]any, index int) map[string]any {
	summaries := make([]map[string]any, 0, len(blocks))
	encryptedContent := ""

	for _, block := range blocks {
		summaryText := reasoningSummaryText(block)
		summaries = append(summaries, map[string]any{
			"type": "summary_text",
			"text": summaryText,
		})

		if encryptedContent == "" && stringValue(block["type"]) == "redacted_thinking" {
			if raw := stringValue(block["encrypted_content"]); raw != "" {
				encryptedContent = raw
			} else {
				encryptedContent = stringifyBlock(block)
			}
		}
	}

	item := map[string]any{
		"type":    "reasoning",
		"id":      fmt.Sprintf("reasoning_%d", index),
		"status":  "completed",
		"summary": summaries,
	}
	if encryptedContent != "" {
		item["encrypted_content"] = encryptedContent
	}
	return item
}

func reasoningSummaryText(block map[string]any) string {
	for _, key := range []string{"text", "thinking", "connector_text", "summary"} {
		if text := strings.TrimSpace(stringValue(block[key])); text != "" {
			return text
		}
	}
	if stringValue(block["type"]) == "redacted_thinking" {
		return "[redacted_thinking]"
	}
	return stringifyBlock(block)
}

func stringifyBlock(block map[string]any) string {
	buf, err := json.Marshal(block)
	if err != nil {
		return ""
	}
	return string(buf)
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func translateReasoning(raw json.RawMessage) reasoningState {
	if len(bytes.TrimSpace(raw)) == 0 {
		return reasoningState{}
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return reasoningState{
			reasoning: &responsesReasoning{Effort: defaultReasoningEffort},
			enabled:   true,
		}
	}

	// Anthropic "thinking" settings do not map 1:1. Preserve only effort-like hints.
	for _, key := range []string{"effort", "level"} {
		if effort := strings.TrimSpace(stringValue(payload[key])); effort != "" {
			return reasoningState{
				reasoning:         &responsesReasoning{Effort: effort},
				enabled:           true,
				hasExplicitEffort: true,
			}
		}
	}

	switch strings.TrimSpace(stringValue(payload["type"])) {
	case "enabled":
		return reasoningState{
			reasoning: &responsesReasoning{Effort: defaultReasoningEffort},
			enabled:   true,
		}
	case "adaptive":
		return reasoningState{
			enabled: true,
		}
	case "disabled":
		return reasoningState{}
	}

	if enabled, ok := payload["enabled"].(bool); ok {
		if !enabled {
			return reasoningState{}
		}
		return reasoningState{
			reasoning: &responsesReasoning{Effort: defaultReasoningEffort},
			enabled:   true,
		}
	}

	return reasoningState{
		reasoning: &responsesReasoning{Effort: defaultReasoningEffort},
		enabled:   true,
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func positiveInt(v any) int {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return n
		}
	case int8:
		if n > 0 {
			return int(n)
		}
	case int16:
		if n > 0 {
			return int(n)
		}
	case int32:
		if n > 0 {
			return int(n)
		}
	case int64:
		if n > 0 {
			return int(n)
		}
	case float32:
		if n > 0 {
			return int(n)
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	}
	return 0
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

type outputConfigState struct {
	text            any
	maxOutputTokens int
}

func translateOutputConfig(raw json.RawMessage) *outputConfigState {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}

	out := &outputConfigState{}
	if format, ok := cfg["format"].(map[string]any); ok {
		if stringValue(format["type"]) == "json_schema" {
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
			if schema != nil {
				out.text = map[string]any{
					"type":   "json_schema",
					"name":   name,
					"schema": schema,
				}
			}
		}
	}

	if taskBudget, ok := cfg["task_budget"].(map[string]any); ok {
		if remaining := positiveInt(taskBudget["remaining"]); remaining > 0 {
			out.maxOutputTokens = remaining
		} else if total := positiveInt(taskBudget["total"]); total > 0 {
			out.maxOutputTokens = total
		}
	}

	if out.text == nil && out.maxOutputTokens == 0 {
		return nil
	}
	return out
}
