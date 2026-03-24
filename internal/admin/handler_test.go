package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// --- Mock stores ---

type mockServerStore struct {
	servers  []types.ServerRecord
	listErr  error
	delErr   error
	deleted  []string
	getByID  map[string]*types.ServerRecord
	updateRL []struct {
		id        string
		rateLimit *int
		rateBurst *int
	}
	updateRLErr error
}

var _ store.ServerStore = (*mockServerStore)(nil)

func (m *mockServerStore) Create(_ context.Context, _ *types.ServerRecord) error { return nil }
func (m *mockServerStore) Get(_ context.Context, id string) (*types.ServerRecord, error) {
	if m.getByID != nil {
		if r, ok := m.getByID[id]; ok {
			return r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockServerStore) GetByName(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockServerStore) GetBySPIFFEID(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockServerStore) List(_ context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if filter.Status != nil {
		var filtered []types.ServerRecord
		for _, s := range m.servers {
			if s.Status == *filter.Status {
				filtered = append(filtered, s)
			}
		}
		return filtered, nil
	}
	return m.servers, nil
}
func (m *mockServerStore) Update(_ context.Context, _ *types.ServerRecord) error { return nil }
func (m *mockServerStore) UpdateStatus(_ context.Context, _ string, _ types.ServerStatus) error {
	return nil
}
func (m *mockServerStore) UpdateHeartbeat(_ context.Context, _ string) error { return nil }
func (m *mockServerStore) AcknowledgeRegistration(_ context.Context, _ string, _ int64, _ string, _ string, _ time.Time) error {
	return nil
}
func (m *mockServerStore) UpdateRateLimit(_ context.Context, id string, rateLimit, rateBurst *int) error {
	if m.updateRLErr != nil {
		return m.updateRLErr
	}
	m.updateRL = append(m.updateRL, struct {
		id        string
		rateLimit *int
		rateBurst *int
	}{id, rateLimit, rateBurst})
	return nil
}
func (m *mockServerStore) Delete(_ context.Context, id string) error {
	if m.delErr != nil {
		return m.delErr
	}
	m.deleted = append(m.deleted, id)
	return nil
}
func (m *mockServerStore) ListStale(_ context.Context, _ time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}
func (m *mockServerStore) ListExpired(_ context.Context, _ time.Duration) ([]types.ServerRecord, error) {
	return nil, nil
}

type mockAuditStore struct {
	entries         []types.AuditEntry
	queryErr        error
	logged          []*types.AuditEntry
	count           int
	countErr        error
	lastQueryFilter types.AuditFilter
	lastCountFilter types.AuditFilter
}

var _ store.AuditStore = (*mockAuditStore)(nil)

func (m *mockAuditStore) Log(_ context.Context, entry *types.AuditEntry) error {
	m.logged = append(m.logged, entry)
	return nil
}
func (m *mockAuditStore) Query(_ context.Context, filter types.AuditFilter) ([]types.AuditEntry, error) {
	m.lastQueryFilter = filter
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.entries, nil
}
func (m *mockAuditStore) Count(_ context.Context, filter types.AuditFilter) (int, error) {
	m.lastCountFilter = filter
	if m.countErr != nil {
		return 0, m.countErr
	}
	if m.count > 0 {
		return m.count, nil
	}
	return len(m.entries), nil
}

type mockClassificationStore struct {
	tools             []types.ToolClassification
	byName            map[string]*types.ToolClassification
	getAllErr         error
	getByRiskErr      error
	getErr            error
	upsertErr         error
	upserted          []*types.ToolClassification
	searchResults     []types.ToolClassification
	searchTotal       int
	searchErr         error
	deleteNotInErr    error
	deleteNotInResult int64
	deleteNotInCalled [][]string
}

var _ store.ToolClassificationStore = (*mockClassificationStore)(nil)

func (m *mockClassificationStore) Get(_ context.Context, toolName string) (*types.ToolClassification, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.byName != nil {
		if t, ok := m.byName[toolName]; ok {
			return t, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockClassificationStore) GetAll(_ context.Context) ([]types.ToolClassification, error) {
	if m.getAllErr != nil {
		return nil, m.getAllErr
	}
	return m.tools, nil
}
func (m *mockClassificationStore) GetByRiskLevel(_ context.Context, level types.RiskLevel) ([]types.ToolClassification, error) {
	if m.getByRiskErr != nil {
		return nil, m.getByRiskErr
	}
	var result []types.ToolClassification
	for _, t := range m.tools {
		if t.RiskLevel == level {
			result = append(result, t)
		}
	}
	return result, nil
}
func (m *mockClassificationStore) Search(_ context.Context, filter types.ToolFilter) ([]types.ToolClassification, int, error) {
	if m.searchErr != nil {
		return nil, 0, m.searchErr
	}
	if m.searchResults != nil {
		return m.searchResults, m.searchTotal, nil
	}
	// Fall back to in-memory filtering of m.tools for backward compatibility.
	var results []types.ToolClassification
	for _, t := range m.tools {
		if filter.RiskLevel.IsValid() && t.RiskLevel != filter.RiskLevel {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(t.ToolName), strings.ToLower(filter.Query)) {
			continue
		}
		results = append(results, t)
	}
	return results, len(results), nil
}
func (m *mockClassificationStore) Upsert(_ context.Context, c *types.ToolClassification) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserted = append(m.upserted, c)
	return nil
}
func (m *mockClassificationStore) Delete(_ context.Context, _ string) error { return nil }
func (m *mockClassificationStore) DeleteNotIn(_ context.Context, activeToolNames []string) (int64, error) {
	m.deleteNotInCalled = append(m.deleteNotInCalled, activeToolNames)
	if m.deleteNotInErr != nil {
		return 0, m.deleteNotInErr
	}
	return m.deleteNotInResult, nil
}

type mockUserStore struct {
	users         []types.User
	byID          map[string]*types.User
	byOIDCSub     map[string]*types.User
	searchResults []types.User
	searchTotal   int
	searchErr     error
	getErr        error
	updateRoleErr error
	permissions   []types.UserToolPermission
	permissionsOK map[string]bool
	getPermsErr   error
	setPermsErr   error
	setPermsCalls []struct {
		userID    string
		toolNames []string
		grantedBy string
	}
	updatedRoles []struct {
		id   string
		role types.UserRole
	}
}

var _ store.UserStore = (*mockUserStore)(nil)

func (m *mockUserStore) Upsert(_ context.Context, _ *types.User) error { return nil }
func (m *mockUserStore) Get(_ context.Context, id string) (*types.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.byID != nil {
		if u, ok := m.byID[id]; ok {
			return u, nil
		}
	}
	return nil, nil
}
func (m *mockUserStore) GetByOIDCSub(_ context.Context, sub string) (*types.User, error) {
	if m.byOIDCSub != nil {
		if u, ok := m.byOIDCSub[sub]; ok {
			return u, nil
		}
	}
	return nil, nil
}
func (m *mockUserStore) Search(_ context.Context, filter types.UserFilter) ([]types.User, int, error) {
	if m.searchErr != nil {
		return nil, 0, m.searchErr
	}
	if m.searchResults != nil {
		return m.searchResults, m.searchTotal, nil
	}
	var results []types.User
	for _, u := range m.users {
		if filter.Role.IsValid() && u.Role != filter.Role {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(u.Email), strings.ToLower(filter.Query)) {
			continue
		}
		results = append(results, u)
	}
	return results, len(results), nil
}
func (m *mockUserStore) UpdateRole(_ context.Context, id string, role types.UserRole) error {
	if m.updateRoleErr != nil {
		return m.updateRoleErr
	}
	m.updatedRoles = append(m.updatedRoles, struct {
		id   string
		role types.UserRole
	}{id, role})
	return nil
}
func (m *mockUserStore) Delete(_ context.Context, _ string) error { return nil }
func (m *mockUserStore) GetToolPermissions(_ context.Context, _ string) ([]types.UserToolPermission, error) {
	if m.getPermsErr != nil {
		return nil, m.getPermsErr
	}
	return m.permissions, nil
}
func (m *mockUserStore) HasToolPermission(_ context.Context, _ string, tool string) (bool, error) {
	if m.permissionsOK != nil {
		return m.permissionsOK[tool], nil
	}
	return false, nil
}
func (m *mockUserStore) SetToolPermissions(_ context.Context, userID string, toolNames []string, grantedBy string) error {
	if m.setPermsErr != nil {
		return m.setPermsErr
	}
	m.setPermsCalls = append(m.setPermsCalls, struct {
		userID    string
		toolNames []string
		grantedBy string
	}{userID, toolNames, grantedBy})
	return nil
}

type mockSynonymStore struct {
	entries   []types.SynonymEntry
	listErr   error
	upsertErr error
	deleteErr error
	upserted  []*types.SynonymEntry
	deleted   []string
}

var _ store.SynonymStore = (*mockSynonymStore)(nil)

func (m *mockSynonymStore) List(_ context.Context) ([]types.SynonymEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.entries, nil
}
func (m *mockSynonymStore) Upsert(_ context.Context, entry *types.SynonymEntry) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserted = append(m.upserted, entry)
	return nil
}
func (m *mockSynonymStore) Delete(_ context.Context, term string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleted = append(m.deleted, term)
	return nil
}

type mockReindexLogStore struct {
	entries   []types.ReindexLogEntry
	recentErr error
}

var _ store.ReindexLogStore = (*mockReindexLogStore)(nil)

func (m *mockReindexLogStore) Log(_ context.Context, _ *types.ReindexLogEntry) error { return nil }
func (m *mockReindexLogStore) Recent(_ context.Context, _ int) ([]types.ReindexLogEntry, error) {
	if m.recentErr != nil {
		return nil, m.recentErr
	}
	return m.entries, nil
}

type mockBroadcaster struct {
	published []struct {
		subject string
		data    []byte
	}
}

var _ registration.Broadcaster = (*mockBroadcaster)(nil)

func (m *mockBroadcaster) Publish(subject string, data []byte) error {
	m.published = append(m.published, struct {
		subject string
		data    []byte
	}{subject, data})
	return nil
}
func (m *mockBroadcaster) Subscribe(_ string, _ func(data []byte)) error { return nil }
func (m *mockBroadcaster) Close() error                                  { return nil }

func setupTestHandler(t *testing.T) (*AdminHandler, cipher.AEAD) {
	t.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)

	handler := NewAdminHandler(AdminDeps{
		RateLimitBackend: ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute),
	})

	return handler, aead
}

