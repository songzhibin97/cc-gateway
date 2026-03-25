package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the SQL database connection.
type DB struct {
	*sql.DB
}

// Open opens or creates the SQLite database at the given path.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite supports concurrent reads but only one writer.
	// Limit to 1 open connection to serialize all writes.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return &DB{DB: db}, nil
}

func migrate(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT 'anthropic',
			api_key TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			proxy_url TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'enabled',
			allowed_models TEXT NOT NULL DEFAULT '[]',
			model_aliases TEXT NOT NULL DEFAULT '{}',
			max_concurrent INTEGER NOT NULL DEFAULT 0,
			cb_failure_threshold INTEGER NOT NULL DEFAULT 5,
			cb_success_threshold INTEGER NOT NULL DEFAULT 2,
			cb_open_duration TEXT NOT NULL DEFAULT '60s',
			extra TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			account_ids TEXT NOT NULL DEFAULT '[]',
			allowed_models TEXT NOT NULL DEFAULT '[]',
			balancer TEXT NOT NULL DEFAULT 'round_robin',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			key_hash TEXT NOT NULL UNIQUE,
			key_hint TEXT NOT NULL DEFAULT '',
			group_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'enabled',
			allowed_models TEXT NOT NULL DEFAULT '[]',
			max_concurrent INTEGER NOT NULL DEFAULT 0,
			max_input_tokens_monthly INTEGER NOT NULL DEFAULT 0,
			max_output_tokens_monthly INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_key_usage (
			key_id TEXT NOT NULL,
			month TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (key_id, month)
		)`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			key_id TEXT NOT NULL DEFAULT '',
			key_hint TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			account_name TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			model_requested TEXT NOT NULL DEFAULT '',
			model_actual TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			thinking_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			cost_usd REAL NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			stop_reason TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 200
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_created_at ON request_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_key_id ON request_logs(key_id)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_account_id ON request_logs(account_id)`,
		`CREATE TABLE IF NOT EXISTS request_payloads (
			log_id INTEGER PRIMARY KEY REFERENCES request_logs(id),
			request_body TEXT NOT NULL DEFAULT '',
			response_body TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_payloads_created_at ON request_payloads(created_at)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	return nil
}
