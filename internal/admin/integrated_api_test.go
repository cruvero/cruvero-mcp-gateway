package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func delegatedHeaders(req *http.Request, token, gatewayRole string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(headerCruveroSubject, "user-123")
	req.Header.Set(headerCruveroEmail, "user@example.com")
	req.Header.Set(headerCruveroTenantID, "tenant-a")
	req.Header.Set(headerCruveroRole, "admin")
	req.Header.Set(headerCruveroGatewayRole, gatewayRole)
}

func buildIntegratedRouterForRBAC() http.Handler {
	discoveryIndex := proxy.NewDiscoveryIndex()
	discoveryIndex.Index([]proxy.ToolDefinition{{
		Name:        "mcp.github.search",
		Description: "Search issues",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}})

	serverStore := &mockServerStore{
		servers: []types.ServerRecord{{
			ID:     "srv-1",
			Name:   "server-1",
			Status: types.StatusActive,
			Capabilities: types.Capability{
				Tools: []string{"mcp.github.search"},
			},
		}},
		getByID: map[string]*types.ServerRecord{
			"srv-1": {
				ID:     "srv-1",
				Name:   "server-1",
				Status: types.StatusActive,
			},
		},
	}

	auditStore := &mockAuditStore{entries: []types.AuditEntry{{
		EventType: "tool_call",
		ClientID:  "u",
		CreatedAt: time.Now().UTC(),
	}}}

	classificationStore := &mockClassificationStore{
		tools: []types.ToolClassification{{
			ToolName:  "mcp.github.search",
			RiskLevel: types.RiskReadOnly,
		}},
		byName: map[string]*types.ToolClassification{
			"mcp.github.search": {
				ToolName:  "mcp.github.search",
				RiskLevel: types.RiskReadOnly,
			},
		},
	}

	userStore := &mockUserStore{
		users: []types.User{{
			ID:    "user-1",
			Email: "user@example.com",
			Role:  types.RoleAdmin,
		}},
	}

	synonymStore := &mockSynonymStore{}

	return NewRouter(AdminDeps{
		Mode:                 "integrated",
		PlatformServiceToken: "svc-token",
		ServerStore:          serverStore,
		AuditStore:           auditStore,
		ClassificationStore:  classificationStore,
		UserStore:            userStore,
		SynonymStore:         synonymStore,
		DiscoveryIndex:       discoveryIndex,
		ProgressiveDiscovery: true,
	})
}

func TestNewRouterIntegratedBlocksStandaloneRoutes(t *testing.T) {
	router := NewRouter(AdminDeps{Mode: "integrated", PlatformServiceToken: "svc-token"})

	for _, path := range []string{"/login", "/callback", "/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s expected 403, got %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), integratedManagedMessage) {
			t.Fatalf("%s expected managed message, got %q", path, rec.Body.String())
		}
	}
}

func TestStandaloneModeParity_LoginRouteStillAvailable(t *testing.T) {
	router := NewRouter(AdminDeps{Mode: "standalone", DevMode: true})

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected standalone /login to redirect, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("expected redirect to /admin/, got %q", got)
	}
	if strings.Contains(rec.Body.String(), integratedManagedMessage) {
		t.Fatalf("expected standalone login flow, got integrated mode block message")
	}
}

func TestIntegratedDelegatedAuthContract(t *testing.T) {
	router := buildIntegratedRouterForRBAC()

	t.Run("missing bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
		delegatedHeaders(req, "", gatewayRoleViewer)
		req.Header.Del("Authorization")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
		var payload integratedErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		if payload.Code != string(errCodeInvalidToken) {
			t.Fatalf("expected INVALID_TOKEN, got %q", payload.Code)
		}
	})

	t.Run("missing required delegated header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
		delegatedHeaders(req, "svc-token", gatewayRoleViewer)
		req.Header.Del(headerCruveroSubject)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		var payload integratedErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		if payload.Code != string(errCodeMissingHeader) {
			t.Fatalf("expected MISSING_HEADER, got %q", payload.Code)
		}
	})

	t.Run("invalid gateway role", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
		delegatedHeaders(req, "svc-token", "owner")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
		var payload integratedErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		if payload.Code != string(errCodeInvalidGatewayRole) {
			t.Fatalf("expected INVALID_GATEWAY_ROLE, got %q", payload.Code)
		}
	})

	t.Run("rejects local admin cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
		delegatedHeaders(req, "svc-token", gatewayRoleViewer)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}

