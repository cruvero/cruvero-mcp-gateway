package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/types"
)

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
		return fmt.Errorf("audit store: %w", err)
	}
	return nil
}

// Query returns audit entries matching filter criteria.
func (s *PostgresAuditStore) Query(ctx context.Context, filter types.AuditFilter) ([]types.AuditEntry, error) {
	query := `SELECT ` + auditColumns + ` FROM audit_log`
	args := make([]any, 0, 8)
	conditions := make([]string, 0, 5)

	if strings.TrimSpace(filter.EventType) != "" {
		args = append(args, filter.EventType)
		conditions = append(conditions, fmt.Sprintf("event_type = $%d", len(args)))
	}
	if strings.TrimSpace(filter.ClientID) != "" {
		args = append(args, filter.ClientID)
		conditions = append(conditions, fmt.Sprintf("client_id = $%d", len(args)))
	}
	if strings.TrimSpace(filter.ServerName) != "" {
		args = append(args, filter.ServerName)
		conditions = append(conditions, fmt.Sprintf("server_name = $%d", len(args)))
	}
	if filter.Since != nil {
		args = append(args, *filter.Since)
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", len(args)))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit store: %w", err)
	}
	defer rows.Close()

	entries := make([]types.AuditEntry, 0)
	for rows.Next() {
		entry, scanErr := scanAuditEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("audit store: %w", scanErr)
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit store: %w", err)
	}

	return entries, nil
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
