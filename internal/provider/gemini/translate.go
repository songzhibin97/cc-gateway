package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type generateContentRequest struct {
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	Contents          []content         `json:"contents"`
	Tools             []tool            `json:"tools,omitempty"`
	ToolConfig        *toolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []safetySetting   `json:"safetySettings,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type functionCall struct {
	Name string `json:"name"`
	Args any    `json:"args,omitempty"`
}

type functionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response,omitempty"`
}

type toolConfig struct {
	FunctionCallingConfig functionCallingConfig `json:"functionCallingConfig"`
}

type functionCallingConfig struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type tool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
}

type functionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type generationConfig struct {
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	TopK            *int            `json:"topK,omitempty"`
	StopSequences   []string        `json:"stopSequences,omitempty"`
	ThinkingConfig  *thinkingConfig `json:"thinkingConfig,omitempty"`
}

type thinkingConfig struct {
	ThinkingBudget int    `json:"thinkingBudget,omitempty"`
	ThinkingLevel  string `json:"thinkingLevel,omitempty"`
	IncludeThoughts bool  `json:"includeThoughts,omitempty"`
}

type safetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type thinkingRequest struct {
	Enabled bool
	Adaptive bool
	Effort  string
	Budget  int
}

// ToolSchema holds allowed parameter names and required fields for a tool.
type ToolSchema struct {
	Allowed  map[string]bool
	Required []string
}

// BuildToolSchemaIndex 从 Anthropic tools 定义中提取每个 tool 的合法参数名和 required 列表。
func BuildToolSchemaIndex(rawTools json.RawMessage) map[string]*ToolSchema {
	if len(bytes.TrimSpace(rawTools)) == 0 {
		return nil
	}

	var tools []anthropicTool
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil
	}

	index := make(map[string]*ToolSchema, len(tools))
	for _, t := range tools {
		props, ok := t.InputSchema["properties"].(map[string]any)
		if !ok {
			continue
		}

		allowed := make(map[string]bool, len(props))
		for k := range props {
			allowed[k] = true
		}

		var required []string
		if reqRaw, ok := t.InputSchema["required"].([]any); ok {
			for _, r := range reqRaw {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}

		index[t.Name] = &ToolSchema{Allowed: allowed, Required: required}
	}

	return index
}

func translateRequest(req *domain.CanonicalRequest, extra map[string]any) (*generateContentRequest, error) {
	out := &generateContentRequest{
		Contents: make([]content, 0, len(req.Messages)),
	}

	if len(bytes.TrimSpace(req.System)) != 0 {
		parts, err := translateRawContent(req.System, nil)
		if err != nil {
			return nil, fmt.Errorf("translate system: %w", err)
		}
		out.SystemInstruction = &content{Parts: parts}
	}

	toolNameByID := make(map[string]string)
	for i, msg := range req.Messages {
		parts, err := translateRawContent(msg.Content, toolNameByID)
		if err != nil {
			return nil, fmt.Errorf("translate message %d: %w", i, err)
		}
		out.Contents = append(out.Contents, content{
			Role:  mapMessageRole(msg.Role),
			Parts: parts,
		})
	}

	if len(bytes.TrimSpace(req.Tools)) != 0 {
		var tools []anthropicTool
		if err := json.Unmarshal(req.Tools, &tools); err != nil {
			return nil, fmt.Errorf("translate tools: %w", err)
		}

		// Filter tools: "none" drops all, "strip_mcp" drops MCP tools,
		// and the default "passthrough" keeps everything.
		toolFilter, _ := extra["tool_filter"].(string)
		toolFilter = strings.TrimSpace(toolFilter)
		if toolFilter == "" {
			toolFilter = "passthrough"
		}

		declarations := make([]functionDeclaration, 0, len(tools))
		for _, t := range tools {
			if toolFilter == "none" {
				break
			}
			if toolFilter != "passthrough" && strings.HasPrefix(t.Name, "mcp__") {
				continue
			}
			declarations = append(declarations, functionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  convertSchemaTypes(t.InputSchema),
			})
		}
		if len(declarations) != 0 {
			out.Tools = []tool{{FunctionDeclarations: declarations}}
		}
	}
	if len(req.ToolChoice) > 0 {
		out.ToolConfig = translateToolChoice(req.ToolChoice)
	}

	cfg := &generationConfig{
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	}
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = req.MaxTokens
	}
	if len(req.StopSequences) > 0 {
		cfg.StopSequences = req.StopSequences
	}
	// thinking 仅在请求明确要求时启用（Gemini 2.5+, Gemini 3+）。
	if thinking := parseThinkingRequest(req.Thinking); thinking.Enabled && modelSupportsThinking(req.Model) {
		cfg.ThinkingConfig = buildThinkingConfig(req.Model, thinking, extra)
	}
	if cfg.MaxOutputTokens > 0 || cfg.Temperature != nil || cfg.TopP != nil || cfg.TopK != nil || len(cfg.StopSequences) > 0 || cfg.ThinkingConfig != nil {
		out.GenerationConfig = cfg
	}
	// safety: 优先 extra 配置，否则默认 "off"（BLOCK_NONE，API 调用无需内容过滤）
	level := strings.TrimSpace(stringValue(extra["safety_level"]))
	if level == "" {
		level = "off"
	}
	if level != "" {
		out.SafetySettings = translateSafetyLevel(level)
	}
	if out.SafetySettings == nil {
		out.SafetySettings = translateSafetySettings(extra["safety_settings"])
	}

	return out, nil
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

