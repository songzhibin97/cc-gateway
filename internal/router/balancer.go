package router

import (
	"sync/atomic"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type Balancer interface {
	Select(candidates []*domain.Account) *domain.Account
}

// RoundRobin selects accounts in rotation.
type RoundRobin struct {
	counter atomic.Uint64
}

func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

func (rr *RoundRobin) Select(candidates []*domain.Account) *domain.Account {
	if len(candidates) == 0 {
		return nil
	}

	idx := rr.counter.Add(1) - 1
	return candidates[idx%uint64(len(candidates))]
}

// LeastConnections selects the account with fewest active requests.
type LeastConnections struct {
	getInflight func(accountID string) int64
}

func NewLeastConnections(getInflight func(accountID string) int64) *LeastConnections {
	return &LeastConnections{getInflight: getInflight}
}

func (lc *LeastConnections) Select(candidates []*domain.Account) *domain.Account {
	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	bestCount := lc.getInflight(best.ID)
	for _, candidate := range candidates[1:] {
		count := lc.getInflight(candidate.ID)
		if count < bestCount {
			best = candidate
			bestCount = count
		}
	}
	return best
}

// Weighted selects accounts with probability proportional to MaxConcurrent.
type Weighted struct {
	counter atomic.Uint64
}

func NewWeighted() *Weighted {
	return &Weighted{}
}

func (w *Weighted) Select(candidates []*domain.Account) *domain.Account {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	totalWeight := 0
	for _, candidate := range candidates {
		weight := candidate.MaxConcurrent
		if weight <= 0 {
			weight = 100
		}
		totalWeight += weight
	}

	idx := int(w.counter.Add(1)-1) % totalWeight
	cumulative := 0
	for _, candidate := range candidates {
		weight := candidate.MaxConcurrent
		if weight <= 0 {
			weight = 100
		}
		cumulative += weight
		if idx < cumulative {
			return candidate
		}
	}

	return candidates[len(candidates)-1]
}

// Priority always picks the first candidate in the list.
// Since candidates are already filtered (enabled, breaker allows, model compatible),
// the "first" is the first healthy account in the group's account_ids order.
// This creates a primary/fallback pattern: traffic goes to account_ids[0],
// and only falls to [1], [2], etc. when earlier ones are unavailable.
type Priority struct{}

func NewPriority() *Priority {
	return &Priority{}
}

func (p *Priority) Select(candidates []*domain.Account) *domain.Account {
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0]
}
