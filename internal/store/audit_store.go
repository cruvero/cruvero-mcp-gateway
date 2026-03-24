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

const auditColumns = "id, event_type, client_id, username, server_name, details, created_at"

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
INSERT INTO audit_log (event_type, client_id, username, server_name, details)
VALUES ($1, $2, $3, $4, $5)
`

	username := strings.TrimSpace(entry.Username)
	if username == "" {
		username = normalizeAuditUsername(entry.ClientID)
	}

	if _, err := s.db.ExecContext(
		ctx,
		query,
		entry.EventType,
		entry.ClientID,
		username,
		entry.ServerName,
		detailsJSON,
	); err != nil {
		return fmt.Errorf(errAuditStore, err)
	}
	return nil
}

const auditWhereClause = `
WHERE ($1::text IS NULL OR event_type = $1)
  AND ($2::text IS NULL OR client_id = $2)
  AND ($3::text IS NULL OR server_name ILIKE '%' || $3 || '%')
  AND ($4::timestamptz IS NULL OR created_at >= $4::timestamptz)
  AND ($5::timestamptz IS NULL OR created_at <= $5::timestamptz)
  AND ($6::text IS NULL OR details::text ILIKE '%' || $6 || '%')
  AND ($7::text IS NULL OR username ILIKE '%' || $7 || '%')
`

func auditFilterArgs(filter types.AuditFilter) []any {
	return []any{
		optionalString(filter.EventType),
		optionalString(filter.ClientID),
		optionalILikeString(filter.ServerName),
		optionalTime(filter.Since),
		optionalTime(filter.Until),
		optionalILikeString(filter.DetailsSearch),
		optionalILikeString(filter.Username),
	}
}

// Query returns audit entries matching filter criteria.
func (s *PostgresAuditStore) Query(ctx context.Context, filter types.AuditFilter) ([]types.AuditEntry, error) {
	orderBy := normalizeAuditSortBy(filter.SortBy)
	sortDir := normalizeAuditSortDir(filter.SortDir)
	query := `
SELECT ` + auditColumns + `
FROM audit_log` + auditWhereClause + `
ORDER BY ` + orderBy + ` ` + sortDir + `
LIMIT $8
OFFSET $9
`

	limit := int64(9223372036854775807)
	if filter.Limit > 0 {
		limit = int64(filter.Limit)
	}
	offset := int64(0)
	if filter.Offset > 0 {
		offset = int64(filter.Offset)
	}

	args := append(auditFilterArgs(filter), limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf(errAuditStore, err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]types.AuditEntry, 0)
	for rows.Next() {
		entry, scanErr := scanAuditEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf(errAuditStore, scanErr)
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(errAuditStore, err)
	}

	return entries, nil
}

// Count returns the total number of audit entries matching filter criteria.
func (s *PostgresAuditStore) Count(ctx context.Context, filter types.AuditFilter) (int, error) {
	const query = `SELECT COUNT(*) FROM audit_log` + auditWhereClause

	var count int
	if err := s.db.QueryRowContext(ctx, query, auditFilterArgs(filter)...).Scan(&count); err != nil {
		return 0, fmt.Errorf(errAuditStore, err)
	}
	return count, nil
}

// escapeILikePattern escapes special ILIKE pattern characters (%, _, \)
// so user-supplied search terms are matched literally.
func escapeILikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func optionalILikeString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return escapeILikePattern(trimmed)
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
		&entry.Username,
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

func normalizeAuditUsername(clientID string) string {
	id := strings.TrimSpace(clientID)
	if id == "" {
		return "system"
	}

	lower := strings.ToLower(id)
	if strings.HasPrefix(lower, "spiffe://") || strings.HasPrefix(lower, "server:") {
		return "system"
	}

	return id
}

func normalizeAuditSortBy(sortBy string) string {
	return types.NormalizeAuditSortBy(sortBy)
}

func normalizeAuditSortDir(sortDir string) string {
	return types.NormalizeAuditSortDir(sortDir)
}
