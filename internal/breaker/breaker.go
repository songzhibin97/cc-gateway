package breaker

import (
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

type Config struct {
	FailureThreshold int
	SuccessThreshold int
	OpenDuration     time.Duration
}

type Breaker struct {
	mu                   sync.Mutex
	state                State
	config               Config
	consecutiveFailures  int
	consecutiveSuccesses int
	openedAt             time.Time
	lastFailureAt        time.Time
}

func New(cfg Config) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 60 * time.Second
	}

	return &Breaker{
		state:  StateClosed,
		config: cfg,
	}
}

// Allow reports whether the request should be allowed through the breaker.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(b.openedAt) >= b.config.OpenDuration {
			b.state = StateHalfOpen
			b.consecutiveSuccesses = 0
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case StateClosed:
		b.consecutiveFailures = 0
	case StateHalfOpen:
		b.consecutiveSuccesses++
		if b.consecutiveSuccesses >= b.config.SuccessThreshold {
			b.state = StateClosed
			b.consecutiveFailures = 0
			b.consecutiveSuccesses = 0
		}
	}
}

func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	b.lastFailureAt = now

	switch b.state {
	case StateClosed:
		b.consecutiveFailures++
		if b.consecutiveFailures >= b.config.FailureThreshold {
			b.state = StateOpen
			b.openedAt = now
		}
	case StateHalfOpen:
		b.state = StateOpen
		b.openedAt = now
		b.consecutiveSuccesses = 0
	}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = StateClosed
	b.consecutiveFailures = 0
	b.consecutiveSuccesses = 0
}
