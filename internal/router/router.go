package router

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/breaker"
	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/internal/metrics"
)

var (
	ErrNoHealthyAccount = errors.New("no healthy account available for model")
	ErrNoGroup          = errors.New("group not found")
)

type Router struct {
	mu              sync.RWMutex
	accounts        map[string]*domain.Account
	groups          map[string]*domain.KeyGroup
	breakers        map[string]*breaker.Breaker
	balancers       map[string]Balancer
	inflightCounts  map[string]*atomic.Int64
	defaultBalancer Balancer
	logger          *slog.Logger
}

func New(accounts []domain.Account, groups []domain.KeyGroup, logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}

	r := &Router{
		accounts:       make(map[string]*domain.Account, len(accounts)),
		groups:         make(map[string]*domain.KeyGroup, len(groups)),
		breakers:       make(map[string]*breaker.Breaker, len(accounts)),
		balancers:      make(map[string]Balancer, len(groups)),
		inflightCounts: make(map[string]*atomic.Int64, len(accounts)),
		logger:         logger,
	}
	r.defaultBalancer = r.createBalancer("")

	r.reloadLocked(accounts, groups)

	return r
}

func (r *Router) Reload(accounts []domain.Account, groups []domain.KeyGroup) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reloadLocked(accounts, groups)
}

func (r *Router) SelectAccount(groupID string, model string) (*domain.Account, error) {
	return r.SelectAccountExcept(groupID, model, nil)
}

func (r *Router) SelectAccountExcept(groupID string, model string, excluded map[string]struct{}) (*domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	group, ok := r.groups[groupID]
	if !ok {
		return nil, ErrNoGroup
	}

	candidates := r.groupCandidates(group, model, excluded)
	if len(candidates) == 0 {
		return nil, ErrNoHealthyAccount
	}

	balancer := r.balancers[groupID]
	if balancer == nil {
		return candidates[0], nil
	}

	selected := balancer.Select(candidates)
	if selected == nil {
		return nil, ErrNoHealthyAccount
	}

	return selected, nil
}

func (r *Router) SelectAccountFromAll(model string) (*domain.Account, error) {
	return r.selectAccountFromAll(model, nil)
}

func (r *Router) SelectAccountFromAllExcept(model string, excluded map[string]struct{}) (*domain.Account, error) {
	return r.selectAccountFromAll(model, excluded)
}

func (r *Router) selectAccountFromAll(model string, excluded map[string]struct{}) (*domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	candidates := r.allCandidates(model, excluded)
	if len(candidates) == 0 {
		return nil, ErrNoHealthyAccount
	}

	selected := r.defaultBalancer.Select(candidates)
	if selected == nil {
		return nil, ErrNoHealthyAccount
	}

	return selected, nil
}

func (r *Router) RecordSuccess(accountID string) {
	r.mu.RLock()
	cb, ok := r.breakers[accountID]
	r.mu.RUnlock()
	if ok {
		cb.RecordSuccess()
		metrics.CircuitBreakerState.WithLabelValues(accountID).Set(float64(cb.State()))
	}
}

func (r *Router) RecordFailure(accountID string) {
	r.mu.RLock()
	cb, ok := r.breakers[accountID]
	r.mu.RUnlock()
	if ok {
		cb.RecordFailure()
		metrics.CircuitBreakerState.WithLabelValues(accountID).Set(float64(cb.State()))
		r.logger.Warn("circuit breaker: recorded failure",
			slog.String("account_id", accountID),
			slog.String("state", cb.State().String()),
		)
	}
}

func (r *Router) ResetBreaker(accountID string) {
	r.mu.RLock()
	cb, ok := r.breakers[accountID]
	r.mu.RUnlock()
	if ok {
		cb.Reset()
		metrics.CircuitBreakerState.WithLabelValues(accountID).Set(float64(cb.State()))
		r.logger.Info("circuit breaker: reset", slog.String("account_id", accountID))
	}
}

func (r *Router) GetAccount(id string) (*domain.Account, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.accounts[id]
	return a, ok
}


func (r *Router) GetBreakerState(accountID string) breaker.State {
	r.mu.RLock()
	cb, ok := r.breakers[accountID]
	r.mu.RUnlock()
	if !ok {
		return breaker.StateClosed
	}
	return cb.State()
}

func (r *Router) IncrementInflight(accountID string) {
	if counter := r.getOrCreateInflightCounter(accountID); counter != nil {
		counter.Add(1)
	}
}

func (r *Router) DecrementInflight(accountID string) {
	r.mu.RLock()
	counter := r.inflightCounts[accountID]
	r.mu.RUnlock()
	if counter != nil {
		counter.Add(-1)
	}
}