func withSession(r *http.Request, session *AdminSession) *http.Request {
	ctx := context.WithValue(r.Context(), sessionContextKey, session)
	return r.WithContext(ctx)
}

func TestHandleDashboard(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	session := &AdminSession{
		Subject:   "user-1",
		Email:     "admin@example.com",
		CSRFToken: "csrf-123",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	req = withSession(req, session)
	w := httptest.NewRecorder()

	handler.HandleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Dashboard") {
		t.Fatalf("expected Dashboard in response, got %s", w.Body.String())
	}
}

func TestHandleTools(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_HTMXPartial(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Partial should not contain full HTML head.
	if strings.Contains(w.Body.String(), "<!DOCTYPE") {
		t.Fatalf("HTMX partial should not contain full page")
	}
}

func TestHandleRateLimits(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/ratelimits", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleRateLimits(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleAudit(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleServers(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/servers", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleServers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleAuditExport(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/export", nil)
	req = withSession(req, &AdminSession{
		Email:     "admin@example.com",
		CSRFToken: "csrf",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	w := httptest.NewRecorder()

	handler.HandleAuditExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected text/csv, got %s", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("expected attachment disposition, got %s", cd)
	}
}

func TestCSVEscape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple text", "simple", "simple"},
		{"has comma", "has,comma", "\"has,comma\""},
		{"has quote", "has\"quote", "\"has\"\"quote\""},
		{"has newline", "has\nnewline", "\"has\nnewline\""},
		{"empty string", "", ""},
		// Formula injection prevention cases.
		{"formula equals", "=CMD|'/C calc'!A0", "'=CMD|'/C calc'!A0"},
		{"formula plus", "+1+1", "'+1+1"},
		{"formula minus", "-1-1", "'-1-1"},
		{"formula at", "@SUM(A1:A10)", "'@SUM(A1:A10)"},
		{"formula tab", "\tcmd", "'\tcmd"},
		{"formula cr", "\rcmd", "\"'\rcmd\""},
		{"mid-string cr", "a\rb", "\"a\rb\""},
		// Formula char + comma triggers both prefix and quoting.
		{"formula with comma", "=a,b", "\"'=a,b\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := csvEscape(tt.input)
			if got != tt.want {
				t.Fatalf("csvEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Tests with mock stores ---

func setupTestHandlerWithStores(t *testing.T, deps AdminDeps) *AdminHandler {
	t.Helper()
	if deps.RateLimitBackend == nil {
		deps.RateLimitBackend = ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	}
	return NewAdminHandler(deps)
}

func defaultSession() *AdminSession {
	return &AdminSession{
		Subject:   "user-1",
		Email:     "admin@example.com",
		CSRFToken: "csrf-123",
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestHandleDashboard_WithStores(t *testing.T) {
	now := time.Now()
	serverStore := &mockServerStore{
		servers: []types.ServerRecord{
			{ID: "s1", Name: "server-1", Status: types.StatusActive},
			{ID: "s2", Name: "server-2", Status: types.StatusActive},
		},
	}
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{EventType: "tool_call", CreatedAt: now},
			{EventType: "tool_call", CreatedAt: now},
			{EventType: "tool_call", CreatedAt: now},
		},
	}
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "tool-1", RiskLevel: types.RiskReadOnly},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ServerStore:         serverStore,
		AuditStore:          auditStore,
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Fatalf("expected Dashboard in response")
	}
}

func TestHandleDashboard_HTMX(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_WithRiskFilter(t *testing.T) {
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "read-tool", RiskLevel: types.RiskReadOnly},
			{ToolName: "write-tool", RiskLevel: types.RiskWrite},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools?risk=read_only", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_WithSearchQuery(t *testing.T) {
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "read-tool", RiskLevel: types.RiskReadOnly},
			{ToolName: "write-tool", RiskLevel: types.RiskWrite},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools?q=read", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_RiskFilterAndSearch(t *testing.T) {
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "read-alpha", RiskLevel: types.RiskReadOnly},
			{ToolName: "read-beta", RiskLevel: types.RiskReadOnly},
			{ToolName: "write-alpha", RiskLevel: types.RiskWrite},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools?risk=read_only&q=alpha", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_InvalidRiskFilter(t *testing.T) {
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "tool-1", RiskLevel: types.RiskReadOnly},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools?risk=invalid_level", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	// Should still succeed but with no tools (invalid risk level is not valid).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTools_HTMXPartialWithStore(t *testing.T) {
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "tool-1", RiskLevel: types.RiskReadOnly},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleToolEdit(t *testing.T) {
	classStore := &mockClassificationStore{
		byName: map[string]*types.ToolClassification{
			"existing-tool": {
				ToolName:  "existing-tool",
				RiskLevel: types.RiskWrite,
				Reason:    "writes data",
			},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	t.Run("existing tool", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/tools/existing-tool", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "existing-tool")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleToolEdit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("new tool defaults to unknown", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/tools/new-tool", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "new-tool")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleToolEdit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/tools/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleToolEdit(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty name, got %d", w.Code)
		}
	})

	t.Run("nil classification store", func(t *testing.T) {
		h := setupTestHandlerWithStores(t, AdminDeps{})

		req := httptest.NewRequest(http.MethodGet, "/admin/tools/some-tool", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "some-tool")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		h.HandleToolEdit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestHandleToolUpdate(t *testing.T) {
	classStore := &mockClassificationStore{}
	auditStore := &mockAuditStore{}
	broadcaster := &mockBroadcaster{}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
		AuditStore:          auditStore,
		Broadcaster:         broadcaster,
	})

	t.Run("successful update", func(t *testing.T) {
		form := url.Values{
			"risk_level": {"write"},
			"reason":     {"modifies data"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/tools/test-tool", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "test-tool")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleToolUpdate(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect (302), got %d: %s", w.Code, w.Body.String())
		}
		if len(classStore.upserted) == 0 {
			t.Fatalf("expected classification to be upserted")
		}
		if classStore.upserted[0].ToolName != "test-tool" {
			t.Fatalf("expected tool name test-tool, got %s", classStore.upserted[0].ToolName)
		}
		if classStore.upserted[0].RiskLevel != types.RiskWrite {
			t.Fatalf("expected risk level write, got %s", classStore.upserted[0].RiskLevel)
		}
		if len(auditStore.logged) == 0 {
			t.Fatalf("expected audit entry to be logged")
		}
		if len(broadcaster.published) == 0 {
			t.Fatalf("expected broadcast event")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		form := url.Values{"risk_level": {"write"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/tools/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleToolUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid risk level", func(t *testing.T) {
		form := url.Values{"risk_level": {"mega_danger"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/tools/tool-x", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "tool-x")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleToolUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid risk level, got %d", w.Code)
		}
	})

	t.Run("upsert error", func(t *testing.T) {
		failStore := &mockClassificationStore{upsertErr: fmt.Errorf("db error")}
		h := setupTestHandlerWithStores(t, AdminDeps{
			ClassificationStore: failStore,
		})

		form := url.Values{"risk_level": {"write"}, "reason": {"test"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/tools/tool-x", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "tool-x")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		h.HandleToolUpdate(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for upsert error, got %d", w.Code)
		}
	})

	t.Run("no session uses admin as updatedBy", func(t *testing.T) {
		cs := &mockClassificationStore{}
		h := setupTestHandlerWithStores(t, AdminDeps{
			ClassificationStore: cs,
		})

		form := url.Values{"risk_level": {"read_only"}, "reason": {"test"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/tools/tool-y", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "tool-y")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		// No session attached.
		w := httptest.NewRecorder()

		h.HandleToolUpdate(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect, got %d", w.Code)
		}
		if len(cs.upserted) == 0 {
			t.Fatalf("expected upsert")
		}
		if cs.upserted[0].UpdatedBy != "admin" {
			t.Fatalf("expected updatedBy=admin, got %s", cs.upserted[0].UpdatedBy)
		}
	})
}

func TestHandleServerDeregister(t *testing.T) {
	t.Run("successful deregister", func(t *testing.T) {
		serverStore := &mockServerStore{}
		auditStore := &mockAuditStore{}
		broadcaster := &mockBroadcaster{}

		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore: serverStore,
			AuditStore:  auditStore,
			Broadcaster: broadcaster,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-1/deregister", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerDeregister(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect (302), got %d: %s", w.Code, w.Body.String())
		}
		if len(serverStore.deleted) != 1 || serverStore.deleted[0] != "srv-1" {
			t.Fatalf("expected server srv-1 to be deleted, got %v", serverStore.deleted)
		}
		if len(auditStore.logged) == 0 {
			t.Fatalf("expected audit entry")
		}
		if auditStore.logged[0].EventType != "server_deregistered" {
			t.Fatalf("expected event_type=server_deregistered, got %s", auditStore.logged[0].EventType)
		}
		if len(broadcaster.published) == 0 {
			t.Fatalf("expected broadcast event")
		}
	})

	t.Run("empty id", func(t *testing.T) {
		handler := setupTestHandlerWithStores(t, AdminDeps{})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers//deregister", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerDeregister(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("delete error", func(t *testing.T) {
		serverStore := &mockServerStore{delErr: fmt.Errorf("db delete error")}
		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore: serverStore,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-1/deregister", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerDeregister(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})

	t.Run("no session uses admin as deregisteredBy", func(t *testing.T) {
		as := &mockAuditStore{}
		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore: &mockServerStore{},
			AuditStore:  as,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-2/deregister", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-2")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		// No session.
		w := httptest.NewRecorder()

		handler.HandleServerDeregister(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect, got %d", w.Code)
		}
		if len(as.logged) == 0 {
			t.Fatalf("expected audit entry")
		}
		if as.logged[0].ClientID != "admin" {
			t.Fatalf("expected deregisteredBy=admin, got %s", as.logged[0].ClientID)
		}
	})
}

func TestHandleServerRateLimitEdit(t *testing.T) {
	rateLimit := 10
	rateBurst := 20
	serverStore := &mockServerStore{
		getByID: map[string]*types.ServerRecord{
			"srv-1": {ID: "srv-1", Name: "test-server", RateLimit: &rateLimit, RateBurst: &rateBurst},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ServerStore: serverStore,
	})

	t.Run("existing server", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/servers/srv-1/ratelimit", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitEdit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/servers//ratelimit", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitEdit(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("server not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/servers/missing/ratelimit", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "missing")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitEdit(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestHandleServerRateLimitUpdate(t *testing.T) {
	serverStore := &mockServerStore{}
	auditStore := &mockAuditStore{}
	broadcaster := &mockBroadcaster{}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ServerStore: serverStore,
		AuditStore:  auditStore,
		Broadcaster: broadcaster,
	})

	t.Run("successful update", func(t *testing.T) {
		form := url.Values{
			"rate_limit": {"10"},
			"rate_burst": {"20"},
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-1/ratelimit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitUpdate(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect, got %d: %s", w.Code, w.Body.String())
		}
		if len(serverStore.updateRL) != 1 {
			t.Fatalf("expected 1 rate limit update, got %d", len(serverStore.updateRL))
		}
		if *serverStore.updateRL[0].rateLimit != 10 {
			t.Fatalf("expected rate limit 10, got %d", *serverStore.updateRL[0].rateLimit)
		}
		if len(auditStore.logged) == 0 {
			t.Fatalf("expected audit entry")
		}
		if len(broadcaster.published) == 0 {
			t.Fatalf("expected broadcast event for multi-pod sync")
		}
	})

	t.Run("clear rate limit", func(t *testing.T) {
		serverStore.updateRL = nil
		form := url.Values{}
		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-2/ratelimit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-2")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitUpdate(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect, got %d", w.Code)
		}
		if len(serverStore.updateRL) != 1 {
			t.Fatalf("expected 1 update call, got %d", len(serverStore.updateRL))
		}
		if serverStore.updateRL[0].rateLimit != nil {
			t.Fatalf("expected nil rate limit, got %v", serverStore.updateRL[0].rateLimit)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		form := url.Values{"rate_limit": {"10"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/servers//ratelimit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid rate limit", func(t *testing.T) {
		form := url.Values{"rate_limit": {"abc"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-1/ratelimit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid input, got %d", w.Code)
		}
	})

	t.Run("negative rate limit", func(t *testing.T) {
		form := url.Values{"rate_limit": {"-5"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-1/ratelimit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for negative value, got %d", w.Code)
		}
	})

	t.Run("burst without limit", func(t *testing.T) {
		form := url.Values{"rate_burst": {"20"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/servers/srv-1/ratelimit", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "srv-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleServerRateLimitUpdate(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for burst without limit, got %d", w.Code)
		}
	})
}

func TestHandleServers_WithStore(t *testing.T) {
	serverStore := &mockServerStore{
		servers: []types.ServerRecord{
			{ID: "s1", Name: "server-alpha", Status: types.StatusActive},
			{ID: "s2", Name: "server-beta", Status: types.StatusStale},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ServerStore: serverStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/servers", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleServers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleServers_HTMX(t *testing.T) {
	serverStore := &mockServerStore{
		servers: []types.ServerRecord{
			{ID: "s1", Name: "server-alpha", Status: types.StatusActive},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ServerStore: serverStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/servers", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleServers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleAudit_WithStoreAndPagination(t *testing.T) {
	now := time.Now()
	entries := make([]types.AuditEntry, 60)
	for i := range entries {
		entries[i] = types.AuditEntry{
			EventType:  "tool_call",
			ClientID:   "client-1",
			ServerName: "server-1",
			CreatedAt:  now.Add(-time.Duration(i) * time.Minute),
			Details:    map[string]any{"index": i},
		}
	}

	auditStore := &mockAuditStore{entries: entries}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	t.Run("first page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?page=1", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleAudit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("second page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?page=2", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleAudit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("invalid page falls back to 1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?page=abc", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleAudit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("negative page falls back to 1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/audit?page=-1", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleAudit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("HTMX partial", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
		req.Header.Set("HX-Request", "true")
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandleAudit(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestHandleAudit_WithFilters(t *testing.T) {
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{EventType: "tool_call", ClientID: "client-1", Username: "client-1", ServerName: "server-1", CreatedAt: time.Now()},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit?client_id=client-1&username=alice&tool_name=server-1&decision=tool_call&since=2025-01-01&until=2025-12-31&sort_by=username&sort_dir=asc", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if auditStore.lastQueryFilter.Username != "alice" {
		t.Fatalf("expected username filter alice, got %q", auditStore.lastQueryFilter.Username)
	}
	if auditStore.lastQueryFilter.SortBy != "username" || auditStore.lastQueryFilter.SortDir != "asc" {
		t.Fatalf("unexpected sort filter: by=%q dir=%q", auditStore.lastQueryFilter.SortBy, auditStore.lastQueryFilter.SortDir)
	}
	if auditStore.lastCountFilter.Username != "alice" {
		t.Fatalf("expected count filter username alice, got %q", auditStore.lastCountFilter.Username)
	}
}

func TestHandleAudit_SortableHeadersAndExportCarryFilters(t *testing.T) {
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{
				EventType:  "tool_call",
				ClientID:   "client-1",
				Username:   "alice@example.com",
				ServerName: "server-1",
				CreatedAt:  time.Now(),
				Details:    map[string]any{"tool": "exec"},
			},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit?client_id=client-1&username=alice&tool_name=server-1&decision=tool_call&details=exec&since=2025-01-01&until=2025-12-31&sort_by=username&sort_dir=asc", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "/admin/audit/export?client_id=client-1&username=alice") {
		t.Fatalf("expected export link to include active username filter")
	}
	if !strings.Contains(body, "sort_by=event_type") {
		t.Fatalf("expected sortable event type header link")
	}
	if !strings.Contains(body, "type=\"hidden\" name=\"sort_by\" class=\"audit-filter\" value=\"username\"") {
		t.Fatalf("expected hidden sort_by filter input to preserve sorting across filter changes")
	}
	if !strings.Contains(body, "type=\"hidden\" name=\"sort_dir\" class=\"audit-filter\" value=\"asc\"") {
		t.Fatalf("expected hidden sort_dir filter input to preserve sorting across filter changes")
	}
}

func TestHandleAuditExport_WithEntries(t *testing.T) {
	now := time.Now()
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{
				EventType:  "tool_call",
				ClientID:   "client-1",
				Username:   "alice@example.com",
				ServerName: "server-1",
				CreatedAt:  now,
				Details:    map[string]any{"tool": "exec", "result": "ok"},
			},
			{
				EventType:  "policy_deny",
				ClientID:   "client-2",
				Username:   "system",
				ServerName: "server-2",
				CreatedAt:  now.Add(-time.Hour),
				Details:    map[string]any{"reason": "denied"},
			},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/export?client_id=client-1&username=alice&sort_by=username&sort_dir=asc", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleAuditExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("expected text/csv, got %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "time,event_type,client_id,username,server_name,details") {
		t.Fatalf("expected CSV header row")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Fatalf("expected username column data in export")
	}
	// Check we have two data rows plus header.
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 data), got %d", len(lines))
	}
	if auditStore.lastQueryFilter.Username != "alice" {
		t.Fatalf("expected export username filter alice, got %q", auditStore.lastQueryFilter.Username)
	}
	if auditStore.lastQueryFilter.SortBy != "username" || auditStore.lastQueryFilter.SortDir != "asc" {
		t.Fatalf("unexpected export sort filter: by=%q dir=%q", auditStore.lastQueryFilter.SortBy, auditStore.lastQueryFilter.SortDir)
	}
}

func TestHandleRateLimits_HTMX(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{
		RateLimitBackend: ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/ratelimits", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleRateLimits(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleRateLimits_NoInspector(t *testing.T) {
	// Use a backend that doesn't implement LimiterInspector.
	handler := setupTestHandlerWithStores(t, AdminDeps{
		RateLimitBackend: &noInspectorBackend{},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/ratelimits", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleRateLimits(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// noInspectorBackend implements LimiterBackend but not LimiterInspector.
type noInspectorBackend struct{}

func (n *noInspectorBackend) Allow(_ context.Context, _ ratelimit.LimiterKey, _ float64, _ int) (bool, int, time.Duration, error) {
	return true, 10, 0, nil
}
func (n *noInspectorBackend) Close() error { return nil }

func TestBuildAuditFilter(t *testing.T) {
	tests := []struct {
		name         string
		queryStr     string
		wantClient   string
		wantUsername string
		wantServer   string
		wantEvent    string
		wantSortBy   string
		wantSortDir  string
		wantSince    bool
		wantUntil    bool
	}{
		{
			name:         "all filters",
			queryStr:     "client_id=client-1&username=alice&tool_name=server-1&decision=tool_call&since=2025-01-15&until=2025-06-30&sort_by=username&sort_dir=asc",
			wantClient:   "client-1",
			wantUsername: "alice",
			wantServer:   "server-1",
			wantEvent:    "tool_call",
			wantSortBy:   "username",
			wantSortDir:  "asc",
			wantSince:    true,
			wantUntil:    true,
		},
		{
			name:         "no filters",
			queryStr:     "",
			wantClient:   "",
			wantUsername: "",
			wantServer:   "",
			wantEvent:    "",
			wantSortBy:   "created_at",
			wantSortDir:  "desc",
			wantSince:    false,
			wantUntil:    false,
		},
		{
			name:        "invalid since date",
			queryStr:    "since=not-a-date",
			wantSortBy:  "created_at",
			wantSortDir: "desc",
			wantSince:   false,
		},
		{
			name:        "invalid until date",
			queryStr:    "until=not-a-date",
			wantSortBy:  "created_at",
			wantSortDir: "desc",
			wantUntil:   false,
		},
		{
			name:        "only client_id",
			queryStr:    "client_id=my-client",
			wantClient:  "my-client",
			wantSortBy:  "created_at",
			wantSortDir: "desc",
		},
		{
			name:        "only since",
			queryStr:    "since=2025-03-15",
			wantSortBy:  "created_at",
			wantSortDir: "desc",
			wantSince:   true,
		},
		{
			name:        "only until",
			queryStr:    "until=2025-12-01",
			wantSortBy:  "created_at",
			wantSortDir: "desc",
			wantUntil:   true,
		},
		{
			name:        "invalid sort falls back",
			queryStr:    "sort_by=drop_table&sort_dir=sideways",
			wantSortBy:  "created_at",
			wantSortDir: "desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/audit?"+tt.queryStr, nil)
			filter := buildAuditFilter(req)

			if filter.ClientID != tt.wantClient {
				t.Errorf("ClientID: got %q, want %q", filter.ClientID, tt.wantClient)
			}
			if filter.Username != tt.wantUsername {
				t.Errorf("Username: got %q, want %q", filter.Username, tt.wantUsername)
			}
			if filter.ServerName != tt.wantServer {
				t.Errorf("ServerName: got %q, want %q", filter.ServerName, tt.wantServer)
			}
			if filter.EventType != tt.wantEvent {
				t.Errorf("EventType: got %q, want %q", filter.EventType, tt.wantEvent)
			}
			if filter.SortBy != tt.wantSortBy {
				t.Errorf("SortBy: got %q, want %q", filter.SortBy, tt.wantSortBy)
			}
			if filter.SortDir != tt.wantSortDir {
				t.Errorf("SortDir: got %q, want %q", filter.SortDir, tt.wantSortDir)
			}
			if tt.wantSince && filter.Since == nil {
				t.Errorf("expected Since to be set")
			}
			if !tt.wantSince && filter.Since != nil {
				t.Errorf("expected Since to be nil, got %v", filter.Since)
			}
			if tt.wantUntil && filter.Until == nil {
				t.Errorf("expected Until to be set")
			}
			if !tt.wantUntil && filter.Until != nil {
				t.Errorf("expected Until to be nil, got %v", filter.Until)
			}
		})
	}
}

func TestBuildAuditFilter_UntilAddsEndOfDay(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/audit?until=2025-06-15", nil)
	filter := buildAuditFilter(req)

	if filter.Until == nil {
		t.Fatalf("expected Until to be set")
	}
	// The until date should be end of day: 2025-06-15 23:59:59.
	expected, _ := time.Parse("2006-01-02", "2025-06-15")
	expected = expected.Add(24*time.Hour - time.Second)
	if !filter.Until.Equal(expected) {
		t.Fatalf("Until: got %v, want %v", filter.Until, expected)
	}
}

func TestNewRouter(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("create gcm: %v", err)
	}

	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}
	mb := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = mb.Close() }()

	serverStore := &mockServerStore{
		servers: []types.ServerRecord{
			{ID: "s1", Name: "srv-1", Status: types.StatusActive},
		},
	}
	auditStore := &mockAuditStore{}
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "tool-1", RiskLevel: types.RiskReadOnly},
		},
	}

	router := NewRouter(AdminDeps{
		Auth:                auth,
		ServerStore:         serverStore,
		AuditStore:          auditStore,
		ClassificationStore: classStore,
		RateLimitBackend:    mb,
	})

	if router == nil {
		t.Fatalf("expected non-nil router")
	}

	// Test that unauthenticated request to / redirects to login.
	srv := httptest.NewServer(router)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect for unauthenticated, got %d", resp.StatusCode)
	}
}

func TestNewRouter_NilLogger(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)

	auth := &AdminAuth{aead: aead, sessionTTL: time.Hour}
	mb := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = mb.Close() }()

	router := NewRouter(AdminDeps{
		Auth:             auth,
		Logger:           nil,
		RateLimitBackend: mb,
	})

	if router == nil {
		t.Fatalf("expected non-nil router even with nil logger")
	}
}

func TestRender_WithoutSession(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	// Request without a session in context.
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()

	handler.HandleDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestNewRouter_NilAuthWithoutDevMode_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil Auth without DevMode")
		}
	}()

	NewRouter(AdminDeps{Auth: nil, DevMode: false})
}

func TestNewRouter_IntegratedMode_AllowsNilAuth(t *testing.T) {
	router := NewRouter(AdminDeps{
		Auth:                 nil,
		Mode:                 "integrated",
		PlatformServiceToken: "svc-token",
	})
	if router == nil {
		t.Fatal("expected non-nil router in integrated mode")
	}
}

func TestHandleTools_SearchError_GracefulDegradation(t *testing.T) {
	classStore := &mockClassificationStore{
		searchErr: fmt.Errorf("database connection lost"),
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even on Search error, got %d", w.Code)
	}
}

func TestHandleTools_Pagination(t *testing.T) {
	classStore := &mockClassificationStore{
		searchResults: []types.ToolClassification{
			{ToolName: "page2-tool", RiskLevel: types.RiskReadOnly},
		},
		searchTotal: 75,
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/tools?page=2", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleTools(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "page2-tool") {
		t.Fatalf("expected page2-tool in response")
	}
}

func TestHandleAudit_DetailsSearch(t *testing.T) {
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{
				EventType:  "tool_call",
				ClientID:   "client-1",
				ServerName: "server-1",
				CreatedAt:  time.Now(),
				Details:    map[string]any{"tool": "exec"},
			},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit?details=exec", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestBuildAuditFilter_DetailsSearch(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/audit?details=search+term", nil)
	filter := buildAuditFilter(req)

	if filter.DetailsSearch != "search term" {
		t.Fatalf("expected DetailsSearch='search term', got %q", filter.DetailsSearch)
	}
}

func TestAuditEntryView_PrettyJSON(t *testing.T) {
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{
				EventType:  "tool_call",
				ClientID:   "client-1",
				ServerName: "server-1",
				CreatedAt:  time.Now(),
				Details:    map[string]any{"key": "value"},
			},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	// Pretty-printed JSON should have indentation (quotes are HTML-encoded as &#34;).
	if !strings.Contains(body, "&#34;key&#34;: &#34;value&#34;") {
		t.Fatalf("expected pretty-printed JSON in response, got: %s", body)
	}
}

func TestNewRouter_DevMode(t *testing.T) {
	mb := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = mb.Close() }()

	router := NewRouter(AdminDeps{
		Auth:             nil,
		DevMode:          true,
		RateLimitBackend: mb,
	})

	if router == nil {
		t.Fatal("expected non-nil router in dev mode")
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Dashboard should be accessible without a cookie.
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for dev mode dashboard, got %d", resp.StatusCode)
	}

	// Login should redirect to /admin/ in dev mode.
	resp, err = client.Get(srv.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect for dev mode login, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/admin/" {
		t.Fatalf("expected redirect to /admin/, got %s", loc)
	}
}

func TestHandleDashboard_DevModeBanner(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{DevMode: true})

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req = withSession(req, defaultSession())
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Get("/admin/", handler.HandleDashboard)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "dev-mode-banner") {
		t.Fatal("expected dev mode banner in response")
	}
}

func TestHandleDashboard_NoDevModeBanner(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{DevMode: false})

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req = withSession(req, defaultSession())
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	r.Get("/admin/", handler.HandleDashboard)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "dev-mode-banner") {
		t.Fatal("expected no dev mode banner in response")
	}
}

// --- Users handler tests ---

func TestHandleUsers(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		users: []types.User{
			{ID: "u1", Email: "alice@example.com", DisplayName: "Alice", Role: types.RoleAdmin, CreatedAt: now, UpdatedAt: now},
			{ID: "u2", Email: "bob@example.com", DisplayName: "Bob", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore: userStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUsers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "alice@example.com") {
		t.Fatal("expected alice in response")
	}
}

func TestHandleUsers_HTMX(t *testing.T) {
	userStore := &mockUserStore{
		users: []types.User{
			{ID: "u1", Email: "alice@example.com", Role: types.RoleAdmin, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore: userStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.Header.Set("HX-Request", "true")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUsers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "<!DOCTYPE") {
		t.Fatal("HTMX partial should not contain full page")
	}
}

func TestHandleUsers_WithRoleFilter(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		users: []types.User{
			{ID: "u1", Email: "alice@example.com", Role: types.RoleAdmin, CreatedAt: now, UpdatedAt: now},
			{ID: "u2", Email: "bob@example.com", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore: userStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users?role=admin", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUsers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleUsers_NilStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUsers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleUserDetail(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		byID: map[string]*types.User{
			"u1": {ID: "u1", OIDCSub: "sub-1", Email: "alice@example.com", DisplayName: "Alice", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "github.list_repos", GrantedBy: "admin", GrantedAt: now},
		},
	}
	classStore := &mockClassificationStore{
		tools: []types.ToolClassification{
			{ToolName: "github.list_repos", RiskLevel: types.RiskReadOnly},
			{ToolName: "github.delete_repo", RiskLevel: types.RiskDestructive},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:           userStore,
		ClassificationStore: classStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/u1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "alice@example.com") {
		t.Fatal("expected alice in response")
	}
}

func TestHandleUserDetail_NotFound(t *testing.T) {
	userStore := &mockUserStore{byID: map[string]*types.User{}}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore: userStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/missing", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleUserRoleUpdate_Success(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		byID: map[string]*types.User{
			"u1": {ID: "u1", Email: "alice@example.com", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
	}
	auditStore := &mockAuditStore{}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"role": {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserRoleUpdate(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d: %s", w.Code, w.Body.String())
	}
	if len(userStore.updatedRoles) != 1 {
		t.Fatalf("expected 1 role update, got %d", len(userStore.updatedRoles))
	}
	if userStore.updatedRoles[0].role != types.RoleAdmin {
		t.Fatalf("expected admin role, got %s", userStore.updatedRoles[0].role)
	}
	if len(auditStore.logged) == 0 {
		t.Fatal("expected audit entry")
	}
	if auditStore.logged[0].EventType != "user.role_changed" {
		t.Fatalf("expected user.role_changed, got %s", auditStore.logged[0].EventType)
	}
}

func TestHandleUserRoleUpdate_InvalidRole(t *testing.T) {
	userStore := &mockUserStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"role": {"superadmin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserRoleUpdate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleUserPermissionsUpdate_Success(t *testing.T) {
	userStore := &mockUserStore{}
	auditStore := &mockAuditStore{}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"tool_names": {"github.list_repos", "k8s.get_pods"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/permissions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserPermissionsUpdate(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) != 1 {
		t.Fatalf("expected 1 SetToolPermissions call, got %d", len(userStore.setPermsCalls))
	}
	if len(userStore.setPermsCalls[0].toolNames) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(userStore.setPermsCalls[0].toolNames))
	}
}

func TestHandleUserToolsAdd(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "github.list_repos", GrantedBy: "admin", GrantedAt: now},
		},
	}
	auditStore := &mockAuditStore{}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"tool_names": {"k8s.get_pods"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) != 1 {
		t.Fatalf("expected 1 SetToolPermissions call, got %d", len(userStore.setPermsCalls))
	}
	// Should have merged: list_repos + get_pods = 2 tools.
	if len(userStore.setPermsCalls[0].toolNames) != 2 {
		t.Fatalf("expected 2 merged tools, got %d", len(userStore.setPermsCalls[0].toolNames))
	}
	if len(auditStore.logged) == 0 {
		t.Fatal("expected audit entry")
	}
}

func TestHandleUserToolsRemove(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "github.list_repos", GrantedBy: "admin", GrantedAt: now},
			{UserID: "u1", ToolName: "k8s.get_pods", GrantedBy: "admin", GrantedAt: now},
		},
	}
	auditStore := &mockAuditStore{}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"tool_names": {"github.list_repos"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) != 1 {
		t.Fatalf("expected 1 SetToolPermissions call, got %d", len(userStore.setPermsCalls))
	}
	// Should have remaining: only get_pods.
	if len(userStore.setPermsCalls[0].toolNames) != 1 {
		t.Fatalf("expected 1 remaining tool, got %d", len(userStore.setPermsCalls[0].toolNames))
	}
	if len(auditStore.logged) == 0 {
		t.Fatal("expected audit entry")
	}
}

func TestSplitFederatedName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantServer string
		wantTool   string
	}{
		{"legacy federated", "mcp.github.list_repos", "github", "list_repos"},
		{"legacy only server", "mcp.github", "github", ""},
		{"legacy deep name", "mcp.k8s.namespace.get_pods", "k8s", "namespace.get_pods"},
		{"new format", "github.list_repos", "github", "list_repos"},
		{"new format deep", "k8s.namespace.get_pods", "k8s", "namespace.get_pods"},
		{"no dot", "list_repos", "", "list_repos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, tool := splitFederatedName(tt.input)
			if server != tt.wantServer {
				t.Errorf("server: got %q, want %q", server, tt.wantServer)
			}
			if tool != tt.wantTool {
				t.Errorf("tool: got %q, want %q", tool, tt.wantTool)
			}
		})
	}
}

// --- parsePage tests ---

func TestParsePage(t *testing.T) {
	tests := []struct {
		name     string
		queryStr string
		want     int
	}{
		{"valid page 3", "page=3", 3},
		{"valid page 1", "page=1", 1},
		{"missing page param", "", 1},
		{"invalid non-numeric", "page=abc", 1},
		{"zero page", "page=0", 1},
		{"negative page", "page=-5", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/test?"+tt.queryStr, nil)
			got := parsePage(req)
			if got != tt.want {
				t.Fatalf("parsePage(%q) = %d, want %d", tt.queryStr, got, tt.want)
			}
		})
	}
}

