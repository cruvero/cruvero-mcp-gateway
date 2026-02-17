package registration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestHandleHeartbeatValidIdentity(t *testing.T) {
	t.Parallel()

	updatedStatus := types.ServerStatus("")
	heartbeatUpdated := false

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{
				ID:       id,
				SPIFFEID: "spiffe://example.org/ns/default/sa/server",
				Status:   types.StatusApproved,
			}, nil
		},
		updateStatusFn: func(ctx context.Context, id string, status types.ServerStatus) error {
			updatedStatus = status
			return nil
		},
		updateHeartbeatFn: func(ctx context.Context, id string) error {
			heartbeatUpdated = true
			return nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{HeartbeatTTL: 30 * time.Second}, nil)
	h := NewHandler(svc, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/server-1/heartbeat", bytes.NewBufferString(`{"status":"healthy"}`))
	req = withIdentity(req, &identitypkg.Identity{
		Type: identitypkg.IdentityMTLS,
		ID:   "spiffe://example.org/ns/default/sa/server",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !heartbeatUpdated {
		t.Fatal("expected heartbeat update")
	}
	if updatedStatus != types.StatusActive {
		t.Fatalf("expected status transition to active, got %q", updatedStatus)
	}

	var resp HeartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ServerStatus != types.StatusActive {
		t.Fatalf("expected response status active, got %q", resp.ServerStatus)
	}
}

func TestHandleHeartbeatWrongIdentity(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{
				ID:       id,
				SPIFFEID: "spiffe://example.org/ns/default/sa/server",
				Status:   types.StatusApproved,
			}, nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	h := NewHandler(svc, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/server-1/heartbeat", nil)
	req = withIdentity(req, &identitypkg.Identity{
		Type: identitypkg.IdentityMTLS,
		ID:   "spiffe://example.org/ns/default/sa/other",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
}

func TestHandleHeartbeatUnknownRegistration(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)
	h := NewHandler(svc, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/server-1/heartbeat", nil)
	req = withIdentity(req, &identitypkg.Identity{
		Type: identitypkg.IdentityMTLS,
		ID:   "spiffe://example.org/ns/default/sa/server",
	})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleHeartbeatPublishesHealthChangedEvent(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{
				ID:       id,
				Name:     "svc-alpha",
				SPIFFEID: "spiffe://example.org/ns/default/sa/server",
				Status:   types.StatusApproved,
			}, nil
		},
		updateStatusFn: func(ctx context.Context, id string, status types.ServerStatus) error {
			return nil
		},
		updateHeartbeatFn: func(ctx context.Context, id string) error {
			return nil
		},
	}

	published := false
	publisher := &mockLifecyclePublisher{
		publishServerHealthFn: func(ctx context.Context, serverID string, name string, oldStatus, newStatus types.ServerStatus) error {
			published = true
			if oldStatus != types.StatusApproved || newStatus != types.StatusActive {
				t.Fatalf("unexpected status transition: %s -> %s", oldStatus, newStatus)
			}
			return nil
		},
	}

	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{HeartbeatTTL: 30 * time.Second}, nil)
	svc.SetLifecycleEventPublisher(publisher)
	h := NewHandler(svc, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/server-1/heartbeat", bytes.NewBufferString(`{"status":"healthy"}`))
	req = withIdentity(req, &identitypkg.Identity{
		Type: identitypkg.IdentityMTLS,
		ID:   "spiffe://example.org/ns/default/sa/server",
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !published {
		t.Fatal("expected server health changed event to be published")
	}
}
