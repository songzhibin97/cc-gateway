package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

type capturedRequest struct {
	Method   string
	Path     string
	Headers  http.Header
	Body     []byte
}

func TestAdapterStreamPassesAnthropicRequestThrough(t *testing.T) {
	var (
		mu       sync.Mutex
		captured capturedRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		mu.Lock()
		captured = capturedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    body,
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":7,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3}}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":19}}\n\n")
	}))
	defer server.Close()

	req := &domain.CanonicalRequest{
		Model:    "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []domain.Message{
			{
				Role:    "user",
				Content: json.RawMessage(`["hello"]`),
			},
			{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"text","text":"answer"},
					{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"a.txt"}},
					{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"done"}]},
					{"type":"thinking","thinking":"step-by-step"}
				]`),
			},
		},
		System:        json.RawMessage(`["system prompt"]`),
		Thinking:      json.RawMessage(`{"type":"enabled","budget_tokens":1024}`),
		ToolChoice:    json.RawMessage(`{"type":"auto"}`),
		StopSequences: []string{"stop-here"},
	}

	expectedBody, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	account := &domain.Account{
		BaseURL:   server.URL,
		APIKey:    "sk-ant-test",
		UserAgent: "",
	}

	writer := sse.NewWriter(httptest.NewRecorder())

	ctx := ContextWithHeaders(
		context.Background(),
		"2024-10-01",
		"beta-x",
		"claude-cli/1.0.25 (external, cli)",
	)

	usage, err := New().Stream(ctx, account, req, writer)
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	if usage.InputTokens != 11 {
		t.Fatalf("expected input tokens 11, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 19 {
		t.Fatalf("expected output tokens 19, got %d", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 2 {
		t.Fatalf("expected cache read tokens 2, got %d", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 3 {
		t.Fatalf("expected cache write tokens 3, got %d", usage.CacheWriteTokens)
	}

	mu.Lock()
	got := captured
	mu.Unlock()

	if got.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", got.Method)
	}
	if got.Path != "/v1/messages" {
		t.Fatalf("expected /v1/messages, got %s", got.Path)
	}
	if ct := got.Headers.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected content-type application/json, got %q", ct)
	}
	if key := got.Headers.Get("x-api-key"); key != "sk-ant-test" {
		t.Fatalf("expected x-api-key sk-ant-test, got %q", key)
	}
	if ver := got.Headers.Get("anthropic-version"); ver != "2024-10-01" {
		t.Fatalf("expected anthropic-version 2024-10-01, got %q", ver)
	}
	if beta := got.Headers.Get("anthropic-beta"); beta != "beta-x" {
		t.Fatalf("expected anthropic-beta beta-x, got %q", beta)
	}
	if ua := got.Headers.Get("User-Agent"); ua != "claude-cli/1.0.25 (external, cli)" {
		t.Fatalf("expected user-agent from context, got %q", ua)
	}

	if !bytes.Equal(got.Body, expectedBody) {
		t.Fatalf("request body was modified\nexpected: %s\ngot:      %s", string(expectedBody), string(got.Body))
	}

}

func TestAdapterStreamUsesDefaultAnthropicVersionAndAccountUserAgent(t *testing.T) {
	var (
		mu       sync.Mutex
		captured capturedRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}

		mu.Lock()
		captured = capturedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    body,
		}
		mu.Unlock()

		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n")
	}))
	defer server.Close()

	req := &domain.CanonicalRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []domain.Message{
			{
				Role:    "user",
				Content: json.RawMessage(`"hello"`),
			},
		},
	}

	expectedBody, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	account := &domain.Account{
		BaseURL:   server.URL,
		APIKey:    "sk-ant-test",
		UserAgent: "account-agent/1.0",
	}

	ctx := ContextWithHeaders(context.Background(), "", "", "ignored-agent/2.0")

	usage, err := New().Stream(ctx, account, req, sse.NewWriter(httptest.NewRecorder()))
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	if usage.InputTokens != 1 || usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", usage)
	}

	mu.Lock()
	got := captured
	mu.Unlock()

	if ver := got.Headers.Get("anthropic-version"); ver != defaultAnthropicVersion {
		t.Fatalf("expected default anthropic-version %q, got %q", defaultAnthropicVersion, ver)
	}
	if beta := got.Headers.Get("anthropic-beta"); beta != "" {
		t.Fatalf("expected no anthropic-beta, got %q", beta)
	}
	if ua := got.Headers.Get("User-Agent"); ua != "account-agent/1.0" {
		t.Fatalf("expected user-agent from account, got %q", ua)
	}
	if !bytes.Equal(got.Body, expectedBody) {
		t.Fatalf("request body was modified\nexpected: %s\ngot:      %s", string(expectedBody), string(got.Body))
	}
}
