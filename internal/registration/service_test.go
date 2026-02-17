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

func TestServiceRegisterUnauthorizedCaller(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{}, nil)

	_, err := svc.Register(context.Background(), nil, validRegistrationRequest())
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestServiceRegisterForbiddenSPIFFE(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{SPIFFEAllowList: []string{"spiffe://other.org"}}, nil)

	_, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err == nil {
		t.Fatal("expected forbidden error")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestServiceRegisterUpdatesExisting(t *testing.T) {
	t.Parallel()

	updateCalled := false
	createCalled := false
	existingCreatedAt := time.Now().Add(-1 * time.Hour).UTC()

	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return &types.ServerRecord{
				ID:        "existing-id",
				SPIFFEID:  spiffeID,
				CreatedAt: existingCreatedAt,
			}, nil
		},
		updateFn: func(ctx context.Context, record *types.ServerRecord) error {
			updateCalled = true
			if record.ID != "existing-id" {
				t.Fatalf("expected existing id to be reused, got %q", record.ID)
			}
			if !record.CreatedAt.Equal(existingCreatedAt) {
				t.Fatalf("expected existing created_at to be preserved")
			}
			return nil
		},
		createFn: func(ctx context.Context, record *types.ServerRecord) error {
			createCalled = true
			return nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{HeartbeatTTL: 500 * time.Millisecond}, nil)
	resp, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err != nil {
		t.Fatalf("register update existing: %v", err)
	}
	if !updateCalled {
		t.Fatal("expected update to be called")
	}
	if createCalled {
		t.Fatal("did not expect create to be called")
	}
	if resp.HeartbeatInterval != 1 || resp.HeartbeatIntervalSeconds != 1 {
		t.Fatalf("expected heartbeat interval seconds to clamp to 1, got %+v", resp)
	}
}

func TestServiceRegisterCreateWhenLookupReturnsNilRecord(t *testing.T) {
	t.Parallel()

	createCalled := false
	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, nil
		},
		createFn: func(ctx context.Context, record *types.ServerRecord) error {
			createCalled = true
			return nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	if _, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest()); err != nil {
		t.Fatalf("register create from nil lookup record: %v", err)
	}
	if !createCalled {
		t.Fatal("expected create to be called")
	}
}

func TestServiceRegisterLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("lookup failure")
	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, lookupErr
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	_, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err == nil {
		t.Fatal("expected lookup error")
	}
	if !errors.Is(err, lookupErr) {
		t.Fatalf("expected wrapped lookup error, got %v", err)
	}
}

func TestServiceRegisterCreateError(t *testing.T) {
	t.Parallel()

	createErr := errors.New("create failure")
	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
		createFn: func(ctx context.Context, record *types.ServerRecord) error {
			return createErr
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	_, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err == nil {
		t.Fatal("expected create error")
	}
	if !errors.Is(err, createErr) {
		t.Fatalf("expected wrapped create error, got %v", err)
	}
}

func TestServiceRegisterUpdateError(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("update failure")
	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return &types.ServerRecord{ID: "existing", SPIFFEID: spiffeID, CreatedAt: time.Now().UTC()}, nil
		},
		updateFn: func(ctx context.Context, record *types.ServerRecord) error {
			return updateErr
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	_, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err == nil {
		t.Fatal("expected update error")
	}
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected wrapped update error, got %v", err)
	}
}

func TestServiceRegisterAuditLogFailureDoesNotFailRequest(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
	}
	auditStore := &mockAuditStore{
		logFn: func(ctx context.Context, entry *types.AuditEntry) error {
			return errors.New("audit write failed")
		},
	}

	svc := NewService(serverStore, auditStore, &config.Config{}, testRegistrationLogger())
	if _, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest()); err != nil {
		t.Fatalf("register should succeed even if audit logging fails: %v", err)
	}
}

func TestServiceRegisterPolicyFromConfig(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
	}
	cfg := &config.Config{
		RateDefault:  42,
		RateBurst:    84,
		HeartbeatTTL: 45 * time.Second,
	}

	svc := NewService(serverStore, &mockAuditStore{}, cfg, nil)
	resp, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err != nil {
		t.Fatalf("register with custom policy config: %v", err)
	}
	if resp.PolicySnapshot == nil {
		t.Fatal("expected policy snapshot")
	}
	if resp.PolicySnapshot.RateLimit != 42 || resp.PolicySnapshot.RateBurst != 84 {
		t.Fatalf("expected policy values from config, got %+v", resp.PolicySnapshot)
	}
	if resp.HeartbeatInterval != 45 || resp.HeartbeatIntervalSeconds != 45 {
		t.Fatalf("expected heartbeat interval from config, got %+v", resp)
	}
}

func TestServiceListFailure(t *testing.T) {
	t.Parallel()

	listErr := errors.New("list failure")
	serverStore := &mockServerStore{
		listFn: func(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
			return nil, listErr
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	_, err := svc.List(context.Background(), types.ServerFilter{})
	if err == nil {
		t.Fatal("expected list error")
	}
	if !errors.Is(err, listErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
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

func TestServiceDeregisterInvalidID(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), validMTLSIdentity(), " ")
	if err == nil {
		t.Fatal("expected invalid request error")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestServiceDeregisterMissingCaller(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), nil, "server-1")
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestServiceDeregisterGetNotFound(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
	}
	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), validMTLSIdentity(), "server-1")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceDeregisterGetError(t *testing.T) {
	t.Parallel()

	getErr := errors.New("get failure")
	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return nil, getErr
		},
	}
	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), validMTLSIdentity(), "server-1")
	if err == nil {
		t.Fatal("expected get error")
	}
	if !errors.Is(err, getErr) {
		t.Fatalf("expected wrapped get error, got %v", err)
	}
}

func TestServiceDeregisterDeleteError(t *testing.T) {
	t.Parallel()

	deleteErr := errors.New("delete failure")
	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{ID: id, Name: "svc-alpha", SPIFFEID: validMTLSIdentity().ID}, nil
		},
		deleteFn: func(ctx context.Context, id string) error {
			return deleteErr
		},
	}
	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	err := svc.Deregister(context.Background(), validMTLSIdentity(), "server-1")
	if err == nil {
		t.Fatal("expected delete error")
	}
	if !errors.Is(err, deleteErr) {
		t.Fatalf("expected wrapped delete error, got %v", err)
	}
}

func validMTLSIdentity() *identitypkg.Identity {
	return &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"}
}

func validRegistrationRequest() RegistrationRequest {
	return RegistrationRequest{
		ServiceName: "svc-alpha",
		Version:     "1.0.0",
		Listen:      ListenConfig{Host: "svc-alpha.default.svc", Port: 8443, Protocol: "https"},
		Capabilities: types.Capability{
			Tools: []string{"tool.alpha"},
		},
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
