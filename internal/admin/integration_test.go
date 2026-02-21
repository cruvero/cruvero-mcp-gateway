//go:build integration

package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
)

func TestAdminIntegration(t *testing.T) {
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

	auth := &AdminAuth{
		aead:       aead,
		sessionTTL: time.Hour,
	}

	mb := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = mb.Close() }()

	deps := AdminDeps{
		Auth:             auth,
		RateLimitBackend: mb,
	}

	router := NewRouter(deps)
	srv := httptest.NewServer(router)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Step 1: Unauthenticated access should redirect to login.
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect, got %d", resp.StatusCode)
	}

	// Step 2: Create a valid session cookie directly.
	session := &AdminSession{
		Subject:   "integration-user",
		Email:     "admin@integration.test",
		Scopes:    []string{"admin"},
		CSRFToken: "integration-csrf-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	sessionData, _ := json.Marshal(session)
	nonce := make([]byte, aead.NonceSize())
	_, _ = io.ReadFull(rand.Reader, nonce)
	sealed := aead.Seal(nonce, nonce, sessionData, nil)
	cookieValue := base64.URLEncoding.EncodeToString(sealed)

	sessionCookie := &http.Cookie{
		Name:  sessionCookieName,
		Value: cookieValue,
		Path:  "/admin/",
	}

	// Step 3: Access dashboard with session.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET / with session: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "Dashboard") {
		t.Fatalf("expected Dashboard in response")
	}

	// Step 4: Access tools page.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/tools", nil)
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /tools: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for tools, got %d", resp.StatusCode)
	}

	// Step 5: Access rate limits page.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/ratelimits", nil)
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /ratelimits: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for ratelimits, got %d", resp.StatusCode)
	}

	// Step 6: Access audit page.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/audit", nil)
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /audit: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for audit, got %d", resp.StatusCode)
	}

	// Step 7: Export CSV.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/audit/export", nil)
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /audit/export: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for export, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected text/csv, got %s", ct)
	}

	// Step 8: POST without CSRF should fail.
	form := url.Values{"risk_level": {"write"}, "reason": {"test"}}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tools/test-tool", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST /tools/test-tool: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d", resp.StatusCode)
	}

	// Step 9: POST with valid CSRF should succeed.
	form.Set("_csrf", session.CSRFToken)
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/tools/test-tool", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST /tools/test-tool with CSRF: %v", err)
	}
	_ = resp.Body.Close()
	// Should redirect to /admin/tools on success.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect after tool update, got %d", resp.StatusCode)
	}
}
