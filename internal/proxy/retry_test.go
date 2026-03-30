package proxy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

func TestExtractRetryConfig(t *testing.T) {
	t.Run("nil account returns zero value", func(t *testing.T) {
		rc := extractRetryConfig(nil)
		if rc.MaxRetries != 0 {
			t.Fatalf("want MaxRetries=0, got %d", rc.MaxRetries)
		}
	})

	t.Run("nil Extra returns zero value", func(t *testing.T) {
		rc := extractRetryConfig(&domain.Account{})
		if rc.MaxRetries != 0 {
			t.Fatalf("want MaxRetries=0, got %d", rc.MaxRetries)
		}
	})

	t.Run("empty Extra returns zero value", func(t *testing.T) {
		rc := extractRetryConfig(&domain.Account{Extra: map[string]any{}})
		if rc.MaxRetries != 0 {
			t.Fatalf("want MaxRetries=0, got %d", rc.MaxRetries)
		}
	})

	t.Run("retry_config key missing returns zero value", func(t *testing.T) {
		rc := extractRetryConfig(&domain.Account{Extra: map[string]any{"other": "val"}})
		if rc.MaxRetries != 0 {
			t.Fatalf("want MaxRetries=0, got %d", rc.MaxRetries)
		}
	})

	t.Run("valid full config parses correctly", func(t *testing.T) {
		acct := &domain.Account{
			Extra: map[string]any{
				"retry_config": map[string]any{
					"max_retries":            float64(3),
					"retry_base_delay":       "500ms",
					"retryable_status_codes": []any{float64(429), float64(503)},
				},
			},
		}
		rc := extractRetryConfig(acct)

		if rc.MaxRetries != 3 {
			t.Errorf("want MaxRetries=3, got %d", rc.MaxRetries)
		}
		if rc.BaseDelay != 500*time.Millisecond {
			t.Errorf("want BaseDelay=500ms, got %v", rc.BaseDelay)
		}
		if !rc.RetryableStatusCodes[429] || !rc.RetryableStatusCodes[503] {
			t.Errorf("want 429 and 503 in RetryableStatusCodes, got %v", rc.RetryableStatusCodes)
		}
		if rc.RetryableStatusCodes[500] {
			t.Errorf("500 should not be in custom list, got %v", rc.RetryableStatusCodes)
		}
	})

	t.Run("missing fields use defaults", func(t *testing.T) {
		acct := &domain.Account{
			Extra: map[string]any{
				"retry_config": map[string]any{
					"max_retries": float64(2),
				},
			},
		}
		rc := extractRetryConfig(acct)

		if rc.MaxRetries != 2 {
			t.Errorf("want MaxRetries=2, got %d", rc.MaxRetries)
		}
		if rc.BaseDelay != time.Second {
			t.Errorf("want BaseDelay=1s, got %v", rc.BaseDelay)
		}
		for _, code := range []int{500, 502, 503, 504} {
			if !rc.RetryableStatusCodes[code] {
				t.Errorf("want default code %d in RetryableStatusCodes", code)
			}
		}
	})

	t.Run("max_retries clamped to 10", func(t *testing.T) {
		acct := &domain.Account{
			Extra: map[string]any{
				"retry_config": map[string]any{
					"max_retries": float64(99),
				},
			},
		}
		rc := extractRetryConfig(acct)
		if rc.MaxRetries != 10 {
			t.Errorf("want MaxRetries clamped to 10, got %d", rc.MaxRetries)
		}
	})

	t.Run("max_retries=0 returns zero-value config", func(t *testing.T) {
		acct := &domain.Account{
			Extra: map[string]any{
				"retry_config": map[string]any{
					"max_retries": float64(0),
				},
			},
		}
		rc := extractRetryConfig(acct)
		if rc.MaxRetries != 0 {
			t.Errorf("want MaxRetries=0, got %d", rc.MaxRetries)
		}
	})
}

func defaultRC() RetryConfig {
	return RetryConfig{
		MaxRetries:           3,
		BaseDelay:            time.Second,
		RetryableStatusCodes: copyStatusCodes(defaultRetryableStatusCodes),
	}
}

