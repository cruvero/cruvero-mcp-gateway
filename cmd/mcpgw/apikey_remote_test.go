package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/auth"
)

func withStubToken(t *testing.T, fn func()) {
	t.Helper()
	orig := loadTokenFunc
	loadTokenFunc = func() (*auth.CachedTokens, error) {
		return &auth.CachedTokens{AccessToken: "test-token"}, nil
	}
	defer func() { loadTokenFunc = orig }()
	fn()
}

func withCapturedStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	origOut := stdout
	buf := &bytes.Buffer{}
	stdout = buf
	defer func() { stdout = origOut }()
	err := fn()
	return buf.String(), err
}

func TestRemoteAPIKeyCreate(t *testing.T) {
	tests := []struct {
		name          string
		serverStatus  int
		serverBody    string
		keyName       string
		scopes        string
		wantErr       bool
		wantSubstring string
	}{
		{
			name:          "successful create",
			serverStatus:  http.StatusCreated,
			serverBody:    `{"id":"k1","name":"test","client_id":"test","scopes":["read"],"profile":"default","created_at":"2026-01-01T00:00:00Z","api_key":"mcpgw_abc"}`,
			keyName:       "test",
			scopes:        "read",
			wantSubstring: "mcpgw_abc",
		},
		{
			name:    "missing name returns error",
			keyName: "",
			scopes:  "read",
			wantErr: true,
		},
		{
			name:    "empty scopes returns error",
			keyName: "test",
			scopes:  "",
			wantErr: true,
		},
		{
			name:         "server error",
			serverStatus: http.StatusInternalServerError,
			serverBody:   `{"error":"db error"}`,
			keyName:      "test",
			scopes:       "read",
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withStubToken(t, func() {
				var srv *httptest.Server
				if tc.serverStatus > 0 {
					srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodPost {
							t.Errorf("expected POST, got %s", r.Method)
						}
						if !strings.HasSuffix(r.URL.Path, "/v1/apikeys") {
							t.Errorf("expected path /v1/apikeys, got %s", r.URL.Path)
						}
						if r.Header.Get("Authorization") != "Bearer test-token" {
							t.Errorf("expected bearer token")
						}
						w.WriteHeader(tc.serverStatus)
						_, _ = w.Write([]byte(tc.serverBody))
					}))
					defer srv.Close()
				}

				baseURL := ""
				if srv != nil {
					baseURL = srv.URL
				}

				output, err := withCapturedStdout(t, func() error {
					return remoteAPIKeyCreate(baseURL, tc.keyName, tc.scopes, "", "", "")
				})
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.wantSubstring != "" && !strings.Contains(output, tc.wantSubstring) {
					t.Fatalf("expected output to contain %q, got %q", tc.wantSubstring, output)
				}
			})
		})
	}
}

