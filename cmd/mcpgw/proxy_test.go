package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/auth"
)

func TestMCPProxyCommand_MissingGatewayURL(t *testing.T) {
	err := mcpProxyCommand(nil)
	if err == nil {
		t.Fatalf("expected error for missing gateway-url")
	}
	if !strings.Contains(err.Error(), "gateway-url") {
		t.Fatalf("expected gateway-url error, got: %v", err)
	}
}

func TestExtractSSEData(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single data line",
			input: "data: {\"result\":\"ok\"}\n\n",
			want:  "{\"result\":\"ok\"}",
		},
		{
			name:  "multiple data lines",
			input: "data: line1\ndata: line2\n\n",
			want:  "line1\nline2",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "non-data lines ignored",
			input: "event: message\ndata: payload\nid: 1\n\n",
			want:  "payload",
		},
		{
			name:  "data line exceeding default 64KB scanner limit",
			input: "data: " + strings.Repeat("x", 128*1024) + "\n\n",
			want:  strings.Repeat("x", 128*1024),
		},
		{
			name:  "mixed notifications and large response",
			input: "data: {\"method\":\"notification1\"}\ndata: {\"method\":\"notification2\"}\ndata: " + strings.Repeat("A", 200*1024) + "\n\n",
			want:  "{\"method\":\"notification1\"}\n{\"method\":\"notification2\"}\n" + strings.Repeat("A", 200*1024),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(extractSSEData([]byte(tt.input)))
			if got != tt.want {
				t.Fatalf("extractSSEData() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSendMCPRequest_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	result, _, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "", []byte(`{"method":"ping"}`))
	if err != nil {
		t.Fatalf("sendMCPRequest: %v", err)
	}
	if !strings.Contains(string(result), "ok") {
		t.Fatalf("expected ok in result, got %q", string(result))
	}
}

func TestSendMCPRequest_SSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"result\":\"streamed\"}\n\n"))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	result, _, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "", []byte(`{"method":"ping"}`))
	if err != nil {
		t.Fatalf("sendMCPRequest SSE: %v", err)
	}
	if !strings.Contains(string(result), "streamed") {
		t.Fatalf("expected streamed in SSE result, got %q", string(result))
	}
}

func TestSendMCPRequest_UnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request body"))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	_, _, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unexpected status")
	}
	if !strings.Contains(err.Error(), "unexpected status 400") {
		t.Fatalf("expected status 400 error, got: %v", err)
	}
}

func TestSendMCPRequest_RateLimited(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := sendMCPRequest(ctx, srv.URL, "test-token", "", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error after retries")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected rate limited error, got: %v", err)
	}
}

func TestSendMCPRequest_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := sendMCPRequest(ctx, srv.URL, "test-token", "", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestEnsureValidToken_ValidToken(t *testing.T) {
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

	result, err := ensureValidToken("")
	if err != nil {
		t.Fatalf("ensureValidToken: %v", err)
	}
	if result.AccessToken != "access-valid" {
		t.Fatalf("expected access-valid, got %q", result.AccessToken)
	}
}

func TestEnsureValidToken_NotLoggedIn(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Device code endpoint returns error so auto-login fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := ensureValidToken(srv.URL)
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "auto-login failed") {
		t.Fatalf("expected auto-login failed error, got: %v", err)
	}
}

func TestEnsureValidToken_ExpiredNoRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tokens := &auth.CachedTokens{
		AccessToken: "access-expired",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
		GatewayURL:  "https://gw.example.com",
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	// Device code endpoint returns error so auto-login fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := ensureValidToken(srv.URL)
	if err == nil {
		t.Fatal("expected error for expired token with no refresh token")
	}
	if !strings.Contains(err.Error(), "auto-login failed") {
		t.Fatalf("expected auto-login failed error, got: %v", err)
	}
}

func TestRunProxy_EmptyInputExitsCleanly(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Save a valid token so ensureValidToken would succeed if called.
	tokens := &auth.CachedTokens{
		AccessToken: "access-valid",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		GatewayURL:  "https://gw.example.com",
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	input := strings.NewReader("")
	output := &bytes.Buffer{}

	err := runProxy(context.Background(), "https://gw.example.com", input, output)
	if err != nil {
		t.Fatalf("runProxy with empty input: %v", err)
	}
}

func TestRunProxy_BlankLinesSkipped(t *testing.T) {
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

	// Only blank lines -- should be skipped without calling the server.
	input := strings.NewReader("   \n\n  \n")
	output := &bytes.Buffer{}

	err := runProxy(context.Background(), "https://gw.example.com", input, output)
	if err != nil {
		t.Fatalf("runProxy with blank lines: %v", err)
	}
}

func TestRunProxy_ContextCancelledBeforeInput(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Provide some input but context is already cancelled.
	input := strings.NewReader(`{"method":"ping"}` + "\n")
	output := &bytes.Buffer{}

	err := runProxy(ctx, "https://gw.example.com", input, output)
	if err != nil {
		t.Fatalf("runProxy with cancelled context: %v", err)
	}
}

func TestEnsureValidToken_ExpiredWithRefreshToken(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create a mock IDP that handles refresh.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-refreshed",
			"refresh_token": "refresh-new",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	tokens := &auth.CachedTokens{
		AccessToken:  "access-expired",
		RefreshToken: "refresh-valid",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		GatewayURL:   srv.URL,
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	result, err := ensureValidToken("")
	if err != nil {
		t.Fatalf("ensureValidToken with refresh: %v", err)
	}
	if result.AccessToken != "access-refreshed" {
		t.Fatalf("expected refreshed token, got %q", result.AccessToken)
	}
}

func TestSendMCPRequest_Unauthorized_RefreshFails(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// No tokens saved, so ensureValidToken will fail during 401 retry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	_, _, err := sendMCPRequest(context.Background(), srv.URL, "bad-token", "", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unauthorized request with no refresh")
	}
	if !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("expected auth failed error, got: %v", err)
	}
}

