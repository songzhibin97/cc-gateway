package proxy

import (
	"context"
	"errors"
	"math/rand"
	"regexp"
	"strconv"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type RetryConfig struct {
	MaxRetries           int
	BaseDelay            time.Duration
	RetryableStatusCodes map[int]bool
}

var upstreamStatusRe = regexp.MustCompile(`upstream returned (\d+):`)

var defaultRetryableStatusCodes = map[int]bool{
	500: true,
	502: true,
	503: true,
	504: true,
}

func extractRetryConfig(account *domain.Account) RetryConfig {
	if account == nil || account.Extra == nil {
		return RetryConfig{}
	}

	raw, ok := account.Extra["retry_config"]
	if !ok {
		return RetryConfig{}
	}

	cfg, ok := raw.(map[string]any)
	if !ok {
		return RetryConfig{}
	}

	rc := RetryConfig{
		BaseDelay:            time.Second,
		RetryableStatusCodes: copyStatusCodes(defaultRetryableStatusCodes),
	}

	if v, ok := cfg["max_retries"]; ok {
		switch n := v.(type) {
		case float64:
			rc.MaxRetries = clampInt(int(n), 0, 10)
		case int:
			rc.MaxRetries = clampInt(n, 0, 10)
		}
	}

	if rc.MaxRetries == 0 {
		return RetryConfig{}
	}

	if v, ok := cfg["retry_base_delay"]; ok {
		if s, ok := v.(string); ok && s != "" {
			if d, err := time.ParseDuration(s); err == nil {
				rc.BaseDelay = d
			}
		}
	}

	if v, ok := cfg["retryable_status_codes"]; ok {
		if list, ok := v.([]any); ok && len(list) > 0 {
			codes := make(map[int]bool, len(list))
			for _, item := range list {
				switch n := item.(type) {
				case float64:
					codes[int(n)] = true
				case int:
					codes[n] = true
				}
			}
			if len(codes) > 0 {
				rc.RetryableStatusCodes = codes
			}
		}
	}

	return rc
}

func (rc RetryConfig) IsRetryable(err error) bool {
	if rc.MaxRetries == 0 || err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	msg := err.Error()
	m := upstreamStatusRe.FindStringSubmatch(msg)
	if m != nil {
		code, parseErr := strconv.Atoi(m[1])
		if parseErr == nil {
			return rc.RetryableStatusCodes[code]
		}
	}

	return true
}

func (rc RetryConfig) Backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}
	base := rc.BaseDelay * (1 << uint(shift))

	jitterFactor := 0.8 + rand.Float64()*0.4 //nolint:gosec
	result := time.Duration(float64(base) * jitterFactor)

	const maxBackoff = 30 * time.Second
	if result > maxBackoff {
		result = maxBackoff
	}
	return result
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func copyStatusCodes(m map[int]bool) map[int]bool {
	out := make(map[int]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
