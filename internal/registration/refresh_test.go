package registration_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// refreshToolLister is a test double for registration.ToolLister.
type refreshToolLister struct {
	tools []string
	err   error
}

func (m *refreshToolLister) ListToolNames(_ context.Context, _ types.ServerRecord) ([]string, error) {
	return m.tools, m.err
}

func refreshTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRefreshCapabilities(t *testing.T) {
	tests := []struct {
		name      string
		server    types.ServerRecord
		newHash   string
		lister    registration.ToolLister
		wantErr   bool
		wantTools int
	}{
		{
			name: "successful refresh updates tools and hash",
			server: types.ServerRecord{
				ID:             "srv-1",
				Name:           "todoist",
				Status:         types.StatusActive,
				Capabilities:   types.Capability{Tools: []string{"old_tool"}},
				CapabilityHash: "old-hash",
			},
			newHash:   "new-hash",
			lister:    &refreshToolLister{tools: []string{"create_task", "list_tasks", "delete_task"}},
			wantErr:   false,
			wantTools: 3,
		},
		{
			name: "backend unreachable returns error without updating state",
			server: types.ServerRecord{
				ID:     "srv-2",
				Name:   "github",
				Status: types.StatusActive,
			},
			newHash: "new-hash",
			lister:  &refreshToolLister{err: errors.New("connection refused")},
			wantErr: true,
		},
		{
			name: "empty tool list succeeds",
			server: types.ServerRecord{
				ID:     "srv-3",
				Name:   "empty",
				Status: types.StatusActive,
			},
			newHash:   "empty-hash",
			lister:    &refreshToolLister{tools: []string{}},
			wantErr:   false,
			wantTools: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := registration.NewCapabilityIndex()
			ms := newRefreshMockStore()
			ms.records[tt.server.ID] = tt.server

			result, err := registration.RefreshCapabilities(
				context.Background(),
				tt.server,
				tt.newHash,
				idx,
				ms,
				nil,
				tt.lister,
				refreshTestLogger(),
			)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.ToolsCount != tt.wantTools {
				t.Errorf("tool count = %d, want %d", result.ToolsCount, tt.wantTools)
			}
			if result.NewHash != tt.newHash {
				t.Errorf("hash = %q, want %q", result.NewHash, tt.newHash)
			}

			updated := ms.records[tt.server.ID]
			if updated.CapabilityHash != tt.newHash {
				t.Errorf("stored hash = %q, want %q", updated.CapabilityHash, tt.newHash)
			}
		})
	}
}

func TestRefreshCapabilitiesNilDeps(t *testing.T) {
	server := types.ServerRecord{ID: "x", Name: "x"}
	_, err := registration.RefreshCapabilities(
		context.Background(), server, "h", nil, nil, nil, nil, refreshTestLogger(),
	)
	if err == nil {
		t.Fatal("expected error for nil dependencies")
	}
}

// refreshMockStore satisfies store.ServerStore for refresh tests.
type refreshMockStore struct {
	records map[string]types.ServerRecord
}

func newRefreshMockStore() *refreshMockStore {
	return &refreshMockStore{records: make(map[string]types.ServerRecord)}
}

// Ensure refreshMockStore satisfies the interface at compile time.
var _ store.ServerStore = (*refreshMockStore)(nil)

func (m *refreshMockStore) Get(_ context.Context, id string) (*types.ServerRecord, error) {
	r, ok := m.records[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &r, nil
}

func (m *refreshMockStore) Update(_ context.Context, record *types.ServerRecord) error {
	m.records[record.ID] = *record
	return nil
}

func (m *refreshMockStore) Create(context.Context, *types.ServerRecord) error { return nil }
func (m *refreshMockStore) GetByName(context.Context, string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *refreshMockStore) GetBySPIFFEID(context.Context, string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *refreshMockStore) List(context.Context, types.ServerFilter) ([]types.ServerRecord, error) {
	return nil, nil
}
func (m *refreshMockStore) UpdateStatus(context.Context, string, types.ServerStatus) error {
	return nil
}
func (m *refreshMockStore) UpdateHeartbeat(context.Context, string) error { return nil }
func (m *refreshMockStore) AcknowledgeRegistration(_ context.Context, _ string, _ int64, _ string, _ string, _ time.Time) error {
	return nil
}
func (m *refreshMockStore) UpdateRateLimit(context.Context, string, *int, *int) error { return nil }
func (m *refreshMockStore) Delete(context.Context, string) error                      { return nil }
func (m *refreshMockStore) ListStale(context.Context, time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}
func (m *refreshMockStore) ListExpired(context.Context, time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}
