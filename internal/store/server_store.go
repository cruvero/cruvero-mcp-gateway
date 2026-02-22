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

const errServerStore = "server store: %w"

const serverColumns = "id, name, spiffe_id, version, host, port, protocol, capabilities, status, policy_profile, last_heartbeat, lease_epoch, capability_hash, sync_state, last_platform_ack_version, last_platform_ack_at, rate_limit, rate_burst, created_at, updated_at"

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
INSERT INTO mcp_servers (
	name,
	spiffe_id,
	version,
	host,
	port,
	protocol,
	capabilities,
	status,
	policy_profile,
	last_heartbeat,
	lease_epoch,
	capability_hash,
	sync_state,
	last_platform_ack_version,
	last_platform_ack_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
`

	protocol := strings.TrimSpace(record.Protocol)
	if protocol == "" {
		protocol = "https"
	}

	if _, err := s.db.ExecContext(
		ctx,
		query,
		record.Name,
		record.SPIFFEID,
		record.Version,
		record.Host,
		record.Port,
		protocol,
		capabilitiesJSON,
		status,
		policyProfile,
		record.LastHeartbeat,
		maxInt64(record.LeaseEpoch, 1),
		strings.TrimSpace(record.CapabilityHash),
		normalizedSyncState(record.SyncState),
		strings.TrimSpace(record.LastPlatformAckVersion),
		record.LastPlatformAckAt,
	); err != nil {
		return fmt.Errorf(errServerStore, err)
	}

	return nil
}

// Get retrieves a server by ID.
func (s *PostgresServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	const query = `SELECT ` + serverColumns + ` FROM mcp_servers WHERE id = $1`
	row := s.db.QueryRowContext(ctx, query, id)
	record, err := scanServerRecord(row)
	if err != nil {
		return nil, fmt.Errorf(errServerStore, err)
	}
	return record, nil
}

// GetByName retrieves a server by name.
func (s *PostgresServerStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	const query = `SELECT ` + serverColumns + ` FROM mcp_servers WHERE name = $1`
	row := s.db.QueryRowContext(ctx, query, name)
	record, err := scanServerRecord(row)
	if err != nil {
		return nil, fmt.Errorf(errServerStore, err)
	}
	return record, nil
}

// GetBySPIFFEID retrieves a server by SPIFFE ID.
func (s *PostgresServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	const query = `SELECT ` + serverColumns + ` FROM mcp_servers WHERE spiffe_id = $1`
	row := s.db.QueryRowContext(ctx, query, spiffeID)
	record, err := scanServerRecord(row)
	if err != nil {
		return nil, fmt.Errorf(errServerStore, err)
	}
	return record, nil
}

// List returns server records filtered by status and/or name pattern.
func (s *PostgresServerStore) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	const query = `
SELECT ` + serverColumns + `
FROM mcp_servers
WHERE ($1::text IS NULL OR status = $1)
  AND ($2::text IS NULL OR name ILIKE $2)
ORDER BY created_at DESC
LIMIT $3
OFFSET $4
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
		optionalServerStatus(filter.Status),
		optionalServerNamePattern(filter.NamePattern),
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf(errServerStore, err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]types.ServerRecord, 0)
	for rows.Next() {
		record, scanErr := scanServerRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf(errServerStore, scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(errServerStore, err)
	}

	return records, nil
}

func optionalServerStatus(status *types.ServerStatus) any {
	if status == nil {
		return nil
	}
	return status.String()
}

func optionalServerNamePattern(namePattern string) any {
	if strings.TrimSpace(namePattern) == "" {
		return nil
	}
	return namePattern
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
    protocol = $6,
    capabilities = $7,
    status = $8,
    policy_profile = $9,
    last_heartbeat = $10,
    lease_epoch = $11,
    capability_hash = $12,
    sync_state = $13,
    last_platform_ack_version = $14,
    last_platform_ack_at = $15,
    updated_at = now()
WHERE id = $16
`

	protocol := strings.TrimSpace(record.Protocol)
	if protocol == "" {
		protocol = "https"
	}

	if _, err := s.db.ExecContext(
		ctx,
		query,
		record.Name,
		record.SPIFFEID,
		record.Version,
		record.Host,
		record.Port,
		protocol,
		capabilitiesJSON,
		record.Status,
		record.PolicyProfile,
		record.LastHeartbeat,
		maxInt64(record.LeaseEpoch, 1),
		strings.TrimSpace(record.CapabilityHash),
		normalizedSyncState(record.SyncState),
		strings.TrimSpace(record.LastPlatformAckVersion),
		record.LastPlatformAckAt,
		record.ID,
	); err != nil {
		return fmt.Errorf(errServerStore, err)
	}

	return nil
}

// UpdateStatus updates server status and updated_at timestamp.
func (s *PostgresServerStore) UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error {
	const query = `UPDATE mcp_servers SET status = $1, updated_at = now() WHERE id = $2`
	if _, err := s.db.ExecContext(ctx, query, status.String(), id); err != nil {
		return fmt.Errorf(errServerStore, err)
	}
	return nil
}

// UpdateHeartbeat updates last_heartbeat and updated_at timestamps to now().
func (s *PostgresServerStore) UpdateHeartbeat(ctx context.Context, id string) error {
	const query = `UPDATE mcp_servers SET last_heartbeat = now(), updated_at = now() WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf(errServerStore, err)
	}
	return nil
}

