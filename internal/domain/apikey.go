package domain

import "time"

type ExternalAPIKey struct {
	ID               string        `yaml:"id" json:"id"`
	Key              string        `yaml:"key" json:"-"`
	KeyHash          string        `yaml:"-" json:"-"`
	KeyHint          string        `yaml:"-" json:"key_hint"`
	GroupID          string        `yaml:"group_id" json:"group_id"`
	Status           AccountStatus `yaml:"status" json:"status"`
	AllowedModels    []string      `yaml:"allowed_models" json:"allowed_models"`
	MaxInputTokens   int64         `yaml:"max_input_tokens_monthly" json:"max_input_tokens_monthly"`
	MaxOutputTokens  int64         `yaml:"max_output_tokens_monthly" json:"max_output_tokens_monthly"`
	UsedInputTokens  int64         `yaml:"-" json:"used_input_tokens"`
	UsedOutputTokens int64         `yaml:"-" json:"used_output_tokens"`
	MaxConcurrent    int           `yaml:"max_concurrent" json:"max_concurrent"`
	CreatedAt        time.Time     `yaml:"-" json:"created_at"`
}

type KeyGroup struct {
	ID            string   `yaml:"id" json:"id"`
	Name          string   `yaml:"name" json:"name"`
	AccountIDs    []string `yaml:"account_ids" json:"account_ids"`
	AllowedModels []string `yaml:"allowed_models" json:"allowed_models"`
	Balancer      string   `yaml:"balancer" json:"balancer"`
}
