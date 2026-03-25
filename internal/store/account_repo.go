package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type AccountRepo struct {
	db *DB
}

func NewAccountRepo(db *DB) *AccountRepo {
	return &AccountRepo{db: db}
}

func (r *AccountRepo) List(ctx context.Context) ([]domain.Account, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, provider, api_key, base_url, proxy_url, user_agent, status,
			allowed_models, model_aliases, max_concurrent,
			cb_failure_threshold, cb_success_threshold, cb_open_duration,
			extra
		FROM accounts
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		account, err := scanAccount(rows.Scan)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (r *AccountRepo) Get(ctx context.Context, id string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, provider, api_key, base_url, proxy_url, user_agent, status,
			allowed_models, model_aliases, max_concurrent,
			cb_failure_threshold, cb_success_threshold, cb_open_duration,
			extra
		FROM accounts
		WHERE id = ?`, id)

	account, err := scanAccount(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *AccountRepo) Create(ctx context.Context, a *domain.Account) error {
	allowedModels, modelAliases, extra, account := marshalAccount(a)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO accounts (
			id, name, provider, api_key, base_url, proxy_url, user_agent, status,
			allowed_models, model_aliases, max_concurrent,
			cb_failure_threshold, cb_success_threshold, cb_open_duration,
			extra
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID, account.Name, account.Provider, account.APIKey, account.BaseURL, account.ProxyURL, account.UserAgent, account.Status,
		allowedModels, modelAliases, account.MaxConcurrent,
		account.CircuitBreaker.FailureThreshold, account.CircuitBreaker.SuccessThreshold, account.CircuitBreaker.OpenDuration,
		extra,
	)
	return err
}

func (r *AccountRepo) Update(ctx context.Context, a *domain.Account) error {
	allowedModels, modelAliases, extra, account := marshalAccount(a)

	_, err := r.db.ExecContext(ctx, `
		UPDATE accounts
		SET name = ?, provider = ?, api_key = ?, base_url = ?, proxy_url = ?, user_agent = ?, status = ?,
			allowed_models = ?, model_aliases = ?, max_concurrent = ?,
			cb_failure_threshold = ?, cb_success_threshold = ?, cb_open_duration = ?,
			extra = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		account.Name, account.Provider, account.APIKey, account.BaseURL, account.ProxyURL, account.UserAgent, account.Status,
		allowedModels, modelAliases, account.MaxConcurrent,
		account.CircuitBreaker.FailureThreshold, account.CircuitBreaker.SuccessThreshold, account.CircuitBreaker.OpenDuration,
		extra, account.ID,
	)
	return err
}

func (r *AccountRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	return err
}

type GroupRepo struct {
	db *DB
}

func NewGroupRepo(db *DB) *GroupRepo {
	return &GroupRepo{db: db}
}

func (r *GroupRepo) List(ctx context.Context) ([]domain.KeyGroup, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, account_ids, allowed_models, balancer
		FROM groups
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []domain.KeyGroup
	for rows.Next() {
		group, err := scanGroup(rows.Scan)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r *GroupRepo) Get(ctx context.Context, id string) (*domain.KeyGroup, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, account_ids, allowed_models, balancer
		FROM groups
		WHERE id = ?`, id)

	group, err := scanGroup(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *GroupRepo) Create(ctx context.Context, g *domain.KeyGroup) error {
	accountIDs, allowedModels, group := marshalGroup(g)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO groups (id, name, account_ids, allowed_models, balancer)
		VALUES (?, ?, ?, ?, ?)`,
		group.ID, group.Name, accountIDs, allowedModels, group.Balancer,
	)
	return err
}

func (r *GroupRepo) Update(ctx context.Context, g *domain.KeyGroup) error {
	accountIDs, allowedModels, group := marshalGroup(g)

	_, err := r.db.ExecContext(ctx, `
		UPDATE groups
		SET name = ?, account_ids = ?, allowed_models = ?, balancer = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		group.Name, accountIDs, allowedModels, group.Balancer, group.ID,
	)
	return err
}

func (r *GroupRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM groups WHERE id = ?`, id)
	return err
}

