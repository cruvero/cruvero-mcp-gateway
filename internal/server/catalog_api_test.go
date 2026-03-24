package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// --- mocks ---

type mockCatalogLister struct {
	entries []CatalogEntry
	err     error
}

func (m *mockCatalogLister) ListCatalogTools(_ context.Context) ([]CatalogEntry, error) {
	return m.entries, m.err
}

type mockClassificationStore struct {
	classifications []types.ToolClassification
	getAllErr       error
}

func (m *mockClassificationStore) Get(_ context.Context, _ string) (*types.ToolClassification, error) {
	return nil, nil
}
func (m *mockClassificationStore) GetAll(_ context.Context) ([]types.ToolClassification, error) {
	if m.getAllErr != nil {
		return nil, m.getAllErr
	}
	return m.classifications, nil
}
func (m *mockClassificationStore) GetByRiskLevel(_ context.Context, _ types.RiskLevel) ([]types.ToolClassification, error) {
	return nil, nil
}
func (m *mockClassificationStore) Search(_ context.Context, _ types.ToolFilter) ([]types.ToolClassification, int, error) {
	return nil, 0, nil
}
func (m *mockClassificationStore) Upsert(_ context.Context, _ *types.ToolClassification) error {
	return nil
}
func (m *mockClassificationStore) Delete(_ context.Context, _ string) error { return nil }
func (m *mockClassificationStore) DeleteNotIn(_ context.Context, _ []string) (int64, error) {
	return 0, nil
}

type mockCatalogServerStore struct {
	records []types.ServerRecord
	listErr error
}

func (m *mockCatalogServerStore) Create(_ context.Context, _ *types.ServerRecord) error { return nil }
func (m *mockCatalogServerStore) Get(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockCatalogServerStore) GetByName(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockCatalogServerStore) GetBySPIFFEID(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockCatalogServerStore) List(_ context.Context, _ types.ServerFilter) ([]types.ServerRecord, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.records, nil
}
func (m *mockCatalogServerStore) Update(_ context.Context, _ *types.ServerRecord) error { return nil }
func (m *mockCatalogServerStore) UpdateStatus(_ context.Context, _ string, _ types.ServerStatus) error {
	return nil
}
func (m *mockCatalogServerStore) UpdateHeartbeat(_ context.Context, _ string) error { return nil }
func (m *mockCatalogServerStore) AcknowledgeRegistration(_ context.Context, _ string, _ int64, _ string, _ string, _ time.Time) error {
	return nil
}
func (m *mockCatalogServerStore) UpdateRateLimit(_ context.Context, _ string, _ *int, _ *int) error {
	return nil
}
func (m *mockCatalogServerStore) Delete(_ context.Context, _ string) error { return nil }
func (m *mockCatalogServerStore) ListStale(_ context.Context, _ time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}
func (m *mockCatalogServerStore) ListExpired(_ context.Context, _ time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}

// --- tests ---

