package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
)

func TestAuthCommand_NoSubcommand(t *testing.T) {
	err := authCommand(nil)
	if err == nil {
		t.Fatalf("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "subcommand") {
		t.Fatalf("expected subcommand error, got: %v", err)
	}
}

func TestAuthCommand_UnknownSubcommand(t *testing.T) {
	err := authCommand([]string{"unknown"})
	if err == nil {
		t.Fatalf("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown subcommand error, got: %v", err)
	}
}

func TestAuthLogin_MissingGatewayURL(t *testing.T) {
	err := authLoginCommand(nil)
	if err == nil {
		t.Fatalf("expected error for missing gateway-url")
	}
	if !strings.Contains(err.Error(), "gateway-url") {
		t.Fatalf("expected gateway-url error, got: %v", err)
	}
}

func TestAuthLogin_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	callCount := 0
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/code":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-code-123",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://idp.example.com/verify",
				"expires_in":       600,
				"interval":         1,
			})
		case "/device/token":
			callCount++
			w.Header().Set("Content-Type", "application/json")
			if callCount < 2 {
				w.WriteHeader(http.StatusTooEarly)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "authorization_pending",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-xyz",
				"refresh_token": "refresh-xyz",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		}
	}))
	defer idp.Close()

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authLoginCommand([]string{"--gateway-url", idp.URL, "--no-browser"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "ABCD-1234") {
		t.Fatalf("expected user code in output, got %q", output)
	}
	if !strings.Contains(output, "Login successful") {
		t.Fatalf("expected success message in output, got %q", output)
	}
}

func TestAuthStatus_NotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	err := authStatusCommand(nil)
	if err == nil {
		t.Fatalf("expected error when not logged in")
	}
}

func TestAuthLogout(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authLogoutCommand(nil)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	if !strings.Contains(buf.String(), "Logged out") {
		t.Fatalf("expected logged out message, got %q", buf.String())
	}
}

func TestAuthStatus_ValidToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tokens := &auth.CachedTokens{
		AccessToken:  "access-valid",
		RefreshToken: "refresh-valid",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		GatewayURL:   "https://gw.example.com",
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authStatusCommand(nil)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Gateway: https://gw.example.com") {
		t.Fatalf("expected gateway URL in output, got %q", output)
	}
	if !strings.Contains(output, "Token type: Bearer") {
		t.Fatalf("expected token type in output, got %q", output)
	}
	if !strings.Contains(output, "Status: valid") {
		t.Fatalf("expected valid status, got %q", output)
	}
}

func TestAuthStatus_ExpiredToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tokens := &auth.CachedTokens{
		AccessToken:  "access-expired",
		RefreshToken: "refresh-expired",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		GatewayURL:   "https://gw.example.com",
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authStatusCommand(nil)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Status: expired") {
		t.Fatalf("expected expired status, got %q", output)
	}
}

func TestAuthStatus_NeedsRefreshToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Token expires in 2 minutes - within the 5-minute refresh window.
	tokens := &auth.CachedTokens{
		AccessToken:  "access-soon",
		RefreshToken: "refresh-soon",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(2 * time.Minute),
		GatewayURL:   "https://gw.example.com",
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authStatusCommand(nil)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Status: needs refresh") {
		t.Fatalf("expected needs refresh status, got %q", output)
	}
}

func TestAuthCommand_RoutesToStatus(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tokens := &auth.CachedTokens{
		AccessToken: "access-valid",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		GatewayURL:  "https://gw.example.com",
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authCommand([]string{"status"})
	if err != nil {
		t.Fatalf("auth status via authCommand: %v", err)
	}

	if !strings.Contains(buf.String(), "Gateway:") {
		t.Fatalf("expected gateway in output, got %q", buf.String())
	}
}

func TestAuthCommand_RoutesToLogout(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	old := stdout
	var buf bytes.Buffer
	stdout = &buf
	defer func() { stdout = old }()

	err := authCommand([]string{"logout"})
	if err != nil {
		t.Fatalf("auth logout via authCommand: %v", err)
	}

	if !strings.Contains(buf.String(), "Logged out") {
		t.Fatalf("expected logged out in output, got %q", buf.String())
	}
}

func TestAuthLogin_DeviceCodeEndpointFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := authLoginCommand([]string{"--gateway-url", srv.URL, "--no-browser"})
	if err == nil {
		t.Fatal("expected error for failed device code request")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("expected status 500 error, got: %v", err)
	}
}

func TestAuthLogin_TokenEndpointNon425Error(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device/code":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-code-err",
				"user_code":        "ERR-1234",
				"verification_uri": "https://idp.example.com/verify",
				"expires_in":       600,
				"interval":         1,
			})
		case "/device/token":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"access_denied"}`))
		}
	}))
	defer idp.Close()

	old := stdout
	stdout = &bytes.Buffer{}
	defer func() { stdout = old }()

	err := authLoginCommand([]string{"--gateway-url", idp.URL, "--no-browser"})
	if err == nil {
		t.Fatal("expected error for forbidden token response")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected status 403 error, got: %v", err)
	}
}
