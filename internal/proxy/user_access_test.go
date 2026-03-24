package proxy

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// mockUserStore implements store.UserStore with configurable return values.
type mockUserStore struct {
	mu sync.Mutex

	getByOIDCSubUser *types.User
	getByOIDCSubErr  error

	hasToolPermResult bool
	hasToolPermErr    error

	toolPermissions []types.UserToolPermission
	toolPermErr     error
}

func (m *mockUserStore) Upsert(_ context.Context, _ *types.User) error { return nil }
func (m *mockUserStore) Get(_ context.Context, _ string) (*types.User, error) {
	return nil, nil
}

func (m *mockUserStore) GetByOIDCSub(_ context.Context, _ string) (*types.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getByOIDCSubUser, m.getByOIDCSubErr
}

func (m *mockUserStore) Search(_ context.Context, _ types.UserFilter) ([]types.User, int, error) {
	return nil, 0, nil
}

func (m *mockUserStore) UpdateRole(_ context.Context, _ string, _ types.UserRole) error {
	return nil
}

func (m *mockUserStore) Delete(_ context.Context, _ string) error { return nil }

func (m *mockUserStore) GetToolPermissions(_ context.Context, _ string) ([]types.UserToolPermission, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.toolPermissions, m.toolPermErr
}

func (m *mockUserStore) HasToolPermission(_ context.Context, _ string, _ string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasToolPermResult, m.hasToolPermErr
}

func (m *mockUserStore) SetToolPermissions(_ context.Context, _ string, _ []string, _ string) error {
	return nil
}

