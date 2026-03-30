package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/accounting"
	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/internal/metrics"
	"github.com/songzhibin97/cc-gateway/internal/provider"
	anthropicprovider "github.com/songzhibin97/cc-gateway/internal/provider/anthropic"
	geminiprovider "github.com/songzhibin97/cc-gateway/internal/provider/gemini"
	openaiprovider "github.com/songzhibin97/cc-gateway/internal/provider/openai"
	"github.com/songzhibin97/cc-gateway/internal/router"
	"github.com/songzhibin97/cc-gateway/pkg/sse"
)

const maxRequestBodyBytes = 10 * 1024 * 1024
const maxRecordedPayloadBytes = 1 * 1024 * 1024
const truncatedPayloadSuffix = "\n...[truncated]"

// Handler handles POST /v1/messages requests.
type Handler struct {
	router    *router.Router
	keyStore  *KeyStore
	recorder  *accounting.Recorder
	costCalc  *accounting.CostCalculator
	providers map[domain.ProviderType]provider.Provider
	logger    *slog.Logger
}

// NewHandler creates a new proxy handler.
func NewHandler(rtr *router.Router, keyStore *KeyStore, recorder *accounting.Recorder, costCalc *accounting.CostCalculator, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}

	anthropic := anthropicprovider.New()
	openai := openaiprovider.New()
	gemini := geminiprovider.New()

	return &Handler{
		router:   rtr,
		keyStore: keyStore,
		recorder: recorder,
		costCalc: costCalc,
		providers: map[domain.ProviderType]provider.Provider{
			domain.ProviderAnthropic:       anthropic,
			domain.ProviderCustomAnthropic: anthropic,
			domain.ProviderOpenAI:          openai,
			domain.ProviderCustomOpenAI:    openai,
			domain.ProviderGemini:          gemini,
		},
		logger: logger,
	}
}

