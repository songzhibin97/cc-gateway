package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

type contentBlockState struct {
	BlockType string
}

type responseEnvelope struct {
	Type string `json:"type"`
}

type responseCreatedEvent struct {
	Response struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"response"`
}

type responseContentPartAddedEvent struct {
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Part         struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"part"`
}

type responseOutputTextDeltaEvent struct {
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

type responseOutputTextDoneEvent struct {
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

type responseOutputItemAddedEvent struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
}

type responseFunctionCallArgumentsDeltaEvent struct {
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

type responseFunctionCallArgumentsDoneEvent struct {
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	Arguments   string `json:"arguments"`
}

type responseCompletedEvent struct {
	Response struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	} `json:"response"`
}

// responseReasoningSummaryPartAddedEvent represents the
// response.reasoning_summary_part.added SSE event from the OpenAI Responses API.
type responseReasoningSummaryPartAddedEvent struct {
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	SummaryIndex int    `json:"summary_index"`
}

// responseReasoningSummaryTextDeltaEvent represents the
// response.reasoning_summary_text.delta SSE event.
type responseReasoningSummaryTextDeltaEvent struct {
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	SummaryIndex int    `json:"summary_index"`
	Delta        string `json:"delta"`
}

// responseReasoningSummaryTextDoneEvent represents the
// response.reasoning_summary_text.done SSE event.
type responseReasoningSummaryTextDoneEvent struct {
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	SummaryIndex int    `json:"summary_index"`
	Text         string `json:"text"`
}

type StreamConverter struct {
	model                 string
	messageID             string
	messageStarted        bool
	stopReasonSent        bool
	sawFunctionCall       bool
	contentBlockCompleted bool // at least one content_block finished (start→stop)
	nextBlockIndex        int
	blocksByIndex         map[int]*contentBlockState
	textBlocks            map[string]int
	functionBlocks        map[string]int
	functionItemIDs       map[int]string
	// reasoning (thinking) block tracking
	thinkingBlocks map[string]int // summaryKey -> block index
	usage          *domain.Usage
}

func NewStreamConverter(model string) *StreamConverter {
	return &StreamConverter{
		model:           model,
		blocksByIndex:   make(map[int]*contentBlockState),
		textBlocks:      make(map[string]int),
		functionBlocks:  make(map[string]int),
		functionItemIDs: make(map[int]string),
		thinkingBlocks:  make(map[string]int),
		usage:           &domain.Usage{},
	}
}

func (c *StreamConverter) ProcessEvent(event sse.Event) ([][]byte, *domain.Usage, error) {
	var payloads [][]byte

	switch event.Type {
	case "response.created":
		var evt responseCreatedEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.created: %w", err)
		}
		if evt.Response.ID != "" {
			c.messageID = evt.Response.ID
		}
		// Do NOT overwrite c.model with upstream model name (e.g. "gpt-5.4-codex").
		// CC uses the model name for tokenizer selection and context window lookup.
		raw, err := c.ensureMessageStart()
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

	case "response.content_part.added":
		var evt responseContentPartAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.content_part.added: %w", err)
		}
		if evt.Part.Type != "output_text" {
			return nil, c.usage, nil
		}
		raw, err := c.ensureMessageStart()
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

		index := c.nextIndex()
		c.blocksByIndex[index] = &contentBlockState{BlockType: "text"}
		c.textBlocks[textBlockKey(evt.ItemID, evt.ContentIndex)] = index

		raw, err = formatAnthropicEvent("content_block_start", map[string]any{
			"index": index,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

	case "response.output_text.delta":
		var evt responseOutputTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.output_text.delta: %w", err)
		}
		index, ok := c.textBlocks[textBlockKey(evt.ItemID, evt.ContentIndex)]
		if !ok {
			return nil, c.usage, fmt.Errorf("missing text block for item_id=%q content_index=%d", evt.ItemID, evt.ContentIndex)
		}
		raw, err := formatAnthropicEvent("content_block_delta", map[string]any{
			"index": index,
			"delta": map[string]any{
				"type": "text_delta",
				"text": evt.Delta,
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

	case "response.output_text.done":
		var evt responseOutputTextDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.output_text.done: %w", err)
		}
		index, ok := c.textBlocks[textBlockKey(evt.ItemID, evt.ContentIndex)]
		if !ok {
			return nil, c.usage, nil
		}
		raw, err := c.closeBlock(index)
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

	case "response.output_item.added":
		var evt responseOutputItemAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.output_item.added: %w", err)
		}
		raw, err := c.ensureMessageStart()
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}
		if evt.Item.Type != "function_call" {
			return payloads, c.usage, nil
		}

		index := c.nextIndex()
		c.blocksByIndex[index] = &contentBlockState{BlockType: "tool_use"}
		c.functionBlocks[evt.Item.ID] = index
		c.functionItemIDs[evt.OutputIndex] = evt.Item.ID
		c.sawFunctionCall = true

		raw, err = formatAnthropicEvent("content_block_start", map[string]any{
			"index": index,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    evt.Item.CallID,
				"name":  evt.Item.Name,
				"input": map[string]any{},
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

	case "response.function_call_arguments.delta":
		var evt responseFunctionCallArgumentsDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.function_call_arguments.delta: %w", err)
		}
		raw, err := c.ensureMessageStart()
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}
		itemID := evt.ItemID
		if itemID == "" {
			itemID = c.functionItemIDs[evt.OutputIndex]
		}
		index, ok := c.functionBlocks[itemID]
		if !ok {
			return nil, c.usage, fmt.Errorf("missing function block for item_id=%q output_index=%d", itemID, evt.OutputIndex)
		}
		raw, err = formatAnthropicEvent("content_block_delta", map[string]any{
			"index": index,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": evt.Delta,
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

	case "response.function_call_arguments.done":
		var evt responseFunctionCallArgumentsDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.function_call_arguments.done: %w", err)
		}
		itemID := evt.ItemID
		if itemID == "" {
			itemID = c.functionItemIDs[evt.OutputIndex]
		}
		index, ok := c.functionBlocks[itemID]
		if !ok {
			return nil, c.usage, nil
		}
		raw, err := c.closeBlock(index)
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

	// ── Reasoning (thinking) events ──────────────────────────────────

	case "response.reasoning_summary_part.added":
		var evt responseReasoningSummaryPartAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.reasoning_summary_part.added: %w", err)
		}
		raw, err := c.ensureMessageStart()
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

		index := c.nextIndex()
		key := thinkingBlockKey(evt.ItemID, evt.SummaryIndex)
		c.blocksByIndex[index] = &contentBlockState{BlockType: "thinking"}
		c.thinkingBlocks[key] = index

		raw, err = formatAnthropicEvent("content_block_start", map[string]any{
			"index": index,
			"content_block": map[string]any{
				"type":     "thinking",
				"thinking": "",
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

	case "response.reasoning_summary_text.delta":
		var evt responseReasoningSummaryTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.reasoning_summary_text.delta: %w", err)
		}
		key := thinkingBlockKey(evt.ItemID, evt.SummaryIndex)
		index, ok := c.thinkingBlocks[key]
		if !ok {
			return nil, c.usage, nil
		}
		raw, err := formatAnthropicEvent("content_block_delta", map[string]any{
			"index": index,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": evt.Delta,
			},
		})
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, raw)

	case "response.reasoning_summary_text.done":
		// Text finalized — close the thinking block.
		var evt responseReasoningSummaryTextDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.reasoning_summary_text.done: %w", err)
		}
		key := thinkingBlockKey(evt.ItemID, evt.SummaryIndex)
		index, ok := c.thinkingBlocks[key]
		if !ok {
			return nil, c.usage, nil
		}
		raw, err := c.closeBlock(index)
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

	case "response.reasoning_summary_part.done":
		// Part done — ensure block is closed if not already.
		var evt responseReasoningSummaryPartAddedEvent // same shape
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, nil
		}
		key := thinkingBlockKey(evt.ItemID, evt.SummaryIndex)
		index, ok := c.thinkingBlocks[key]
		if !ok {
			return nil, c.usage, nil
		}
		raw, err := c.closeBlock(index)
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

	// ── Completion ───────────────────────────────────────────────────

	case "response.completed":
		var evt responseCompletedEvent
		if err := json.Unmarshal([]byte(event.Data), &evt); err != nil {
			return nil, c.usage, fmt.Errorf("decode response.completed: %w", err)
		}
		if evt.Response.ID != "" {
			c.messageID = evt.Response.ID
		}
		c.usage.InputTokens = evt.Response.Usage.InputTokens
		c.usage.OutputTokens = evt.Response.Usage.OutputTokens

		raw, err := c.ensureMessageStart()
		if err != nil {
			return nil, c.usage, err
		}
		if len(raw) != 0 {
			payloads = append(payloads, raw)
		}

		closed, err := c.closeOpenBlocks()
		if err != nil {
			return nil, c.usage, err
		}
		payloads = append(payloads, closed...)

		if !c.stopReasonSent {
			// CC requires at least one completed content_block before message_stop.
			filler, err := c.ensureContentBlock()
			if err != nil {
				return nil, c.usage, err
			}
			payloads = append(payloads, filler...)

			stopReason := "end_turn"
			if c.sawFunctionCall {
				stopReason = "tool_use"
			}

			raw, err = formatAnthropicEvent("message_delta", map[string]any{
				"delta": map[string]any{
					"stop_reason":   stopReason,
					"stop_sequence": nil,
				},
				"usage": map[string]any{
					"input_tokens":                evt.Response.Usage.InputTokens,
					"output_tokens":               evt.Response.Usage.OutputTokens,
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
			c.stopReasonSent = true
		}

	case "response.in_progress":
		return nil, c.usage, nil

	default:
		var envelope responseEnvelope
		if err := json.Unmarshal([]byte(event.Data), &envelope); err == nil && envelope.Type == event.Type {
			return nil, c.usage, nil
		}
		return nil, c.usage, nil
	}

	return payloads, c.usage, nil
}

func (c *StreamConverter) Finalize() [][]byte {
	if !c.messageStarted {
		return nil
	}

	var payloads [][]byte

	closed, err := c.closeOpenBlocks()
	if err == nil {
		payloads = append(payloads, closed...)
	}

	if c.stopReasonSent {
		return payloads
	}

	filler, err := c.ensureContentBlock()
	if err == nil {
		payloads = append(payloads, filler...)
	}

	stopReason := "end_turn"
	if c.sawFunctionCall {
		stopReason = "tool_use"
	}

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
	if err == nil {
		payloads = append(payloads, raw)
	}

	raw, err = formatAnthropicEvent("message_stop", map[string]any{})
	if err == nil {
		payloads = append(payloads, raw)
		c.stopReasonSent = true
	}

	return payloads
}

func (c *StreamConverter) ensureMessageStart() ([]byte, error) {
	if c.messageStarted {
		return nil, nil
	}
	c.messageStarted = true
	return formatAnthropicEvent("message_start", map[string]any{
		"message": map[string]any{
			"id":            c.messageID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         c.model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":                0,
				"output_tokens":               0,
				"cache_creation_input_tokens":  0,
				"cache_read_input_tokens":      0,
			},
		},
	})
}