// --- pagination tests ---

func TestPagination(t *testing.T) {
	tests := []struct {
		name           string
		total          int
		pageSize       int
		wantTotalPages int
		wantPageCount  int
	}{
		{"zero total", 0, 50, 0, 0},
		{"one page exact", 50, 50, 1, 1},
		{"one page partial", 10, 50, 1, 1},
		{"multiple pages", 120, 50, 3, 3},
		{"multiple pages exact boundary", 100, 50, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalPages, pages := pagination(tt.total, tt.pageSize)
			if totalPages != tt.wantTotalPages {
				t.Fatalf("totalPages = %d, want %d", totalPages, tt.wantTotalPages)
			}
			if len(pages) != tt.wantPageCount {
				t.Fatalf("len(pages) = %d, want %d", len(pages), tt.wantPageCount)
			}
			// Verify page numbers are sequential starting from 1.
			for i, p := range pages {
				if p != i+1 {
					t.Fatalf("pages[%d] = %d, want %d", i, p, i+1)
				}
			}
		})
	}
}

// --- emailFromUser tests ---

func TestEmailFromUser_NilUser(t *testing.T) {
	got := emailFromUser(nil)
	if got != "" {
		t.Fatalf("emailFromUser(nil) = %q, want empty string", got)
	}
}