func translateToolChoice(raw json.RawMessage) *toolConfig {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	tc := stringValue(payload["type"])
	switch tc {
	case "auto":
		return &toolConfig{FunctionCallingConfig: functionCallingConfig{Mode: "AUTO"}}
	case "any":
		return &toolConfig{FunctionCallingConfig: functionCallingConfig{Mode: "ANY"}}
	case "none":
		return &toolConfig{FunctionCallingConfig: functionCallingConfig{Mode: "NONE"}}
	case "tool":
		name := stringValue(payload["name"])
		if name == "" {
			return &toolConfig{FunctionCallingConfig: functionCallingConfig{Mode: "ANY"}}
		}
		return &toolConfig{FunctionCallingConfig: functionCallingConfig{
			Mode:                 "ANY",
			AllowedFunctionNames: []string{name},
		}}
	}
	return nil
}

// Map short category names to Gemini API full names.
var harmCategoryMap = map[string]string{
	"harassment":        "HARM_CATEGORY_HARASSMENT",
	"hate_speech":       "HARM_CATEGORY_HATE_SPEECH",
	"sexually_explicit": "HARM_CATEGORY_SEXUALLY_EXPLICIT",
	"dangerous_content": "HARM_CATEGORY_DANGEROUS_CONTENT",
	"civic_integrity":   "HARM_CATEGORY_CIVIC_INTEGRITY",
}

func translateSafetySettings(v any) []safetySetting {
	raw, ok := v.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}

	out := make([]safetySetting, 0, len(raw))
	for category, thresholdValue := range raw {
		category = strings.TrimSpace(category)
		threshold := strings.TrimSpace(fmt.Sprint(thresholdValue))
		if category == "" || threshold == "" || threshold == "default" {
			continue // skip empty or "default" (means no override)
		}

		// Map short names to full Gemini category names
		if full, ok := harmCategoryMap[category]; ok {
			category = full
		}
		// Ensure category has HARM_CATEGORY_ prefix
		if !strings.HasPrefix(category, "HARM_CATEGORY_") {
			category = "HARM_CATEGORY_" + strings.ToUpper(category)
		}

		out = append(out, safetySetting{
			Category:  category,
			Threshold: threshold,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func translateSafetyLevel(level string) []safetySetting {
	var threshold string
	switch level {
	case "off":
		threshold = "BLOCK_NONE"
	case "low":
		threshold = "BLOCK_ONLY_HIGH"
	case "medium":
		threshold = "BLOCK_MEDIUM_AND_ABOVE"
	case "high":
		threshold = "BLOCK_LOW_AND_ABOVE"
	default:
		return nil
	}

	categories := []string{
		"HARM_CATEGORY_HARASSMENT",
		"HARM_CATEGORY_HATE_SPEECH",
		"HARM_CATEGORY_SEXUALLY_EXPLICIT",
		"HARM_CATEGORY_DANGEROUS_CONTENT",
		"HARM_CATEGORY_CIVIC_INTEGRITY",
	}

	out := make([]safetySetting, 0, len(categories))
	for _, cat := range categories {
		out = append(out, safetySetting{Category: cat, Threshold: threshold})
	}
	return out
}

func translateRawContent(raw json.RawMessage, toolNameByID map[string]string) ([]part, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []part{{Text: text}}, nil
	}

	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("unsupported content payload: %w", err)
	}

	out := make([]part, 0, len(blocks))
	var pendingThoughtSignature string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			out = append(out, part{Text: block.Text})
		case "tool_use":
			var input any
			if len(bytes.TrimSpace(block.Input)) != 0 {
				if err := json.Unmarshal(block.Input, &input); err != nil {
					return nil, fmt.Errorf("decode tool_use input for %q: %w", block.Name, err)
				}
			}
			if block.ID != "" && block.Name != "" && toolNameByID != nil {
				toolNameByID[block.ID] = block.Name
			}
			callPart := part{
				FunctionCall: &functionCall{
					Name: block.Name,
					Args: input,
				},
			}
			if pendingThoughtSignature != "" {
				callPart.ThoughtSignature = pendingThoughtSignature
				pendingThoughtSignature = ""
			}
			out = append(out, callPart)
		case "tool_result":
			name := toolNameByID[block.ToolUseID]
			if name == "" {
				name = "unknown_tool"
			}
			response, err := translateToolResultResponse(block.Content)
			if err != nil {
				return nil, fmt.Errorf("translate tool_result for %q: %w", block.ToolUseID, err)
			}
			out = append(out, part{
				FunctionResponse: &functionResponse{
					ID:   block.ToolUseID,
					Name: name,
					Response: response,
				},
			})
		case "thinking":
			out = append(out, part{
				Text:             block.Text,
				Thought:          true,
				ThoughtSignature: block.Signature,
			})
			if block.Signature != "" {
				pendingThoughtSignature = block.Signature
			}
		default:
			return nil, fmt.Errorf("unsupported content block type %q", block.Type)
		}
	}

	return out, nil
}