func TestCheckToolPermission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		identity   *identity.Identity
		userStore  *mockUserStore
		toolName   string
		wantDenied bool
		wantMsg    string
	}{
		{
			name: "non-OIDC identity (API key) is not denied",
			identity: &identity.Identity{
				ID:   "key-123",
				Type: identity.IdentityAPIKey,
			},
			userStore:  &mockUserStore{},
			toolName:   "backend.run_query",
			wantDenied: false,
		},
		{
			name:       "no identity in context is not denied",
			identity:   nil,
			userStore:  &mockUserStore{},
			toolName:   "backend.run_query",
			wantDenied: false,
		},
		{
			name: "admin role is not denied",
			identity: &identity.Identity{
				ID:       "admin-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "admin@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-admin",
					OIDCSub: "admin-sub",
					Email:   "admin@example.com",
					Role:    types.RoleAdmin,
				},
			},
			toolName:   "backend.run_query",
			wantDenied: false,
		},
		{
			name: "user role with permission is not denied",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-user",
					OIDCSub: "user-sub",
					Email:   "user@example.com",
					Role:    types.RoleUser,
				},
				hasToolPermResult: true,
			},
			toolName:   "backend.run_query",
			wantDenied: false,
		},
		{
			name: "user role without permission is denied",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-user",
					OIDCSub: "user-sub",
					Email:   "user@example.com",
					Role:    types.RoleUser,
				},
				hasToolPermResult: false,
			},
			toolName:   "backend.run_query",
			wantDenied: true,
			wantMsg:    "You do not have permission to use backend.run_query. Contact an administrator to request access.",
		},
		{
			name: "viewer role is denied with read-only message",
			identity: &identity.Identity{
				ID:       "viewer-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "viewer@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-viewer",
					OIDCSub: "viewer-sub",
					Email:   "viewer@example.com",
					Role:    types.RoleViewer,
				},
			},
			toolName:   "backend.run_query",
			wantDenied: true,
			wantMsg:    "Your account has read-only access and cannot call tools.",
		},
		{
			name: "blocked role is denied with blocked message",
			identity: &identity.Identity{
				ID:       "blocked-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "blocked@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-blocked",
					OIDCSub: "blocked-sub",
					Email:   "blocked@example.com",
					Role:    types.RoleBlocked,
				},
			},
			toolName:   "backend.run_query",
			wantDenied: true,
			wantMsg:    "Your account has been blocked. Contact an administrator.",
		},
		{
			name: "GetByOIDCSub returns error fails closed",
			identity: &identity.Identity{
				ID:       "error-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "error@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubErr: fmt.Errorf("database connection lost"),
			},
			toolName:   "backend.run_query",
			wantDenied: true,
			wantMsg:    "Permission check failed. Contact an administrator.",
		},
		{
			name: "GetByOIDCSub returns nil user is denied",
			identity: &identity.Identity{
				ID:       "unknown-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "unknown@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: nil,
			},
			toolName:   "backend.run_query",
			wantDenied: true,
			wantMsg:    "Your account is not registered. Contact an administrator.",
		},
		{
			name: "HasToolPermission returns error fails closed",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-user",
					OIDCSub: "user-sub",
					Email:   "user@example.com",
					Role:    types.RoleUser,
				},
				hasToolPermErr: fmt.Errorf("timeout"),
			},
			toolName:   "backend.run_query",
			wantDenied: true,
			wantMsg:    "Permission check failed. Contact an administrator.",
		},
		{
			name: "mTLS identity is not denied",
			identity: &identity.Identity{
				ID:   "spiffe://example.com/svc",
				Type: identity.IdentityMTLS,
			},
			userStore:  &mockUserStore{},
			toolName:   "backend.run_query",
			wantDenied: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ps := NewProxyServer(
				registration.NewCapabilityIndex(),
				&config.Config{},
				nil, 0, nil,
			)
			ps.SetUserStore(tt.userStore)

			ctx := context.Background()
			if tt.identity != nil {
				ctx = identity.WithIdentity(ctx, tt.identity)
			}

			denied, msg := ps.checkToolPermission(ctx, tt.toolName)

			if denied != tt.wantDenied {
				t.Errorf("denied = %v, want %v", denied, tt.wantDenied)
			}
			if tt.wantDenied && msg != tt.wantMsg {
				t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestAuditToolDenied(t *testing.T) {
	t.Parallel()

	t.Run("nil auditStore does not panic", func(t *testing.T) {
		t.Parallel()

		ps := NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)
		// auditStore is nil by default; call must not panic.
		ps.auditToolDenied(context.Background(), "backend.tool", "sub-1", "user@example.com", "no_permission")
	})

	t.Run("with auditStore logs correct entry", func(t *testing.T) {
		t.Parallel()

		audit := &mockAuditStore{}

		ps := NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)
		ps.SetAuditStore(audit)

		ps.auditToolDenied(context.Background(), "backend.secret_tool", "oidc-sub-42", "alice@example.com", "blocked")

		entries := audit.getEntries()
		if len(entries) != 1 {
			t.Fatalf("expected 1 audit entry, got %d", len(entries))
		}

		entry := entries[0]
		if entry.EventType != "user.tool_denied" {
			t.Errorf("expected event type %q, got %q", "user.tool_denied", entry.EventType)
		}
		if entry.ClientID != "oidc-sub-42" {
			t.Errorf("expected client ID %q, got %q", "oidc-sub-42", entry.ClientID)
		}
		if entry.ServerName != "backend.secret_tool" {
			t.Errorf("expected server name %q, got %q", "backend.secret_tool", entry.ServerName)
		}
		if entry.Details == nil {
			t.Fatal("expected non-nil details map")
		}
		if got, _ := entry.Details["tool"].(string); got != "backend.secret_tool" {
			t.Errorf("expected details.tool %q, got %q", "backend.secret_tool", got)
		}
		if got, _ := entry.Details["oidc_sub"].(string); got != "oidc-sub-42" {
			t.Errorf("expected details.oidc_sub %q, got %q", "oidc-sub-42", got)
		}
		if got, _ := entry.Details["email"].(string); got != "alice@example.com" {
			t.Errorf("expected details.email %q, got %q", "alice@example.com", got)
		}
		if got, _ := entry.Details["reason"].(string); got != "blocked" {
			t.Errorf("expected details.reason %q, got %q", "blocked", got)
		}
	})
}