func TestEmailFromUser_ValidUser(t *testing.T) {
	u := &types.User{Email: "test@example.com"}
	got := emailFromUser(u)
	if got != "test@example.com" {
		t.Fatalf("emailFromUser = %q, want test@example.com", got)
	}
}

// --- findTool tests ---

func TestFindTool_Found(t *testing.T) {
	tools := []types.ToolClassification{
		{ToolName: "alpha", RiskLevel: types.RiskReadOnly},
		{ToolName: "beta", RiskLevel: types.RiskWrite},
	}
	tc, ok := findTool(tools, "beta")
	if !ok {
		t.Fatal("expected findTool to return true for existing tool")
	}
	if tc.ToolName != "beta" {
		t.Fatalf("expected beta, got %s", tc.ToolName)
	}
}

func TestFindTool_NotFound(t *testing.T) {
	tools := []types.ToolClassification{
		{ToolName: "alpha", RiskLevel: types.RiskReadOnly},
	}
	_, ok := findTool(tools, "missing")
	if ok {
		t.Fatal("expected findTool to return false for missing tool")
	}
}

func TestFindTool_EmptySlice(t *testing.T) {
	_, ok := findTool(nil, "anything")
	if ok {
		t.Fatal("expected findTool to return false for nil slice")
	}
}

