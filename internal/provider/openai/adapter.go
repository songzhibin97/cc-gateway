package openai

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

const defaultBaseURL = "https://api.openai.com"

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Type() domain.ProviderType {
	return domain.ProviderOpenAI
}

func (a *Adapter) Stream(ctx context.Context, account *domain.Account, req *domain.CanonicalRequest, w *sse.Writer) (*domain.Usage, error) {
	translated, err := translateRequest(req, account.Extra)
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	body, err := json.Marshal(translated)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := buildResponsesURL(account.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+account.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

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

	reader := sse.NewReader(resp.Body)
	displayModel := req.OriginalModel
	if displayModel == "" {
		displayModel = req.Model
	}
	converter := NewStreamConverter(displayModel)
	usage := &domain.Usage{}

	for {
		event, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if finalizeErr := writeFinalSSE(w, converter); finalizeErr != nil {
				return usage, fmt.Errorf("read upstream SSE: %w (failed to finalize SSE: %v)", err, finalizeErr)
			}
			return usage, fmt.Errorf("read upstream SSE: %w", err)
		}
		if event.Data == "[DONE]" {
			continue
		}

		rawEvents, currentUsage, err := converter.ProcessEvent(event)
		if err != nil {
			if finalizeErr := writeFinalSSE(w, converter); finalizeErr != nil {
				return usage, fmt.Errorf("translate upstream SSE %q: %w (failed to finalize SSE: %v)", event.Type, err, finalizeErr)
			}
			return usage, fmt.Errorf("translate upstream SSE %q: %w", event.Type, err)
		}
		if currentUsage != nil {
			*usage = *currentUsage
		}

		for _, raw := range rawEvents {
			if len(raw) == 0 {
				continue
			}
			if err := w.WriteRawEvent(raw); err != nil {
				return usage, fmt.Errorf("write SSE event: %w", err)
			}
		}
	}

	if err := writeFinalSSE(w, converter); err != nil {
		return usage, fmt.Errorf("write final SSE event: %w", err)
	}

	return usage, nil
}

func writeFinalSSE(w *sse.Writer, converter *StreamConverter) error {
	for _, raw := range converter.Finalize() {
		if len(raw) == 0 {
			continue
		}
		if err := w.WriteRawEvent(raw); err != nil {
			return err
		}
	}
	return nil
}

type contextKey string

const contextKeyUserAgent contextKey = "user-agent"

func ContextWithHeaders(ctx context.Context, userAgent string) context.Context {
	return context.WithValue(ctx, contextKeyUserAgent, userAgent)
}

func buildResponsesURL(baseURL string) string {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}

	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/responses"
	}
	return trimmed + "/v1/responses"
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
