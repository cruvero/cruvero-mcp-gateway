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

func TestServerListHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		servers    []types.ServerRecord
		cfg        *config.Config
		wantStatus int
		wantCount  int
		validate   func(t *testing.T, entries []ServerListEntry)
	}{
		{
			name: "returns JSON array with correct metadata",
			servers: []types.ServerRecord{
				activeServer("s1", "todoist", "1.2.0", []string{"add_task", "list_tasks"}, []string{"tasks://"}),
				activeServer("s2", "github", "0.5.0", []string{"create_issue"}, nil),
			},
			cfg:        &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus: http.StatusOK,
			wantCount:  2,
			validate: func(t *testing.T, entries []ServerListEntry) {
				t.Helper()
				for _, e := range entries {
					if e.Name == "" {
						t.Error("entry has empty name")
					}
					if e.URL == "" {
						t.Error("entry has empty URL")
					}
					if e.Status != "active" {
						t.Errorf("entry %q status = %q, want active", e.Name, e.Status)
					}
					if e.Version == "" {
						t.Errorf("entry %q has empty version", e.Name)
					}
				}
			},
		},
		{
			name: "only active servers included",
			servers: []types.ServerRecord{
				activeServer("s1", "todoist", "1.0.0", []string{"add_task"}, nil),
				{
					ID: "s2", Name: "stale-svc", Version: "0.1.0",
					Status:       types.StatusStale,
					Capabilities: types.Capability{Tools: []string{"old_tool"}},
					CreatedAt:    time.Now().UTC(),
					UpdatedAt:    time.Now().UTC(),
				},
				{
					ID: "s3", Name: "expired-svc", Version: "0.1.0",
					Status:       types.StatusExpired,
					Capabilities: types.Capability{Tools: []string{"dead_tool"}},
					CreatedAt:    time.Now().UTC(),
					UpdatedAt:    time.Now().UTC(),
				},
			},
			cfg:        &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "empty list when no active servers",
			servers:    nil,
			cfg:        &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name: "tool and resource counts are correct",
			servers: []types.ServerRecord{
				activeServer("s1", "todoist", "1.2.0",
					[]string{"add_task", "list_tasks", "delete_task"},
					[]string{"tasks://", "projects://"},
				),
			},
			cfg:        &config.Config{GatewayBaseURL: "https://gw.example.com"},
			wantStatus: http.StatusOK,
			wantCount:  1,
			validate: func(t *testing.T, entries []ServerListEntry) {
				t.Helper()
				if len(entries) != 1 {
					t.Fatalf("expected 1 entry, got %d", len(entries))
				}
				e := entries[0]
				if e.ToolCount != 3 {
					t.Errorf("tool_count = %d, want 3", e.ToolCount)
				}
				if e.ResourceCount != 2 {
					t.Errorf("resource_count = %d, want 2", e.ResourceCount)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			index := registration.NewCapabilityIndex()
			index.Rebuild(tc.servers)

			handler := NewServerListHandler(index, tc.cfg, slog.Default())

			req := httptest.NewRequest(http.MethodGet, "/mcp/servers", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			if rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
			}

			var entries []ServerListEntry
			if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if len(entries) != tc.wantCount {
				t.Fatalf("entry count = %d, want %d", len(entries), tc.wantCount)
			}

			if tc.validate != nil {
				tc.validate(t, entries)
			}
		})
	}
}

func TestServerListHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := NewServerListHandler(registration.NewCapabilityIndex(), &config.Config{}, slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/mcp/servers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