// --- buildUserFilter tests ---

func TestBuildUserFilter_NoParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	filter := buildUserFilter(req, 1)

	if filter.Query != "" {
		t.Fatalf("expected empty query, got %q", filter.Query)
	}
	if filter.Role != "" {
		t.Fatalf("expected empty role, got %q", filter.Role)
	}
	if filter.Limit != defaultPageSize {
		t.Fatalf("expected limit %d, got %d", defaultPageSize, filter.Limit)
	}
	if filter.Offset != 0 {
		t.Fatalf("expected offset 0, got %d", filter.Offset)
	}
}

func TestBuildUserFilter_WithRoleFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/users?role=admin&q=alice", nil)
	filter := buildUserFilter(req, 2)

	if filter.Query != "alice" {
		t.Fatalf("expected query 'alice', got %q", filter.Query)
	}
	if filter.Role != types.RoleAdmin {
		t.Fatalf("expected role admin, got %q", filter.Role)
	}
	if filter.Offset != defaultPageSize {
		t.Fatalf("expected offset %d for page 2, got %d", defaultPageSize, filter.Offset)
	}
}

func TestBuildUserFilter_InvalidRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/users?role=superadmin", nil)
	filter := buildUserFilter(req, 1)

	if filter.Role != "" {
		t.Fatalf("expected empty role for invalid value, got %q", filter.Role)
	}
}

