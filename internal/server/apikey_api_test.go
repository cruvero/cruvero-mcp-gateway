package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

type mockAPIKeyStore struct {
	createFunc          func(ctx context.Context, key *types.APIKey) error
	getByLookupHashFunc func(ctx context.Context, lookupHash string) (*types.APIKey, error)
	listFunc            func(ctx context.Context) ([]types.APIKey, error)
	revokeFunc          func(ctx context.Context, id string) error
	deleteExpiredFunc   func(ctx context.Context) (int64, error)
}

func (m *mockAPIKeyStore) Create(ctx context.Context, key *types.APIKey) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, key)
	}
	return nil
}

func (m *mockAPIKeyStore) GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error) {
	if m.getByLookupHashFunc != nil {
		return m.getByLookupHashFunc(ctx, lookupHash)
	}
	return &types.APIKey{ID: "key-1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
}

func (m *mockAPIKeyStore) List(ctx context.Context) ([]types.APIKey, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return nil, nil
}

func (m *mockAPIKeyStore) Revoke(ctx context.Context, id string) error {
	if m.revokeFunc != nil {
		return m.revokeFunc(ctx, id)
	}
	return nil
}

func (m *mockAPIKeyStore) DeleteExpired(ctx context.Context) (int64, error) {
	if m.deleteExpiredFunc != nil {
		return m.deleteExpiredFunc(ctx)
	}
	return 0, nil
}

func testAPIKeyHandler(store *mockAPIKeyStore) *APIKeyAPIHandler {
	return NewAPIKeyAPIHandler(store, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func TestAPIKeyAPIHandlerCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		body           string
		store          *mockAPIKeyStore
		wantStatus     int
		wantSubstring  string
	}{
		{
			name:          "valid create returns 201 with api key",
			body:          `{"name":"test-key","scopes":["read","write"]}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusCreated,
			wantSubstring: "api_key",
		},
		{
			name:          "missing name returns 400",
			body:          `{"scopes":["read"]}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusBadRequest,
			wantSubstring: "name is required",
		},
		{
			name:          "empty name returns 400",
			body:          `{"name":"  ","scopes":["read"]}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusBadRequest,
			wantSubstring: "name is required",
		},
		{
			name:          "invalid json returns 400",
			body:          `{invalid`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusBadRequest,
			wantSubstring: "invalid request body",
		},
		{
			name:          "empty scopes defaults to read",
			body:          `{"name":"test-key"}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusCreated,
			wantSubstring: `"read"`,
		},
		{
			name:          "invalid expires returns 400",
			body:          `{"name":"test-key","expires":"invalid"}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusBadRequest,
			wantSubstring: "invalid day duration",
		},
		{
			name: "store create error returns 500",
			body: `{"name":"test-key"}`,
			store: &mockAPIKeyStore{
				createFunc: func(_ context.Context, _ *types.APIKey) error {
					return fmt.Errorf("db error")
				},
			},
			wantStatus:    http.StatusInternalServerError,
			wantSubstring: "failed to create api key",
		},
		{
			name:          "with client_id and profile",
			body:          `{"name":"test-key","client_id":"my-client","profile":"premium"}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusCreated,
			wantSubstring: "my-client",
		},
		{
			name:          "with day duration expires",
			body:          `{"name":"test-key","expires":"30d"}`,
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusCreated,
			wantSubstring: "expires_at",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := testAPIKeyHandler(tc.store)
			router := handler.Routes()

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}

			if tc.wantSubstring != "" && !strings.Contains(rec.Body.String(), tc.wantSubstring) {
				t.Fatalf("expected response to contain %q, got %s", tc.wantSubstring, rec.Body.String())
			}
		})
	}
}

func TestAPIKeyAPIHandlerCreateResponseFields(t *testing.T) {
	t.Parallel()

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(_ context.Context, _ string) (*types.APIKey, error) {
			return &types.APIKey{
				ID:        "key-123",
				Name:      "test-key",
				ClientID:  "test-key",
				Scopes:    []string{"read"},
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	handler := testAPIKeyHandler(store)
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"test-key"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var resp createAPIKeyAPIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != "key-123" {
		t.Fatalf("expected id key-123, got %q", resp.ID)
	}
	if resp.APIKey == "" {
		t.Fatal("expected non-empty api_key")
	}
	if !strings.HasPrefix(resp.APIKey, "mcpgw_") {
		t.Fatalf("expected api_key prefix mcpgw_, got %q", resp.APIKey)
	}
}

func TestAPIKeyAPIHandlerList(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		store         *mockAPIKeyStore
		wantStatus    int
		wantSubstring string
	}{
		{
			name: "returns keys from store",
			store: &mockAPIKeyStore{
				listFunc: func(_ context.Context) ([]types.APIKey, error) {
					return []types.APIKey{
						{
							ID:            "key-1",
							Name:          "my-key",
							ClientID:      "client-1",
							Scopes:        []string{"read"},
							PolicyProfile: "default",
							ExpiresAt:     &expiresAt,
							CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						},
					}, nil
				},
			},
			wantStatus:    http.StatusOK,
			wantSubstring: "my-key",
		},
		{
			name: "empty list returns empty array",
			store: &mockAPIKeyStore{
				listFunc: func(_ context.Context) ([]types.APIKey, error) {
					return []types.APIKey{}, nil
				},
			},
			wantStatus:    http.StatusOK,
			wantSubstring: "[]",
		},
		{
			name: "store error returns 500",
			store: &mockAPIKeyStore{
				listFunc: func(_ context.Context) ([]types.APIKey, error) {
					return nil, fmt.Errorf("db error")
				},
			},
			wantStatus:    http.StatusInternalServerError,
			wantSubstring: "failed to list",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := testAPIKeyHandler(tc.store)
			router := handler.Routes()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantSubstring != "" && !strings.Contains(rec.Body.String(), tc.wantSubstring) {
				t.Fatalf("expected response to contain %q, got %s", tc.wantSubstring, rec.Body.String())
			}
		})
	}
}