// AcknowledgeRegistration marks the current registration lease as platform
// acknowledged when registration id, lease epoch, and capability hash match.
func (s *PostgresServerStore) AcknowledgeRegistration(
	ctx context.Context,
	id string,
	leaseEpoch int64,
	capabilityHash string,
	ackVersion string,
	ackedAt time.Time,
) error {
	const query = `
UPDATE mcp_servers
SET sync_state = $1,
    last_platform_ack_version = $2,
    last_platform_ack_at = $3,
    updated_at = now()
WHERE id = $4
  AND lease_epoch = $5
  AND ($6 = '' OR capability_hash = $6)
`
	result, err := s.db.ExecContext(
		ctx,
		query,
		types.SyncStateAcked.String(),
		strings.TrimSpace(ackVersion),
		ackedAt.UTC(),
		strings.TrimSpace(id),
		leaseEpoch,
		strings.TrimSpace(capabilityHash),
	)
	if err != nil {
		return fmt.Errorf(errServerStore, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(errServerStore, err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateRateLimit sets rate limit and burst for a server by ID.
func (s *PostgresServerStore) UpdateRateLimit(ctx context.Context, id string, rateLimit, rateBurst *int) error {
	const query = `UPDATE mcp_servers SET rate_limit = $1, rate_burst = $2, updated_at = now() WHERE id = $3`
	if _, err := s.db.ExecContext(ctx, query, rateLimit, rateBurst, id); err != nil {
		return fmt.Errorf(errServerStore, err)
	}
	return nil
}

// Delete removes a server record by ID.
func (s *PostgresServerStore) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM mcp_servers WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf(errServerStore, err)
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
		return nil, fmt.Errorf(errServerStore, err)
	}
	defer func() { _ = rows.Close() }()

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
		return nil, fmt.Errorf(errServerStore, err)
	}
	defer func() { _ = rows.Close() }()

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
			return nil, fmt.Errorf(errServerStore, scanErr)
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(errServerStore, err)
	}
	return records, nil
}

func scanServerRecord(scanner serverScanner) (*types.ServerRecord, error) {
	var (
		record           types.ServerRecord
		capabilitiesJSON []byte
		status           string
		syncState        string
		lastHeartbeat    sql.NullTime
		lastPlatformAck  sql.NullTime
		rateLimit        sql.NullInt32
		rateBurst        sql.NullInt32
	)

	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.SPIFFEID,
		&record.Version,
		&record.Host,
		&record.Port,
		&record.Protocol,
		&capabilitiesJSON,
		&status,
		&record.PolicyProfile,
		&lastHeartbeat,
		&record.LeaseEpoch,
		&record.CapabilityHash,
		&syncState,
		&record.LastPlatformAckVersion,
		&lastPlatformAck,
		&rateLimit,
		&rateBurst,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}

	record.Status = types.ServerStatus(status)
	record.SyncState = parseSyncState(syncState)
	if lastHeartbeat.Valid {
		t := lastHeartbeat.Time
		record.LastHeartbeat = &t
	}
	if lastPlatformAck.Valid {
		t := lastPlatformAck.Time
		record.LastPlatformAckAt = &t
	}
	if rateLimit.Valid {
		v := int(rateLimit.Int32)
		record.RateLimit = &v
	}
	if rateBurst.Valid {
		v := int(rateBurst.Int32)
		record.RateBurst = &v
	}

	if len(capabilitiesJSON) > 0 {
		if err := json.Unmarshal(capabilitiesJSON, &record.Capabilities); err != nil {
			return nil, fmt.Errorf("unmarshal capabilities: %w", err)
		}
	}

	return &record, nil
}

func maxInt64(value int64, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func normalizedSyncState(state types.RegistrationSyncState) string {
	switch state {
	case types.SyncStateAcked:
		return state.String()
	case types.SyncStateStale:
		return state.String()
	default:
		return types.SyncStateUnacked.String()
	}
}

func parseSyncState(raw string) types.RegistrationSyncState {
	normalized := types.RegistrationSyncState(strings.ToLower(strings.TrimSpace(raw)))
	switch normalized {
	case types.SyncStateAcked, types.SyncStateStale:
		return normalized
	default:
		return types.SyncStateUnacked
	}
}