// --- searchUsers tests ---

func TestSearchUsers_NilStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})
	users, total := handler.searchUsers(context.Background(), types.UserFilter{})

	if users != nil {
		t.Fatalf("expected nil users, got %v", users)
	}
	if total != 0 {
		t.Fatalf("expected total 0, got %d", total)
	}
}

func TestSearchUsers_SearchError(t *testing.T) {
	userStore := &mockUserStore{searchErr: fmt.Errorf("db connection lost")}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	users, total := handler.searchUsers(context.Background(), types.UserFilter{})

	if users != nil {
		t.Fatalf("expected nil users on error, got %v", users)
	}
	if total != 0 {
		t.Fatalf("expected total 0 on error, got %d", total)
	}
}

func TestSearchUsers_Success(t *testing.T) {
	userStore := &mockUserStore{
		searchResults: []types.User{
			{ID: "u1", Email: "alice@example.com"},
		},
		searchTotal: 1,
	}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	users, total := handler.searchUsers(context.Background(), types.UserFilter{})

	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
}

// --- HandleUserDetail error path tests ---

func TestHandleUserDetail_EmptyID(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore: &mockUserStore{},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ID, got %d", w.Code)
	}
}

func TestHandleUserDetail_NilUserStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/u1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil userStore, got %d", w.Code)
	}
}

