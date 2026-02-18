package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestAPIKeyMiddlewareValidKey(t *testing.T) {
	t.Parallel()

	plaintext, lookupHash, bcryptHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			if lookup != lookupHash {
				t.Fatalf("expected lookup hash %q, got %q", lookupHash, lookup)
			}
			return &types.APIKey{
				KeyLookupHash: lookupHash,
				KeyBcryptHash: bcryptHash,
				ClientID:      "client-1",
				Scopes:        []string{identity.ScopeRead, identity.ScopeWrite},
			}, nil
		},
	}

	handler := APIKeyMiddleware(store, testAuthLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identity.FromContext(r.Context())
		if !ok {
			t.Fatal("expected identity in context")
		}
		if id.Type != identity.IdentityAPIKey {
			t.Fatalf("expected identity type apikey, got %q", id.Type)
		}
		if id.ID != "client-1" {
			t.Fatalf("expected client id client-1, got %q", id.ID)
		}
		if !id.HasScope(identity.ScopeWrite) {
			t.Fatal("expected write scope")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIKeyMiddlewareValidKeyFromXAPIKeyHeader(t *testing.T) {
	t.Parallel()

	plaintext, lookupHash, bcryptHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			if lookup != lookupHash {
				t.Fatalf("expected lookup hash %q, got %q", lookupHash, lookup)
			}
			return &types.APIKey{
				KeyLookupHash: lookupHash,
				KeyBcryptHash: bcryptHash,
				ClientID:      "client-1",
				Scopes:        []string{identity.ScopeRead},
			}, nil
		},
	}

	handler := APIKeyMiddleware(store, testAuthLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	req.Header.Set("X-API-Key", plaintext)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIKeyMiddlewareLookupHashMismatch(t *testing.T) {
	t.Parallel()

	plaintext, _, bcryptHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			return &types.APIKey{
				KeyLookupHash: "different-lookup-hash",
				KeyBcryptHash: bcryptHash,
				ClientID:      "client-1",
			}, nil
		},
	}

	rec := runAPIKeyRequest(t, store, "Bearer "+plaintext)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKeyMiddlewareBcryptMismatch(t *testing.T) {
	t.Parallel()

	plaintext, lookupHash, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	otherHash, err := HashAPIKey(APIKeyPrefix + "different")
	if err != nil {
		t.Fatalf("hash mismatched key: %v", err)
	}

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			return &types.APIKey{
				KeyLookupHash: lookupHash,
				KeyBcryptHash: otherHash,
				ClientID:      "client-1",
			}, nil
		},
	}

	rec := runAPIKeyRequest(t, store, "Bearer "+plaintext)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKeyMiddlewareMissingOrInvalidHeader(t *testing.T) {
	t.Parallel()

	store := &mockAPIKeyStore{}

	rec := runAPIKeyRequest(t, store, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for missing auth header, got %d", rec.Code)
	}

	rec = runAPIKeyRequest(t, store, "Bearer not-a-gateway-key")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for invalid key format, got %d", rec.Code)
	}
}

func TestAPIKeyMiddlewareExpiredKey(t *testing.T) {
	t.Parallel()

	plaintext, lookupHash, bcryptHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	expired := time.Now().UTC().Add(-1 * time.Minute)
	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			return &types.APIKey{
				KeyLookupHash: lookupHash,
				KeyBcryptHash: bcryptHash,
				ClientID:      "client-1",
				ExpiresAt:     &expired,
			}, nil
		},
	}

	rec := runAPIKeyRequest(t, store, "Bearer "+plaintext)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
	assertAuthError(t, rec, "api key is expired")
}

func TestAPIKeyMiddlewareKeyNotFound(t *testing.T) {
	t.Parallel()

	plaintext, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			return nil, sql.ErrNoRows
		},
	}

	rec := runAPIKeyRequest(t, store, "Bearer "+plaintext)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestAPIKeyMiddlewareStoreError(t *testing.T) {
	t.Parallel()

	plaintext, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			return nil, errors.New("db unavailable")
		},
	}

	rec := runAPIKeyRequest(t, store, "Bearer "+plaintext)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func runAPIKeyRequest(t *testing.T, store *mockAPIKeyStore, authHeader string) *httptest.ResponseRecorder {
	t.Helper()

	handler := APIKeyMiddleware(store, testAuthLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/protected", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type mockAPIKeyStore struct {
	getByLookupHashFunc func(ctx context.Context, lookupHash string) (*types.APIKey, error)
}

func (m *mockAPIKeyStore) Create(ctx context.Context, key *types.APIKey) error {
	return nil
}

func (m *mockAPIKeyStore) GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error) {
	if m.getByLookupHashFunc != nil {
		return m.getByLookupHashFunc(ctx, lookupHash)
	}
	return nil, sql.ErrNoRows
}

func (m *mockAPIKeyStore) List(ctx context.Context) ([]types.APIKey, error) {
	return nil, nil
}

func (m *mockAPIKeyStore) Revoke(ctx context.Context, id string) error {
	return nil
}

func (m *mockAPIKeyStore) DeleteExpired(ctx context.Context) (int64, error) {
	return 0, nil
}

func assertAuthError(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if payload["error"] != expected {
		t.Fatalf("expected error %q, got %q", expected, payload["error"])
	}
}

func testAuthLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
