package accounting

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/internal/metrics"
	"github.com/songzhibin97/cc-gateway/internal/store"
)

type RequestRecord struct {
	KeyID          string
	KeyHint        string
	AccountID      string
	AccountName    string
	Provider       string
	ModelRequested string
	ModelActual    string
	Usage          domain.Usage
	CostUSD        float64
	LatencyMs      int64
	StopReason     string
	Error          string
	StatusCode     int
	RequestBody    string
	ResponseBody   string
}

type Recorder struct {
	repo   *store.LogRepo
	queue  chan RequestRecord
	logger *slog.Logger
	wg     sync.WaitGroup
	once   sync.Once
}

func NewRecorder(repo *store.LogRepo, logger *slog.Logger) *Recorder {
	r := &Recorder{
		repo:   repo,
		queue:  make(chan RequestRecord, 1000),
		logger: logger,
	}
	r.wg.Add(1)
	go r.processLoop()
	return r
}

func (r *Recorder) Record(rec RequestRecord) {
	if r == nil {
		return
	}

	select {
	case r.queue <- rec:
	default:
		metrics.RecorderDropped.Inc()
		r.logger.Warn("recording queue full, dropping record")
	}
}

func (r *Recorder) processLoop() {
	defer r.wg.Done()

	for rec := range r.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		log := &store.RequestLog{
			KeyID:            rec.KeyID,
			KeyHint:          rec.KeyHint,
			AccountID:        rec.AccountID,
			AccountName:      rec.AccountName,
			Provider:         rec.Provider,
			ModelRequested:   rec.ModelRequested,
			ModelActual:      rec.ModelActual,
			InputTokens:      rec.Usage.InputTokens,
			OutputTokens:     rec.Usage.OutputTokens,
			ThinkingTokens:   rec.Usage.ThinkingTokens,
			CacheReadTokens:  rec.Usage.CacheReadTokens,
			CacheWriteTokens: rec.Usage.CacheWriteTokens,
			CostUSD:          rec.CostUSD,
			LatencyMs:        rec.LatencyMs,
			StopReason:       rec.StopReason,
			Error:            rec.Error,
			StatusCode:       rec.StatusCode,
		}

		logID, err := r.repo.InsertLog(ctx, log)
		if err != nil {
			r.logger.Error("failed to insert request log", slog.String("error", err.Error()))
			cancel()
			continue
		}

		if rec.RequestBody != "" || rec.ResponseBody != "" {
			payload := &store.RequestPayload{
				LogID:        logID,
				RequestBody:  rec.RequestBody,
				ResponseBody: rec.ResponseBody,
			}
			if err := r.repo.InsertPayload(ctx, payload); err != nil {
				r.logger.Error("failed to insert request payload", slog.String("error", err.Error()))
			}
		}

		cancel()
	}
}

// Close closes the recorder's queue and waits for in-flight writes.
func (r *Recorder) Close() {
	if r == nil {
		return
	}

	r.once.Do(func() {
		close(r.queue)
		r.wg.Wait()
	})
}
