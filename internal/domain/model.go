package domain

type ModelPricing struct {
	ModelPattern      string  `yaml:"model" json:"model"`
	InputPricePerM    float64 `yaml:"input_per_million" json:"input_per_million"`
	OutputPricePerM   float64 `yaml:"output_per_million" json:"output_per_million"`
	ThinkingPricePerM float64 `yaml:"thinking_per_million" json:"thinking_per_million"`
}