func (c *StreamConverter) nextIndex() int {
	index := c.nextBlockIndex
	c.nextBlockIndex++
	return index
}

func (c *StreamConverter) closeOpenBlocks() ([][]byte, error) {
	indices := make([]int, 0, len(c.blocksByIndex))
	for index := range c.blocksByIndex {
		indices = append(indices, index)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))

	out := make([][]byte, 0, len(indices))
	for _, index := range indices {
		raw, err := formatAnthropicEvent("content_block_stop", map[string]any{"index": index})
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
		delete(c.blocksByIndex, index)
		c.contentBlockCompleted = true
	}
	return out, nil
}

func (c *StreamConverter) closeBlock(index int) ([]byte, error) {
	if _, ok := c.blocksByIndex[index]; !ok {
		return nil, nil
	}
	delete(c.blocksByIndex, index)
	c.contentBlockCompleted = true
	return formatAnthropicEvent("content_block_stop", map[string]any{"index": index})
}

// ensureContentBlock emits an empty text block (start+stop) if no content block
// has been completed yet. CC requires at least one completed content_block before
// message_stop, otherwise it throws and triggers a non-streaming fallback.
func (c *StreamConverter) ensureContentBlock() ([][]byte, error) {
	if c.contentBlockCompleted {
		return nil, nil
	}
	index := c.nextIndex()
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

func textBlockKey(itemID string, contentIndex int) string {
	return fmt.Sprintf("%s:%d", itemID, contentIndex)
}

func thinkingBlockKey(itemID string, summaryIndex int) string {
	return fmt.Sprintf("think:%s:%d", itemID, summaryIndex)
}

func formatAnthropicEvent(eventType string, payload any) ([]byte, error) {
	// Anthropic SSE requires "type" field in every event data JSON.
	// Inject it if payload is a map.
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