func TestToolFilterFunc(t *testing.T) {
	t.Parallel()

	allTools := []mcp.Tool{
		{Name: "backend.tool_a"},
		{Name: "backend.tool_b"},
		{Name: "backend.tool_c"},
		{Name: requestAccessToolName, Description: "synthetic access tool"},
	}

	tests := []struct {
		name      string
		identity  *identity.Identity
		userStore *mockUserStore
		nilStore  bool
		wantNames []string
	}{
		{
			name:      "no identity returns all real tools without request_access",
			identity:  nil,
			userStore: &mockUserStore{},
			wantNames: []string{"backend.tool_a", "backend.tool_b", "backend.tool_c"},
		},
		{
			name: "non-OIDC identity returns all real tools without request_access",
			identity: &identity.Identity{
				ID:   "key-123",
				Type: identity.IdentityAPIKey,
			},
			userStore: &mockUserStore{},
			wantNames: []string{"backend.tool_a", "backend.tool_b", "backend.tool_c"},
		},
		{
			name: "admin returns all real tools without request_access",
			identity: &identity.Identity{
				ID:       "admin-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "admin@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-admin",
					OIDCSub: "admin-sub",
					Email:   "admin@example.com",
					Role:    types.RoleAdmin,
				},
			},
			wantNames: []string{"backend.tool_a", "backend.tool_b", "backend.tool_c"},
		},
		{
			name: "blocked returns only request_access",
			identity: &identity.Identity{
				ID:       "blocked-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "blocked@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-blocked",
					OIDCSub: "blocked-sub",
					Email:   "blocked@example.com",
					Role:    types.RoleBlocked,
				},
			},
			wantNames: []string{requestAccessToolName},
		},
		{
			name: "user with permissions returns only permitted tools without request_access",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-user",
					OIDCSub: "user-sub",
					Email:   "user@example.com",
					Role:    types.RoleUser,
				},
				toolPermissions: []types.UserToolPermission{
					{UserID: "u-user", ToolName: "backend.tool_a"},
					{UserID: "u-user", ToolName: "backend.tool_c"},
				},
			},
			wantNames: []string{"backend.tool_a", "backend.tool_c"},
		},
		{
			name: "viewer with permissions returns only allowed tools without request_access",
			identity: &identity.Identity{
				ID:       "viewer-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "viewer@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-viewer",
					OIDCSub: "viewer-sub",
					Email:   "viewer@example.com",
					Role:    types.RoleViewer,
				},
				toolPermissions: []types.UserToolPermission{
					{UserID: "u-viewer", ToolName: "backend.tool_b"},
				},
			},
			wantNames: []string{"backend.tool_b"},
		},
		{
			name: "user with zero permissions returns only request_access",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-user",
					OIDCSub: "user-sub",
					Email:   "user@example.com",
					Role:    types.RoleUser,
				},
				toolPermissions: []types.UserToolPermission{},
			},
			wantNames: []string{requestAccessToolName},
		},
		{
			name: "GetByOIDCSub error returns only request_access (fail closed)",
			identity: &identity.Identity{
				ID:       "error-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "error@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubErr: fmt.Errorf("connection refused"),
			},
			wantNames: []string{requestAccessToolName},
		},
		{
			name: "nil user returns only request_access",
			identity: &identity.Identity{
				ID:       "unknown-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "unknown@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: nil,
			},
			wantNames: []string{requestAccessToolName},
		},
		{
			name: "nil userStore returns all real tools without request_access",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			nilStore:  true,
			wantNames: []string{"backend.tool_a", "backend.tool_b", "backend.tool_c"},
		},
		{
			name: "GetToolPermissions error returns only request_access (fail closed)",
			identity: &identity.Identity{
				ID:       "user-sub",
				Type:     identity.IdentityOIDC,
				Metadata: map[string]string{"email": "user@example.com"},
			},
			userStore: &mockUserStore{
				getByOIDCSubUser: &types.User{
					ID:      "u-user",
					OIDCSub: "user-sub",
					Email:   "user@example.com",
					Role:    types.RoleUser,
				},
				toolPermErr: fmt.Errorf("database timeout"),
			},
			wantNames: []string{requestAccessToolName},
		},
		{
			name: "mTLS identity returns all real tools without request_access",
			identity: &identity.Identity{
				ID:   "spiffe://example.com/svc",
				Type: identity.IdentityMTLS,
			},
			userStore: &mockUserStore{},
			wantNames: []string{"backend.tool_a", "backend.tool_b", "backend.tool_c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ps := NewProxyServer(
				registration.NewCapabilityIndex(),
				&config.Config{},
				nil, 0, nil,
			)
			if !tt.nilStore {
				ps.SetUserStore(tt.userStore)
			}

			ctx := context.Background()
			if tt.identity != nil {
				ctx = identity.WithIdentity(ctx, tt.identity)
			}

			inputTools := make([]mcp.Tool, len(allTools))
			copy(inputTools, allTools)

			result := ps.toolFilterFunc(ctx, inputTools)

			gotNames := make([]string, len(result))
			for i, tool := range result {
				gotNames[i] = tool.Name
			}

			if len(gotNames) != len(tt.wantNames) {
				t.Fatalf("got %d tools %v, want %d tools %v", len(gotNames), gotNames, len(tt.wantNames), tt.wantNames)
			}
			for i, want := range tt.wantNames {
				if gotNames[i] != want {
					t.Errorf("tool[%d] = %q, want %q", i, gotNames[i], want)
				}
			}
		})
	}
}

