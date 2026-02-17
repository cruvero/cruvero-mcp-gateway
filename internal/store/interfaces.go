package store

import (
	"context"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// ServerStore defines persistence operations for backend MCP servers.
type ServerStore interface {
	Create(ctx context.Context, record *types.ServerRecord) error
	Get(ctx context.Context, id string) (*types.ServerRecord, error)
	GetByName(ctx context.Context, name string) (*types.ServerRecord, error)
	GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error)
	List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error)
	Update(ctx context.Context, record *types.ServerRecord) error
	UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error
	UpdateHeartbeat(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error)
	ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error)
}

// APIKeyStore defines persistence operations for API keys.
type APIKeyStore interface {
	Create(ctx context.Context, key *types.APIKey) error
	GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error)
	List(ctx context.Context) ([]types.APIKey, error)
	Revoke(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) (int64, error)
}

// AuditStore defines append/query operations for audit events.
type AuditStore interface {
	Log(ctx context.Context, entry *types.AuditEntry) error
	Query(ctx context.Context, filter types.AuditFilter) ([]types.AuditEntry, error)
}
