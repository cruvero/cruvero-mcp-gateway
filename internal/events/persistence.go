package events

import (
	"context"
	"database/sql"
	"fmt"
)

const errConfigStoreKeys = "config store keys: %w"

// ConfigStore persists last-known-good control-plane config snapshots.
type ConfigStore interface {
	Save(ctx context.Context, key string, value []byte) error
	Load(ctx context.Context, key string) ([]byte, error)
	Keys(ctx context.Context) ([]string, error)
}

// PostgresConfigStore stores config snapshots in Postgres.
type PostgresConfigStore struct {
	db *sql.DB
}

// NewPostgresConfigStore creates a Postgres config store.
func NewPostgresConfigStore(db *sql.DB) *PostgresConfigStore {
	return &PostgresConfigStore{db: db}
}

// Save upserts a config value by key.
func (s *PostgresConfigStore) Save(ctx context.Context, key string, value []byte) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("config store save: store is not initialized")
	}

	const query = `
INSERT INTO config_cache (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now()
`

	if _, err := s.db.ExecContext(ctx, query, key, value); err != nil {
		return fmt.Errorf("config store save: %w", err)
	}
	return nil
}

// Load reads a config value by key.
func (s *PostgresConfigStore) Load(ctx context.Context, key string) ([]byte, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("config store load: store is not initialized")
	}

	const query = `SELECT value FROM config_cache WHERE key = $1`
	var value []byte
	if err := s.db.QueryRowContext(ctx, query, key).Scan(&value); err != nil {
		return nil, fmt.Errorf("config store load: %w", err)
	}
	return value, nil
}

// Keys lists all available config keys.
func (s *PostgresConfigStore) Keys(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("config store keys: store is not initialized")
	}

	const query = `SELECT key FROM config_cache`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf(errConfigStoreKeys, err)
	}
	defer func() { _ = rows.Close() }()

	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			return nil, fmt.Errorf(errConfigStoreKeys, scanErr)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(errConfigStoreKeys, err)
	}

	return keys, nil
}

var _ ConfigStore = (*PostgresConfigStore)(nil)