func TestIntegratedRBACMatrix_AllEndpointsAllRoles(t *testing.T) {
	router := buildIntegratedRouterForRBAC()

	type routeCase struct {
		name          string
		method        string
		path          string
		body          string
		contentType   string
		accept        string
		requiredRole  string
		successStatus int
	}

	cases := []routeCase{
		{name: "servers list", method: http.MethodGet, path: "/api/v1/servers", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "server detail", method: http.MethodGet, path: "/api/v1/servers/srv-1", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "server deregister", method: http.MethodPost, path: "/api/v1/servers/srv-1/deregister", requiredRole: gatewayRoleAdmin, successStatus: http.StatusOK},
		{name: "server rate limit", method: http.MethodPut, path: "/api/v1/servers/srv-1/rate-limits", body: `{"rate_limit":10,"rate_burst":20}`, contentType: "application/json", requiredRole: gatewayRoleEditor, successStatus: http.StatusOK},
		{name: "server prune", method: http.MethodPost, path: "/api/v1/servers/prune", requiredRole: gatewayRoleAdmin, successStatus: http.StatusOK},
		{name: "tools list", method: http.MethodGet, path: "/api/v1/tools", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "tool detail", method: http.MethodGet, path: "/api/v1/tools/mcp.github.search", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "tool update", method: http.MethodPut, path: "/api/v1/tools/mcp.github.search", body: `{"risk_level":"write","reason":"test"}`, contentType: "application/json", requiredRole: gatewayRoleEditor, successStatus: http.StatusOK},
		{name: "tool classification", method: http.MethodPut, path: "/api/v1/tools/mcp.github.search/classification", body: `{"risk_level":"read_only","reason":"test"}`, contentType: "application/json", requiredRole: gatewayRoleEditor, successStatus: http.StatusOK},
		{name: "tool schema", method: http.MethodGet, path: "/api/v1/tools/mcp.github.search/schema", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "tool discovery stats", method: http.MethodGet, path: "/api/v1/tools/discovery-stats", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "users list", method: http.MethodGet, path: "/api/v1/users", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "user permissions", method: http.MethodPut, path: "/api/v1/users/user-1/permissions", body: `{"tool_names":["mcp.github.search"]}`, contentType: "application/json", requiredRole: gatewayRoleAdmin, successStatus: http.StatusOK},
		{name: "audit query", method: http.MethodGet, path: "/api/v1/audit", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "audit export", method: http.MethodGet, path: "/api/v1/audit/export", accept: "application/json", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "search tuning get", method: http.MethodGet, path: "/api/v1/search-tuning", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
		{name: "search tuning update", method: http.MethodPut, path: "/api/v1/search-tuning", body: `{"term":"issue","synonyms":["ticket"]}`, contentType: "application/json", requiredRole: gatewayRoleEditor, successStatus: http.StatusOK},
		{name: "search tuning test", method: http.MethodPost, path: "/api/v1/search-tuning/test", body: `{"query":"github"}`, contentType: "application/json", requiredRole: gatewayRoleViewer, successStatus: http.StatusOK},
	}

	roles := []string{gatewayRoleViewer, gatewayRoleEditor, gatewayRoleAdmin}
	for _, tc := range cases {
		for _, role := range roles {
			t.Run(tc.name+"_as_"+role, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
				delegatedHeaders(req, "svc-token", role)
				if tc.contentType != "" {
					req.Header.Set("Content-Type", tc.contentType)
				}
				if tc.accept != "" {
					req.Header.Set("Accept", tc.accept)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				if roleRank(role) < roleRank(tc.requiredRole) {
					if rec.Code != http.StatusForbidden {
						t.Fatalf("expected 403 for %s on %s, got %d body=%s", role, tc.path, rec.Code, rec.Body.String())
					}
					var payload integratedErrorResponse
					_ = json.Unmarshal(rec.Body.Bytes(), &payload)
					if payload.Code != string(errCodeInsufficientRole) {
						t.Fatalf("expected INSUFFICIENT_ROLE for %s on %s, got %q", role, tc.path, payload.Code)
					}
					return
				}

				if rec.Code != tc.successStatus {
					t.Fatalf("expected %d for %s on %s, got %d body=%s", tc.successStatus, role, tc.path, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestIntegratedAuditExportContentNegotiation(t *testing.T) {
	router := buildIntegratedRouterForRBAC()

	t.Run("audit export respects JSON accept", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export", nil)
		delegatedHeaders(req, "svc-token", gatewayRoleViewer)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("expected JSON content type, got %q", rec.Header().Get("Content-Type"))
		}
	})

	t.Run("audit export respects CSV accept", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export", nil)
		delegatedHeaders(req, "svc-token", gatewayRoleViewer)
		req.Header.Set("Accept", "text/csv")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "text/csv") {
			t.Fatalf("expected CSV content type, got %q", rec.Header().Get("Content-Type"))
		}
	})
}