func TestRemoteAPIKeyCreateFields(t *testing.T) {
	withStubToken(t, func() {
		var capturedBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"k1","name":"test","client_id":"mc","scopes":["read"],"profile":"premium","created_at":"2026-01-01T00:00:00Z","api_key":"mcpgw_x"}`))
		}))
		defer srv.Close()

		_, err := withCapturedStdout(t, func() error {
			return remoteAPIKeyCreate(srv.URL, "test", "read,write", "24h", "mc", "premium")
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedBody["name"] != "test" {
			t.Fatalf("expected name=test, got %v", capturedBody["name"])
		}
		if capturedBody["expires"] != "24h" {
			t.Fatalf("expected expires=24h, got %v", capturedBody["expires"])
		}
		if capturedBody["client_id"] != "mc" {
			t.Fatalf("expected client_id=mc, got %v", capturedBody["client_id"])
		}
		if capturedBody["profile"] != "premium" {
			t.Fatalf("expected profile=premium, got %v", capturedBody["profile"])
		}
	})
}

func TestRemoteAPIKeyList(t *testing.T) {
	tests := []struct {
		name          string
		serverStatus  int
		serverBody    string
		format        string
		wantErr       bool
		wantSubstring string
	}{
		{
			name:          "table format",
			serverStatus:  http.StatusOK,
			serverBody:    `[{"id":"k1","name":"my-key","client_id":"c1","scopes":["read"],"profile":"default","created_at":"2026-01-01T00:00:00Z"}]`,
			format:        "table",
			wantSubstring: "my-key",
		},
		{
			name:          "json format",
			serverStatus:  http.StatusOK,
			serverBody:    `[{"id":"k1","name":"my-key","client_id":"c1","scopes":["read"],"profile":"default","created_at":"2026-01-01T00:00:00Z"}]`,
			format:        "json",
			wantSubstring: "my-key",
		},
		{
			name:         "invalid format",
			format:       "xml",
			wantErr:      true,
			serverStatus: http.StatusOK,
			serverBody:   `[]`,
		},
		{
			name:         "server error",
			serverStatus: http.StatusInternalServerError,
			serverBody:   `{"error":"fail"}`,
			format:       "table",
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withStubToken(t, func() {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Errorf("expected GET, got %s", r.Method)
					}
					w.WriteHeader(tc.serverStatus)
					_, _ = w.Write([]byte(tc.serverBody))
				}))
				defer srv.Close()

				output, err := withCapturedStdout(t, func() error {
					return remoteAPIKeyList(srv.URL, tc.format)
				})
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.wantSubstring != "" && !strings.Contains(output, tc.wantSubstring) {
					t.Fatalf("expected output to contain %q, got %q", tc.wantSubstring, output)
				}
			})
		})
	}
}

func TestRemoteAPIKeyRevoke(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		serverStatus  int
		serverBody    string
		wantErr       bool
		wantSubstring string
	}{
		{
			name:          "successful revoke",
			id:            "key-1",
			serverStatus:  http.StatusOK,
			serverBody:    `{"status":"revoked","id":"key-1"}`,
			wantSubstring: "revoked api key key-1",
		},
		{
			name:         "server error",
			id:           "key-1",
			serverStatus: http.StatusInternalServerError,
			serverBody:   `{"error":"fail"}`,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withStubToken(t, func() {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodDelete {
						t.Errorf("expected DELETE, got %s", r.Method)
					}
					if !strings.HasSuffix(r.URL.Path, "/"+tc.id) {
						t.Errorf("expected path to end with /%s, got %s", tc.id, r.URL.Path)
					}
					w.WriteHeader(tc.serverStatus)
					_, _ = w.Write([]byte(tc.serverBody))
				}))
				defer srv.Close()

				output, err := withCapturedStdout(t, func() error {
					return remoteAPIKeyRevoke(srv.URL, tc.id)
				})
				if tc.wantErr {
					if err == nil {
						t.Fatal("expected error")
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if tc.wantSubstring != "" && !strings.Contains(output, tc.wantSubstring) {
					t.Fatalf("expected output to contain %q, got %q", tc.wantSubstring, output)
				}
			})
		})
	}
}

func TestAPIKeyHTTPRequestUnauthorized(t *testing.T) {
	withStubToken(t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		_, err := apiKeyHTTPRequest(http.MethodGet, srv.URL+"/v1/apikeys", nil)
		if err == nil {
			t.Fatal("expected error for 401")
		}
		if !strings.Contains(err.Error(), "unauthorized") {
			t.Fatalf("expected unauthorized error, got %v", err)
		}
	})
}

func TestAPIKeyHTTPRequestTokenLoadError(t *testing.T) {
	orig := loadTokenFunc
	loadTokenFunc = func() (*auth.CachedTokens, error) {
		return nil, fmt.Errorf("no tokens found")
	}
	defer func() { loadTokenFunc = orig }()

	_, err := apiKeyHTTPRequest(http.MethodGet, "http://localhost/v1/apikeys", nil)
	if err == nil {
		t.Fatal("expected error when token loading fails")
	}
	if !strings.Contains(err.Error(), "load auth tokens") {
		t.Fatalf("expected token load error, got %v", err)
	}
}

func TestRemoteAPIKeyListWithExpires(t *testing.T) {
	withStubToken(t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"k1","name":"key","client_id":"c","scopes":["r"],"profile":"d","expires_at":"2027-01-01T00:00:00Z","created_at":"2026-01-01T00:00:00Z"}]`))
		}))
		defer srv.Close()

		output, err := withCapturedStdout(t, func() error {
			return remoteAPIKeyList(srv.URL, "table")
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(output, "2027") {
			t.Fatalf("expected output to contain expiry year, got %q", output)
		}
	})
}

func TestRemoteAPIKeyCreateWithExpires(t *testing.T) {
	withStubToken(t, func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"k1","name":"test","client_id":"test","scopes":["read"],"profile":"default","expires_at":"2027-01-01T00:00:00Z","created_at":"2026-01-01T00:00:00Z","api_key":"mcpgw_x"}`))
		}))
		defer srv.Close()

		output, err := withCapturedStdout(t, func() error {
			return remoteAPIKeyCreate(srv.URL, "test", "read", "30d", "", "")
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(output, "2027") {
			t.Fatalf("expected output to contain expiry year, got %q", output)
		}
	})
}
