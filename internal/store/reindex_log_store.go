package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// PostgresReindexLogStore is a Postgres-backed implementation of ReindexLogStore.
type PostgresReindexLogStore struct {
	db *sql.DB
}

var _ ReindexLogStore = (*PostgresReindexLogStore)(nil)

// NewPostgresReindexLogStore creates a PostgresReindexLogStore.
func NewPostgresReindexLogStore(db *sql.DB) *PostgresReindexLogStore {
	return &PostgresReindexLogStore{db: db}
}

// Log appends a reindex log entry.
func (s *PostgresReindexLogStore) Log(ctx context.Context, entry *types.ReindexLogEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO search_reindex_log (engine, trigger, tools_count, duration_ms, status, error)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.Engine, entry.Trigger, entry.ToolsCount, entry.DurationMs, entry.Status, nullableString(entry.Error))
	if err != nil {
		return fmt.Errorf("reindex log store: log: %w", err)
	}
	return nil
}

// Recent returns the most recent reindex log entries.
func (s *PostgresReindexLogStore) Recent(ctx context.Context, limit int) ([]types.ReindexLogEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, engine, trigger, tools_count, duration_ms, status, COALESCE(error, ''), created_at
		 FROM search_reindex_log
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("reindex log store: recent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []types.ReindexLogEntry
	for rows.Next() {
		var e types.ReindexLogEntry
		if err := rows.Scan(&e.ID, &e.Engine, &e.Trigger, &e.ToolsCount, &e.DurationMs, &e.Status, &e.Error, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("reindex log store: scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
