package store

import (
	"context"
	"database/sql"
	"time"
)

type RequestLog struct {
	ID               int64
	CreatedAt        time.Time
	KeyID            string
	KeyHint          string
	AccountID        string
	AccountName      string
	Provider         string
	ModelRequested   string
	ModelActual      string
	InputTokens      int
	OutputTokens     int
	ThinkingTokens   int
	CacheReadTokens  int
	CacheWriteTokens int
	CostUSD          float64
	LatencyMs        int64
	StopReason       string
	Error            string
	StatusCode       int
}

type RequestPayload struct {
	LogID        int64
	RequestBody  string
	ResponseBody string
}

type LogRepo struct {
	db *DB
}

func NewLogRepo(db *DB) *LogRepo {
	return &LogRepo{db: db}
}

// InsertLog inserts a request log and returns its ID.
func (r *LogRepo) InsertLog(ctx context.Context, log *RequestLog) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO request_logs (key_id, key_hint, account_id, account_name, provider, model_requested, model_actual, input_tokens, output_tokens, thinking_tokens, cache_read_tokens, cache_write_tokens, cost_usd, latency_ms, stop_reason, error, status_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.KeyID, log.KeyHint, log.AccountID, log.AccountName, log.Provider,
		log.ModelRequested, log.ModelActual, log.InputTokens, log.OutputTokens,
		log.ThinkingTokens, log.CacheReadTokens, log.CacheWriteTokens,
		log.CostUSD, log.LatencyMs, log.StopReason, log.Error, log.StatusCode,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// InsertPayload inserts request/response bodies.
func (r *LogRepo) InsertPayload(ctx context.Context, payload *RequestPayload) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO request_payloads (log_id, request_body, response_body) VALUES (?, ?, ?)`,
		payload.LogID, payload.RequestBody, payload.ResponseBody,
	)
	return err
}

// QueryLogs queries request logs with optional filters.
func (r *LogRepo) QueryLogs(ctx context.Context, filter LogFilter) ([]RequestLog, error) {
	query := `SELECT id, created_at, key_id, key_hint, account_id, account_name, provider, model_requested, model_actual, input_tokens, output_tokens, thinking_tokens, cache_read_tokens, cache_write_tokens, cost_usd, latency_ms, stop_reason, error, status_code FROM request_logs WHERE 1=1`
	var args []any

	if filter.KeyID != "" {
		query += " AND key_id = ?"
		args = append(args, filter.KeyID)
	}
	if filter.AccountID != "" {
		query += " AND account_id = ?"
		args = append(args, filter.AccountID)
	}
	if filter.Model != "" {
		query += " AND (model_requested = ? OR model_actual = ?)"
		args = append(args, filter.Model, filter.Model)
	}
	if !filter.From.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, filter.From)
	}
	if !filter.To.IsZero() {
		query += " AND created_at <= ?"
		args = append(args, filter.To)
	}

	query += " ORDER BY id DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	} else {
		query += " LIMIT 100"
	}

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		err := rows.Scan(&l.ID, &l.CreatedAt, &l.KeyID, &l.KeyHint, &l.AccountID, &l.AccountName,
			&l.Provider, &l.ModelRequested, &l.ModelActual, &l.InputTokens, &l.OutputTokens,
			&l.ThinkingTokens, &l.CacheReadTokens, &l.CacheWriteTokens, &l.CostUSD,
			&l.LatencyMs, &l.StopReason, &l.Error, &l.StatusCode)
		if err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

type LogFilter struct {
	KeyID     string
	AccountID string
	Model     string
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
}

// GetPayload gets the payload for a log entry.
func (r *LogRepo) GetPayload(ctx context.Context, logID int64) (*RequestPayload, error) {
	var p RequestPayload
	err := r.db.QueryRowContext(ctx,
		`SELECT log_id, request_body, response_body FROM request_payloads WHERE log_id = ?`, logID,
	).Scan(&p.LogID, &p.RequestBody, &p.ResponseBody)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &p, err
}

// CostStats represents aggregated cost statistics.
type CostStats struct {
	GroupBy      string  `json:"group_by"`
	GroupValue   string  `json:"group_value"`
	TotalCost    float64 `json:"total_cost_usd"`
	TotalInput   int64   `json:"total_input_tokens"`
	TotalOutput  int64   `json:"total_output_tokens"`
	RequestCount int64   `json:"request_count"`
}

// QueryCostStats returns aggregated cost stats grouped by the given field.
func (r *LogRepo) QueryCostStats(ctx context.Context, groupBy string, from, to time.Time) ([]CostStats, error) {
	var column string
	switch groupBy {
	case "key":
		column = "key_id"
	case "account":
		column = "account_id"
	case "model":
		column = "model_requested"
	default:
		column = "account_id"
	}

	query := `SELECT ` + column + `, SUM(cost_usd), SUM(input_tokens), SUM(output_tokens), COUNT(*) FROM request_logs WHERE 1=1`
	var args []any

	if !from.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, from)
	}
	if !to.IsZero() {
		query += " AND created_at <= ?"
		args = append(args, to)
	}

	query += ` GROUP BY ` + column + ` ORDER BY SUM(cost_usd) DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []CostStats
	for rows.Next() {
		var s CostStats
		s.GroupBy = groupBy
		err := rows.Scan(&s.GroupValue, &s.TotalCost, &s.TotalInput, &s.TotalOutput, &s.RequestCount)
		if err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// CleanPayloads deletes payloads older than the given number of days.
func (r *LogRepo) CleanPayloads(ctx context.Context, retentionDays int) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM request_payloads WHERE created_at < datetime('now', '-' || ? || ' days')`,
		retentionDays,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
