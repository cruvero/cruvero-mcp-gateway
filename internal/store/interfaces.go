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
	AcknowledgeRegistration(
		ctx context.Context,
		id string,
		leaseEpoch int64,
		capabilityHash string,
		ackVersion string,
		ackedAt time.Time,
	) error
	UpdateRateLimit(ctx context.Context, id string, rateLimit, rateBurst *int) error
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
	Count(ctx context.Context, filter types.AuditFilter) (int, error)
}

// UserStore defines persistence operations for gateway users and tool permissions.
type UserStore interface {
	Upsert(ctx context.Context, user *types.User) error
	Get(ctx context.Context, id string) (*types.User, error)
	GetByOIDCSub(ctx context.Context, oidcSub string) (*types.User, error)
	Search(ctx context.Context, filter types.UserFilter) ([]types.User, int, error)
	UpdateRole(ctx context.Context, id string, role types.UserRole) error
	Delete(ctx context.Context, id string) error
	GetToolPermissions(ctx context.Context, userID string) ([]types.UserToolPermission, error)
	HasToolPermission(ctx context.Context, userID string, toolName string) (bool, error)
	SetToolPermissions(ctx context.Context, userID string, toolNames []string, grantedBy string) error
}

// SynonymStore defines CRUD operations for search synonym groups.
type SynonymStore interface {
	List(ctx context.Context) ([]types.SynonymEntry, error)
	Upsert(ctx context.Context, entry *types.SynonymEntry) error
	Delete(ctx context.Context, term string) error
}

// ReindexLogStore defines append/query operations for search reindex events.
type ReindexLogStore interface {
	Log(ctx context.Context, entry *types.ReindexLogEntry) error
	Recent(ctx context.Context, limit int) ([]types.ReindexLogEntry, error)
}

// ToolClassificationStore defines CRUD operations for tool risk classifications.
type ToolClassificationStore interface {
	Get(ctx context.Context, toolName string) (*types.ToolClassification, error)
	GetAll(ctx context.Context) ([]types.ToolClassification, error)
	GetByRiskLevel(ctx context.Context, level types.RiskLevel) ([]types.ToolClassification, error)
	Search(ctx context.Context, filter types.ToolFilter) ([]types.ToolClassification, int, error)
	Upsert(ctx context.Context, classification *types.ToolClassification) error
	Delete(ctx context.Context, toolName string) error
	DeleteNotIn(ctx context.Context, activeToolNames []string) (int64, error)
}
