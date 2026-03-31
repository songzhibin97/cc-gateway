package gemini

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

type rawJSONNumber string

func (r rawJSONNumber) MarshalJSON() ([]byte, error) {
	return []byte(r), nil
}

func TestStreamConverterDoesNotSanitizeFunctionArgs(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", map[string]*ToolSchema{
		"Read": {
			Allowed:  map[string]bool{"path": true},
			Required: []string{"path", "mode"},
		},
	})

	resp := &generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							FunctionCall: &functionCall{
								Name: "Read",
								Args: map[string]any{
									"path":  "a.txt",
									"extra": "keep-me",
								},
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	raw, _, err := converter.ProcessResponse(resp)
	if err != nil {
		t.Fatalf("ProcessResponse returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, raw)
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var delta struct {
		Delta struct {
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &delta); err != nil {
		t.Fatalf("decode input_json_delta: %v", err)
	}
	if delta.Delta.PartialJSON != `{"extra":"keep-me","path":"a.txt"}` &&
		delta.Delta.PartialJSON != `{"path":"a.txt","extra":"keep-me"}` {
		t.Fatalf("expected original args to be preserved, got %q", delta.Delta.PartialJSON)
	}
}

func TestStreamConverterTextSnapshotsEmitStableDeltas(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw1, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{Text: "hel"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse first snapshot returned error: %v", err)
	}

	raw2, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{Text: "hello"},
					},
				},
				FinishReason: "STOP",
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse second snapshot returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, append(raw1, raw2...))
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var first struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &first); err != nil {
		t.Fatalf("decode first text delta: %v", err)
	}
	if first.Delta.Type != "text_delta" || first.Delta.Text != "hel" {
		t.Fatalf("unexpected first text delta: %+v", first.Delta)
	}

	var second struct {
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[3].Data), &second); err != nil {
		t.Fatalf("decode second text delta: %v", err)
	}
	if second.Delta.Type != "text_delta" || second.Delta.Text != "lo" {
		t.Fatalf("unexpected second text delta: %+v", second.Delta)
	}
}

func TestStreamConverterThinkingSnapshotsEmitIncrementalDeltas(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw1, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							Text:    "hel",
							Thought: true,
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse first snapshot returned error: %v", err)
	}

	raw2, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							Text:             "hello",
							Thought:          true,
							ThoughtSignature: "sig_123",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse second snapshot returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, append(raw1, raw2...))
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var first struct {
		Delta struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &first); err != nil {
		t.Fatalf("decode first thinking delta: %v", err)
	}
	if first.Delta.Type != "thinking_delta" || first.Delta.Thinking != "hel" {
		t.Fatalf("unexpected first thinking delta: %+v", first.Delta)
	}

	var second struct {
		Delta struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[3].Data), &second); err != nil {
		t.Fatalf("decode second thinking delta: %v", err)
	}
	if second.Delta.Type != "thinking_delta" || second.Delta.Thinking != "lo" {
		t.Fatalf("unexpected second thinking delta: %+v", second.Delta)
	}

	var signature struct {
		Delta struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[4].Data), &signature); err != nil {
		t.Fatalf("decode thinking signature delta: %v", err)
	}
	if signature.Delta.Type != "signature_delta" || signature.Delta.Signature != "sig_123" {
		t.Fatalf("unexpected thinking signature delta: %+v", signature.Delta)
	}
}

func TestStreamConverterThinkingSignatureDoesNotRepeatAcrossSnapshots(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw1, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							Text:             "plan",
							Thought:          true,
							ThoughtSignature: "sig_123",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse first snapshot returned error: %v", err)
	}

	raw2, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							Text:             "plan more",
							Thought:          true,
							ThoughtSignature: "sig_123",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse second snapshot returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, append(raw1, raw2...))
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var signatureDeltas int
	for _, event := range events {
		if event.Type != "content_block_delta" {
			continue
		}
		var payload struct {
			Delta struct {
				Type string `json:"type"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			t.Fatalf("decode delta event: %v", err)
		}
		if payload.Delta.Type == "signature_delta" {
			signatureDeltas++
		}
	}
	if signatureDeltas != 1 {
		t.Fatalf("expected one signature delta across snapshots, got %d", signatureDeltas)
	}
}

func TestStreamConverterFunctionCallSnapshotsEmitStableDeltas(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw1, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							FunctionCall: &functionCall{
								Name: "Read",
								Args: rawJSONNumber(`1`),
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse first snapshot returned error: %v", err)
	}

	raw2, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							FunctionCall: &functionCall{
								Name: "Read",
								Args: rawJSONNumber(`10`),
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse second snapshot returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, append(raw1, raw2...))
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var first struct {
		Delta struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &first); err != nil {
		t.Fatalf("decode first function delta: %v", err)
	}
	if first.Delta.Type != "input_json_delta" || first.Delta.PartialJSON != `1` {
		t.Fatalf("unexpected first function delta: %+v", first.Delta)
	}

	var second struct {
		Delta struct {
			Type        string `json:"type"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[3].Data), &second); err != nil {
		t.Fatalf("decode second function delta: %v", err)
	}
	if second.Delta.Type != "input_json_delta" || second.Delta.PartialJSON != "0" {
		t.Fatalf("unexpected second function delta: %+v", second.Delta)
	}
}

