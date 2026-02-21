package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
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

	handler := AdminAuthMiddleware(auth, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	handler := AdminAuthMiddleware(auth, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := AdminAuthMiddleware(auth, false)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	verifier, err := generatePKCEVerifier()
	if err != nil {
		t.Fatalf("generatePKCEVerifier: %v", err)
	}
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

func TestEncryptDecryptSession_RoundTrip(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	original := &AdminSession{
		Subject:   "user-42",
		Email:     "test@example.com",
		Scopes:    []string{"admin", "read"},
		CSRFToken: "csrf-round-trip",
		ExpiresAt: time.Now().Add(time.Hour).Truncate(time.Second),
	}

	encrypted, err := auth.encryptSession(original)
	if err != nil {
		t.Fatalf("encryptSession: %v", err)
	}
	if encrypted == "" {
		t.Fatalf("expected non-empty encrypted string")
	}

	decrypted, err := auth.DecryptSession(encrypted)
	if err != nil {
		t.Fatalf("DecryptSession: %v", err)
	}

	if decrypted.Subject != original.Subject {
		t.Fatalf("subject mismatch: got %q, want %q", decrypted.Subject, original.Subject)
	}
	if decrypted.Email != original.Email {
		t.Fatalf("email mismatch: got %q, want %q", decrypted.Email, original.Email)
	}
	if decrypted.CSRFToken != original.CSRFToken {
		t.Fatalf("csrf mismatch: got %q, want %q", decrypted.CSRFToken, original.CSRFToken)
	}
	if len(decrypted.Scopes) != len(original.Scopes) {
		t.Fatalf("scopes length mismatch: got %d, want %d", len(decrypted.Scopes), len(original.Scopes))
	}
}

func TestEncryptSession_UniqueNonces(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	session := &AdminSession{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	enc1, err := auth.encryptSession(session)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := auth.encryptSession(session)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}

	if enc1 == enc2 {
		t.Fatalf("encryptions of same session should differ (unique nonce)")
	}
}

func TestDecryptSession_InvalidBase64(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	_, err := auth.DecryptSession("not-valid-base64!!!")
	if err == nil {
		t.Fatalf("expected error for invalid base64")
	}
}

func TestDecryptSession_TooShort(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	short := base64.URLEncoding.EncodeToString([]byte("x"))
	_, err := auth.DecryptSession(short)
	if err == nil {
		t.Fatalf("expected error for too-short data")
	}
}

func TestDecryptSession_Tampered(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	session := &AdminSession{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	encrypted, err := auth.encryptSession(session)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	raw, _ := base64.URLEncoding.DecodeString(encrypted)
	// Flip a byte to simulate tampering.
	raw[len(raw)-1] ^= 0xff
	tampered := base64.URLEncoding.EncodeToString(raw)

	_, err = auth.DecryptSession(tampered)
	if err == nil {
		t.Fatalf("expected error for tampered data")
	}
}

func TestDecryptSession_Expired(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	session := &AdminSession{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	encrypted, err := auth.encryptSession(session)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = auth.DecryptSession(encrypted)
	if err == nil {
		t.Fatalf("expected error for expired session")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected 'expired' in error, got: %v", err)
	}
}

func TestGenerateState_ExtractVerifier_RoundTrip(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	verifier := "test-pkce-verifier-string-12345"
	state, err := auth.generateState(verifier)
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if state == "" {
		t.Fatalf("expected non-empty state")
	}

	extracted, err := auth.extractVerifierFromState(state)
	if err != nil {
		t.Fatalf("extractVerifierFromState: %v", err)
	}

	if extracted != verifier {
		t.Fatalf("verifier mismatch: got %q, want %q", extracted, verifier)
	}
}

func TestExtractVerifierFromState_InvalidBase64(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	_, err := auth.extractVerifierFromState("not-valid-base64!!!")
	if err == nil {
		t.Fatalf("expected error for invalid base64")
	}
}

func TestExtractVerifierFromState_TooShort(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	short := base64.URLEncoding.EncodeToString([]byte("x"))
	_, err := auth.extractVerifierFromState(short)
	if err == nil {
		t.Fatalf("expected error for too-short state")
	}
}

func TestExtractVerifierFromState_Tampered(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	state, err := auth.generateState("verifier")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}

	raw, _ := base64.URLEncoding.DecodeString(state)
	raw[len(raw)-1] ^= 0xff
	tampered := base64.URLEncoding.EncodeToString(raw)

	_, err = auth.extractVerifierFromState(tampered)
	if err == nil {
		t.Fatalf("expected error for tampered state")
	}
}

func TestGenerateRandomBase64(t *testing.T) {
	result, err := generateRandomBase64(32)
	if err != nil {
		t.Fatalf("generateRandomBase64: %v", err)
	}
	if result == "" {
		t.Fatalf("expected non-empty result")
	}

	// Verify it decodes properly.
	decoded, err := base64.RawURLEncoding.DecodeString(result)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(decoded))
	}

	// Two calls should produce different values.
	result2, err := generateRandomBase64(32)
	if err != nil {
		t.Fatalf("second generateRandomBase64: %v", err)
	}
	if result == result2 {
		t.Fatalf("two random values should differ")
	}
}

func TestCSRFMiddleware_POST_NoSession(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/tools/exec", strings.NewReader("_csrf=token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for POST without session, got %d", w.Code)
	}
}

