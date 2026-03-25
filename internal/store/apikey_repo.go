package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/songzhibin97/cc-gateway/internal/domain"
)

type APIKeyRepo struct {
	db *DB
}

func NewAPIKeyRepo(db *DB) *APIKeyRepo {
	return &APIKeyRepo{db: db}
}

func (r *APIKeyRepo) List(ctx context.Context) ([]domain.ExternalAPIKey, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, key_hash, key_hint, group_id, status, allowed_models, max_concurrent,
			max_input_tokens_monthly, max_output_tokens_monthly, created_at
		FROM api_keys
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []domain.ExternalAPIKey
	for rows.Next() {
		key, err := scanAPIKey(rows.Scan)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *APIKeyRepo) Get(ctx context.Context, id string) (*domain.ExternalAPIKey, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, key_hash, key_hint, group_id, status, allowed_models, max_concurrent,
			max_input_tokens_monthly, max_output_tokens_monthly, created_at
		FROM api_keys
		WHERE id = ?`, id)

	key, err := scanAPIKey(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *APIKeyRepo) Create(ctx context.Context, key *domain.ExternalAPIKey, rawKey string) error {
	stored := normalizeAPIKey(*key)
	stored.KeyHash = hashRawKey(rawKey)
	stored.KeyHint = keyHint(rawKey)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_keys (
			id, key_hash, key_hint, group_id, status, allowed_models, max_concurrent,
			max_input_tokens_monthly, max_output_tokens_monthly
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stored.ID, stored.KeyHash, stored.KeyHint, stored.GroupID, stored.Status,
		mustJSON(stored.AllowedModels), stored.MaxConcurrent, stored.MaxInputTokens, stored.MaxOutputTokens,
	)
	return err
}

func (r *APIKeyRepo) Update(ctx context.Context, key *domain.ExternalAPIKey) error {
	stored := normalizeAPIKey(*key)

	_, err := r.db.ExecContext(ctx, `
		UPDATE api_keys
		SET group_id = ?, status = ?, allowed_models = ?, max_concurrent = ?,
			max_input_tokens_monthly = ?, max_output_tokens_monthly = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		stored.GroupID, stored.Status, mustJSON(stored.AllowedModels), stored.MaxConcurrent,
		stored.MaxInputTokens, stored.MaxOutputTokens, stored.ID,
	)
	return err
}

func (r *APIKeyRepo) Rotate(ctx context.Context, id, rawKey string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE api_keys
		SET key_hash = ?, key_hint = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		hashRawKey(rawKey), keyHint(rawKey), id,
	)
	return err
}

func (r *APIKeyRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

func (r *APIKeyRepo) FindByHash(ctx context.Context, hash string) (*domain.ExternalAPIKey, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, key_hash, key_hint, group_id, status, allowed_models, max_concurrent,
			max_input_tokens_monthly, max_output_tokens_monthly, created_at
		FROM api_keys
		WHERE key_hash = ?`, hash)

	key, err := scanAPIKey(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// GetUsage returns the usage for a key in the given month (format "2026-03").
func (r *APIKeyRepo) GetUsage(ctx context.Context, keyID, month string) (inputTokens, outputTokens int64, err error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT input_tokens, output_tokens
		FROM api_key_usage
		WHERE key_id = ? AND month = ?`, keyID, month)

	err = row.Scan(&inputTokens, &outputTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return inputTokens, outputTokens, nil
}

// IncrementUsage atomically adds tokens to the key's usage for the given month.
func (r *APIKeyRepo) IncrementUsage(ctx context.Context, keyID, month string, inputTokens, outputTokens int) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_key_usage (key_id, month, input_tokens, output_tokens, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key_id, month) DO UPDATE SET
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			updated_at = CURRENT_TIMESTAMP`,
		keyID, month, inputTokens, outputTokens,
	)
	return err
}

// GetAllUsage returns usage for all keys in the given month.
func (r *APIKeyRepo) GetAllUsage(ctx context.Context, month string) (map[string][2]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT key_id, input_tokens, output_tokens
		FROM api_key_usage
		WHERE month = ?`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	usage := make(map[string][2]int64)
	for rows.Next() {
		var (
			keyID        string
			inputTokens  int64
			outputTokens int64
		)
		if err := rows.Scan(&keyID, &inputTokens, &outputTokens); err != nil {
			return nil, err
		}
		usage[keyID] = [2]int64{inputTokens, outputTokens}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return usage, nil
}

func scanAPIKey(scan func(dest ...any) error) (domain.ExternalAPIKey, error) {
	var (
		key               domain.ExternalAPIKey
		allowedModelsJSON string
		createdAt         time.Time
	)

	err := scan(
		&key.ID, &key.KeyHash, &key.KeyHint, &key.GroupID, &key.Status, &allowedModelsJSON, &key.MaxConcurrent,
		&key.MaxInputTokens, &key.MaxOutputTokens, &createdAt,
	)
	if err != nil {
		return domain.ExternalAPIKey{}, err
	}
	if err := decodeJSONString(allowedModelsJSON, &key.AllowedModels); err != nil {
		return domain.ExternalAPIKey{}, fmt.Errorf("decode api key allowed_models for %s: %w", key.ID, err)
	}
	key.CreatedAt = createdAt
	key = normalizeAPIKey(key)
	return key, nil
}

func normalizeAPIKey(key domain.ExternalAPIKey) domain.ExternalAPIKey {
	if key.Status == "" {
		key.Status = domain.AccountEnabled
	}
	if key.AllowedModels == nil {
		key.AllowedModels = []string{}
	}
	return key
}

func hashRawKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func keyHint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) <= 4 {
		return trimmed
	}
	return trimmed[len(trimmed)-4:]
}
