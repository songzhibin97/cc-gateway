package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type generateContentResponse struct {
	Candidates    []candidate `json:"candidates"`
	UsageMetadata *usage      `json:"usageMetadata,omitempty"`
}

type candidate struct {
	Content      content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index"`
}

type usage struct {
	PromptTokenCount     int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount,omitempty"`
}

type toolStreamState struct {
	ID           string
	Name         string
	Index        int
	PreviousJSON string
	Open         bool
}

type StreamConverter struct {
	model                 string
	toolSchemas           map[string]*ToolSchema
	messageID             string
	started               bool
	finished              bool
	contentBlockCompleted bool
	nextIndex             int
	textIndex             int
	textBlockOpen         bool
	previousText          string
	toolStates            map[string]*toolStreamState
	sawFunctionCall       bool
	usage                 *domain.Usage
}

func NewStreamConverter(model string, toolSchemas map[string]*ToolSchema) *StreamConverter {
	return &StreamConverter{
		model:       model,
		toolSchemas: toolSchemas,
		messageID:   fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		textIndex:   -1,
		toolStates:  make(map[string]*toolStreamState),
		usage:       &domain.Usage{},
	}
}

func (c *StreamConverter) ProcessResponse(resp *generateContentResponse) ([][]byte, *domain.Usage, error) {
	var payloads [][]byte

	if resp != nil && resp.UsageMetadata != nil {
		c.usage.InputTokens = resp.UsageMetadata.PromptTokenCount
		c.usage.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
	}

	if !c.started {
		c.started = true
		raw, err := formatAnthropicEvent("message_start", map[string]any{
			"message": map[string]any{
				"id":            c.messageID,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         c.model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":                c.usage.InputTokens,
					"output_tokens":               c.usage.OutputTokens,
					"cache_creation_input_tokens":  0,
					"cache_read_input_tokens":      0,
				},
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return payloads, c.usage, nil
	}

	candidate := resp.Candidates[0]
	for i, p := range candidate.Content.Parts {
		switch {
		case p.Thought && p.Text != "":
			thinkIndex := c.nextIndex
			c.nextIndex++
			raw, err := formatAnthropicEvent("content_block_start", map[string]any{
				"index": thinkIndex,
				"content_block": map[string]any{
					"type":     "thinking",
					"thinking": "",
				},
			})
			if err != nil {
				return nil, c.usage, err
			}
			payloads = append(payloads, raw)

			raw, err = formatAnthropicEvent("content_block_delta", map[string]any{
				"index": thinkIndex,
				"delta": map[string]any{
					"type":     "thinking_delta",
					"thinking": p.Text,
				},
			})
			if err != nil {
				return nil, c.usage, err
			}
			payloads = append(payloads, raw)

			raw, err = formatAnthropicEvent("content_block_stop", map[string]any{
				"index": thinkIndex,
			})
			if err != nil {
				return nil, c.usage, err
			}
			c.contentBlockCompleted = true
			payloads = append(payloads, raw)

		case p.Text != "":
			if !c.textBlockOpen {
				c.textIndex = c.nextIndex
				c.nextIndex++
				c.textBlockOpen = true
				raw, err := formatAnthropicEvent("content_block_start", map[string]any{
					"index": c.textIndex,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				})
				if err != nil {
					return nil, c.usage, err
				}
				payloads = append(payloads, raw)
			}

			delta := computeStreamDelta(p.Text, &c.previousText)
			if delta == "" {
				continue
			}
			raw, err := formatAnthropicEvent("content_block_delta", map[string]any{
				"index": c.textIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": delta,
				},
			})
			if err != nil {
				return nil, c.usage, err
			}
			payloads = append(payloads, raw)

		case p.FunctionCall != nil:
			c.sawFunctionCall = true
			key := fmt.Sprintf("part_%d", i)
			state := c.toolStates[key]
			if state == nil {
				state = &toolStreamState{
					ID:    fmt.Sprintf("toolu_%d", time.Now().UnixNano()),
					Name:  p.FunctionCall.Name,
					Index: c.nextIndex,
				}
				c.toolStates[key] = state
				c.nextIndex++
			}
			if state.Name == "" {
				state.Name = p.FunctionCall.Name
			}
			if !state.Open {
				state.Open = true
				raw, err := formatAnthropicEvent("content_block_start", map[string]any{
					"index": state.Index,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    state.ID,
						"name":  state.Name,
						"input": map[string]any{},
					},
				})
				if err != nil {
					return nil, c.usage, err
				}
				payloads = append(payloads, raw)
			}

			if c.toolSchemas != nil && p.FunctionCall.Args != nil {
				if schema, ok := c.toolSchemas[p.FunctionCall.Name]; ok {
					p.FunctionCall.Args = sanitizeArgs(p.FunctionCall.Args, schema)
				}
			}

			argsJSON, err := marshalCompactJSON(p.FunctionCall.Args)
			if err != nil {
				return nil, c.usage, fmt.Errorf("marshal function args: %w", err)
			}
			delta := computeStreamDelta(argsJSON, &state.PreviousJSON)
			if delta != "" {
				raw, err := formatAnthropicEvent("content_block_delta", map[string]any{
					"index": state.Index,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": delta,
					},
				})
				if err != nil {
					return nil, c.usage, err
				}
				payloads = append(payloads, raw)
			}
		}
	}

	if candidate.FinishReason != "" {
		closed, err := c.closeOpenBlocks()
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, closed...)

		filler, err := c.ensureContentBlock()
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, filler...)

		stopReason := mapFinishReason(candidate.FinishReason)
		if candidate.FinishReason == "STOP" && c.sawFunctionCall {
			stopReason = "tool_use"
		}
		// MALFORMED_FUNCTION_CALL is mapped to end_turn above and should not be
		// rewritten to tool_use because the tool call payload was invalid.

		raw, err := formatAnthropicEvent("message_delta", map[string]any{
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"input_tokens":                c.usage.InputTokens,
				"output_tokens":               c.usage.OutputTokens,
				"cache_creation_input_tokens":  0,
				"cache_read_input_tokens":      0,
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

		raw, err = formatAnthropicEvent("message_stop", map[string]any{})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)
		c.finished = true
	}

	return payloads, c.usage, nil
}

