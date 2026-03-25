package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/domain"
	"github.com/songzhibin97/cc-gateway/internal/store"
)

var (
	ErrInvalidAPIKey    = errors.New("invalid api key")
	ErrKeyDisabled      = errors.New("api key is disabled")
	ErrModelNotAllowed  = errors.New("model not allowed for this api key")
	ErrConcurrencyLimit = errors.New("concurrency limit exceeded")
	ErrUsageLimit       = errors.New("monthly usage limit exceeded")
)

// KeyStore manages API keys, concurrency semaphores, and usage tracking.
type KeyStore struct {
	mu sync.RWMutex

	keys   map[string]*keyEntry // keyed by SHA256 hash of raw key
	groups map[string]*domain.KeyGroup

	// Per-account concurrency tracking.
	accountSems map[string]*semaphore // keyed by account ID
	repo        *store.APIKeyRepo
	month       string
}

type keyEntry struct {
	Key *domain.ExternalAPIKey

	// Concurrency tracking.
	inflight atomic.Int64

	// Usage tracking. This is in-memory for now and resets on restart.
	usedInput  atomic.Int64
	usedOutput atomic.Int64
}

type semaphore struct {
	limit    int
	inflight atomic.Int64
}

func (s *semaphore) Acquire() bool {
	if s.limit <= 0 {
		return true
	}

	for {
		current := s.inflight.Load()
		if current >= int64(s.limit) {
			return false
		}
		if s.inflight.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func (s *semaphore) Release() {
	s.inflight.Add(-1)
}

func NewKeyStore(repo *store.APIKeyRepo, groups []domain.KeyGroup, accounts []domain.Account) *KeyStore {
	var keys []domain.ExternalAPIKey
	if repo != nil {
		var err error
		keys, err = repo.List(context.Background())
		if err != nil {
			keys = nil
		}
	}
	return NewKeyStoreFromData(keys, groups, accounts, repo)
}

func NewKeyStoreFromData(keys []domain.ExternalAPIKey, groups []domain.KeyGroup, accounts []domain.Account, repo *store.APIKeyRepo) *KeyStore {
	ks := &KeyStore{
		keys:        make(map[string]*keyEntry, len(keys)),
		groups:      make(map[string]*domain.KeyGroup, len(groups)),
		accountSems: make(map[string]*semaphore, len(accounts)),
		repo:        repo,
		month:       currentMonth(),
	}
	ks.reloadLocked(keys, groups, accounts)
	return ks
}

func (ks *KeyStore) Reload(keys []domain.ExternalAPIKey, groups []domain.KeyGroup, accounts []domain.Account) {
	if ks == nil {
		return
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.reloadLocked(keys, groups, accounts)
}

func (ks *KeyStore) HasKeys() bool {
	if ks == nil {
		return false
	}

	ks.mu.RLock()
	defer ks.mu.RUnlock()

	return len(ks.keys) > 0
}

// Authenticate validates the API key and returns the key entry and its group.
func (ks *KeyStore) Authenticate(rawKey string) (*domain.ExternalAPIKey, *domain.KeyGroup, error) {
	hash := hashKey(rawKey)

	ks.mu.RLock()
	entry, ok := ks.keys[hash]
	ks.mu.RUnlock()
	if !ok {
		return nil, nil, ErrInvalidAPIKey
	}

	if entry.Key.Status != domain.AccountEnabled {
		return nil, nil, ErrKeyDisabled
	}

	group, ok := ks.groups[entry.Key.GroupID]
	if !ok {
		return nil, nil, ErrInvalidAPIKey
	}

	return entry.Key, group, nil
}

// CheckModelAllowed checks if the key is allowed to use the given model.
func (ks *KeyStore) CheckModelAllowed(key *domain.ExternalAPIKey, model string) error {
	if len(key.AllowedModels) == 0 {
		return nil
	}
	for _, allowed := range key.AllowedModels {
		if allowed == model {
			return nil
		}
	}
	return ErrModelNotAllowed
}

// CheckUsageLimit checks if the key has exceeded monthly usage limits.
func (ks *KeyStore) CheckUsageLimit(key *domain.ExternalAPIKey) error {
	ks.syncMonth()

	entry, err := ks.lookupKeyEntry(key)
	if err != nil {
		return err
	}

	if key.MaxInputTokens > 0 && entry.usedInput.Load() >= key.MaxInputTokens {
		return ErrUsageLimit
	}
	if key.MaxOutputTokens > 0 && entry.usedOutput.Load() >= key.MaxOutputTokens {
		return ErrUsageLimit
	}
	return nil
}

// AcquireKeyConcurrency tries to acquire a concurrency slot for the key.
func (ks *KeyStore) AcquireKeyConcurrency(key *domain.ExternalAPIKey) (func(), error) {
	if key.MaxConcurrent <= 0 {
		return func() {}, nil
	}

	entry, err := ks.lookupKeyEntry(key)
	if err != nil {
		return nil, err
	}

	for {
		current := entry.inflight.Load()
		if current >= int64(key.MaxConcurrent) {
			return nil, ErrConcurrencyLimit
		}
		if entry.inflight.CompareAndSwap(current, current+1) {
			return func() { entry.inflight.Add(-1) }, nil
		}
	}
}

// AcquireAccountConcurrency tries to acquire a concurrency slot for the account.
func (ks *KeyStore) AcquireAccountConcurrency(accountID string) (func(), error) {
	ks.mu.RLock()
	sem, ok := ks.accountSems[accountID]
	ks.mu.RUnlock()

	if !ok || sem.limit <= 0 {
		return func() {}, nil
	}

	if !sem.Acquire() {
		return nil, ErrConcurrencyLimit
	}

	return func() { sem.Release() }, nil
}

// RecordUsage adds token usage for a key.
func (ks *KeyStore) RecordUsage(key *domain.ExternalAPIKey, inputTokens, outputTokens int) {
	month := ks.syncMonth()

	entry, err := ks.lookupKeyEntry(key)
	if err != nil {
		return
	}

	entry.usedInput.Add(int64(inputTokens))
	entry.usedOutput.Add(int64(outputTokens))

	if ks.repo == nil {
		return
	}

	keyID := key.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := ks.repo.IncrementUsage(ctx, keyID, month, inputTokens, outputTokens); err != nil {
			slog.Warn("persist api key usage failed",
				slog.String("key_id", keyID),
				slog.String("month", month),
				slog.String("error", err.Error()),
			)
		}
	}()
}

func (ks *KeyStore) lookupKeyEntry(key *domain.ExternalAPIKey) (*keyEntry, error) {
	if ks == nil || key == nil {
		return nil, ErrInvalidAPIKey
	}

	ks.mu.RLock()
	hash := key.KeyHash
	if hash == "" {
		hash = hashKey(key.Key)
	}
	entry, ok := ks.keys[hash]
	ks.mu.RUnlock()
	if !ok {
		return nil, ErrInvalidAPIKey
	}

	return entry, nil
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func currentMonth() string {
	return time.Now().Format("2006-01")
}

func (ks *KeyStore) syncMonth() string {
	now := currentMonth()
	if ks == nil {
		return now
	}

	ks.mu.RLock()
	current := ks.month
	ks.mu.RUnlock()
	if now == current {
		return now
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.month == now {
		return now
	}

	ks.month = now
	for _, entry := range ks.keys {
		entry.usedInput.Store(0)
		entry.usedOutput.Store(0)
	}

	return now
}

func (ks *KeyStore) reloadLocked(keys []domain.ExternalAPIKey, groups []domain.KeyGroup, accounts []domain.Account) {
	existingKeys := ks.keys
	existingSems := ks.accountSems
	month := currentMonth()
	usageByKeyID := map[string][2]int64{}

	if ks.repo != nil {
		usage, err := ks.repo.GetAllUsage(context.Background(), month)
		if err != nil {
			slog.Warn("load api key usage failed",
				slog.String("month", month),
				slog.String("error", err.Error()),
			)
		} else {
			usageByKeyID = usage
		}
	}

	ks.keys = make(map[string]*keyEntry, len(keys))
	ks.groups = make(map[string]*domain.KeyGroup, len(groups))
	ks.accountSems = make(map[string]*semaphore, len(accounts))
	ks.month = month

	for i := range groups {
		g := &groups[i]
		ks.groups[g.ID] = g
	}

	for i := range keys {
		keyCopy := keys[i]
		hash := keyCopy.KeyHash
		if hash == "" && keyCopy.Key != "" {
			hash = hashKey(keyCopy.Key)
			keyCopy.KeyHash = hash
		}

		if existing, ok := existingKeys[hash]; ok {
			existing.Key = &keyCopy
			if usage, ok := usageByKeyID[keyCopy.ID]; ok {
				existing.usedInput.Store(usage[0])
				existing.usedOutput.Store(usage[1])
			} else {
				existing.usedInput.Store(0)
				existing.usedOutput.Store(0)
			}
			ks.keys[hash] = existing
			continue
		}

		entry := &keyEntry{Key: &keyCopy}
		if usage, ok := usageByKeyID[keyCopy.ID]; ok {
			entry.usedInput.Store(usage[0])
			entry.usedOutput.Store(usage[1])
		}
		ks.keys[hash] = entry
	}

	for i := range accounts {
		a := &accounts[i]
		if existing, ok := existingSems[a.ID]; ok {
			existing.limit = a.MaxConcurrent
			ks.accountSems[a.ID] = existing
			continue
		}
		ks.accountSems[a.ID] = &semaphore{limit: a.MaxConcurrent}
	}
}
