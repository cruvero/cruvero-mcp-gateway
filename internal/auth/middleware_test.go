package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestAuthMiddlewarePassthroughForExistingIdentity(t *testing.T) {
	t.Parallel()

	handler := AuthMiddleware(AuthOptions{Logger: testAuthLogger()})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Type: identity.IdentityMTLS, ID: "spiffe://example.org/service"}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRoutesJWTToOIDC(t *testing.T) {
	t.Parallel()

	validatorCalled := false
	validator := &OIDCValidator{
		validateFunc: func(ctx context.Context, rawToken string) (*identity.Identity, error) {
			validatorCalled = true
			if rawToken != "header.payload.signature" {
				t.Fatalf("unexpected raw token: %q", rawToken)
			}
			return &identity.Identity{Type: identity.IdentityOIDC, ID: "user-1", Scopes: []string{identity.ScopeRead}}, nil
		},
	}

	handler := AuthMiddleware(AuthOptions{
		OIDCValidator: validator,
		Logger:        testAuthLogger(),
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identity.FromContext(r.Context())
		if !ok || id.Type != identity.IdentityOIDC {
			t.Fatal("expected oidc identity in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer header.payload.signature")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !validatorCalled {
		t.Fatal("expected OIDC validator to be called for JWT-shaped token")
	}
}

func TestAuthMiddlewareRoutesNonJWTToAPIKey(t *testing.T) {
	t.Parallel()

	plaintext, lookupHash, bcryptHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	storeCalled := false
	store := &mockAPIKeyStore{
		getByLookupHashFunc: func(ctx context.Context, lookup string) (*types.APIKey, error) {
			storeCalled = true
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

	handler := AuthMiddleware(AuthOptions{
		APIKeyStore: store,
		Logger:      testAuthLogger(),
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := identity.FromContext(r.Context())
		if !ok || id.Type != identity.IdentityAPIKey {
			t.Fatal("expected api key identity in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !storeCalled {
		t.Fatal("expected API key store lookup to be called")
	}
}

func TestAuthMiddlewareNoAuthHeader(t *testing.T) {
	t.Parallel()

	handler := AuthMiddleware(AuthOptions{Logger: testAuthLogger()})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	assertAuthError(t, rec, "missing authorization header")
}

func TestAuthMiddlewareOIDCNotConfigured(t *testing.T) {
	t.Parallel()

	handler := AuthMiddleware(AuthOptions{Logger: testAuthLogger()})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer header.payload.signature")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	assertAuthError(t, rec, "oidc validator is not configured")
}

func TestAuthMiddlewareAPIKeyStoreNotConfigured(t *testing.T) {
	t.Parallel()

	handler := AuthMiddleware(AuthOptions{OIDCValidator: &OIDCValidator{validateFunc: func(ctx context.Context, rawToken string) (*identity.Identity, error) {
		return nil, errors.New("should not be called")
	}}, Logger: testAuthLogger()})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer opaque-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	assertAuthError(t, rec, "api key store is not configured")
}

func TestIsJWTToken(t *testing.T) {
	t.Parallel()

	if !isJWTToken("a.b.c") {
		t.Fatal("expected jwt-like token to be detected")
	}
	if isJWTToken("a.b") {
		t.Fatal("expected two-segment token to be non-jwt")
	}
	if isJWTToken("a..c") {
		t.Fatal("expected token with empty segment to be non-jwt")
	}
}