// ServeHTTP handles Anthropic Messages API requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := newResponseStateWriter(w)

	var (
		requestBody    string
		requestedModel string
		actualModel    string
		stopReason     string
		recordErr      string
		apiKey         *domain.ExternalAPIKey
		recordAccount  *domain.Account
		recordUsage    domain.Usage
	)

	defer func() {
		h.recordRequest(start, apiKey, recordAccount, requestedModel, actualModel, recordUsage, rw, stopReason, recordErr, requestBody)
		h.recordMetrics(start, recordAccount, requestedModel, actualModel, recordUsage, recordErr)
	}()

	if r.Method != http.MethodPost {
		recordErr = "only POST is supported"
		h.writeError(rw, http.StatusMethodNotAllowed, "method_not_allowed", recordErr)
		return
	}

	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		recordErr = "failed to read request body"
		h.writeError(rw, http.StatusBadRequest, "invalid_request_error", recordErr)
		return
	}
	requestBody = truncateForStorage(string(body))

	var req domain.CanonicalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		recordErr = fmt.Sprintf("invalid JSON: %v", err)
		h.writeError(rw, http.StatusBadRequest, "invalid_request_error", recordErr)
		return
	}
	if req.Model == "" {
		recordErr = "model is required"
		h.writeError(rw, http.StatusBadRequest, "invalid_request_error", recordErr)
		return
	}

	requestedModel = req.Model
	req.Stream = true

	var (
		keyGroup       *domain.KeyGroup
		releaseKeySlot = func() {}
	)

	if h.authEnabled() {
		rawAPIKey := r.Header.Get("x-api-key")
		if rawAPIKey == "" {
			// Also check Authorization: Bearer (used by ANTHROPIC_AUTH_TOKEN)
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				rawAPIKey = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if rawAPIKey == "" {
			recordErr = ErrInvalidAPIKey.Error()
			h.writeMappedError(rw, ErrInvalidAPIKey)
			return
		}

		var authErr error
		apiKey, keyGroup, authErr = h.keyStore.Authenticate(rawAPIKey)
		if authErr != nil {
			recordErr = authErr.Error()
			h.writeMappedError(rw, authErr)
			return
		}
		if err := h.keyStore.CheckModelAllowed(apiKey, requestedModel); err != nil {
			recordErr = err.Error()
			h.writeMappedError(rw, err)
			return
		}
		if err := h.keyStore.CheckUsageLimit(apiKey); err != nil {
			recordErr = err.Error()
			h.writeMappedError(rw, err)
			return
		}

		releaseKeySlot, authErr = h.keyStore.AcquireKeyConcurrency(apiKey)
		if authErr != nil {
			recordErr = authErr.Error()
			h.writeMappedError(rw, authErr)
			return
		}
		defer releaseKeySlot()
	}

	ctx := anthropicprovider.ContextWithHeaders(
		r.Context(),
		r.Header.Get("anthropic-version"),
		r.Header.Get("anthropic-beta"),
		r.Header.Get("User-Agent"),
	)
	ctx = openaiprovider.ContextWithHeaders(ctx, r.Header.Get("User-Agent"))
	ctx = geminiprovider.ContextWithHeaders(ctx, r.Header.Get("User-Agent"))

	var lastErr error
	maxAttempts := 2
	if keyGroup != nil && len(keyGroup.AccountIDs) > maxAttempts {
		maxAttempts = len(keyGroup.AccountIDs)
	}
	excluded := make(map[string]struct{}, maxAttempts)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		account, err := h.selectAccount(keyGroup, requestedModel, excluded)
		if err != nil {
			if lastErr != nil {
				if lastErr == ErrConcurrencyLimit || lastErr == ErrUsageLimit {
					recordErr = lastErr.Error()
					h.writeMappedError(rw, lastErr)
					return
				}
				recordErr = lastErr.Error()
				h.writeError(rw, http.StatusBadGateway, "api_error", recordErr)
				return
			}
			recordErr = err.Error()
			h.writeError(rw, http.StatusBadRequest, "invalid_request_error", recordErr)
			return
		}
		recordAccount = account

		releaseAccountSlot := func() {}
		if h.keyStore != nil {
			releaseAccountSlot, err = h.keyStore.AcquireAccountConcurrency(account.ID)
			if err != nil {
				lastErr = err
				excluded[account.ID] = struct{}{}
				if len(excluded) >= maxAttempts {
					recordErr = err.Error()
					h.writeMappedError(rw, err)
					return
				}
				continue
			}
		}
		h.router.IncrementInflight(account.ID)
		releaseInflight := func() {
			h.router.DecrementInflight(account.ID)
			releaseAccountSlot()
		}

		req.OriginalModel = requestedModel
		req.Model = account.ResolveModel(requestedModel)
		actualModel = req.Model
		prov, ok := h.providers[account.Provider]
		if !ok {
			releaseInflight()
			recordErr = fmt.Sprintf("no provider configured for %q", account.Provider)
			h.writeError(rw, http.StatusInternalServerError, "api_error", recordErr)
			return
		}

		retryConfig := extractRetryConfig(account)
		activeRequests := metrics.ActiveRequests.WithLabelValues(account.ID)

		var usage *domain.Usage
		var streamErr error
		for retry := 0; retry <= retryConfig.MaxRetries; retry++ {
			if retry > 0 {
				delay := retryConfig.Backoff(retry)
				h.logger.Warn("retrying request on same account",
					slog.String("account_id", account.ID),
					slog.Int("retry", retry),
					slog.Duration("backoff", delay),
				)
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					releaseInflight()
					recordErr = "context cancelled during retry backoff"
					h.writeError(rw, http.StatusBadGateway, "api_error", recordErr)
					return
				}
			}

			sseWriter := sse.NewWriter(rw)
			activeRequests.Inc()
			usage, streamErr = func() (*domain.Usage, error) {
				defer activeRequests.Dec()
				return prov.Stream(ctx, account, &req, sseWriter)
			}()
			if streamErr == nil {
				break
			}
			if rw.writeCount > 0 || rw.wroteHeader || !retryConfig.IsRetryable(streamErr) {
				break
			}
		}
		releaseInflight()
		duration := time.Since(start)

		logAttrs := []any{
			slog.Int("attempt", attempt+1),
			slog.String("model_requested", requestedModel),
			slog.String("model_actual", req.Model),
			slog.String("account_id", account.ID),
			slog.String("provider", string(account.Provider)),
			slog.Duration("duration", duration),
		}

		if usage != nil {
			recordUsage = *usage
			logAttrs = append(logAttrs,
				slog.Int("input_tokens", usage.InputTokens),
				slog.Int("output_tokens", usage.OutputTokens),
				slog.Int("thinking_tokens", usage.ThinkingTokens),
				slog.Int("cache_read_tokens", usage.CacheReadTokens),
				slog.Int("cache_write_tokens", usage.CacheWriteTokens),
			)
		}

		if usage != nil && apiKey != nil {
			h.keyStore.RecordUsage(apiKey, usage.InputTokens, usage.OutputTokens)
		}

		if streamErr == nil {
			h.router.RecordSuccess(account.ID)
			stopReason = extractStopReason(rw.BufferedString())
			h.logger.Info("request completed", logAttrs...)
			return
		}

		h.router.RecordFailure(account.ID)
		recordErr = streamErr.Error()
		logAttrs = append(logAttrs,
			slog.String("error", recordErr),
			slog.Int64("bytes_written", rw.writeCount),
		)
		h.logger.Error("request failed", logAttrs...)
		lastErr = streamErr

		if rw.writeCount > 0 || rw.wroteHeader || attempt == maxAttempts-1 {
			if !rw.wroteHeader {
				h.writeError(rw, http.StatusBadGateway, "api_error", recordErr)
			}
			stopReason = extractStopReason(rw.BufferedString())
			return
		}

		excluded[account.ID] = struct{}{}
	}

	if lastErr != nil {
		recordErr = lastErr.Error()
		h.writeMappedError(rw, lastErr)
		return
	}

	recordErr = "no account available"
	h.writeError(rw, http.StatusBadGateway, "api_error", recordErr)
}

