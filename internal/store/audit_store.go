package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const errAuditStore = "audit store: %w"

const auditColumns = "id, event_type, client_id, server_name, details, created_at"

// PostgresAuditStore is a Postgres-backed implementation of AuditStore.
type PostgresAuditStore struct {
	db *sql.DB
}

var _ AuditStore = (*PostgresAuditStore)(nil)

// NewPostgresAuditStore creates a PostgresAuditStore.
func NewPostgresAuditStore(db *sql.DB) *PostgresAuditStore {
	return &PostgresAuditStore{db: db}
}

// Log appends a new audit entry.
func (s *PostgresAuditStore) Log(ctx context.Context, entry *types.AuditEntry) error {
	if entry == nil {
		return fmt.Errorf("audit store: entry is nil")
	}

	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		return fmt.Errorf("audit store: marshal details: %w", err)
	}

	const query = `
INSERT INTO audit_log (event_type, client_id, server_name, details)
VALUES ($1, $2, $3, $4)
`

	if _, err := s.db.ExecContext(ctx, query, entry.EventType, entry.ClientID, entry.ServerName, detailsJSON); err != nil {
		return fmt.Errorf(errAuditStore,err)
	}
	return nil
}

// Query returns audit entries matching filter criteria.
func (s *PostgresAuditStore) Query(ctx context.Context, filter types.AuditFilter) ([]types.AuditEntry, error) {
	const query = `
SELECT ` + auditColumns + `
FROM audit_log
WHERE ($1::text IS NULL OR event_type = $1)
  AND ($2::text IS NULL OR client_id = $2)
  AND ($3::text IS NULL OR server_name = $3)
  AND ($4::timestamptz IS NULL OR created_at >= $4::timestamptz)
  AND ($5::timestamptz IS NULL OR created_at <= $5::timestamptz)
ORDER BY created_at DESC
LIMIT $6
OFFSET $7
`

	limit := int64(9223372036854775807)
	if filter.Limit > 0 {
		limit = int64(filter.Limit)
	}
	offset := int64(0)
	if filter.Offset > 0 {
		offset = int64(filter.Offset)
	}

	rows, err := s.db.QueryContext(
		ctx,
		query,
		optionalString(filter.EventType),
		optionalString(filter.ClientID),
		optionalString(filter.ServerName),
		optionalTime(filter.Since),
		optionalTime(filter.Until),
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf(errAuditStore,err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]types.AuditEntry, 0)
	for rows.Next() {
		entry, scanErr := scanAuditEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf(errAuditStore,scanErr)
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(errAuditStore,err)
	}

	return entries, nil
}

func optionalString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func optionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

type auditScanner interface {
	Scan(dest ...any) error
}

func scanAuditEntry(scanner auditScanner) (*types.AuditEntry, error) {
	var (
		entry       types.AuditEntry
		detailsJSON []byte
	)

	if err := scanner.Scan(
		&entry.ID,
		&entry.EventType,
		&entry.ClientID,
		&entry.ServerName,
		&detailsJSON,
		&entry.CreatedAt,
	); err != nil {
		return nil, err
	}

	if len(detailsJSON) > 0 {
		if err := json.Unmarshal(detailsJSON, &entry.Details); err != nil {
			return nil, fmt.Errorf("unmarshal details: %w", err)
		}
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}

	return &entry, nil
}
