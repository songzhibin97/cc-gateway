package accounting

import (
	"path"
	"sync"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type CostCalculator struct {
	mu      sync.RWMutex
	pricing []domain.ModelPricing
}

func NewCostCalculator(pricing []domain.ModelPricing) *CostCalculator {
	return &CostCalculator{pricing: pricing}
}

type CostBreakdown struct {
	InputCostUSD    float64
	OutputCostUSD   float64
	ThinkingCostUSD float64
	TotalCostUSD    float64
}

func (c *CostCalculator) Calculate(model string, usage domain.Usage) CostBreakdown {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pricing := c.findPricing(model)
	if pricing == nil {
		return CostBreakdown{}
	}

	cb := CostBreakdown{
		InputCostUSD:    float64(usage.InputTokens) / 1_000_000 * pricing.InputPricePerM,
		OutputCostUSD:   float64(usage.OutputTokens) / 1_000_000 * pricing.OutputPricePerM,
		ThinkingCostUSD: float64(usage.ThinkingTokens) / 1_000_000 * pricing.ThinkingPricePerM,
	}
	cb.TotalCostUSD = cb.InputCostUSD + cb.OutputCostUSD + cb.ThinkingCostUSD
	return cb
}

func (c *CostCalculator) UpdatePricing(pricing []domain.ModelPricing) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pricing = pricing
}

func (c *CostCalculator) findPricing(model string) *domain.ModelPricing {
	for i := range c.pricing {
		if c.pricing[i].ModelPattern == model {
			return &c.pricing[i]
		}
	}

	for i := range c.pricing {
		matched, _ := path.Match(c.pricing[i].ModelPattern, model)
		if matched {
			return &c.pricing[i]
		}
	}

	return nil
}
