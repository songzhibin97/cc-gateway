package domain

type ProviderType string

const (
	ProviderAnthropic       ProviderType = "anthropic"
	ProviderOpenAI          ProviderType = "openai"
	ProviderGemini          ProviderType = "gemini"
	ProviderCustomOpenAI    ProviderType = "custom_openai"
	ProviderCustomAnthropic ProviderType = "custom_anthropic"
)
