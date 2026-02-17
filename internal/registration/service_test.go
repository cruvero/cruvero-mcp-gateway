package registration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestServiceRegisterHappyPath(t *testing.T) {
	t.Parallel()

	createCalled := false
	auditCalled := false

	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
		createFn: func(ctx context.Context, record *types.ServerRecord) error {
			createCalled = true
			if record.Status != types.StatusPending {
				t.Fatalf("expected pending status, got %q", record.Status)
			}
			if record.SPIFFEID != "spiffe://example.org/ns/default/sa/server" {
				t.Fatalf("unexpected spiffe id: %q", record.SPIFFEID)
			}
			return nil
		},
	}
	auditStore := &mockAuditStore{
		logFn: func(ctx context.Context, entry *types.AuditEntry) error {
			auditCalled = true
			if entry.EventType != "server.registered" {
				t.Fatalf("unexpected audit event type: %q", entry.EventType)
			}
			return nil
		},
	}

	svc := NewService(serverStore, auditStore, &config.Config{HeartbeatTTL: 30 * time.Second, SPIFFEAllowList: []string{"spiffe://example.org"}}, nil)

	resp, err := svc.Register(context.Background(), &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"}, RegistrationRequest{
		ServiceName: "svc-alpha",
		Version:     "1.0.0",
		Listen:      ListenConfig{Host: "svc-alpha.default.svc", Port: 8443, Protocol: "https"},
		Capabilities: types.Capability{
			Tools: []string{"tool.alpha"},
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if !createCalled {
		t.Fatal("expected store.Create to be called")
	}
	if !auditCalled {
		t.Fatal("expected audit log to be called")
	}
	if resp.Status != types.StatusPending {
		t.Fatalf("expected status pending, got %q", resp.Status)
	}
	if resp.HeartbeatInterval != 30 || resp.HeartbeatIntervalSeconds != 30 {
		t.Fatalf("unexpected heartbeat interval values: %+v", resp)
	}
	if resp.ConfigVersion != 0 {
		t.Fatalf("expected placeholder config version 0, got %d", resp.ConfigVersion)
	}
	if resp.PolicySnapshot == nil || resp.PolicySnapshot.Name != "default" {
		t.Fatalf("expected default policy snapshot, got %+v", resp.PolicySnapshot)
	}
}

func TestServiceRegisterValidationFailure(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{}, nil)

	_, err := svc.Register(context.Background(), &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"}, RegistrationRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestServiceDeregisterByAdmin(t *testing.T) {
	t.Parallel()

	deleteCalled := false
	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{ID: id, Name: "svc-alpha", SPIFFEID: "spiffe://example.org/ns/default/sa/server"}, nil
		},
		deleteFn: func(ctx context.Context, id string) error {
			deleteCalled = true
			return nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), &identitypkg.Identity{Type: identitypkg.IdentityAPIKey, ID: "admin", Scopes: []string{identitypkg.ScopeAdmin}}, "server-1")
	if err != nil {
		t.Fatalf("deregister by admin: %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected delete to be called")
	}
}

func TestServiceDeregisterBySelf(t *testing.T) {
	t.Parallel()

	deleteCalled := false
	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{ID: id, Name: "svc-alpha", SPIFFEID: "spiffe://example.org/ns/default/sa/server"}, nil
		},
		deleteFn: func(ctx context.Context, id string) error {
			deleteCalled = true
			return nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"}, "server-1")
	if err != nil {
		t.Fatalf("deregister by self: %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected delete to be called")
	}
}

func TestServiceDeregisterUnauthorized(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{ID: id, Name: "svc-alpha", SPIFFEID: "spiffe://example.org/ns/default/sa/server"}, nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/other"}, "server-1")
	if err == nil {
		t.Fatal("expected unauthorized deregister error")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

type mockServerStore struct {
	createFn          func(ctx context.Context, record *types.ServerRecord) error
	getFn             func(ctx context.Context, id string) (*types.ServerRecord, error)
	getByNameFn       func(ctx context.Context, name string) (*types.ServerRecord, error)
	getBySPIFFEIDFn   func(ctx context.Context, spiffeID string) (*types.ServerRecord, error)
	listFn            func(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error)
	updateFn          func(ctx context.Context, record *types.ServerRecord) error
	updateStatusFn    func(ctx context.Context, id string, status types.ServerStatus) error
	updateHeartbeatFn func(ctx context.Context, id string) error
	deleteFn          func(ctx context.Context, id string) error
	listStaleFn       func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error)
	listExpiredFn     func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error)
}

func (m *mockServerStore) Create(ctx context.Context, record *types.ServerRecord) error {
	if m.createFn != nil {
		return m.createFn(ctx, record)
	}
	return nil
}

func (m *mockServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, sql.ErrNoRows
}

func (m *mockServerStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	if m.getByNameFn != nil {
		return m.getByNameFn(ctx, name)
	}
	return nil, sql.ErrNoRows
}

func (m *mockServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	if m.getBySPIFFEIDFn != nil {
		return m.getBySPIFFEIDFn(ctx, spiffeID)
	}
	return nil, sql.ErrNoRows
}

func (m *mockServerStore) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, nil
}

func (m *mockServerStore) Update(ctx context.Context, record *types.ServerRecord) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, record)
	}
	return nil
}

func (m *mockServerStore) UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, id, status)
	}
	return nil
}

func (m *mockServerStore) UpdateHeartbeat(ctx context.Context, id string) error {
	if m.updateHeartbeatFn != nil {
		return m.updateHeartbeatFn(ctx, id)
	}
	return nil
}

func (m *mockServerStore) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockServerStore) ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	if m.listStaleFn != nil {
		return m.listStaleFn(ctx, threshold)
	}
	return nil, nil
}

func (m *mockServerStore) ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	if m.listExpiredFn != nil {
		return m.listExpiredFn(ctx, threshold)
	}
	return nil, nil
}

type mockAuditStore struct {
	logFn   func(ctx context.Context, entry *types.AuditEntry) error
	queryFn func(ctx context.Context, filter types.AuditFilter) ([]types.AuditEntry, error)
}

func (m *mockAuditStore) Log(ctx context.Context, entry *types.AuditEntry) error {
	if m.logFn != nil {
		return m.logFn(ctx, entry)
	}
	return nil
}

func (m *mockAuditStore) Query(ctx context.Context, filter types.AuditFilter) ([]types.AuditEntry, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, filter)
	}
	return nil, nil
}