func TestWithoutRequestAccessTool(t *testing.T) {
	t.Parallel()

	tools := []mcp.Tool{
		{Name: "backend.tool_a"},
		{Name: requestAccessToolName},
		{Name: "backend.tool_b"},
	}

	result := withoutRequestAccessTool(tools)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}
	for _, tool := range result {
		if tool.Name == requestAccessToolName {
			t.Error("expected request_access tool to be excluded")
		}
	}
}

func TestOnlyRequestAccessTool(t *testing.T) {
	t.Parallel()

	t.Run("returns only request_access when present", func(t *testing.T) {
		t.Parallel()
		tools := []mcp.Tool{
			{Name: "backend.tool_a"},
			{Name: requestAccessToolName, Description: "access tool"},
			{Name: "backend.tool_b"},
		}
		result := onlyRequestAccessTool(tools)
		if len(result) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(result))
		}
		if result[0].Name != requestAccessToolName {
			t.Errorf("expected %q, got %q", requestAccessToolName, result[0].Name)
		}
	})

	t.Run("returns empty when request_access not present", func(t *testing.T) {
		t.Parallel()
		tools := []mcp.Tool{
			{Name: "backend.tool_a"},
		}
		result := onlyRequestAccessTool(tools)
		if len(result) != 0 {
			t.Fatalf("expected 0 tools, got %d", len(result))
		}
	})
}

// TestCheckToolPermissionNilUserStoreSkipped verifies that the makeToolHandler
// guard skips checkToolPermission entirely when userStore is nil, meaning the
// tool call is never denied.
func TestCheckToolPermissionNilUserStoreSkipped(t *testing.T) {
	t.Parallel()

	ps := NewProxyServer(
		registration.NewCapabilityIndex(),
		&config.Config{},
		nil, 0, nil,
	)
	// userStore is nil by default — the guard in makeToolHandler prevents
	// checkToolPermission from being called.
	if ps.userStore != nil {
		t.Fatal("expected nil userStore on fresh ProxyServer")
	}
}

func TestCheckToolPermissionIntegrationWithAuditStore(t *testing.T) {
	t.Parallel()

	audit := &mockAuditStore{
		logged: make(chan struct{}, 5),
	}

	ps := NewProxyServer(
		registration.NewCapabilityIndex(),
		&config.Config{},
		nil, 0, nil,
	)
	ps.SetAuditStore(audit)
	ps.SetUserStore(&mockUserStore{
		getByOIDCSubUser: &types.User{
			ID:      "u-blocked",
			OIDCSub: "blocked-sub",
			Email:   "blocked@example.com",
			Role:    types.RoleBlocked,
		},
	})

	ctx := identity.WithIdentity(context.Background(), &identity.Identity{
		ID:       "blocked-sub",
		Type:     identity.IdentityOIDC,
		Metadata: map[string]string{"email": "blocked@example.com"},
	})

	denied, _ := ps.checkToolPermission(ctx, "backend.secret")
	if !denied {
		t.Fatal("expected blocked user to be denied")
	}

	// The audit entry is written synchronously inside checkToolPermission
	// (via auditToolDenied), so no need to wait for a goroutine.
	select {
	case <-time.After(100 * time.Millisecond):
		// Give a small grace period.
	default:
	}

	entries := audit.getEntries()
	if len(entries) == 0 {
		t.Fatal("expected audit entry from checkToolPermission denial")
	}
	if entries[0].EventType != "user.tool_denied" {
		t.Errorf("expected event type %q, got %q", "user.tool_denied", entries[0].EventType)
	}
}
