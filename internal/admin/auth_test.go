package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testAEAD(t *testing.T) cipher.AEAD {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("create gcm: %v", err)
	}
	return aead
}

func encryptTestSession(t *testing.T, aead cipher.AEAD, session *AdminSession) string {
	t.Helper()
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	sealed := aead.Seal(nonce, nonce, data, nil)
	return base64.URLEncoding.EncodeToString(sealed)
}

func TestAdminAuthMiddleware_NoSession(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	handler := AdminAuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/admin/login" {
		t.Fatalf("expected redirect to /admin/login, got %s", loc)
	}
}

func TestAdminAuthMiddleware_ValidSession(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	session := &AdminSession{
		Subject:   "user-1",
		Email:     "user@example.com",
		Scopes:    []string{"admin"},
		CSRFToken: "csrf-123",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	cookieValue := encryptTestSession(t, aead, session)

	var gotSession *AdminSession
	handler := AdminAuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFromContext(r.Context())
		if ok {
			gotSession = s
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotSession == nil {
		t.Fatalf("expected session in context")
	}
	if gotSession.Subject != "user-1" {
		t.Fatalf("expected subject user-1, got %q", gotSession.Subject)
	}
}

func TestAdminAuthMiddleware_ExpiredSession(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	session := &AdminSession{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	cookieValue := encryptTestSession(t, aead, session)

	handler := AdminAuthMiddleware(auth)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect for expired session, got %d", w.Code)
	}
}

func TestCSRFMiddleware_GET(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET should not require CSRF, got %d", w.Code)
	}
}

func TestCSRFMiddleware_POST_Missing(t *testing.T) {
	session := &AdminSession{CSRFToken: "valid-token"}
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/tools/exec", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), sessionContextKey, session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req.WithContext(ctx))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing CSRF token, got %d", w.Code)
	}
}

func TestCSRFMiddleware_POST_Valid(t *testing.T) {
	session := &AdminSession{CSRFToken: "valid-token"}
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/tools/exec", strings.NewReader("_csrf=valid-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), sessionContextKey, session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req.WithContext(ctx))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid CSRF, got %d", w.Code)
	}
}

func TestCSRFMiddleware_POST_InvalidToken(t *testing.T) {
	session := &AdminSession{CSRFToken: "valid-token"}
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/tools/exec", strings.NewReader("_csrf=wrong-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), sessionContextKey, session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req.WithContext(ctx))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid CSRF, got %d", w.Code)
	}
}

func TestPKCEVerifier(t *testing.T) {
	verifier := generatePKCEVerifier()
	if len(verifier) == 0 {
		t.Fatalf("expected non-empty verifier")
	}

	challenge := pkceS256Challenge(verifier)
	if len(challenge) == 0 {
		t.Fatalf("expected non-empty challenge")
	}

	// Verify determinism.
	challenge2 := pkceS256Challenge(verifier)
	if challenge != challenge2 {
		t.Fatalf("challenge should be deterministic")
	}
}

func TestMergeScopes(t *testing.T) {
	scopes := mergeScopes([]string{"admin", "user"}, "admin openid")
	if len(scopes) != 3 {
		t.Fatalf("expected 3 unique scopes, got %d: %v", len(scopes), scopes)
	}
}

func TestHasScope(t *testing.T) {
	if !hasScope([]string{"admin", "user"}, "admin") {
		t.Fatalf("expected admin scope to be found")
	}
	if hasScope([]string{"user"}, "admin") {
		t.Fatalf("expected admin scope not found")
	}
}

func TestSessionFromContext_Missing(t *testing.T) {
	_, ok := SessionFromContext(context.Background())
	if ok {
		t.Fatalf("expected no session from empty context")
	}
}
