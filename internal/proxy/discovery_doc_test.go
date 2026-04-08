package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func activeServer(id, name, version string, tools []string, resources []string) types.ServerRecord {
	return types.ServerRecord{
		ID:           id,
		Name:         name,
		Version:      version,
		Status:       types.StatusActive,
		Capabilities: types.Capability{Tools: tools, Resources: resources},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func TestDiscoveryDocHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		servers        []types.ServerRecord
		cfg            *config.Config
		host           string
		wantStatus     int
		wantServerKeys []string
		wantEmpty      bool
	}{
		{
			name: "returns correct JSON structure with active servers",
			servers: []types.ServerRecord{
				activeServer("s1", "mcp-todoist", "1.2.0", []string{"add_task", "list_tasks"}, nil),
				activeServer("s2", "mcp-github", "0.5.0", []string{"create_issue"}, nil),
			},
			cfg:            &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus:     http.StatusOK,
			wantServerKeys: []string{"github", "todoist"},
		},
		{
			name: "only active servers included, stale excluded",
			servers: []types.ServerRecord{
				activeServer("s1", "todoist", "1.0.0", []string{"add_task"}, nil),
				{
					ID: "s2", Name: "stale-svc", Version: "0.1.0",
					Status:       types.StatusStale,
					Capabilities: types.Capability{Tools: []string{"old_tool"}},
					CreatedAt:    time.Now().UTC(),
					UpdatedAt:    time.Now().UTC(),
				},
			},
			cfg:            &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus:     http.StatusOK,
			wantServerKeys: []string{"todoist"},
		},
		{
			name: "only active servers included, expired excluded",
			servers: []types.ServerRecord{
				activeServer("s1", "todoist", "1.0.0", []string{"add_task"}, nil),
				{
					ID: "s2", Name: "expired-svc", Version: "0.1.0",
					Status:       types.StatusExpired,
					Capabilities: types.Capability{Tools: []string{"dead_tool"}},
					CreatedAt:    time.Now().UTC(),
					UpdatedAt:    time.Now().UTC(),
				},
			},
			cfg:            &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus:     http.StatusOK,
			wantServerKeys: []string{"todoist"},
		},
		{
			name:       "empty index returns empty mcpServers object",
			servers:    nil,
			cfg:        &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus: http.StatusOK,
			wantEmpty:  true,
		},
		{
			name: "uses X-Forwarded-Host when no base URL configured",
			servers: []types.ServerRecord{
				activeServer("s1", "todoist", "1.0.0", []string{"add_task"}, nil),
			},
			cfg:            &config.Config{},
			host:           "proxy.example.com",
			wantStatus:     http.StatusOK,
			wantServerKeys: []string{"todoist"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			index := registration.NewCapabilityIndex()
			index.Rebuild(tc.servers)

			handler := NewDiscoveryDocHandler(index, tc.cfg, slog.Default())

			req := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
			if tc.host != "" {
				req.Header.Set("X-Forwarded-Host", tc.host)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
			}

			var doc discoveryDocument
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if tc.wantEmpty {
				if len(doc.MCPServers) != 0 {
					t.Errorf("expected empty mcpServers, got %d entries", len(doc.MCPServers))
				}
				return
			}

			if len(doc.MCPServers) != len(tc.wantServerKeys) {
				t.Fatalf("mcpServers count = %d, want %d", len(doc.MCPServers), len(tc.wantServerKeys))
			}

			for _, key := range tc.wantServerKeys {
				entry, ok := doc.MCPServers[key]
				if !ok {
					t.Errorf("missing server key %q", key)
					continue
				}
				if entry.Status != "active" {
					t.Errorf("server %q status = %q, want active", key, entry.Status)
				}
				if entry.URL == "" {
					t.Errorf("server %q has empty URL", key)
				}
			}

			// Verify X-Forwarded-Host is used in URL when configured
			if tc.host != "" {
				for _, entry := range doc.MCPServers {
					if entry.URL == "" {
						continue
					}
					if !containsSubstring(entry.URL, tc.host) {
						t.Errorf("URL %q does not contain forwarded host %q", entry.URL, tc.host)
					}
				}
			}
		})
	}
}

func TestDiscoveryDocHandler_CacheReturnsSameResponseWithinTTL(t *testing.T) {
	t.Parallel()

	index := registration.NewCapabilityIndex()
	index.Rebuild([]types.ServerRecord{
		activeServer("s1", "todoist", "1.0.0", []string{"add_task"}, nil),
	})

	cfg := &config.Config{
		GatewayBaseURL:    "https://gw.example.com",
		WellKnownCacheTTL: 10 * time.Second,
	}
	handler := NewDiscoveryDocHandler(index, cfg, slog.Default())

	// First request populates cache.
	req1 := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	firstBody := rec1.Body.String()

	// Add a new server to the index; cache should still return old result.
	index.Add(activeServer("s2", "github", "2.0.0", []string{"create_issue"}, nil))

	req2 := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	secondBody := rec2.Body.String()

	if firstBody != secondBody {
		t.Errorf("cached response changed within TTL:\nfirst:  %s\nsecond: %s", firstBody, secondBody)
	}
}

func TestDiscoveryDocHandler_InvalidationForcesRegeneration(t *testing.T) {
	t.Parallel()

	index := registration.NewCapabilityIndex()
	index.Rebuild([]types.ServerRecord{
		activeServer("s1", "todoist", "1.0.0", []string{"add_task"}, nil),
	})

	cfg := &config.Config{
		GatewayBaseURL:    "https://gw.example.com",
		WellKnownCacheTTL: 10 * time.Minute, // Long TTL to prove invalidation works.
	}
	handler := NewDiscoveryDocHandler(index, cfg, slog.Default())

	// Populate cache.
	req1 := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	var doc1 discoveryDocument
	if err := json.Unmarshal(rec1.Body.Bytes(), &doc1); err != nil {
		t.Fatalf("unmarshal first response: %v", err)
	}
	if len(doc1.MCPServers) != 1 {
		t.Fatalf("expected 1 server in first response, got %d", len(doc1.MCPServers))
	}

	// Add server and invalidate.
	index.Add(activeServer("s2", "github", "2.0.0", []string{"create_issue"}, nil))
	handler.Invalidate()

	req2 := httptest.NewRequest(http.MethodGet, "/.well-known/mcp.json", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	var doc2 discoveryDocument
	if err := json.Unmarshal(rec2.Body.Bytes(), &doc2); err != nil {
		t.Fatalf("unmarshal second response: %v", err)
	}
	if len(doc2.MCPServers) != 2 {
		t.Fatalf("expected 2 servers after invalidation, got %d", len(doc2.MCPServers))
	}
}

func TestDiscoveryDocHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := NewDiscoveryDocHandler(registration.NewCapabilityIndex(), &config.Config{}, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/.well-known/mcp.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestServerKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "mcp-todoist", want: "todoist"},
		{name: "mcp_github", want: "github"},
		{name: "plain-name", want: "plain-name"},
		{name: "  mcp-spaced  ", want: "spaced"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := serverKey(tc.name)
			if got != tc.want {
				t.Errorf("serverKey(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