func TestCSRFMiddleware_POST_HeaderToken(t *testing.T) {
	session := &AdminSession{CSRFToken: "header-token"}
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/tools/exec", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "header-token")
	ctx := context.WithValue(req.Context(), sessionContextKey, session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req.WithContext(ctx))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid header CSRF, got %d", w.Code)
	}
}

func TestMergeScopes_Empty(t *testing.T) {
	scopes := mergeScopes(nil, "")
	if len(scopes) != 0 {
		t.Fatalf("expected empty scopes, got %v", scopes)
	}
}

func TestMergeScopes_DuplicatesAndWhitespace(t *testing.T) {
	scopes := mergeScopes([]string{"admin", " ", "user", "admin"}, "user openid  admin")
	// Expected: admin, user, openid (deduped, whitespace-only skipped).
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes, got %d: %v", len(scopes), scopes)
	}
}

func TestNewAdminAuth_NilConfig(t *testing.T) {
	_, err := NewAdminAuth(nil, nil)
	if err == nil {
		t.Fatalf("expected error for nil config")
	}
	if !strings.Contains(err.Error(), "config is nil") {
		t.Fatalf("expected 'config is nil' in error, got: %v", err)
	}
}

func TestNewAdminAuth_InvalidOIDCIssuer(t *testing.T) {
	cfg := &config.Config{
		OIDCIssuer: "http://127.0.0.1:1/invalid-issuer",
	}
	_, err := NewAdminAuth(cfg, nil)
	if err == nil {
		t.Fatalf("expected error for invalid OIDC issuer")
	}
	if !strings.Contains(err.Error(), "discover oidc provider") {
		t.Fatalf("expected 'discover oidc provider' in error, got: %v", err)
	}
}