func (r *Router) getAccountInflight(accountID string) int64 {
	r.mu.RLock()
	counter := r.inflightCounts[accountID]
	r.mu.RUnlock()
	if counter == nil {
		return 0
	}
	return counter.Load()
}

func (r *Router) reloadLocked(accounts []domain.Account, groups []domain.KeyGroup) {
	existingBreakers := r.breakers
	existingInflightCounts := r.inflightCounts

	r.accounts = make(map[string]*domain.Account, len(accounts))
	r.groups = make(map[string]*domain.KeyGroup, len(groups))
	r.breakers = make(map[string]*breaker.Breaker, len(accounts))
	r.balancers = make(map[string]Balancer, len(groups))
	r.inflightCounts = make(map[string]*atomic.Int64, len(accounts))

	for i := range accounts {
		a := &accounts[i]
		r.accounts[a.ID] = a
		if counter, ok := existingInflightCounts[a.ID]; ok {
			r.inflightCounts[a.ID] = counter
		} else {
			r.inflightCounts[a.ID] = &atomic.Int64{}
		}

		if cb, ok := existingBreakers[a.ID]; ok {
			r.breakers[a.ID] = cb
			metrics.CircuitBreakerState.WithLabelValues(a.ID).Set(float64(cb.State()))
			continue
		}

		openDur, err := time.ParseDuration(a.CircuitBreaker.OpenDuration)
		if err != nil {
			openDur = 60 * time.Second
		}

		r.breakers[a.ID] = breaker.New(breaker.Config{
			FailureThreshold: a.CircuitBreaker.FailureThreshold,
			SuccessThreshold: a.CircuitBreaker.SuccessThreshold,
			OpenDuration:     openDur,
		})
		metrics.CircuitBreakerState.WithLabelValues(a.ID).Set(float64(r.breakers[a.ID].State()))
	}

	for i := range groups {
		g := &groups[i]
		r.groups[g.ID] = g
		r.balancers[g.ID] = r.createBalancer(g.Balancer)
	}
}

func (r *Router) groupCandidates(group *domain.KeyGroup, model string, excluded map[string]struct{}) []*domain.Account {
	candidates := make([]*domain.Account, 0, len(group.AccountIDs))
	for _, accountID := range group.AccountIDs {
		a, ok := r.accountCandidate(accountID, model, excluded)
		if ok {
			candidates = append(candidates, a)
		}
	}
	return candidates
}

func (r *Router) allCandidates(model string, excluded map[string]struct{}) []*domain.Account {
	candidates := make([]*domain.Account, 0, len(r.accounts))
	seen := make(map[string]bool, len(r.accounts))

	for _, group := range r.groups {
		for _, accountID := range group.AccountIDs {
			if seen[accountID] {
				continue
			}
			seen[accountID] = true

			a, ok := r.accountCandidate(accountID, model, excluded)
			if ok {
				candidates = append(candidates, a)
			}
		}
	}

	for accountID, account := range r.accounts {
		if seen[accountID] {
			continue
		}
		a, ok := r.accountCandidate(account.ID, model, excluded)
		if ok {
			candidates = append(candidates, a)
		}
	}

	return candidates
}

func (r *Router) accountCandidate(accountID string, model string, excluded map[string]struct{}) (*domain.Account, bool) {
	if _, skip := excluded[accountID]; skip {
		return nil, false
	}

	a, ok := r.accounts[accountID]
	if !ok {
		return nil, false
	}
	if a.Status != domain.AccountEnabled {
		return nil, false
	}

	cb, ok := r.breakers[accountID]
	if ok && !cb.Allow() {
		return nil, false
	}
	if !a.CanServeModel(model) {
		return nil, false
	}

	return a, true
}

func (r *Router) createBalancer(strategy string) Balancer {
	switch strategy {
	case "least_connections":
		return NewLeastConnections(r.getAccountInflight)
	case "priority":
		return NewPriority()
	case "weighted":
		return NewWeighted()
	case "", "round_robin":
		return NewRoundRobin()
	default:
		return NewRoundRobin()
	}
}

func (r *Router) getOrCreateInflightCounter(accountID string) *atomic.Int64 {
	r.mu.RLock()
	counter := r.inflightCounts[accountID]
	r.mu.RUnlock()
	if counter != nil {
		return counter
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if counter = r.inflightCounts[accountID]; counter == nil {
		counter = &atomic.Int64{}
		r.inflightCounts[accountID] = counter
	}
	return counter
}
