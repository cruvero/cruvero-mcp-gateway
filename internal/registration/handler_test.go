package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestHandleRegisterValid(t *testing.T) {
	t.Parallel()

	service := &mockRegistrationService{
		registerFn: func(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error) {
			if caller.ID != "spiffe://example.org/ns/default/sa/server" {
				t.Fatalf("unexpected caller id: %q", caller.ID)
			}
			return &RegistrationResponse{InstanceID: "server-1", Status: types.StatusPending}, nil
		},
	}

	h := NewHandler(service, testRegistrationLogger())
	router := h.Routes()

	body := `{"service_name":"svc-alpha","version":"1.0.0","listen":{"host":"svc-alpha","port":8080,"protocol":"https"},"capabilities":{"tools":["tool.alpha"],"resources":[],"prompts":[]},"labels":{"team":"platform"}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identitypkg.ScopeAdmin}})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
}

func TestHandleRegisterInvalidBody(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{"))
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identitypkg.ScopeAdmin}})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleList(t *testing.T) {
	t.Parallel()

	service := &mockRegistrationService{
		listFn: func(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
			if filter.NamePattern != "svc" {
				t.Fatalf("unexpected name filter: %q", filter.NamePattern)
			}
			return []types.ServerRecord{{ID: "server-1", Name: "svc-alpha"}}, nil
		},
	}

	h := NewHandler(service, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/?name=svc", nil)
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identitypkg.ScopeAdmin}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var records []types.ServerRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &records); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestHandleDelete(t *testing.T) {
	t.Parallel()

	service := &mockRegistrationService{
		deregisterFn: func(ctx context.Context, caller *identitypkg.Identity, id string) error {
			if id != "server-1" {
				t.Fatalf("unexpected id: %q", id)
			}
			return nil
		},
	}

	h := NewHandler(service, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/server-1", nil)
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
}

func TestHandleDeleteNotFound(t *testing.T) {
	t.Parallel()

	service := &mockRegistrationService{
		deregisterFn: func(ctx context.Context, caller *identitypkg.Identity, id string) error {
			return ErrNotFound
		},
	}

	h := NewHandler(service, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/server-1", nil)
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleListRequiresAdminScope(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identitypkg.ScopeRead}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
}

type mockRegistrationService struct {
	registerFn   func(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error)
	listFn       func(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error)
	deregisterFn func(ctx context.Context, caller *identitypkg.Identity, id string) error
}

func (m *mockRegistrationService) Register(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, caller, req)
	}
	return nil, ErrInvalidRequest
}

func (m *mockRegistrationService) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, nil
}

func (m *mockRegistrationService) Deregister(ctx context.Context, caller *identitypkg.Identity, id string) error {
	if m.deregisterFn != nil {
		return m.deregisterFn(ctx, caller, id)
	}
	return nil
}

func withIdentity(req *http.Request, id *identitypkg.Identity) *http.Request {
	return req.WithContext(identitypkg.WithIdentity(req.Context(), id))
}

func testRegistrationLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