func scanAccount(scan func(dest ...any) error) (domain.Account, error) {
	var (
		account           domain.Account
		allowedModelsJSON string
		modelAliasesJSON  string
		extraJSON         string
		failureThreshold  int
		successThreshold  int
		openDuration      string
	)

	err := scan(
		&account.ID, &account.Name, &account.Provider, &account.APIKey, &account.BaseURL, &account.ProxyURL, &account.UserAgent, &account.Status,
		&allowedModelsJSON, &modelAliasesJSON, &account.MaxConcurrent,
		&failureThreshold, &successThreshold, &openDuration,
		&extraJSON,
	)
	if err != nil {
		return domain.Account{}, err
	}

	account.Health = domain.HealthHealthy
	account.CircuitBreaker = domain.CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		SuccessThreshold: successThreshold,
		OpenDuration:     openDuration,
	}

	if err := decodeJSONString(allowedModelsJSON, &account.AllowedModels); err != nil {
		return domain.Account{}, fmt.Errorf("decode account allowed_models for %s: %w", account.ID, err)
	}
	if err := decodeJSONString(modelAliasesJSON, &account.ModelAliases); err != nil {
		return domain.Account{}, fmt.Errorf("decode account model_aliases for %s: %w", account.ID, err)
	}
	if err := decodeJSONString(extraJSON, &account.Extra); err != nil {
		return domain.Account{}, fmt.Errorf("decode account extra for %s: %w", account.ID, err)
	}

	account = normalizeAccount(account)
	return account, nil
}

func scanGroup(scan func(dest ...any) error) (domain.KeyGroup, error) {
	var (
		group             domain.KeyGroup
		accountIDsJSON    string
		allowedModelsJSON string
	)

	err := scan(&group.ID, &group.Name, &accountIDsJSON, &allowedModelsJSON, &group.Balancer)
	if err != nil {
		return domain.KeyGroup{}, err
	}
	if err := decodeJSONString(accountIDsJSON, &group.AccountIDs); err != nil {
		return domain.KeyGroup{}, fmt.Errorf("decode group account_ids for %s: %w", group.ID, err)
	}
	if err := decodeJSONString(allowedModelsJSON, &group.AllowedModels); err != nil {
		return domain.KeyGroup{}, fmt.Errorf("decode group allowed_models for %s: %w", group.ID, err)
	}

	group = normalizeGroup(group)
	return group, nil
}

func marshalAccount(a *domain.Account) (string, string, string, domain.Account) {
	account := normalizeAccount(*a)
	return mustJSON(account.AllowedModels), mustJSON(account.ModelAliases), mustJSON(account.Extra), account
}

func marshalGroup(g *domain.KeyGroup) (string, string, domain.KeyGroup) {
	group := normalizeGroup(*g)
	return mustJSON(group.AccountIDs), mustJSON(group.AllowedModels), group
}

func normalizeAccount(account domain.Account) domain.Account {
	if account.Status == "" {
		account.Status = domain.AccountEnabled
	}
	if account.Health == "" {
		account.Health = domain.HealthHealthy
	}
	if account.AllowedModels == nil {
		account.AllowedModels = []string{}
	}
	if account.ModelAliases == nil {
		account.ModelAliases = map[string]string{}
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	if account.CircuitBreaker.FailureThreshold == 0 {
		account.CircuitBreaker.FailureThreshold = 5
	}
	if account.CircuitBreaker.SuccessThreshold == 0 {
		account.CircuitBreaker.SuccessThreshold = 2
	}
	if account.CircuitBreaker.OpenDuration == "" {
		account.CircuitBreaker.OpenDuration = "60s"
	}
	return account
}

func normalizeGroup(group domain.KeyGroup) domain.KeyGroup {
	if group.AccountIDs == nil {
		group.AccountIDs = []string{}
	}
	if group.AllowedModels == nil {
		group.AllowedModels = []string{}
	}
	if group.Balancer == "" {
		group.Balancer = "round_robin"
	}
	return group
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func decodeJSONString(raw string, target any) error {
	if raw == "" {
		raw = "null"
	}
	return json.Unmarshal([]byte(raw), target)
}