func TestAPIKeyAPIHandlerListNoSecrets(t *testing.T) {
	t.Parallel()

	store := &mockAPIKeyStore{
		listFunc: func(_ context.Context) ([]types.APIKey, error) {
			return []types.APIKey{
				{
					ID:            "key-1",
					Name:          "my-key",
					ClientID:      "client-1",
					Scopes:        []string{"read"},
					KeyLookupHash: "secret-lookup-hash",
					KeyBcryptHash: "secret-bcrypt-hash",
					CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			}, nil
		},
	}

	handler := testAPIKeyHandler(store)
	router := handler.Routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "secret-lookup-hash") || strings.Contains(body, "secret-bcrypt-hash") {
		t.Fatal("response must not contain key hashes")
	}
}

func TestAPIKeyAPIHandlerRevoke(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		id            string
		store         *mockAPIKeyStore
		wantStatus    int
		wantSubstring string
	}{
		{
			name:          "revoke existing key",
			id:            "key-1",
			store:         &mockAPIKeyStore{},
			wantStatus:    http.StatusOK,
			wantSubstring: "revoked",
		},
		{
			name: "store error returns 500",
			id:   "key-1",
			store: &mockAPIKeyStore{
				revokeFunc: func(_ context.Context, _ string) error {
					return fmt.Errorf("db error")
				},
			},
			wantStatus:    http.StatusInternalServerError,
			wantSubstring: "failed to revoke",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := testAPIKeyHandler(tc.store)
			router := handler.Routes()

			req := httptest.NewRequest(http.MethodDelete, "/"+tc.id, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if tc.wantSubstring != "" && !strings.Contains(rec.Body.String(), tc.wantSubstring) {
				t.Fatalf("expected response to contain %q, got %s", tc.wantSubstring, rec.Body.String())
			}
		})
	}
}

func TestMountAPIKeyAPI(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	store := &mockAPIKeyStore{
		listFunc: func(_ context.Context) ([]types.APIKey, error) {
			return []types.APIKey{}, nil
		},
	}
	handler := NewAPIKeyAPIHandler(store, testLogger())
	srv.MountAPIKeyAPI(handler.Routes())

	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMountAPIKeyAPINilGuards(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	srv.MountAPIKeyAPI(nil)

	var nilSrv *Server
	nilSrv.MountAPIKeyAPI(http.NotFoundHandler())
}

func TestMountAPIKeyAPIWithAuthMiddleware(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	authCalled := false
	srv.SetProxyAuthMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCalled = true
			next.ServeHTTP(w, r)
		})
	})

	store := &mockAPIKeyStore{
		listFunc: func(_ context.Context) ([]types.APIKey, error) {
			return []types.APIKey{}, nil
		},
	}
	handler := NewAPIKeyAPIHandler(store, testLogger())
	srv.MountAPIKeyAPI(handler.Routes())

	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if !authCalled {
		t.Fatal("expected auth middleware to be called")
	}
}

func TestDedupeScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  int
	}{
		{"deduplicates", []string{"read", "read", "write"}, 2},
		{"trims whitespace", []string{" read ", "write"}, 2},
		{"skips empty", []string{"read", "", "write"}, 2},
		{"empty input", []string{}, 0},
		{"nil input", nil, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dedupeScopes(tc.input)
			if len(got) != tc.want {
				t.Fatalf("expected %d scopes, got %d: %v", tc.want, len(got), got)
			}
		})
	}
}

func TestFormatTimePtr(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()
		if got := formatTimePtr(nil); got != nil {
			t.Fatalf("expected nil, got %v", *got)
		}
	})

	t.Run("non-nil formats as rfc3339", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
		got := formatTimePtr(&ts)
		if got == nil {
			t.Fatal("expected non-nil")
		}
		if *got != "2026-06-15T12:00:00Z" {
			t.Fatalf("expected 2026-06-15T12:00:00Z, got %s", *got)
		}
	})
}

func TestParseAPIKeyExpiry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantNil bool
		wantErr bool
	}{
		{"empty returns nil", "", true, false},
		{"valid hours", "24h", false, false},
		{"valid days", "30d", false, false},
		{"invalid string", "invalid", false, true},
		{"zero days", "0d", false, true},
		{"negative hours", "-1h", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseAPIKeyExpiry(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil && got != nil {
				t.Fatal("expected nil result")
			}
			if !tc.wantNil && !tc.wantErr && got == nil {
				t.Fatal("expected non-nil result")
			}
		})
	}
}

func TestNewAPIKeyAPIHandler(t *testing.T) {
	t.Parallel()

	store := &mockAPIKeyStore{}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	handler := NewAPIKeyAPIHandler(store, logger)
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.store != store {
		t.Fatal("store not set correctly")
	}
}