func TestIsRetryable(t *testing.T) {
	t.Run("MaxRetries=0 always returns false", func(t *testing.T) {
		rc := RetryConfig{MaxRetries: 0}
		if rc.IsRetryable(fmt.Errorf("upstream returned 502: bad gateway")) {
			t.Error("want false when MaxRetries=0")
		}
	})

	t.Run("nil error returns false", func(t *testing.T) {
		if defaultRC().IsRetryable(nil) {
			t.Error("want false for nil error")
		}
	})

	t.Run("502 error is retryable", func(t *testing.T) {
		err := fmt.Errorf("upstream returned 502: bad gateway")
		if !defaultRC().IsRetryable(err) {
			t.Error("want true for 502")
		}
	})

	t.Run("500 error is retryable", func(t *testing.T) {
		err := fmt.Errorf("upstream returned 500: internal server error")
		if !defaultRC().IsRetryable(err) {
			t.Error("want true for 500")
		}
	})

	t.Run("429 not in default codes returns false", func(t *testing.T) {
		err := fmt.Errorf("upstream returned 429: too many requests")
		if defaultRC().IsRetryable(err) {
			t.Error("want false for 429 (not in default retryable codes)")
		}
	})

	t.Run("400 error is not retryable", func(t *testing.T) {
		err := fmt.Errorf("upstream returned 400: bad request")
		if defaultRC().IsRetryable(err) {
			t.Error("want false for 400")
		}
	})

	t.Run("network error connection refused is retryable", func(t *testing.T) {
		err := errors.New("dial tcp: connection refused")
		if !defaultRC().IsRetryable(err) {
			t.Error("want true for network/connection error")
		}
	})

	t.Run("context.Canceled is not retryable", func(t *testing.T) {
		if defaultRC().IsRetryable(context.Canceled) {
			t.Error("want false for context.Canceled")
		}
	})

	t.Run("context.DeadlineExceeded is not retryable", func(t *testing.T) {
		if defaultRC().IsRetryable(context.DeadlineExceeded) {
			t.Error("want false for context.DeadlineExceeded")
		}
	})

	t.Run("wrapped context.Canceled is not retryable", func(t *testing.T) {
		err := fmt.Errorf("request failed: %w", context.Canceled)
		if defaultRC().IsRetryable(err) {
			t.Error("want false for wrapped context.Canceled")
		}
	})
}

func TestBackoff(t *testing.T) {
	rc := RetryConfig{
		MaxRetries: 5,
		BaseDelay:  time.Second,
	}

	t.Run("attempt=0 returns 0", func(t *testing.T) {
		if d := rc.Backoff(0); d != 0 {
			t.Errorf("want 0, got %v", d)
		}
	})

	t.Run("negative attempt returns 0", func(t *testing.T) {
		if d := rc.Backoff(-1); d != 0 {
			t.Errorf("want 0, got %v", d)
		}
	})

	t.Run("attempt=1 is within jitter range of BaseDelay", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			d := rc.Backoff(1)
			lo := time.Duration(float64(rc.BaseDelay) * 0.8)
			hi := time.Duration(float64(rc.BaseDelay) * 1.2)
			if d < lo || d > hi {
				t.Errorf("attempt=1: want [%v, %v], got %v", lo, hi, d)
			}
		}
	})

	t.Run("attempt=2 is within jitter range of 2*BaseDelay", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			d := rc.Backoff(2)
			expected := 2 * rc.BaseDelay
			lo := time.Duration(float64(expected) * 0.8)
			hi := time.Duration(float64(expected) * 1.2)
			if d < lo || d > hi {
				t.Errorf("attempt=2: want [%v, %v], got %v", lo, hi, d)
			}
		}
	})

	t.Run("large attempt is capped at 30s", func(t *testing.T) {
		const maxBackoff = 30 * time.Second
		for attempt := 10; attempt <= 50; attempt++ {
			d := rc.Backoff(attempt)
			if d > maxBackoff {
				t.Errorf("attempt=%d: got %v, exceeds cap %v", attempt, d, maxBackoff)
			}
		}
	})

	t.Run("attempt=3 is within jitter range of 4*BaseDelay", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			d := rc.Backoff(3)
			expected := 4 * rc.BaseDelay
			lo := time.Duration(float64(expected) * 0.8)
			hi := time.Duration(float64(expected) * 1.2)
			if d < lo || d > hi {
				t.Errorf("attempt=3: want [%v, %v], got %v", lo, hi, d)
			}
		}
	})
}