func TestCatalogHandlerList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		entries            []CatalogEntry
		listerErr          error
		classifications    []types.ToolClassification
		servers            []types.ServerRecord
		wantStatus         int
		wantTotalTools     int
		wantServersLen     int
		checkTools         func(t *testing.T, tools []catalogToolView)
		checkGatewayID     bool
		checkGeneratedAt   bool
		checkServerCounts  map[string]int
	}{
		{
			name: "returns tools with correct JSON shape",
			entries: []CatalogEntry{
				{
					Name:        "mcp.k8s.list_pods",
					Description: "List pods in a namespace",
					InputSchema: json.RawMessage(`{"type":"object"}`),
					ServerID:    "srv-1",
					ServerName:  "k8s",
				},
			},
			servers: []types.ServerRecord{
				{ID: "srv-1", Name: "mcp-k8s", Version: "1.0.0", Status: types.StatusActive},
			},
			wantStatus:       http.StatusOK,
			wantTotalTools:   1,
			wantServersLen:   1,
			checkGatewayID:   true,
			checkGeneratedAt: true,
			checkServerCounts: map[string]int{"srv-1": 1},
			checkTools: func(t *testing.T, tools []catalogToolView) {
				t.Helper()
				if tools[0].Name != "mcp.k8s.list_pods" {
					t.Fatalf("expected tool name mcp.k8s.list_pods, got %q", tools[0].Name)
				}
				if tools[0].ServerID != "srv-1" {
					t.Fatalf("expected server ID srv-1, got %q", tools[0].ServerID)
				}
				if tools[0].RiskLevel != "read_only" {
					t.Fatalf("expected risk level read_only, got %q", tools[0].RiskLevel)
				}
			},
		},
		{
			name:           "empty catalog returns empty arrays",
			entries:        []CatalogEntry{},
			servers:        []types.ServerRecord{},
			wantStatus:     http.StatusOK,
			wantTotalTools: 0,
			wantServersLen: 0,
		},
		{
			name:       "lister error returns 500",
			listerErr:  fmt.Errorf("backend down"),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "risk level from store classification",
			entries: []CatalogEntry{
				{Name: "mcp.k8s.delete_pod", Description: "Delete a pod", ServerID: "srv-1", ServerName: "k8s"},
			},
			classifications: []types.ToolClassification{
				{ToolName: "mcp.k8s.delete_pod", RiskLevel: types.RiskDestructive},
			},
			servers:        []types.ServerRecord{{ID: "srv-1", Name: "mcp-k8s", Status: types.StatusActive}},
			wantStatus:     http.StatusOK,
			wantTotalTools: 1,
			wantServersLen: 1,
			checkTools: func(t *testing.T, tools []catalogToolView) {
				t.Helper()
				if tools[0].RiskLevel != "destructive" {
					t.Fatalf("expected risk level destructive from store, got %q", tools[0].RiskLevel)
				}
			},
		},
		{
			name: "risk level falls back to AutoClassify",
			entries: []CatalogEntry{
				{Name: "mcp.github.create_issue", Description: "Create an issue", ServerID: "srv-2", ServerName: "github"},
			},
			servers:        []types.ServerRecord{{ID: "srv-2", Name: "mcp-github", Status: types.StatusActive}},
			wantStatus:     http.StatusOK,
			wantTotalTools: 1,
			wantServersLen: 1,
			checkTools: func(t *testing.T, tools []catalogToolView) {
				t.Helper()
				if tools[0].RiskLevel != "write" {
					t.Fatalf("expected risk level write from AutoClassify, got %q", tools[0].RiskLevel)
				}
			},
		},
		{
			name: "tool_count per server matches actual tool count",
			entries: []CatalogEntry{
				{Name: "mcp.k8s.list_pods", ServerID: "srv-1", ServerName: "k8s"},
				{Name: "mcp.k8s.get_pod", ServerID: "srv-1", ServerName: "k8s"},
				{Name: "mcp.github.list_repos", ServerID: "srv-2", ServerName: "github"},
			},
			servers: []types.ServerRecord{
				{ID: "srv-1", Name: "mcp-k8s", Status: types.StatusActive},
				{ID: "srv-2", Name: "mcp-github", Status: types.StatusActive},
			},
			wantStatus:     http.StatusOK,
			wantTotalTools: 3,
			wantServersLen: 2,
			checkServerCounts: map[string]int{
				"srv-1": 2,
				"srv-2": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lister := &mockCatalogLister{entries: tt.entries, err: tt.listerErr}
			classStore := &mockClassificationStore{classifications: tt.classifications}
			srvStore := &mockCatalogServerStore{records: tt.servers}

			handler := NewCatalogHandler(lister, classStore, srvStore, "test-gateway", nil)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()

			handler.Routes().ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, w.Code)
			}

			if tt.wantStatus != http.StatusOK {
				return
			}

			var resp catalogResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if resp.TotalTools != tt.wantTotalTools {
				t.Fatalf("expected total_tools %d, got %d", tt.wantTotalTools, resp.TotalTools)
			}
			if len(resp.Servers) != tt.wantServersLen {
				t.Fatalf("expected %d servers, got %d", tt.wantServersLen, len(resp.Servers))
			}
			if tt.checkGatewayID && resp.GatewayID != "test-gateway" {
				t.Fatalf("expected gateway_id test-gateway, got %q", resp.GatewayID)
			}
			if tt.checkGeneratedAt && resp.GeneratedAt.IsZero() {
				t.Fatal("expected generated_at to be a valid timestamp")
			}

			if tt.checkTools != nil {
				tt.checkTools(t, resp.Tools)
			}

			if tt.checkServerCounts != nil {
				for _, srv := range resp.Servers {
					want, ok := tt.checkServerCounts[srv.ID]
					if !ok {
						continue
					}
					if srv.ToolCount != want {
						t.Fatalf("server %s: expected tool_count %d, got %d", srv.ID, want, srv.ToolCount)
					}
				}
			}
		})
	}
}

func TestMountCatalog(t *testing.T) {
	t.Parallel()

	t.Run("non-empty prefixes makes route reachable", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			DBURL:                "postgres://db",
			RateDefault:         10,
			RateBurst:           20,
			CircuitThreshold:    5,
			RetryMax:            3,
			ShutdownTimeout:     30 * time.Second,
			DBMaxOpenConns:      25,
			DBMaxIdleConns:      10,
			DBConnMaxLifetime:   5 * time.Minute,
			AuditRetentionDays:  90,
			AuditCleanupInterval: time.Hour,
		}
		srv := New(cfg, nil, nil)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv.MountCatalog(handler, []string{"spiffe://test/"})

		req := httptest.NewRequest(http.MethodGet, "/admin/api/tools/catalog", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		// Without mTLS cert, should get 401 (middleware rejects)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without mTLS cert, got %d", w.Code)
		}
	})

	t.Run("empty prefixes means 404", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			DBURL:                "postgres://db",
			RateDefault:         10,
			RateBurst:           20,
			CircuitThreshold:    5,
			RetryMax:            3,
			ShutdownTimeout:     30 * time.Second,
			DBMaxOpenConns:      25,
			DBMaxIdleConns:      10,
			DBConnMaxLifetime:   5 * time.Minute,
			AuditRetentionDays:  90,
			AuditCleanupInterval: time.Hour,
		}
		srv := New(cfg, nil, nil)
		srv.MountCatalog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}), nil)

		req := httptest.NewRequest(http.MethodGet, "/admin/api/tools/catalog", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 with empty prefixes, got %d", w.Code)
		}
	})
}

func TestMountCatalogNilGuards(t *testing.T) {
	t.Parallel()

	t.Run("nil server does not panic", func(t *testing.T) {
		t.Parallel()
		var srv *Server
		srv.MountCatalog(http.NotFoundHandler(), []string{"spiffe://test/"})
	})

	t.Run("nil handler does not panic", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			DBURL:                "postgres://db",
			RateDefault:         10,
			RateBurst:           20,
			CircuitThreshold:    5,
			RetryMax:            3,
			ShutdownTimeout:     30 * time.Second,
			DBMaxOpenConns:      25,
			DBMaxIdleConns:      10,
			DBConnMaxLifetime:   5 * time.Minute,
			AuditRetentionDays:  90,
			AuditCleanupInterval: time.Hour,
		}
		srv := New(cfg, nil, nil)
		srv.MountCatalog(nil, []string{"spiffe://test/"})
	})
}
