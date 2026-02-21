package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
)

func setupTestHandler(t *testing.T) (*AdminHandler, cipher.AEAD) {
	t.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)

	handler := NewAdminHandler(AdminDeps{
		RateLimitBackend: ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute),
	})

	return handler, aead
}

func withSession(r *http.Request, session *AdminSession) *http.Request {
	ctx := context.WithValue(r.Context(), sessionContextKey, session)
	return r.WithContext(ctx)
}

func TestHandleDashboard(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	session := &AdminSession{
		Subject:   "user-1",
		Email:     "admin@example.com",
		CSRFToken: "csrf-123",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	req = withSession(req, session)
	w := httptest.NewRecorder()

	handler.HandleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Dashboard") {
		t.Fatalf("expected Dashboard in response, got %s", w.Body.String())
	}
}

func TestHandleTools(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_HTMXPartial(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Partial should not contain full HTML head.
	if strings.Contains(w.Body.String(), "<!DOCTYPE") {
		t.Fatalf("HTMX partial should not contain full page")
	}
}

func TestHandleRateLimits(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/ratelimits", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleRateLimits(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleAudit(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleServers(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/servers", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleServers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleAuditExport(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/export", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleAuditExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected text/csv, got %s", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("expected attachment disposition, got %s", cd)
	}
}

func TestCSVEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"has,comma", "\"has,comma\""},
		{"has\"quote", "\"has\"\"quote\""},
		{"has\nnewline", "\"has\nnewline\""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := csvEscape(tt.input)
			if got != tt.want {
				t.Fatalf("csvEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
