package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/identity"
)

func TestRequireScope(t *testing.T) {
	t.Parallel()

	handler := RequireScope(identity.ScopeWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Scopes: []string{identity.ScopeWrite}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	forbiddenReq = forbiddenReq.WithContext(identity.WithIdentity(forbiddenReq.Context(), &identity.Identity{Scopes: []string{identity.ScopeRead}}))
	forbiddenRec := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", forbiddenRec.Code)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for missing identity, got %d", missingRec.Code)
	}
}

func TestRequireAnyScope(t *testing.T) {
	t.Parallel()

	handler := RequireAnyScope(identity.ScopeWrite, identity.ScopeAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Scopes: []string{identity.ScopeRead, identity.ScopeAdmin}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	forbiddenReq := httptest.NewRequest(http.MethodGet, "/protected", nil)
	forbiddenReq = forbiddenReq.WithContext(identity.WithIdentity(forbiddenReq.Context(), &identity.Identity{Scopes: []string{identity.ScopeRead}}))
	forbiddenRec := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", forbiddenRec.Code)
	}
}

func TestRequireAnyScopeNoScopesConfigured(t *testing.T) {
	t.Parallel()

	handler := RequireAnyScope()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