func TestNewAdminAuth_InvalidOIDCIssuerWithLogger(t *testing.T) {
	cfg := &config.Config{
		OIDCIssuer: "http://127.0.0.1:1/invalid-issuer",
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	_, err := NewAdminAuth(cfg, logger)
	if err == nil {
		t.Fatalf("expected error for invalid OIDC issuer")
	}
}

func TestNewAdminAuth_SuccessfulInit(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"issuer": "` + srvURL + `",
				"authorization_endpoint": "` + srvURL + `/authorize",
				"token_endpoint": "` + srvURL + `/token",
				"jwks_uri": "` + srvURL + `/keys",
				"id_token_signing_alg_values_supported": ["RS256"]
			}`))
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	var sessionKey [32]byte
	copy(sessionKey[:], []byte("test-session-key-32-bytes-long!!"))

	cfg := &config.Config{
		OIDCIssuer:            srvURL,
		AdminOIDCClientID:     "test-client-id",
		AdminOIDCClientSecret: "test-client-secret",
		AdminSessionKey:       sessionKey,
		AdminSessionTTL:       time.Hour,
		ListenAddr:            ":8443",
	}

	auth, err := NewAdminAuth(cfg, nil)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	if auth == nil {
		t.Fatalf("expected non-nil auth")
	}
	if auth.sessionTTL != time.Hour {
		t.Fatalf("expected sessionTTL=1h, got %v", auth.sessionTTL)
	}
}

func TestNewAdminAuth_DefaultSessionTTL(t *testing.T) {
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"issuer": "` + srvURL + `",
				"authorization_endpoint": "` + srvURL + `/authorize",
				"token_endpoint": "` + srvURL + `/token",
				"jwks_uri": "` + srvURL + `/keys",
				"id_token_signing_alg_values_supported": ["RS256"]
			}`))
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	var sessionKey [32]byte
	copy(sessionKey[:], []byte("test-session-key-32-bytes-long!!"))

	cfg := &config.Config{
		OIDCIssuer:            srvURL,
		AdminOIDCClientID:     "test-client",
		AdminOIDCClientSecret: "test-secret",
		AdminSessionKey:       sessionKey,
		AdminSessionTTL:       0, // should default to 8h
		ListenAddr:            "https://example.com",
	}

	auth, err := NewAdminAuth(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	if auth.sessionTTL != 8*time.Hour {
		t.Fatalf("expected default sessionTTL=8h, got %v", auth.sessionTTL)
	}
}

func TestDecryptSession_InvalidJSON(t *testing.T) {
	aead := testAEAD(t)
	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}

	// Encrypt raw bytes that are not valid JSON.
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	invalidJSON := []byte("this is not json{{{")
	sealed := aead.Seal(nonce, nonce, invalidJSON, nil)
	encoded := base64.URLEncoding.EncodeToString(sealed)

	_, err := auth.DecryptSession(encoded)
	if err == nil {
		t.Fatalf("expected error for invalid JSON in session")
	}
	if !strings.Contains(err.Error(), "unmarshal session") {
		t.Fatalf("expected 'unmarshal session' in error, got: %v", err)
	}
}

func TestAdminAuthMiddleware_DevMode(t *testing.T) {
	var gotSession *AdminSession
	handler := AdminAuthMiddleware(nil, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFromContext(r.Context())
		if ok {
			gotSession = s
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if gotSession == nil {
		t.Fatal("expected synthetic session in context")
	}
	if gotSession.Subject != "dev-user" {
		t.Fatalf("expected subject dev-user, got %q", gotSession.Subject)
	}
	if gotSession.Email != "dev@localhost" {
		t.Fatalf("expected email dev@localhost, got %q", gotSession.Email)
	}
	if len(gotSession.Scopes) != 1 || gotSession.Scopes[0] != "admin" {
		t.Fatalf("expected scopes [admin], got %v", gotSession.Scopes)
	}
	if gotSession.CSRFToken != "dev-csrf-token" {
		t.Fatalf("expected csrf token dev-csrf-token, got %q", gotSession.CSRFToken)
	}
	if gotSession.ExpiresAt.Before(time.Now()) {
		t.Fatal("expected dev session to not be expired")
	}
}

func TestCSRFMiddleware_DevMode_StaticToken(t *testing.T) {
	session := &AdminSession{CSRFToken: "dev-csrf-token"}
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/tools/exec", strings.NewReader("_csrf=dev-csrf-token"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), sessionContextKey, session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req.WithContext(ctx))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for dev mode CSRF token, got %d", w.Code)
	}
}