func TestStreamConverterThoughtSignatureEmitsSignatureDelta(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	resp := &generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							Text:             "plan",
							Thought:          true,
							ThoughtSignature: "sig_123",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	raw, _, err := converter.ProcessResponse(resp)
	if err != nil {
		t.Fatalf("ProcessResponse returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, raw)
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var signature struct {
		Delta struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[3].Data), &signature); err != nil {
		t.Fatalf("decode signature delta: %v", err)
	}
	if signature.Delta.Type != "signature_delta" || signature.Delta.Signature != "sig_123" {
		t.Fatalf("unexpected signature delta: %+v", signature.Delta)
	}
}

func TestStreamConverterEmptyThoughtChunkEmitsSignatureDelta(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							Thought:          true,
							ThoughtSignature: "sig_empty",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, raw)
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var delta struct {
		Delta struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &delta); err != nil {
		t.Fatalf("decode empty-thought signature delta: %v", err)
	}
	if delta.Delta.Type != "signature_delta" || delta.Delta.Signature != "sig_empty" {
		t.Fatalf("unexpected empty-thought signature delta: %+v", delta.Delta)
	}
}

func TestStreamConverterFunctionCallSignatureCreatesThinkingCarrier(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	resp := &generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{
							ThoughtSignature: "sig_tool",
							FunctionCall: &functionCall{
								Name: "Read",
								Args: map[string]any{"path": "a.txt"},
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	raw, _, err := converter.ProcessResponse(resp)
	if err != nil {
		t.Fatalf("ProcessResponse returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, raw)
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var carrier struct {
		Delta struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[2].Data), &carrier); err != nil {
		t.Fatalf("decode carrier signature delta: %v", err)
	}
	if carrier.Delta.Type != "signature_delta" || carrier.Delta.Signature != "sig_tool" {
		t.Fatalf("unexpected carrier delta: %+v", carrier.Delta)
	}

	var stop struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[7].Data), &stop); err != nil {
		t.Fatalf("decode stop reason: %v", err)
	}
	if stop.Delta.StopReason != "tool_use" {
		t.Fatalf("expected stop_reason tool_use, got %q", stop.Delta.StopReason)
	}
}

func TestStreamConverterUnknownFinishReasonFallsBackToEndTurn(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw, _, err := converter.ProcessResponse(&generateContentResponse{
		Candidates: []candidate{
			{
				Content: content{
					Parts: []part{
						{Text: "hello"},
					},
				},
				FinishReason: "RECITATION",
			},
		},
	})
	if err != nil {
		t.Fatalf("ProcessResponse returned error: %v", err)
	}

	events := decodeGeminiAnthropicSSE(t, raw)
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var delta struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[4].Data), &delta); err != nil {
		t.Fatalf("decode fallback stop reason: %v", err)
	}
	if delta.Delta.StopReason != "end_turn" {
		t.Fatalf("expected unknown finish reason to map to end_turn, got %q", delta.Delta.StopReason)
	}
}

func TestStreamConverterFinalizeAfterPartialStartProducesCompleteMessage(t *testing.T) {
	converter := NewStreamConverter("claude-sonnet-4-20250514", nil)

	raw, _, err := converter.ProcessResponse(&generateContentResponse{})
	if err != nil {
		t.Fatalf("ProcessResponse returned error: %v", err)
	}
	raw = append(raw, converter.Finalize()...)

	events := decodeGeminiAnthropicSSE(t, raw)
	assertGeminiEventTypes(t, events, []string{
		"message_start",
		"content_block_start",
		"content_block_stop",
		"message_delta",
		"message_stop",
	})

	var delta struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(events[3].Data), &delta); err != nil {
		t.Fatalf("decode message_delta: %v", err)
	}
	if delta.Delta.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason end_turn, got %q", delta.Delta.StopReason)
	}
	if delta.Usage.InputTokens != 0 || delta.Usage.OutputTokens != 0 {
		t.Fatalf("unexpected usage in partial finalize: %+v", delta.Usage)
	}
}

func decodeGeminiAnthropicSSE(t *testing.T, raw [][]byte) []sse.Event {
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

func assertGeminiEventTypes(t *testing.T, events []sse.Event, want []string) {
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