func sanitizeArgs(args any, schema *ToolSchema) any {
	m, ok := args.(map[string]any)
	if !ok {
		m = make(map[string]any)
	}

	// 补全缺失的 required 参数：用已有参数中最长的 string 值作为 fallback，
	// 比空字符串更有意义（例如 Agent 的 description 可以填充缺失的 prompt）。
	for _, req := range schema.Required {
		if v, exists := m[req]; !exists || v == nil || v == "" {
			m[req] = longestStringValue(m, req)
		}
	}

	// 过滤多余参数
	filtered := make(map[string]any, len(schema.Allowed))
	for k, v := range m {
		if schema.Allowed[k] {
			filtered[k] = v
		}
	}
	return filtered
}

// longestStringValue 从 args 中找最长的 string 值（排除 excludeKey 自身）作为 fallback。
func longestStringValue(m map[string]any, excludeKey string) string {
	var best string
	for k, v := range m {
		if k == excludeKey {
			continue
		}
		if s, ok := v.(string); ok && len(s) > len(best) {
			best = s
		}
	}
	if best == "" {
		best = "execute"
	}
	return best
}

func (c *StreamConverter) Finalize() [][]byte {
	if !c.started {
		return nil
	}

	var payloads [][]byte

	closed, err := c.closeOpenBlocks()
	if err == nil {
		payloads = append(payloads, closed...)
	}

	if c.finished {
		return payloads
	}

	filler, err := c.ensureContentBlock()
	if err == nil {
		payloads = append(payloads, filler...)
	}

	raw, err := formatAnthropicEvent("message_delta", map[string]any{
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":                c.usage.InputTokens,
			"output_tokens":               c.usage.OutputTokens,
			"cache_creation_input_tokens":  0,
			"cache_read_input_tokens":      0,
		},
	})
	if err == nil {
		payloads = append(payloads, raw)
	}

	raw, err = formatAnthropicEvent("message_stop", map[string]any{})
	if err == nil {
		payloads = append(payloads, raw)
		c.finished = true
	}

	return payloads
}

func (c *StreamConverter) closeOpenBlocks() ([][]byte, error) {
	var payloads [][]byte

	if c.textBlockOpen {
		raw, err := formatAnthropicEvent("content_block_stop", map[string]any{
			"index": c.textIndex,
		})
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, raw)
		c.textBlockOpen = false
		c.contentBlockCompleted = true
	}

	keys := make([]string, 0, len(c.toolStates))
	for key, state := range c.toolStates {
		if state.Open {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return c.toolStates[keys[i]].Index < c.toolStates[keys[j]].Index
	})

	for _, key := range keys {
		state := c.toolStates[key]
		raw, err := formatAnthropicEvent("content_block_stop", map[string]any{
			"index": state.Index,
		})
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, raw)
		state.Open = false
		c.contentBlockCompleted = true
	}

	return payloads, nil
}

// ensureContentBlock emits an empty text block (start+stop) if no content block
// has been completed yet. CC requires at least one completed content_block.
func (c *StreamConverter) ensureContentBlock() ([][]byte, error) {
	if c.contentBlockCompleted {
		return nil, nil
	}
	index := c.nextIndex
	c.nextIndex++
	start, err := formatAnthropicEvent("content_block_start", map[string]any{
		"index":         index,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	if err != nil {
		return nil, err
	}
	stop, err := formatAnthropicEvent("content_block_stop", map[string]any{"index": index})
	if err != nil {
		return nil, err
	}
	c.contentBlockCompleted = true
	return [][]byte{start, stop}, nil
}

func computeStreamDelta(incoming string, previous *string) string {
	if incoming == "" {
		return ""
	}
	if *previous == "" {
		*previous = incoming
		return incoming
	}
	if strings.HasPrefix(incoming, *previous) {
		delta := incoming[len(*previous):]
		*previous = incoming
		return delta
	}
	*previous = incoming
	return incoming
}

func mapFinishReason(reason string) string {
	switch reason {
	case "STOP", "SAFETY":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "MALFORMED_FUNCTION_CALL":
		return "end_turn"
	default:
		return strings.ToLower(reason)
	}
}

func marshalCompactJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func formatAnthropicEvent(eventType string, payload any) ([]byte, error) {
	// Anthropic SSE requires "type" field in every event data JSON.
	if m, ok := payload.(map[string]any); ok {
		m["type"] = eventType
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", eventType, err)
	}

	var buf bytes.Buffer
	if _, err := fmt.Fprintf(&buf, "event: %s\n", eventType); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(&buf, "data: %s\n\n", data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
