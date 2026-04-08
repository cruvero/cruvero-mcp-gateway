package registration

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// stubToolLister returns the tools from the server record's capabilities.
type stubToolLister struct{}

func (s *stubToolLister) ListToolNames(_ context.Context, server types.ServerRecord) ([]string, error) {
	return server.Capabilities.Tools, nil
}

func newTestRefreshHandler(t *testing.T, serverStore *stubServerStore) (*RefreshHandler, *Service) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	svc := NewService(serverStore, nil, nil, logger)
	svc.SetCapabilityIndex(NewCapabilityIndex())
	svc.SetToolLister(&stubToolLister{})
	return NewRefreshHandler(svc, serverStore, logger), svc
}

func buildRefreshRequest(t *testing.T, id string, caller *identitypkg.Identity) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/"+id+"/capabilities", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if caller != nil {
		req = req.WithContext(identitypkg.WithIdentity(req.Context(), caller))
	}
	return req
}

func TestRefreshHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         string
		caller     *identitypkg.Identity
		record     *types.ServerRecord
		storeErr   error
		wantStatus int
		wantField  string
	}{
		{
			name:       "success",
			id:         "server-1",
			caller:     &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://test/server"},
			record:     &types.ServerRecord{ID: "server-1", SPIFFEID: "spiffe://test/server", Capabilities: types.Capability{Tools: []string{"tool1", "tool2"}, Resources: []string{"res1"}}},
			wantStatus: http.StatusOK,
			wantField:  "tools_count",
		},
		{
			name:       "missing identity",
			id:         "server-1",
			caller:     nil,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "server not found",
			id:         "missing",
			caller:     &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://test/server"},
			storeErr:   sql.ErrNoRows,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "SPIFFE mismatch",
			id:         "server-1",
			caller:     &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://test/attacker"},
			record:     &types.ServerRecord{ID: "server-1", SPIFFEID: "spiffe://test/server"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "empty id",
			id:         "",
			caller:     &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://test/server"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &stubServerStore{
				getFn: func(_ context.Context, id string) (*types.ServerRecord, error) {
					if tt.storeErr != nil {
						return nil, tt.storeErr
					}
					if tt.record != nil && tt.record.ID == id {
						return tt.record, nil
					}
					return nil, sql.ErrNoRows
				},
				updateFn: func(_ context.Context, _ *types.ServerRecord) error {
					return nil
				},
			}
			handler, _ := newTestRefreshHandler(t, store)

			req := buildRefreshRequest(t, tt.id, tt.caller)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d; body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantField != "" {
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if _, ok := body[tt.wantField]; !ok {
					t.Fatalf("expected response field %q, got %v", tt.wantField, body)
				}
			}
		})
	}
}

func TestRefreshHandlerRateLimit(t *testing.T) {
	t.Parallel()

	record := &types.ServerRecord{
		ID:           "server-1",
		SPIFFEID:     "spiffe://test/server",
		Capabilities: types.Capability{Tools: []string{"tool1"}},
	}
	store := &stubServerStore{
		getFn: func(_ context.Context, id string) (*types.ServerRecord, error) {
			if id == record.ID {
				return record, nil
			}
			return nil, sql.ErrNoRows
		},
		updateFn: func(_ context.Context, _ *types.ServerRecord) error {
			return nil
		},
	}
	handler, _ := newTestRefreshHandler(t, store)
	caller := &identitypkg.Identity{Type: identitypkg.IdentityMTLS, ID: "spiffe://test/server"}

	// Send refreshRateLimit requests; all should succeed.
	for i := 0; i < refreshRateLimit; i++ {
		req := buildRefreshRequest(t, "server-1", caller)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	// The next request should be rate limited.
	req := buildRefreshRequest(t, "server-1", caller)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", rec.Code)
	}
}

func TestRefreshHandlerNilLogger(t *testing.T) {
	t.Parallel()

	handler := NewRefreshHandler(nil, nil, nil)
	if handler.logger == nil {
		t.Fatal("expected default logger")
	}
}

// stubServerStore provides a minimal server store for refresh handler tests.
type stubServerStore struct {
	getFn         func(ctx context.Context, id string) (*types.ServerRecord, error)
	updateFn      func(ctx context.Context, record *types.ServerRecord) error
	getBySPIFFEFn func(ctx context.Context, spiffeID string) (*types.ServerRecord, error)
}

func (s *stubServerStore) Create(_ context.Context, _ *types.ServerRecord) error { return nil }
func (s *stubServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	if s.getFn != nil {
		return s.getFn(ctx, id)
	}
	return nil, sql.ErrNoRows
}
func (s *stubServerStore) GetByName(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, sql.ErrNoRows
}
func (s *stubServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	if s.getBySPIFFEFn != nil {
		return s.getBySPIFFEFn(ctx, spiffeID)
	}
	return nil, sql.ErrNoRows
}
func (s *stubServerStore) List(_ context.Context, _ types.ServerFilter) ([]types.ServerRecord, error) {
	return nil, nil
}
func (s *stubServerStore) Update(ctx context.Context, record *types.ServerRecord) error {
	if s.updateFn != nil {
		return s.updateFn(ctx, record)
	}
	return nil
}
func (s *stubServerStore) UpdateStatus(_ context.Context, _ string, _ types.ServerStatus) error {
	return nil
}
func (s *stubServerStore) UpdateHeartbeat(_ context.Context, _ string) error { return nil }
func (s *stubServerStore) AcknowledgeRegistration(_ context.Context, _ string, _ int64, _ string, _ string, _ time.Time) error {
	return nil
}
func (s *stubServerStore) UpdateRateLimit(_ context.Context, _ string, _, _ *int) error { return nil }
func (s *stubServerStore) Delete(_ context.Context, _ string) error                    { return nil }
func (s *stubServerStore) ListStale(_ context.Context, _ time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}
func (s *stubServerStore) ListExpired(_ context.Context, _ time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}
