package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/lib/pq"
)

// PostgresSynonymStore is a Postgres-backed implementation of SynonymStore.
type PostgresSynonymStore struct {
	db *sql.DB
}

var _ SynonymStore = (*PostgresSynonymStore)(nil)

// NewPostgresSynonymStore creates a PostgresSynonymStore.
func NewPostgresSynonymStore(db *sql.DB) *PostgresSynonymStore {
	return &PostgresSynonymStore{db: db}
}

// List returns all synonym entries ordered by term.
func (s *PostgresSynonymStore) List(ctx context.Context) ([]types.SynonymEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT term, synonyms, created_at, updated_at FROM search_synonyms ORDER BY term`)
	if err != nil {
		return nil, fmt.Errorf("synonym store: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []types.SynonymEntry
	for rows.Next() {
		var e types.SynonymEntry
		if err := rows.Scan(&e.Term, pq.Array(&e.Synonyms), &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("synonym store: scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Upsert inserts or updates a synonym entry.
func (s *PostgresSynonymStore) Upsert(ctx context.Context, entry *types.SynonymEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO search_synonyms (term, synonyms, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW())
		 ON CONFLICT (term) DO UPDATE SET synonyms = $2, updated_at = NOW()`,
		entry.Term, pq.Array(entry.Synonyms))
	if err != nil {
		return fmt.Errorf("synonym store: upsert: %w", err)
	}
	return nil
}

// Delete removes a synonym entry by term.
func (s *PostgresSynonymStore) Delete(ctx context.Context, term string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM search_synonyms WHERE term = $1`, term)
	if err != nil {
		return fmt.Errorf("synonym store: delete: %w", err)
	}
	return nil
}