func TestHandleUserDetail_GetError(t *testing.T) {
	userStore := &mockUserStore{getErr: fmt.Errorf("database unavailable")}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/u1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for Get error, got %d", w.Code)
	}
}

func TestHandleUserDetail_GetReturnsNilUser(t *testing.T) {
	// byID map is empty so Get returns nil, nil.
	userStore := &mockUserStore{byID: map[string]*types.User{}}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nil user, got %d", w.Code)
	}
}

func TestHandleUserDetail_GetToolPermissionsError(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		byID: map[string]*types.User{
			"u1": {ID: "u1", Email: "alice@example.com", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
		getPermsErr: fmt.Errorf("permissions table unavailable"),
	}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/u1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserDetail(w, req)

	// GetToolPermissions error is logged but the page still renders.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even with GetToolPermissions error, got %d", w.Code)
	}
}

// --- HandleUserRoleUpdate error path tests ---

func TestHandleUserRoleUpdate_EmptyID(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: &mockUserStore{}})

	form := url.Values{"role": {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users//role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserRoleUpdate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ID, got %d", w.Code)
	}
}

func TestHandleUserRoleUpdate_NilUserStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	form := url.Values{"role": {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserRoleUpdate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil userStore, got %d", w.Code)
	}
}

func TestHandleUserRoleUpdate_UpdateRoleError(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		byID: map[string]*types.User{
			"u1": {ID: "u1", Email: "alice@example.com", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
		updateRoleErr: fmt.Errorf("db write error"),
	}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"role": {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserRoleUpdate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for UpdateRole error, got %d", w.Code)
	}
}

func TestHandleUserRoleUpdate_NoSession(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		byID: map[string]*types.User{
			"u1": {ID: "u1", Email: "alice@example.com", Role: types.RoleUser, CreatedAt: now, UpdatedAt: now},
		},
	}
	auditStore := &mockAuditStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"role": {"admin"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/role", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// No session attached.
	w := httptest.NewRecorder()

	handler.HandleUserRoleUpdate(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(auditStore.logged) == 0 {
		t.Fatal("expected audit entry")
	}
	if auditStore.logged[0].ClientID != "admin" {
		t.Fatalf("expected changedBy=admin without session, got %s", auditStore.logged[0].ClientID)
	}
}

// --- HandleUserPermissionsUpdate error path tests ---

func TestHandleUserPermissionsUpdate_EmptyID(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: &mockUserStore{}})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users//permissions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserPermissionsUpdate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ID, got %d", w.Code)
	}
}

func TestHandleUserPermissionsUpdate_NilUserStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/permissions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserPermissionsUpdate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil userStore, got %d", w.Code)
	}
}

func TestHandleUserPermissionsUpdate_SetPermsError(t *testing.T) {
	userStore := &mockUserStore{setPermsErr: fmt.Errorf("db error")}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/permissions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserPermissionsUpdate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for SetToolPermissions error, got %d", w.Code)
	}
}

func TestHandleUserPermissionsUpdate_NoSession(t *testing.T) {
	userStore := &mockUserStore{}
	auditStore := &mockAuditStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/permissions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// No session.
	w := httptest.NewRecorder()

	handler.HandleUserPermissionsUpdate(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(auditStore.logged) == 0 {
		t.Fatal("expected audit entry")
	}
	if auditStore.logged[0].ClientID != "admin" {
		t.Fatalf("expected grantedBy=admin without session, got %s", auditStore.logged[0].ClientID)
	}
}

func TestHandleUserPermissionsUpdate_EmptyToolNames(t *testing.T) {
	userStore := &mockUserStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	// Submit with no tool_names field at all (clears all permissions).
	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/permissions", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserPermissionsUpdate(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) != 1 {
		t.Fatalf("expected 1 SetToolPermissions call, got %d", len(userStore.setPermsCalls))
	}
	if len(userStore.setPermsCalls[0].toolNames) != 0 {
		t.Fatalf("expected 0 tool names, got %d", len(userStore.setPermsCalls[0].toolNames))
	}
}

// --- HandleUserToolsAdd error path tests ---

func TestHandleUserToolsAdd_EmptyID(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: &mockUserStore{}})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users//tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ID, got %d", w.Code)
	}
}

func TestHandleUserToolsAdd_NilUserStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil userStore, got %d", w.Code)
	}
}

func TestHandleUserToolsAdd_GetToolPermissionsError(t *testing.T) {
	userStore := &mockUserStore{getPermsErr: fmt.Errorf("permissions read error")}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for GetToolPermissions error, got %d", w.Code)
	}
}

func TestHandleUserToolsAdd_SetPermsError(t *testing.T) {
	userStore := &mockUserStore{setPermsErr: fmt.Errorf("write error")}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for SetToolPermissions error, got %d", w.Code)
	}
}