func translateToolResultResponse(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return map[string]any{"content": ""}, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return map[string]any{"content": text}, nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		textOnly := true
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if stringValue(block["type"]) != "text" {
				textOnly = false
				break
			}
			parts = append(parts, stringValue(block["text"]))
		}
		if textOnly {
			return map[string]any{"content": strings.Join(parts, "\n")}, nil
		}
		return map[string]any{"content": blocks}, nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}

	if obj, ok := value.(map[string]any); ok {
		return obj, nil
	}
	return map[string]any{"content": value}, nil
}

func convertSchemaTypes(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	converted, ok := convertSchemaValue(schema).(map[string]any)
	if !ok {
		return schema
	}
	cleanRequiredFields(converted)
	return converted
}

// cleanRequiredFields recursively removes entries from "required" arrays
// that reference properties not present in "properties", and removes
// properties whose value is an empty object (no meaningful fields left after filtering).
func cleanRequiredFields(schema map[string]any) {
	// First recurse into nested properties
	if props, ok := schema["properties"].(map[string]any); ok {
		for key, val := range props {
			if nested, ok := val.(map[string]any); ok {
				cleanRequiredFields(nested)
				// If property became empty after cleaning (no type, no description, nothing useful), drop it
				if len(nested) == 0 {
					delete(props, key)
				}
			}
		}
		// Also recurse into items (array element schema)
		if items, ok := schema["items"].(map[string]any); ok {
			cleanRequiredFields(items)
		}
	}

	// Now clean required list: only keep fields that exist in properties
	reqRaw, hasReq := schema["required"]
	propsRaw, hasProps := schema["properties"]
	if !hasReq {
		return
	}
	reqArr, ok := reqRaw.([]any)
	if !ok || len(reqArr) == 0 {
		return
	}
	props, _ := propsRaw.(map[string]any)
	if !hasProps || props == nil {
		delete(schema, "required")
		return
	}

	cleaned := make([]any, 0, len(reqArr))
	for _, r := range reqArr {
		name, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := props[name]; exists {
			cleaned = append(cleaned, name)
		}
	}
	if len(cleaned) == 0 {
		delete(schema, "required")
	} else {
		schema["required"] = cleaned
	}
}

// geminiCoreTools is the set of Claude Code tools that are essential for basic
// operation. Gemini's function calling reliability degrades significantly with
// 20+ tools (Google recommends max 10-20), so by default we only forward these.
var geminiCoreTools = map[string]bool{
	"Agent":           true,
	"Bash":            true,
	"Read":            true,
	"Edit":            true,
	"Write":           true,
	"Glob":            true,
	"Grep":            true,
	"WebSearch":       true,
	"WebFetch":        true,
	"AskUserQuestion": true,
	"TaskCreate":      true,
	"TaskUpdate":      true,
	"TaskList":        true,
	"Skill":           true,
}

// Gemini function declarations support only a minimal subset of OpenAPI schema.
// Per docs: type, description, enum at property level; type, properties, required at object level.
// Everything else causes 400 errors.
var geminiAllowedSchemaFields = map[string]bool{
	"type":        true,
	"description": true,
	"properties":  true,
	"required":    true,
	"items":       true,
	"enum":        true,
	"nullable":    true,
	"title":       true,
	"format":      true,
	"minimum":     true,
	"maximum":     true,
	"minItems":    true,
	"maxItems":    true,
	"minLength":   true,
	"maxLength":   true,
	"pattern":     true,
}

func convertSchemaValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		if anyOfRaw, hasAnyOf := value["anyOf"]; hasAnyOf {
			if anyOf, ok := anyOfRaw.([]any); ok {
				return flattenAnyOf(anyOf)
			}
		}

		out := make(map[string]any, len(value))
		for key, item := range value {
			// Whitelist: only keep fields Gemini understands
			if !geminiAllowedSchemaFields[key] {
				continue
			}
			if key == "type" {
				if typeName, ok := item.(string); ok {
					out[key] = mapSchemaType(typeName)
					continue
				}
			}
			out[key] = convertSchemaValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, convertSchemaValue(item))
		}
		return out
	default:
		return v
	}
}

func flattenAnyOf(anyOf []any) any {
	result := make(map[string]any)
	for _, item := range anyOf {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if strings.ToLower(t) == "null" {
			result["nullable"] = true
			continue
		}
		for k, v := range m {
			if geminiAllowedSchemaFields[k] {
				result[k] = convertSchemaValue(v)
			}
		}
	}
	if ts, ok := result["type"].(string); ok {
		result["type"] = mapSchemaType(ts)
	}
	return result
}

func mapSchemaType(typeName string) string {
	switch strings.ToLower(typeName) {
	case "string":
		return "STRING"
	case "number":
		return "NUMBER"
	case "object":
		return "OBJECT"
	case "array":
		return "ARRAY"
	case "boolean":
		return "BOOLEAN"
	case "integer":
		return "INTEGER"
	default:
		return strings.ToUpper(typeName)
	}
}

func parseThinkingRequest(raw json.RawMessage) thinkingRequest {
	if len(bytes.TrimSpace(raw)) == 0 {
		return thinkingRequest{}
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return thinkingRequest{Enabled: true}
	}

	req := thinkingRequest{
		Effort: strings.TrimSpace(stringValue(payload["effort"])),
		Budget: positiveInt(payload["budget_tokens"]),
	}
	if req.Effort == "" {
		req.Effort = strings.TrimSpace(stringValue(payload["level"]))
	}

	switch strings.TrimSpace(stringValue(payload["type"])) {
	case "disabled":
		return thinkingRequest{}
	case "adaptive":
		req.Enabled = true
		req.Adaptive = true
		return req
	case "enabled":
		req.Enabled = true
		return req
	}

	if enabled, ok := payload["enabled"].(bool); ok {
		req.Enabled = enabled
		return req
	}

	req.Enabled = true
	return req
}

func buildThinkingConfig(model string, request thinkingRequest, extra map[string]any) *thinkingConfig {
	cfg := &thinkingConfig{IncludeThoughts: true}

	effort := strings.TrimSpace(request.Effort)
	if effort == "" {
		effort = strings.TrimSpace(stringValue(extra["thinking_effort"]))
	}
	if effort == "" {
		effort = strings.TrimSpace(stringValue(extra["reasoning_effort"]))
	}

	if usesThinkingLevel(model) {
		if level := mapThinkingLevel(effort); level != "" {
			cfg.ThinkingLevel = level
		} else if !request.Adaptive {
			cfg.ThinkingLevel = "high"
		}
		return cfg
	}

	budget := request.Budget
	if budget <= 0 {
		budget = positiveInt(extra["thinking_budget"])
	}
	if budget <= 0 && !request.Adaptive {
		budget = defaultThinkingBudget(effort)
	}
	if budget > 0 {
		cfg.ThinkingBudget = budget
	}
	return cfg
}

func defaultThinkingBudget(effort string) int {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low":
		return 1024
	case "high", "xhigh":
		return 8192
	default:
		return 4096
	}
}

func usesThinkingLevel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "gemini-3")
}

func mapThinkingLevel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal":
		return "minimal"
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh":
		return "high"
	default:
		return ""
	}
}

// modelSupportsThinking 判断 Gemini 模型是否支持 thinking/thinkingConfig。
// gemini-2.5-flash, gemini-2.5-pro, gemini-3+ 支持；gemini-2.0-flash 等不支持。
func modelSupportsThinking(model string) bool {
	m := strings.ToLower(model)
	// gemini-2.5+ 和 gemini-3+ 支持 thinking
	if strings.Contains(m, "2.5") || strings.Contains(m, "3.0") ||
		strings.Contains(m, "3.5") || strings.HasPrefix(m, "gemini-3") {
		return true
	}
	// gemini-2.0 及更早版本不支持
	return false
}

func mapMessageRole(role string) string {
	switch role {
	case "assistant":
		return "model"
	default:
		return "user"
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
