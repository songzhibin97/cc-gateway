package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

const (
	defaultBaseURL          = "https://api.anthropic.com"
	defaultAnthropicVersion = "2023-06-01"
)

// Adapter is the Anthropic native provider (pass-through).
type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Type() domain.ProviderType {
	return domain.ProviderAnthropic
}

func (a *Adapter) Stream(ctx context.Context, account *domain.Account, req *domain.CanonicalRequest, w *sse.Writer) (*domain.Usage, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	baseURL := account.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/messages"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", account.APIKey)
	httpReq.Header.Set("anthropic-version", defaultAnthropicVersion)

	if ver, ok := ctx.Value(contextKeyAnthropicVersion).(string); ok && ver != "" {
		httpReq.Header.Set("anthropic-version", ver)
	}
	if beta, ok := ctx.Value(contextKeyAnthropicBeta).(string); ok && beta != "" {
		httpReq.Header.Set("anthropic-beta", beta)
	}

	if account.UserAgent != "" {
		httpReq.Header.Set("User-Agent", account.UserAgent)
	} else if ua, ok := ctx.Value(contextKeyUserAgent).(string); ok && ua != "" {
		httpReq.Header.Set("User-Agent", ua)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if account.ProxyURL != "" {
		proxyURL, err := url.Parse(account.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %s", maskAPIKeyInError(err, account.APIKey))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	usage := &domain.Usage{}
	reader := sse.NewReader(resp.Body)

	for {
		event, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return usage, fmt.Errorf("read upstream SSE: %w", err)
		}

		extractUsage(event, usage)

		var buf bytes.Buffer
		if event.Type != "" {
			if _, err := fmt.Fprintf(&buf, "event: %s\n", event.Type); err != nil {
				return usage, fmt.Errorf("format SSE event type: %w", err)
			}
		}
		if _, err := fmt.Fprintf(&buf, "data: %s\n\n", event.Data); err != nil {
			return usage, fmt.Errorf("format SSE event data: %w", err)
		}

		if err := w.WriteRawEvent(buf.Bytes()); err != nil {
			return usage, fmt.Errorf("write SSE event: %w", err)
		}
	}

	return usage, nil
}

// extractUsage parses token usage from Anthropic SSE events.
func extractUsage(event sse.Event, usage *domain.Usage) {
	if event.Data == "" {
		return
	}

	var envelope struct {
		Type    string `json:"type"`
		Message struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				CacheRead    int `json:"cache_read_input_tokens"`
				CacheWrite   int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal([]byte(event.Data), &envelope); err != nil {
		return
	}

	switch envelope.Type {
	case "message_start":
		usage.InputTokens = envelope.Message.Usage.InputTokens
		usage.OutputTokens = envelope.Message.Usage.OutputTokens
		usage.CacheReadTokens = envelope.Message.Usage.CacheRead
		usage.CacheWriteTokens = envelope.Message.Usage.CacheWrite
	case "message_delta":
		usage.OutputTokens = envelope.Usage.OutputTokens
	}
}

type contextKey string

const (
	contextKeyAnthropicVersion contextKey = "anthropic-version"
	contextKeyAnthropicBeta    contextKey = "anthropic-beta"
	contextKeyUserAgent        contextKey = "user-agent"
)

// ContextWithHeaders creates a context with the original request headers.
func ContextWithHeaders(ctx context.Context, anthropicVersion, anthropicBeta, userAgent string) context.Context {
	ctx = context.WithValue(ctx, contextKeyAnthropicVersion, anthropicVersion)
	ctx = context.WithValue(ctx, contextKeyAnthropicBeta, anthropicBeta)
	ctx = context.WithValue(ctx, contextKeyUserAgent, userAgent)
	return ctx
}

func maskAPIKeyInError(err error, apiKey string) string {
	if err == nil {
		return ""
	}
	if apiKey == "" {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), apiKey, "***")
}