func TestRunProxy_WithMockServer(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": "pong"})
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	tokens := &auth.CachedTokens{
		AccessToken: "access-proxy",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		GatewayURL:  srv.URL,
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	input := strings.NewReader(`{"method":"ping"}` + "\n")
	output := &bytes.Buffer{}

	err := runProxy(context.Background(), srv.URL, input, output)
	if err != nil {
		t.Fatalf("runProxy with mock server: %v", err)
	}

	if !strings.Contains(output.String(), "pong") {
		t.Fatalf("expected pong in output, got %q", output.String())
	}
}

func TestEnsureValidToken_AutoLogin(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Mock IDP that handles the device code flow.
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "auto-code",
				"user_code":        "AUTO-1234",
				"verification_uri": "https://idp.example.com/verify",
				"expires_in":       600,
				"interval":         1,
			})
		case "/device/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "auto-access",
				"refresh_token": "auto-refresh",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer idp.Close()

	result, err := ensureValidToken(idp.URL)
	if err != nil {
		t.Fatalf("ensureValidToken auto-login: %v", err)
	}
	if result.AccessToken != "auto-access" {
		t.Fatalf("expected auto-access token, got %q", result.AccessToken)
	}
}

func TestSendMCPRequest_SessionIDCaptured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-abc-123")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	_, sid, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "", []byte(`{"method":"initialize"}`))
	if err != nil {
		t.Fatalf("sendMCPRequest: %v", err)
	}
	if sid != "sess-abc-123" {
		t.Fatalf("expected session ID %q, got %q", "sess-abc-123", sid)
	}
}

func TestSendMCPRequest_SessionIDSent(t *testing.T) {
	var receivedSessionID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	_, _, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "my-session-42", []byte(`{"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("sendMCPRequest: %v", err)
	}
	if receivedSessionID != "my-session-42" {
		t.Fatalf("expected server to receive session ID %q, got %q", "my-session-42", receivedSessionID)
	}
}

func TestSendMCPRequest_SessionIDNotSentWhenEmpty(t *testing.T) {
	var hasHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasHeader = r.Header["Mcp-Session-Id"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	_, _, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "", []byte(`{"method":"initialize"}`))
	if err != nil {
		t.Fatalf("sendMCPRequest: %v", err)
	}
	if hasHeader {
		t.Fatal("expected Mcp-Session-Id header to be absent when sessionID is empty")
	}
}

func TestSendMCPRequest_AcceptedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Mcp-Session-Id", "sess-202")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	result, sid, err := sendMCPRequest(context.Background(), srv.URL, "test-token", "sess-202", []byte(`{"method":"notifications/initialized"}`))
	if err != nil {
		t.Fatalf("sendMCPRequest 202: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty body for 202, got %q", string(result))
	}
	if sid != "sess-202" {
		t.Fatalf("expected session ID %q, got %q", "sess-202", sid)
	}
}

func TestRunProxy_SessionIDPropagation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	requestNum := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		switch requestNum {
		case 1:
			// First request (initialize): return session ID, no session expected
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-sess-123")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`))
		case 2:
			// Second request (notification): require session ID, return 202
			if r.Header.Get("Mcp-Session-Id") != "test-sess-123" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"invalid session"}`))
				return
			}
			w.Header().Set("Mcp-Session-Id", "test-sess-123")
			w.WriteHeader(http.StatusAccepted)
		case 3:
			// Third request (tools/list): require session ID
			if r.Header.Get("Mcp-Session-Id") != "test-sess-123" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"invalid session"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "test-sess-123")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	origClient := httpClientForProxy
	httpClientForProxy = srv.Client()
	defer func() { httpClientForProxy = origClient }()

	tokens := &auth.CachedTokens{
		AccessToken: "access-proxy",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		GatewayURL:  srv.URL,
	}
	if err := auth.SaveTokens(tokens); err != nil {
		t.Fatalf("save tokens: %v", err)
	}

	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n",
	)
	output := &bytes.Buffer{}

	err := runProxy(context.Background(), srv.URL, input, output)
	if err != nil {
		t.Fatalf("runProxy: %v", err)
	}

	if requestNum != 3 {
		t.Fatalf("expected 3 requests, got %d", requestNum)
	}

	out := output.String()
	if !strings.Contains(out, "protocolVersion") {
		t.Fatalf("expected initialize response in output, got %q", out)
	}
	if !strings.Contains(out, `"tools":[]`) {
		t.Fatalf("expected tools/list response in output, got %q", out)
	}
}
