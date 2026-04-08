package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/lib/pq"
)

const errAPIKeyStore = "api key store: %w"

// #nosec G101 -- column names are not credentials.
const apiKeyColumns = "id, key_lookup_hash, key_bcrypt_hash, name, scopes, client_id, policy_profile, expires_at, created_at, server_scope"

// PostgresAPIKeyStore is a Postgres-backed implementation of APIKeyStore.
type PostgresAPIKeyStore struct {
	db *sql.DB
}

var _ APIKeyStore = (*PostgresAPIKeyStore)(nil)

// NewPostgresAPIKeyStore creates a PostgresAPIKeyStore.
func NewPostgresAPIKeyStore(db *sql.DB) *PostgresAPIKeyStore {
	return &PostgresAPIKeyStore{db: db}
}

// Create inserts a new API key record.
func (s *PostgresAPIKeyStore) Create(ctx context.Context, key *types.APIKey) error {
	if key == nil {
		return fmt.Errorf("api key store: key is nil")
	}

	const query = `
INSERT INTO api_keys (key_lookup_hash, key_bcrypt_hash, name, scopes, client_id, policy_profile, expires_at, server_scope)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`

	profile := key.PolicyProfile
	if profile == "" {
		profile = "default"
	}

	if _, err := s.db.ExecContext(
		ctx,
		query,
		key.KeyLookupHash,
		key.KeyBcryptHash,
		key.Name,
		pq.Array(key.Scopes),
		key.ClientID,
		profile,
		key.ExpiresAt,
		pq.Array(key.ServerScope),
	); err != nil {
		return fmt.Errorf(errAPIKeyStore, err)
	}

	return nil
}

// GetByLookupHash retrieves a non-expired API key by deterministic lookup hash.
func (s *PostgresAPIKeyStore) GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error) {
	const query = `
SELECT ` + apiKeyColumns + `
FROM api_keys
WHERE key_lookup_hash = $1
  AND (expires_at IS NULL OR expires_at > now())
`

	row := s.db.QueryRowContext(ctx, query, lookupHash)
	key, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf(errAPIKeyStore, err)
	}

	return key, nil
}

// List returns all API keys ordered by creation time descending.
func (s *PostgresAPIKeyStore) List(ctx context.Context) ([]types.APIKey, error) {
	const query = `SELECT ` + apiKeyColumns + ` FROM api_keys ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf(errAPIKeyStore, err)
	}
	defer func() { _ = rows.Close() }()

	keys := make([]types.APIKey, 0)
	for rows.Next() {
		key, scanErr := scanAPIKey(rows)
		if scanErr != nil {
			return nil, fmt.Errorf(errAPIKeyStore, scanErr)
		}
		keys = append(keys, *key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(errAPIKeyStore, err)
	}

	return keys, nil
}

// Revoke marks an API key as expired immediately.
func (s *PostgresAPIKeyStore) Revoke(ctx context.Context, id string) error {
	const query = `UPDATE api_keys SET expires_at = now() WHERE id = $1`
	if _, err := s.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf(errAPIKeyStore, err)
	}
	return nil
}

// DeleteExpired deletes expired API keys and returns number of deleted rows.
func (s *PostgresAPIKeyStore) DeleteExpired(ctx context.Context) (int64, error) {
	const query = `DELETE FROM api_keys WHERE expires_at IS NOT NULL AND expires_at < now()`
	result, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf(errAPIKeyStore, err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(errAPIKeyStore, err)
	}

	return deleted, nil
}

type apiKeyScanner interface {
	Scan(dest ...any) error
}

func scanAPIKey(scanner apiKeyScanner) (*types.APIKey, error) {
	var (
		key         types.APIKey
		scopes      pq.StringArray
		serverScope pq.StringArray
		expiresAt   sql.NullTime
	)

	if err := scanner.Scan(
		&key.ID,
		&key.KeyLookupHash,
		&key.KeyBcryptHash,
		&key.Name,
		&scopes,
		&key.ClientID,
		&key.PolicyProfile,
		&expiresAt,
		&key.CreatedAt,
		&serverScope,
	); err != nil {
		return nil, err
	}

	key.Scopes = []string(scopes)
	key.ServerScope = []string(serverScope)
	if expiresAt.Valid {
		t := expiresAt.Time
		key.ExpiresAt = &t
	}

	return &key, nil
}