func (h *Handler) authEnabled() bool {
	return h.keyStore != nil && h.keyStore.HasKeys()
}

func (h *Handler) selectAccount(group *domain.KeyGroup, model string, excluded map[string]struct{}) (*domain.Account, error) {
	if h.router == nil {
		return nil, fmt.Errorf("router is not configured")
	}

	if group != nil {
		return h.router.SelectAccountExcept(group.ID, model, excluded)
	}

	return h.router.SelectAccountFromAllExcept(model, excluded)
}

func (h *Handler) writeMappedError(w http.ResponseWriter, err error) {
	switch err {
	case ErrInvalidAPIKey, ErrKeyDisabled:
		h.writeError(w, http.StatusUnauthorized, "authentication_error", err.Error())
	case ErrModelNotAllowed:
		h.writeError(w, http.StatusForbidden, "permission_error", err.Error())
	case ErrConcurrencyLimit, ErrUsageLimit:
		h.writeError(w, http.StatusTooManyRequests, "rate_limit_error", err.Error())
	default:
		h.writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, errorType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errorType,
			"message": message,
		},
	})
}

func (h *Handler) recordRequest(start time.Time, apiKey *domain.ExternalAPIKey, account *domain.Account, requestedModel, actualModel string, usage domain.Usage, rw *responseStateWriter, stopReason, errText, requestBody string) {
	if h.recorder == nil || rw == nil {
		return
	}

	if stopReason == "" {
		stopReason = extractStopReason(rw.BufferedString())
	}

	modelForCost := actualModel
	if modelForCost == "" {
		modelForCost = requestedModel
	}

	var costUSD float64
	if h.costCalc != nil {
		costUSD = h.costCalc.Calculate(modelForCost, usage).TotalCostUSD
	}

	rec := accounting.RequestRecord{
		ModelRequested: requestedModel,
		ModelActual:    actualModel,
		Usage:          usage,
		CostUSD:        costUSD,
		LatencyMs:      time.Since(start).Milliseconds(),
		StopReason:     stopReason,
		Error:          errText,
		StatusCode:     rw.StatusCode(),
		RequestBody:    requestBody,
		ResponseBody:   rw.BufferedString(),
	}

	if apiKey != nil {
		rec.KeyID = apiKey.ID
		rec.KeyHint = apiKey.KeyHint
	}
	if account != nil {
		rec.AccountID = account.ID
		rec.AccountName = account.Name
		rec.Provider = string(account.Provider)
	}

	h.recorder.Record(rec)
}

