package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
