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
	servers []types.ServerRecord
	listErr error
	delErr  error
	deleted []string
}

var _ store.ServerStore = (*mockServerStore)(nil)

func (m *mockServerStore) Create(_ context.Context, _ *types.ServerRecord) error { return nil }
func (m *mockServerStore) Get(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockServerStore) GetByName(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockServerStore) GetBySPIFFEID(_ context.Context, _ string) (*types.ServerRecord, error) {
	return nil, nil
}
func (m *mockServerStore) List(_ context.Context, _ types.ServerFilter) ([]types.ServerRecord, error) {
	if m.listErr != nil {
		return nil, m.listErr
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
	entries  []types.AuditEntry
	queryErr error
	logged   []*types.AuditEntry
}

var _ store.AuditStore = (*mockAuditStore)(nil)

func (m *mockAuditStore) Log(_ context.Context, entry *types.AuditEntry) error {
	m.logged = append(m.logged, entry)
	return nil
}
func (m *mockAuditStore) Query(_ context.Context, _ types.AuditFilter) ([]types.AuditEntry, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.entries, nil
}

type mockClassificationStore struct {
	tools     []types.ToolClassification
	byName    map[string]*types.ToolClassification
	getAllErr error
	getErr    error
	upsertErr error
	upserted  []*types.ToolClassification
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
	var result []types.ToolClassification
	for _, t := range m.tools {
		if t.RiskLevel == level {
			result = append(result, t)
		}
	}
	return result, nil
}
func (m *mockClassificationStore) Upsert(_ context.Context, c *types.ToolClassification) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.upserted = append(m.upserted, c)
	return nil
}
func (m *mockClassificationStore) Delete(_ context.Context, _ string) error { return nil }

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
			{EventType: "tool_call", ClientID: "client-1", ServerName: "server-1", CreatedAt: time.Now()},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit?client_id=client-1&tool_name=server-1&decision=tool_call&since=2025-01-01&until=2025-12-31", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleAudit(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleAuditExport_WithEntries(t *testing.T) {
	now := time.Now()
	auditStore := &mockAuditStore{
		entries: []types.AuditEntry{
			{
				EventType:  "tool_call",
				ClientID:   "client-1",
				ServerName: "server-1",
				CreatedAt:  now,
				Details:    map[string]any{"tool": "exec", "result": "ok"},
			},
			{
				EventType:  "policy_deny",
				ClientID:   "client-2",
				ServerName: "server-2",
				CreatedAt:  now.Add(-time.Hour),
				Details:    map[string]any{"reason": "denied"},
			},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		AuditStore: auditStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/audit/export?client_id=client-1", nil)
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
	if !strings.Contains(body, "time,event_type,client_id,server_name,details") {
		t.Fatalf("expected CSV header row")
	}
	// Check we have two data rows plus header.
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 data), got %d", len(lines))
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
		name       string
		queryStr   string
		wantClient string
		wantServer string
		wantEvent  string
		wantSince  bool
		wantUntil  bool
	}{
		{
			name:       "all filters",
			queryStr:   "client_id=client-1&tool_name=server-1&decision=tool_call&since=2025-01-15&until=2025-06-30",
			wantClient: "client-1",
			wantServer: "server-1",
			wantEvent:  "tool_call",
			wantSince:  true,
			wantUntil:  true,
		},
		{
			name:       "no filters",
			queryStr:   "",
			wantClient: "",
			wantServer: "",
			wantEvent:  "",
			wantSince:  false,
			wantUntil:  false,
		},
		{
			name:       "invalid since date",
			queryStr:   "since=not-a-date",
			wantSince:  false,
		},
		{
			name:       "invalid until date",
			queryStr:   "until=not-a-date",
			wantUntil:  false,
		},
		{
			name:       "only client_id",
			queryStr:   "client_id=my-client",
			wantClient: "my-client",
		},
		{
			name:       "only since",
			queryStr:   "since=2025-03-15",
			wantSince:  true,
		},
		{
			name:       "only until",
			queryStr:   "until=2025-12-01",
			wantUntil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/audit?"+tt.queryStr, nil)
			filter := buildAuditFilter(req)

			if filter.ClientID != tt.wantClient {
				t.Errorf("ClientID: got %q, want %q", filter.ClientID, tt.wantClient)
			}
			if filter.ServerName != tt.wantServer {
				t.Errorf("ServerName: got %q, want %q", filter.ServerName, tt.wantServer)
			}
			if filter.EventType != tt.wantEvent {
				t.Errorf("EventType: got %q, want %q", filter.EventType, tt.wantEvent)
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
