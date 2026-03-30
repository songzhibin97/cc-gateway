package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

func TestStreamConverterTextResponseLifecycle(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514")

	raw := processOpenAIEvents(t, converter,
		sse.Event{Type: "response.created", Data: `{"response":{"id":"resp_123","model":"gpt-5.2"}}`},
		sse.Event{Type: "response.content_part.added", Data: `{"item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`},
		sse.Event{Type: "response.output_text.delta", Data: `{"item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`},
		sse.Event{Type: "response.output_text.done", Data: `{"item_id":"msg_1","output_index":0,"content_index":0,"text":"hello"}`},
		sse.Event{Type: "response.completed", Data: `{"response":{"id":"resp_123","model":"gpt-5.2","status":"completed","usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}}`},
	)

	events := decodeAnthropicSSE(t, raw)
	assertEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var start struct {
		Message struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &start); err != nil {
		t.Fatalf("decode message_start: %v", err)
	}
	if start.Message.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("expected display model to stay as Claude model, got %q", start.Message.Model)
	}

	var delta struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(events[4].Data), &delta); err != nil {
		t.Fatalf("decode message_delta: %v", err)
	}
	if delta.Delta.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason end_turn, got %q", delta.Delta.StopReason)
	}
	if delta.Usage.InputTokens != 5 || delta.Usage.OutputTokens != 7 {
		t.Fatalf("unexpected usage in message_delta: %+v", delta.Usage)
	}
}

func TestStreamConverterFunctionCallStopsWithToolUse(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514")

	raw := processOpenAIEvents(t, converter,
		sse.Event{Type: "response.created", Data: `{"response":{"id":"resp_tool","model":"gpt-5.2"}}`},
		sse.Event{Type: "response.output_item.added", Data: `{"output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"toolu_1","name":"Read","arguments":""}}`},
		sse.Event{Type: "response.function_call_arguments.delta", Data: `{"item_id":"fc_1","output_index":0,"delta":"{\"path\":\"a.txt\"}"}`},
		sse.Event{Type: "response.function_call_arguments.done", Data: `{"item_id":"fc_1","output_index":0,"arguments":"{\"path\":\"a.txt\"}"}`},
		sse.Event{Type: "response.completed", Data: `{"response":{"id":"resp_tool","model":"gpt-5.2","status":"completed","usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11}}}`},
	)

	events := decodeAnthropicSSE(t, raw)
	assertEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var start struct {
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal([]byte(events[1].Data), &start); err != nil {
		t.Fatalf("decode tool_use start: %v", err)
	}
	if start.ContentBlock.Type != "tool_use" || start.ContentBlock.ID != "toolu_1" || start.ContentBlock.Name != "Read" {
		t.Fatalf("unexpected tool_use block: %+v", start.ContentBlock)
	}

	var delta struct {
		Delta struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
			StopReason  string `json:"stop_reason"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &delta); err != nil {
		t.Fatalf("decode input_json_delta: %v", err)
	}
	if delta.Delta.Type != "input_json_delta" || delta.Delta.PartialJSON != "{\"path\":\"a.txt\"}" {
		t.Fatalf("unexpected function delta: %+v", delta.Delta)
	}

	var stop struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[4].Data), &stop); err != nil {
		t.Fatalf("decode tool_use stop: %v", err)
	}
	if stop.Delta.StopReason != "tool_use" {
		t.Fatalf("expected stop_reason tool_use, got %q", stop.Delta.StopReason)
	}
}

func TestStreamConverterReasoningSummaryMapsToThinking(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514")

	raw := processOpenAIEvents(t, converter,
		sse.Event{Type: "response.created", Data: `{"response":{"id":"resp_reason","model":"gpt-5.2"}}`},
		sse.Event{Type: "response.reasoning_summary_part.added", Data: `{"item_id":"rs_1","output_index":0,"summary_index":0}`},
		sse.Event{Type: "response.reasoning_summary_text.delta", Data: `{"item_id":"rs_1","output_index":0,"summary_index":0,"delta":"plan"}`},
		sse.Event{Type: "response.reasoning_summary_text.done", Data: `{"item_id":"rs_1","output_index":0,"summary_index":0,"text":"plan"}`},
		sse.Event{Type: "response.completed", Data: `{"response":{"id":"resp_reason","model":"gpt-5.2","status":"completed","usage":{"input_tokens":4,"output_tokens":6,"total_tokens":10}}}`},
	)

	events := decodeAnthropicSSE(t, raw)
	assertEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var start struct {
		ContentBlock struct {
			Type string `json:"type"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal([]byte(events[1].Data), &start); err != nil {
		t.Fatalf("decode thinking start: %v", err)
	}
	if start.ContentBlock.Type != "thinking" {
		t.Fatalf("expected thinking content block, got %+v", start.ContentBlock)
	}

	var delta struct {
		Delta struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &delta); err != nil {
		t.Fatalf("decode thinking delta: %v", err)
	}
	if delta.Delta.Type != "thinking_delta" || delta.Delta.Thinking != "plan" {
		t.Fatalf("unexpected thinking delta: %+v", delta.Delta)
	}
}

func TestStreamConverterFinalizeEmitsFillerForEmptyResponse(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514")

	raw := processOpenAIEvents(t, converter,
		sse.Event{Type: "response.created", Data: `{"response":{"id":"resp_empty","model":"gpt-5.2"}}`},
	)
	raw = append(raw, converter.Finalize()...)

	events := decodeAnthropicSSE(t, raw)
	assertEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})
}

func processOpenAIEvents(t *testing.T, converter *StreamConverter, input ...sse.Event) [][]byte {
	t.Helper()

	var raw [][]byte
	for _, event := range input {
		converted, _, err := converter.ProcessEvent(event)
		if err != nil {
			t.Fatalf("ProcessEvent(%s) returned error: %v", event.Type, err)
		}
		raw = append(raw, converted...)
	}
	return raw
}

func decodeAnthropicSSE(t *testing.T, raw [][]byte) []sse.Event {
	t.Helper()

	var buf bytes.Buffer
	for _, event := range raw {
		buf.Write(event)
	}

	reader := sse.NewReader(&buf)
	var events []sse.Event
	for {
		event, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read Anthropic SSE: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func assertEventTypes(t *testing.T, events []sse.Event, want []string) {
	t.Helper()

	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d", len(want), len(events))
	}
	for i, event := range events {
		if event.Type != want[i] {
			t.Fatalf("event %d: expected %q, got %q", i, want[i], event.Type)
		}
	}
}
