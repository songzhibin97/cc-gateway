package domain

import "path"

type AccountStatus string

const (
	AccountEnabled  AccountStatus = "enabled"
	AccountDisabled AccountStatus = "disabled"
)

type HealthState string

const (
	HealthHealthy     HealthState = "healthy"
	HealthDegraded    HealthState = "degraded"
	HealthCircuitOpen HealthState = "open"
)

type Account struct {
	ID             string               `yaml:"id" json:"id"`
	Name           string               `yaml:"name" json:"name"`
	Provider       ProviderType         `yaml:"provider" json:"provider"`
	BaseURL        string               `yaml:"base_url" json:"base_url"`
	APIKey         string               `yaml:"api_key" json:"api_key"`
	ProxyURL       string               `yaml:"proxy_url" json:"proxy_url"`
	UserAgent      string               `yaml:"user_agent" json:"user_agent"`
	Status         AccountStatus        `yaml:"status" json:"status"`
	Health         HealthState          `yaml:"-" json:"health"`
	AllowedModels  []string             `yaml:"allowed_models" json:"allowed_models"`
	ModelAliases   map[string]string    `yaml:"model_aliases" json:"model_aliases"`
	MaxConcurrent  int                  `yaml:"max_concurrent" json:"max_concurrent"`
	Extra          map[string]any       `yaml:"extra" json:"extra"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`
}

type CircuitBreakerConfig struct {
	FailureThreshold int    `yaml:"failure_threshold" json:"failure_threshold"`
	SuccessThreshold int    `yaml:"success_threshold" json:"success_threshold"`
	OpenDuration     string `yaml:"open_duration" json:"open_duration"`
}

// CanServeModel returns true if the account can serve the given model.
// Supports glob patterns in both model_aliases keys and allowed_models.
// Resolution order:
//  1. If model matches any alias key (exact or glob) → implicitly allowed
//  2. If allowed_models is empty → allow all
//  3. Check resolved model against allowed_models (exact or glob)
func (a *Account) CanServeModel(model string) bool {
	// If model matches any alias (exact or glob), this account supports it
	if _, target := a.matchAlias(model); target != "" {
		return true
	}

	resolved := a.ResolveModel(model)

	// No restriction = allow all
	if len(a.AllowedModels) == 0 {
		return true
	}

	// Check both original and resolved model against allowed_models (with glob)
	for _, pattern := range a.AllowedModels {
		if matchGlob(pattern, model) || matchGlob(pattern, resolved) {
			return true
		}
	}
	return false
}

// ResolveModel returns the actual model name to send to the provider.
// Supports glob patterns in alias keys, e.g. "claude-*" → "gpt-5.2".
// Exact match takes priority over glob match.
func (a *Account) ResolveModel(requestedModel string) string {
	_, target := a.matchAlias(requestedModel)
	if target != "" {
		return target
	}
	return requestedModel
}

// matchAlias finds the best alias match for the model.
// Returns the matched key and target. Exact match takes priority over glob.
func (a *Account) matchAlias(model string) (key, target string) {
	if a.ModelAliases == nil {
		return "", ""
	}

	// Exact match first
	if t, ok := a.ModelAliases[model]; ok {
		return model, t
	}

	// Glob match (first match wins)
	for pattern, t := range a.ModelAliases {
		if matchGlob(pattern, model) {
			return pattern, t
		}
	}
	return "", ""
}

// matchGlob performs glob matching using path.Match.
// Returns true on match, false on mismatch or invalid pattern.
func matchGlob(pattern, name string) bool {
	matched, _ := path.Match(pattern, name)
	return matched
}
