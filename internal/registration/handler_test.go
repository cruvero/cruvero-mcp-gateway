package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestHandleRegisterMissingIdentity(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, testRegistrationLogger())
	router := h.Routes()

	body := `{"service_name":"svc-alpha","version":"1.0.0","listen":{"host":"svc-alpha","port":8080,"protocol":"https"},"capabilities":{"tools":["tool.alpha"]}}`
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
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

func TestHandleListInvalidFilters(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, testRegistrationLogger())
	router := h.Routes()

	tests := []string{
		"/?status=unknown",
		"/?limit=abc",
		"/?offset=-1",
	}

	for _, url := range tests {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identitypkg.ScopeAdmin}})
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400 for %s, got %d", url, rec.Code)
		}
	}
}

func TestHandleListFilterFields(t *testing.T) {
	t.Parallel()

	service := &mockRegistrationService{
		listFn: func(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
			if filter.Status == nil || *filter.Status != types.StatusPending {
				t.Fatalf("unexpected status filter: %+v", filter.Status)
			}
			if filter.Limit != 5 || filter.Offset != 7 {
				t.Fatalf("unexpected pagination filter: %+v", filter)
			}
			return []types.ServerRecord{}, nil
		},
	}

	h := NewHandler(service, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/?status=pending&limit=5&offset=7", nil)
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server", Scopes: []string{identitypkg.ScopeAdmin}})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
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

func TestHandleDeleteMissingID(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/%20", nil)
	req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleDeleteMissingIdentity(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, testRegistrationLogger())
	router := h.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/server-1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
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

func TestHandlerMapsServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		err        error
		wantStatus int
	}{
		{name: "invalid request", path: "/server-1", err: ErrInvalidRequest, wantStatus: http.StatusBadRequest},
		{name: "unauthorized", path: "/server-1", err: ErrUnauthorized, wantStatus: http.StatusUnauthorized},
		{name: "forbidden", path: "/server-1", err: ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "not found", path: "/server-1", err: ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "internal", path: "/server-1", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &mockRegistrationService{
				deregisterFn: func(ctx context.Context, caller *identitypkg.Identity, id string) error {
					return tt.err
				},
			}
			h := NewHandler(service, testRegistrationLogger())
			router := h.Routes()

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			req = withIdentity(req, &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/server"})
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestNewHandlerDefaultsLogger(t *testing.T) {
	t.Parallel()

	h := NewHandler(&mockRegistrationService{}, nil)
	if h.logger == nil {
		t.Fatal("expected logger to be initialized")
	}
}

type mockRegistrationService struct {
	registerFn   func(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error)
	heartbeatFn  func(ctx context.Context, caller *identitypkg.Identity, id string, req HeartbeatRequest) (*HeartbeatResponse, error)
	listFn       func(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error)
	deregisterFn func(ctx context.Context, caller *identitypkg.Identity, id string) error
}

func (m *mockRegistrationService) Register(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, caller, req)
	}
	return nil, ErrInvalidRequest
}

func (m *mockRegistrationService) Heartbeat(ctx context.Context, caller *identitypkg.Identity, id string, req HeartbeatRequest) (*HeartbeatResponse, error) {
	if m.heartbeatFn != nil {
		return m.heartbeatFn(ctx, caller, id, req)
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