func (h *Handler) recordMetrics(start time.Time, account *domain.Account, requestedModel, actualModel string, usage domain.Usage, errText string) {
	if account == nil {
		return
	}

	model := actualModel
	if model == "" {
		model = requestedModel
	}
	if model == "" {
		model = "unknown"
	}

	status := "success"
	if errText != "" {
		status = "error"
	}

	provider := string(account.Provider)
	accountID := account.ID
	duration := time.Since(start)
	costUSD := 0.0
	if h.costCalc != nil {
		costUSD = h.costCalc.Calculate(model, usage).TotalCostUSD
	}

	metrics.RequestsTotal.WithLabelValues(provider, model, accountID, status).Inc()
	metrics.RequestDuration.WithLabelValues(provider, model).Observe(duration.Seconds())
	metrics.TokensTotal.WithLabelValues(provider, accountID, "input").Add(float64(usage.InputTokens))
	metrics.TokensTotal.WithLabelValues(provider, accountID, "output").Add(float64(usage.OutputTokens))
	metrics.CostTotal.WithLabelValues(provider, accountID, model).Add(costUSD)
}

func truncateForStorage(raw string) string {
	if len(raw) <= maxRecordedPayloadBytes {
		return raw
	}

	return raw[:maxRecordedPayloadBytes-len(truncatedPayloadSuffix)] + truncatedPayloadSuffix
}

func extractStopReason(raw string) string {
	if raw == "" {
		return ""
	}

	var lastStop string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		var payload struct {
			Type  string `json:"type"`
			Delta struct {
				StopReason *string `json:"stop_reason"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		if payload.Type == "message_delta" && payload.Delta.StopReason != nil && *payload.Delta.StopReason != "" {
			lastStop = *payload.Delta.StopReason
		}
	}

	return lastStop
}

type responseStateWriter struct {
	http.ResponseWriter
	wroteHeader bool
	writeCount  int64
	statusCode  int
	body        bytes.Buffer
	truncated   bool
}

func newResponseStateWriter(w http.ResponseWriter) *responseStateWriter {
	return &responseStateWriter{ResponseWriter: w}
}

func (w *responseStateWriter) WriteHeader(statusCode int) {
	w.wroteHeader = true
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseStateWriter) Write(p []byte) (int, error) {
	w.wroteHeader = true
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.writeCount += int64(n)
	w.capture(p[:n])
	return n, err
}

func (w *responseStateWriter) StatusCode() int {
	if w == nil || w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *responseStateWriter) BufferedString() string {
	if w == nil {
		return ""
	}
	return w.body.String()
}

func (w *responseStateWriter) capture(p []byte) {
	if len(p) == 0 || w.truncated {
		return
	}

	remaining := maxRecordedPayloadBytes - w.body.Len()
	if remaining <= 0 {
		w.body.WriteString(truncatedPayloadSuffix)
		w.truncated = true
		return
	}

	if len(p) > remaining {
		_, _ = w.body.Write(p[:remaining])
		w.body.WriteString(truncatedPayloadSuffix)
		w.truncated = true
		return
	}

	_, _ = w.body.Write(p)
}

func (w *responseStateWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
