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

const serverColumns = "id, name, spiffe_id, version, host, port, capabilities, status, policy_profile, last_heartbeat, created_at, updated_at"

// PostgresServerStore is a Postgres-backed implementation of ServerStore.
type PostgresServerStore struct {
	db *sql.DB
}

var _ ServerStore = (*PostgresServerStore)(nil)

// NewPostgresServerStore creates a PostgresServerStore.
func NewPostgresServerStore(db *sql.DB) *PostgresServerStore {
	return &PostgresServerStore{db: db}
}

// Create inserts a new MCP server record.
func (s *PostgresServerStore) Create(ctx context.Context, record *types.ServerRecord) error {
	if record == nil {
		return fmt.Errorf("server store: record is nil")
	}

	capabilitiesJSON, err := json.Marshal(record.Capabilities)
	if err != nil {
		return fmt.Errorf("server store: marshal capabilities: %w", err)
	}

	status := record.Status
	if status == "" {
		status = types.StatusPending
	}

	policyProfile := record.PolicyProfile
	if policyProfile == "" {
		policyProfile = "default"
	}

	const query = `
INSERT INTO mcp_servers (name, spiffe_id, version, host, port, capabilities, status, policy_profile, last_heartbeat)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
`

	if _, err := s.db.ExecContext(
		ctx,
		query,
		record.Name,
		record.SPIFFEID,
		record.Version,
		record.Host,
		record.Port,
		capabilitiesJSON,
		status,
		policyProfile,
		record.LastHeartbeat,
	); err != nil {
		return fmt.Errorf("server store: %w", err)
	}

	return nil
}

// Get retrieves a server by ID.
func (s *PostgresServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	const query = `SELECT ` + serverColumns + ` FROM mcp_servers WHERE id = $1`
	row := s.db.QueryRowContext(ctx, query, id)
	record, err := scanServerRecord(row)
	if err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}
	return record, nil
}

// GetByName retrieves a server by name.
func (s *PostgresServerStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	const query = `SELECT ` + serverColumns + ` FROM mcp_servers WHERE name = $1`
	row := s.db.QueryRowContext(ctx, query, name)
	record, err := scanServerRecord(row)
	if err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}
	return record, nil
}

// GetBySPIFFEID retrieves a server by SPIFFE ID.
func (s *PostgresServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	const query = `SELECT ` + serverColumns + ` FROM mcp_servers WHERE spiffe_id = $1`
	row := s.db.QueryRowContext(ctx, query, spiffeID)
	record, err := scanServerRecord(row)
	if err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}
	return record, nil
}

// List returns server records filtered by status and/or name pattern.
func (s *PostgresServerStore) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	query := `SELECT ` + serverColumns + ` FROM mcp_servers`
	args := make([]any, 0, 4)
	conditions := make([]string, 0, 2)

	if filter.Status != nil {
		args = append(args, filter.Status.String())
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(filter.NamePattern) != "" {
		args = append(args, filter.NamePattern)
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", len(args)))
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
		return nil, fmt.Errorf("server store: %w", err)
	}
	defer rows.Close()

	records := make([]types.ServerRecord, 0)
	for rows.Next() {
		record, scanErr := scanServerRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("server store: %w", scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}

	return records, nil
}

// Update updates all mutable server fields.
func (s *PostgresServerStore) Update(ctx context.Context, record *types.ServerRecord) error {
	if record == nil {
		return fmt.Errorf("server store: record is nil")
	}

	capabilitiesJSON, err := json.Marshal(record.Capabilities)
	if err != nil {
		return fmt.Errorf("server store: marshal capabilities: %w", err)
	}

	const query = `
UPDATE mcp_servers
SET name = $1,
    spiffe_id = $2,
    version = $3,
    host = $4,
    port = $5,
    capabilities = $6,
    status = $7,
    policy_profile = $8,
    last_heartbeat = $9,
    updated_at = now()
WHERE id = $10
`

	if _, err := s.db.ExecContext(
		ctx,
		query,
		record.Name,
		record.SPIFFEID,
		record.Version,
		record.Host,
		record.Port,
		capabilitiesJSON,
		record.Status,
		record.PolicyProfile,
		record.LastHeartbeat,
		record.ID,
	); err != nil {
		return fmt.Errorf("server store: %w", err)
	}

	return nil
}

// UpdateStatus updates server status and updated_at timestamp.
func (s *PostgresServerStore) UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error {
	const query = `UPDATE mcp_servers SET status = $1, updated_at = now() WHERE id = $2`
	if _, err := s.db.ExecContext(ctx, query, status.String(), id); err != nil {
		return fmt.Errorf("server store: %w", err)
	}
	return nil
}

// UpdateHeartbeat updates last_heartbeat and updated_at timestamps to now().
func (s *PostgresServerStore) UpdateHeartbeat(ctx context.Context, id string) error {
	const query = `UPDATE mcp_servers SET last_heartbeat = now(), updated_at = now() WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("server store: %w", err)
	}
	return nil
}

// Delete removes a server record by ID.
func (s *PostgresServerStore) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM mcp_servers WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("server store: %w", err)
	}
	return nil
}

// ListStale returns active servers whose heartbeat is older than threshold.
func (s *PostgresServerStore) ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	if threshold <= 0 {
		return nil, fmt.Errorf("server store: threshold must be positive")
	}

	const query = `
SELECT ` + serverColumns + `
FROM mcp_servers
WHERE last_heartbeat < now() - $1::interval
  AND status = 'active'
ORDER BY last_heartbeat ASC
`

	rows, err := s.db.QueryContext(ctx, query, threshold.String())
	if err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}
	defer rows.Close()

	return scanServerRows(rows)
}

// ListExpired returns active or stale servers whose heartbeat is older than 3x threshold.
func (s *PostgresServerStore) ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	if threshold <= 0 {
		return nil, fmt.Errorf("server store: threshold must be positive")
	}

	const query = `
SELECT ` + serverColumns + `
FROM mcp_servers
WHERE last_heartbeat < now() - ($1::interval * 3)
  AND status IN ('active', 'stale')
ORDER BY last_heartbeat ASC
`

	rows, err := s.db.QueryContext(ctx, query, threshold.String())
	if err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}
	defer rows.Close()

	return scanServerRows(rows)
}

type serverScanner interface {
	Scan(dest ...any) error
}

func scanServerRows(rows *sql.Rows) ([]types.ServerRecord, error) {
	records := make([]types.ServerRecord, 0)
	for rows.Next() {
		record, scanErr := scanServerRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("server store: %w", scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("server store: %w", err)
	}
	return records, nil
}

func scanServerRecord(scanner serverScanner) (*types.ServerRecord, error) {
	var (
		record           types.ServerRecord
		capabilitiesJSON []byte
		status           string
		lastHeartbeat    sql.NullTime
	)

	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.SPIFFEID,
		&record.Version,
		&record.Host,
		&record.Port,
		&capabilitiesJSON,
		&status,
		&record.PolicyProfile,
		&lastHeartbeat,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}

	record.Status = types.ServerStatus(status)
	if lastHeartbeat.Valid {
		t := lastHeartbeat.Time
		record.LastHeartbeat = &t
	}

	if len(capabilitiesJSON) > 0 {
		if err := json.Unmarshal(capabilitiesJSON, &record.Capabilities); err != nil {
			return nil, fmt.Errorf("unmarshal capabilities: %w", err)
		}
	}

	return &record, nil
}
