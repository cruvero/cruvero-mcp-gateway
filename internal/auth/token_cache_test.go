package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadTokens(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tokens := &CachedTokens{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		GatewayURL:   "https://gw.example.com",
	}

	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	// Verify file permissions.
	cachePath := filepath.Join(tmpDir, tokenCacheDir, tokenCacheFile)
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected file perm 0600, got %o", perm)
	}

	dirInfo, err := os.Stat(filepath.Dir(cachePath))
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Fatalf("expected dir perm 0700, got %o", perm)
	}

	loaded, err := LoadTokens()
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}

	if loaded.AccessToken != "access-123" {
		t.Fatalf("expected access token access-123, got %q", loaded.AccessToken)
	}
	if loaded.RefreshToken != "refresh-456" {
		t.Fatalf("expected refresh token refresh-456, got %q", loaded.RefreshToken)
	}
	if loaded.GatewayURL != "https://gw.example.com" {
		t.Fatalf("expected gateway url, got %q", loaded.GatewayURL)
	}
}

func TestLoadTokens_NotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := LoadTokens()
	if err == nil {
		t.Fatalf("expected error for missing tokens")
	}
}

func TestDeleteTokens(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tokens := &CachedTokens{
		AccessToken: "access-123",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	if err := SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	if err := DeleteTokens(); err != nil {
		t.Fatalf("delete tokens: %v", err)
	}

	_, err := LoadTokens()
	if err == nil {
		t.Fatalf("expected error after delete")
	}

	// Deleting again should not error.
	if err := DeleteTokens(); err != nil {
		t.Fatalf("second delete should not error: %v", err)
	}
}

func TestIsExpiredAndNeedsRefresh(t *testing.T) {
	tests := []struct {
		name         string
		tokens       *CachedTokens
		wantExpired  bool
		wantRefresh  bool
	}{
		{
			name:        "nil tokens",
			tokens:      nil,
			wantExpired: true,
			wantRefresh: true,
		},
		{
			name: "expired",
			tokens: &CachedTokens{
				ExpiresAt: time.Now().Add(-time.Hour),
			},
			wantExpired: true,
			wantRefresh: true,
		},
		{
			name: "needs refresh soon",
			tokens: &CachedTokens{
				ExpiresAt: time.Now().Add(2 * time.Minute),
			},
			wantExpired: false,
			wantRefresh: true,
		},
		{
			name: "valid",
			tokens: &CachedTokens{
				ExpiresAt: time.Now().Add(time.Hour),
			},
			wantExpired: false,
			wantRefresh: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tokens.IsExpired(); got != tt.wantExpired {
				t.Fatalf("IsExpired() = %v, want %v", got, tt.wantExpired)
			}
			if got := tt.tokens.NeedsRefresh(); got != tt.wantRefresh {
				t.Fatalf("NeedsRefresh() = %v, want %v", got, tt.wantRefresh)
			}
		})
	}
}

func TestSaveTokens_NilError(t *testing.T) {
	if err := SaveTokens(nil); err == nil {
		t.Fatalf("expected error for nil tokens")
	}
}

func TestRefreshAccessToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer idp.Close()

	tokens := &CachedTokens{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-time.Hour),
		GatewayURL:   "https://gw.example.com",
	}

	updated, err := RefreshAccessToken(tokens, idp.URL, "client-id")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if updated.AccessToken != "new-access" {
		t.Fatalf("expected new-access, got %q", updated.AccessToken)
	}
	if updated.RefreshToken != "new-refresh" {
		t.Fatalf("expected new-refresh, got %q", updated.RefreshToken)
	}
	if updated.GatewayURL != "https://gw.example.com" {
		t.Fatalf("expected gateway url preserved, got %q", updated.GatewayURL)
	}
}

func TestRefreshAccessToken_NoRefreshToken(t *testing.T) {
	tokens := &CachedTokens{
		AccessToken: "access",
		TokenType:   "Bearer",
	}
	_, err := RefreshAccessToken(tokens, "http://localhost", "client-id")
	if err == nil {
		t.Fatalf("expected error for missing refresh token")
	}
}
