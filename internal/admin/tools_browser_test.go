package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/go-chi/chi/v5"
)

func testDiscoveryIndex() *proxy.DiscoveryIndex {
	tools := []proxy.ToolDefinition{
		{Name: "github.create_issue", Description: "Create a new issue in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`)},
		{Name: "github.list_issues", Description: "List issues in a GitHub repository.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "github.close_issue", Description: "Close an existing issue.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.send_message", Description: "Send a message to a Slack channel.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "slack.list_channels", Description: "List available Slack channels.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "jira.create_ticket", Description: "Create a Jira ticket for issue tracking.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	idx := proxy.NewDiscoveryIndex()
	idx.Index(tools)
	return idx
}

func setupBrowserHandler(t *testing.T, progressive bool) *AdminHandler {
	t.Helper()
	var idx *proxy.DiscoveryIndex
	if progressive {
		idx = testDiscoveryIndex()
	}
	return NewAdminHandler(AdminDeps{
		DiscoveryIndex:       idx,
		ProgressiveDiscovery: progressive,
	})
}

func TestHandleToolBrowse_DefaultPagination(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/browse", nil)
	w := httptest.NewRecorder()

	handler.HandleToolBrowse(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	total, ok := resp["total"].(float64)
	if !ok || int(total) != 6 {
		t.Fatalf("expected total=6, got %v", resp["total"])
	}
	tools, ok := resp["tools"].([]any)
	if !ok || len(tools) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(tools))
	}
}

func TestHandleToolBrowse_SearchByKeyword(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/browse?q=issue", nil)
	w := httptest.NewRecorder()

	handler.HandleToolBrowse(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	total := int(resp["total"].(float64))
	if total < 3 {
		t.Fatalf("expected at least 3 matches for 'issue', got %d", total)
	}
}

func TestHandleToolBrowse_FilterByCategory(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/browse?category=github", nil)
	w := httptest.NewRecorder()

	handler.HandleToolBrowse(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	total := int(resp["total"].(float64))
	if total != 3 {
		t.Fatalf("expected 3 github tools, got %d", total)
	}
}

func TestHandleToolBrowse_ProgressiveDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, false)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/browse", nil)
	w := httptest.NewRecorder()

	handler.HandleToolBrowse(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestHandleToolSchema_ValidTool(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools/github.create_issue/schema", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "github.create_issue")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.HandleToolSchema(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tool proxy.ToolDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.Name != "github.create_issue" {
		t.Fatalf("expected tool name github.create_issue, got %q", tool.Name)
	}
	// Should have full schema, not stripped.
	if string(tool.InputSchema) == "{}" {
		t.Fatal("expected full input schema, got stripped version")
	}
}

func TestHandleToolSchema_UnknownTool(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools/nonexistent/schema", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.HandleToolSchema(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleToolSchema_ProgressiveDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, false)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools/github.create_issue/schema", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "github.create_issue")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.HandleToolSchema(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestHandleDiscoveryStats_ReturnsValidStats(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/discovery/stats", nil)
	w := httptest.NewRecorder()

	handler.HandleDiscoveryStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	totalTools := int(resp["total_tools"].(float64))
	if totalTools != 6 {
		t.Fatalf("expected 6 total_tools, got %d", totalTools)
	}

	categories, ok := resp["categories"].(map[string]any)
	if !ok {
		t.Fatal("expected categories map")
	}
	if categories["github"].(float64) != 3 {
		t.Fatalf("expected 3 github tools, got %v", categories["github"])
	}

	if resp["progressive_discovery"] != true {
		t.Fatal("expected progressive_discovery=true")
	}
	if resp["estimated_full_tokens"] == nil {
		t.Fatal("expected estimated_full_tokens field")
	}
	if resp["estimated_summary_tokens"] == nil {
		t.Fatal("expected estimated_summary_tokens field")
	}
	if resp["token_savings_percent"] == nil {
		t.Fatal("expected token_savings_percent field")
	}
}

func TestHandleDiscoveryStats_ProgressiveDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, false)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/discovery/stats", nil)
	w := httptest.NewRecorder()

	handler.HandleDiscoveryStats(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestHandleToolBrowse_Pagination(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)
	req := httptest.NewRequest(http.MethodGet, "/admin/tools/browse?page=2&per_page=2", nil)
	w := httptest.NewRecorder()

	handler.HandleToolBrowse(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	total := int(resp["total"].(float64))
	if total != 6 {
		t.Fatalf("expected total=6, got %d", total)
	}
	tools := resp["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools on page 2, got %d", len(tools))
	}
	page := int(resp["page"].(float64))
	if page != 2 {
		t.Fatalf("expected page=2, got %d", page)
	}
}

func TestHandleToolSchema_EmptyName(t *testing.T) {
	t.Parallel()

	handler := setupBrowserHandler(t, true)

	req := httptest.NewRequest(http.MethodGet, "/admin/tools//schema", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.HandleToolSchema(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