func TestHandleUserToolsAdd_NoSession(t *testing.T) {
	userStore := &mockUserStore{}
	auditStore := &mockAuditStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// No session.
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) == 0 {
		t.Fatal("expected SetToolPermissions call")
	}
	if userStore.setPermsCalls[0].grantedBy != "admin" {
		t.Fatalf("expected grantedBy=admin without session, got %s", userStore.setPermsCalls[0].grantedBy)
	}
}

func TestHandleUserToolsAdd_DuplicateToolMerge(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "tool-a", GrantedBy: "admin", GrantedAt: now},
		},
	}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	// Add tool-a again (duplicate) plus tool-b (new).
	form := url.Values{"tool_names": {"tool-a", "tool-b"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsAdd(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(userStore.setPermsCalls))
	}
	// Should have deduplicated: tool-a + tool-b = 2.
	if len(userStore.setPermsCalls[0].toolNames) != 2 {
		t.Fatalf("expected 2 merged tools, got %d", len(userStore.setPermsCalls[0].toolNames))
	}
}

// --- HandleUserToolsRemove error path tests ---

func TestHandleUserToolsRemove_EmptyID(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: &mockUserStore{}})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users//tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty ID, got %d", w.Code)
	}
}

func TestHandleUserToolsRemove_NilUserStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nil userStore, got %d", w.Code)
	}
}

func TestHandleUserToolsRemove_GetToolPermissionsError(t *testing.T) {
	userStore := &mockUserStore{getPermsErr: fmt.Errorf("permissions read error")}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"tool_names": {"tool-1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for GetToolPermissions error, got %d", w.Code)
	}
}

func TestHandleUserToolsRemove_SetPermsError(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "tool-a", GrantedBy: "admin", GrantedAt: now},
			{UserID: "u1", ToolName: "tool-b", GrantedBy: "admin", GrantedAt: now},
		},
		setPermsErr: fmt.Errorf("write error"),
	}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	form := url.Values{"tool_names": {"tool-a"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for SetToolPermissions error, got %d", w.Code)
	}
}

func TestHandleUserToolsRemove_NoSession(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "tool-a", GrantedBy: "admin", GrantedAt: now},
		},
	}
	auditStore := &mockAuditStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{
		UserStore:  userStore,
		AuditStore: auditStore,
	})

	form := url.Values{"tool_names": {"tool-a"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// No session.
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	if len(userStore.setPermsCalls) == 0 {
		t.Fatal("expected SetToolPermissions call")
	}
	if userStore.setPermsCalls[0].grantedBy != "admin" {
		t.Fatalf("expected revokedBy=admin without session, got %s", userStore.setPermsCalls[0].grantedBy)
	}
}

func TestHandleUserToolsRemove_RemoveNonexistentTool(t *testing.T) {
	now := time.Now()
	userStore := &mockUserStore{
		permissions: []types.UserToolPermission{
			{UserID: "u1", ToolName: "tool-a", GrantedBy: "admin", GrantedAt: now},
		},
	}
	handler := setupTestHandlerWithStores(t, AdminDeps{UserStore: userStore})

	// Try to remove a tool that doesn't exist in the user's permissions.
	form := url.Values{"tool_names": {"tool-nonexistent"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/tools/remove", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "u1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleUserToolsRemove(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	// Original tool-a should remain since we tried to remove a non-existent one.
	if len(userStore.setPermsCalls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(userStore.setPermsCalls))
	}
	if len(userStore.setPermsCalls[0].toolNames) != 1 {
		t.Fatalf("expected 1 remaining tool, got %d", len(userStore.setPermsCalls[0].toolNames))
	}
}

func TestHandlePruneOrphanedTools(t *testing.T) {
	t.Run("successful prune with active servers", func(t *testing.T) {
		serverStore := &mockServerStore{
			servers: []types.ServerRecord{
				{
					ID:     "s1",
					Name:   "active-server",
					Status: types.StatusActive,
					Capabilities: types.Capability{
						Tools: []string{"tool.a", "tool.b"},
					},
				},
				{
					ID:     "s2",
					Name:   "stale-server",
					Status: types.StatusStale,
					Capabilities: types.Capability{
						Tools: []string{"tool.c"},
					},
				},
			},
		}
		classStore := &mockClassificationStore{deleteNotInResult: 2}
		auditStore := &mockAuditStore{}
		broadcaster := &mockBroadcaster{}

		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore:         serverStore,
			ClassificationStore: classStore,
			AuditStore:          auditStore,
			Broadcaster:         broadcaster,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/prune-orphaned-tools", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandlePruneOrphanedTools(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect (302), got %d: %s", w.Code, w.Body.String())
		}
		if len(classStore.deleteNotInCalled) != 1 {
			t.Fatalf("expected 1 DeleteNotIn call, got %d", len(classStore.deleteNotInCalled))
		}
		// Only active server's tools should be passed.
		called := classStore.deleteNotInCalled[0]
		if len(called) != 2 {
			t.Fatalf("expected 2 active tool names, got %d: %v", len(called), called)
		}
		// Verify stale server's tools were excluded.
		for _, name := range called {
			if name == "tool.c" {
				t.Fatalf("stale server tool 'tool.c' should not be in active tools list")
			}
		}
		if len(auditStore.logged) == 0 {
			t.Fatal("expected audit entry")
		}
		if auditStore.logged[0].EventType != "tools_pruned" {
			t.Fatalf("expected event_type=tools_pruned, got %s", auditStore.logged[0].EventType)
		}
		if len(broadcaster.published) == 0 {
			t.Fatal("expected broadcast event")
		}
	})

	t.Run("no active servers prunes all", func(t *testing.T) {
		serverStore := &mockServerStore{
			servers: []types.ServerRecord{
				{ID: "s1", Name: "stale-server", Status: types.StatusStale},
			},
		}
		classStore := &mockClassificationStore{deleteNotInResult: 5}
		auditStore := &mockAuditStore{}

		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore:         serverStore,
			ClassificationStore: classStore,
			AuditStore:          auditStore,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/prune-orphaned-tools", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandlePruneOrphanedTools(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect, got %d", w.Code)
		}
		if len(classStore.deleteNotInCalled) != 1 {
			t.Fatalf("expected 1 DeleteNotIn call, got %d", len(classStore.deleteNotInCalled))
		}
		if len(classStore.deleteNotInCalled[0]) != 0 {
			t.Fatalf("expected empty active tools list, got %v", classStore.deleteNotInCalled[0])
		}
	})

	t.Run("server list error", func(t *testing.T) {
		serverStore := &mockServerStore{listErr: fmt.Errorf("db connection lost")}
		classStore := &mockClassificationStore{}

		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore:         serverStore,
			ClassificationStore: classStore,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/prune-orphaned-tools", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandlePruneOrphanedTools(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})

	t.Run("DeleteNotIn error", func(t *testing.T) {
		serverStore := &mockServerStore{
			servers: []types.ServerRecord{
				{ID: "s1", Status: types.StatusActive, Capabilities: types.Capability{Tools: []string{"tool.a"}}},
			},
		}
		classStore := &mockClassificationStore{deleteNotInErr: fmt.Errorf("delete failed")}

		handler := setupTestHandlerWithStores(t, AdminDeps{
			ServerStore:         serverStore,
			ClassificationStore: classStore,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/prune-orphaned-tools", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandlePruneOrphanedTools(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})

	t.Run("nil stores graceful no-op", func(t *testing.T) {
		handler := setupTestHandlerWithStores(t, AdminDeps{})

		req := httptest.NewRequest(http.MethodPost, "/admin/servers/prune-orphaned-tools", nil)
		req = withSession(req, defaultSession())
		w := httptest.NewRecorder()

		handler.HandlePruneOrphanedTools(w, req)

		if w.Code != http.StatusFound {
			t.Fatalf("expected redirect (302), got %d", w.Code)
		}
	})
}
