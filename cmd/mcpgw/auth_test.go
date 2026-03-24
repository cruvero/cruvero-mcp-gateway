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
	"github.com/cruvero/mcp-gateway/internal/config"
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
	if !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("expected access denied error, got: %v", err)
	}
}

func TestPollTokenOnce_Expired(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"expired_token"}`))
	}))
	defer idp.Close()

	_, err := pollTokenOnce(idp.URL, "expired-code")
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired error, got: %v", err)
	}
}

func TestPollTokenOnce_Forbidden(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	}))
	defer idp.Close()

	_, err := pollTokenOnce(idp.URL, "denied-code")
	if err == nil {
		t.Fatal("expected error for denied token")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("expected access denied error, got: %v", err)
	}
}

func TestParseErrorDescription(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "with description",
			body: `{"error":"invalid_grant","error_description":"The device code has expired"}`,
			want: "The device code has expired",
		},
		{
			name: "without description",
			body: `{"error":"invalid_grant"}`,
			want: "",
		},
		{
			name: "invalid json",
			body: `not-json`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseErrorDescription([]byte(tt.body))
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestPerformLogin(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "perf-code",
				"user_code":        "PERF-5678",
				"verification_uri": "https://idp.example.com/verify",
				"expires_in":       600,
				"interval":         1,
			})
		case "/device/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "perf-access",
				"refresh_token": "perf-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		}
	}))
	defer idp.Close()

	var buf bytes.Buffer
	err := performLogin(idp.URL, loginOptions{noBrowser: true, msgWriter: &buf})
	if err != nil {
		t.Fatalf("performLogin: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "PERF-5678") {
		t.Fatalf("expected user code in output, got %q", output)
	}
	if !strings.Contains(output, "Login successful") {
		t.Fatalf("expected success message in output, got %q", output)
	}

	tokens, err := auth.LoadTokens()
	if err != nil {
		t.Fatalf("load tokens after performLogin: %v", err)
	}
	if tokens.AccessToken != "perf-access" {
		t.Fatalf("expected perf-access, got %q", tokens.AccessToken)
	}
}

func TestAuthLogin_LargeTokenResponse(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Generate large JWT-sized tokens (~20KB each, total >60KB) to stress-test
	// the full-read path without LimitReader.
	fakeJWT := func(size int) string {
		return "eyJhbGciOiJSUzI1NiJ9." + strings.Repeat("a", size) + ".sig"
	}
	accessToken := fakeJWT(20000)
	idToken := fakeJWT(20000)
	refreshToken := fakeJWT(15000)

	// Mock IdP returning large JWT tokens.
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device/authorize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "large-token-code",
				"user_code":        "LRG-9999",
				"verification_uri": "https://idp.example.com/verify",
				"expires_in":       600,
				"interval":         1,
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  accessToken,
				"id_token":      idToken,
				"refresh_token": refreshToken,
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		}
	}))
	defer idp.Close()

	// Real gateway handler proxying to mock IdP — exercises the LimitReader fix.
	cfg := &config.Config{
		DeviceFlowEnabled:      true,
		DeviceFlowIDPDeviceURL: idp.URL + "/device/authorize",
		DeviceFlowIDPTokenURL:  idp.URL + "/token",
		DeviceFlowClientID:     "test-client",
	}
	handler := auth.NewDeviceFlowHandler(cfg, nil)
	mux := http.NewServeMux()
	mux.Handle("/device/", http.StripPrefix("/device", handler.Routes()))
	gw := httptest.NewServer(mux)
	defer gw.Close()

	var buf bytes.Buffer
	err := performLogin(gw.URL, loginOptions{noBrowser: true, msgWriter: &buf})
	if err != nil {
		t.Fatalf("performLogin with large tokens: %v", err)
	}

	tokens, err := auth.LoadTokens()
	if err != nil {
		t.Fatalf("load tokens: %v", err)
	}
	if tokens.AccessToken != accessToken {
		t.Fatalf("access token mismatch: got %d bytes, want %d bytes", len(tokens.AccessToken), len(accessToken))
	}
	if tokens.IDToken != idToken {
		t.Fatalf("id token mismatch: got %d bytes, want %d bytes", len(tokens.IDToken), len(idToken))
	}
	if tokens.RefreshToken != refreshToken {
		t.Fatalf("refresh token mismatch: got %d bytes, want %d bytes", len(tokens.RefreshToken), len(refreshToken))
	}
}

func TestPerformLogin_DeviceCodeFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	err := performLogin(srv.URL, loginOptions{noBrowser: true, msgWriter: &buf})
	if err == nil {
		t.Fatal("expected error when device code request fails")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("expected status 502 error, got: %v", err)
	}
}
