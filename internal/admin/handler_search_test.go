package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/search"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

func TestHandleSearch_NilStores(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodGet, "/admin/search", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Search") {
		t.Fatalf("expected Search page title in body")
	}
	if !strings.Contains(body, "substring") {
		t.Fatalf("expected default engine type 'substring' in body")
	}
}

func TestHandleSearch_WithStores(t *testing.T) {
	synonymStore := &mockSynonymStore{
		entries: []types.SynonymEntry{
			{Term: "deploy", Synonyms: []string{"ship", "release"}, UpdatedAt: time.Now()},
		},
	}
	reindexStore := &mockReindexLogStore{
		entries: []types.ReindexLogEntry{
			{ID: 1, Engine: "bm25", Status: "success", CreatedAt: time.Now()},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		SynonymStore:     synonymStore,
		ReindexLogStore:  reindexStore,
		SearchEngineType: "bm25",
		EmbedderType:     "onnx-in-process",
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/search", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "bm25") {
		t.Fatalf("expected engine type 'bm25' in body")
	}
}

func TestHandleSearch_WithDiscoveryIndex(t *testing.T) {
	cfg := &config.Config{ProgressiveDiscovery: true, SearchEngine: "bm25"}
	ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), cfg, nil, 0, nil)
	di := ps.DiscoveryIndex()

	handler := setupTestHandlerWithStores(t, AdminDeps{
		DiscoveryIndex:   di,
		SearchEngineType: "bm25",
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/search", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleSearch_WithSharedStatusSnapshot(t *testing.T) {
	vectorReady := false
	handler := setupTestHandlerWithStores(t, AdminDeps{
		SearchEngineType: "bm25",
		EmbedderType:     "none",
		SearchStatusFunc: func() SearchStatusSnapshot {
			return SearchStatusSnapshot{
				EngineType:            "hybrid",
				EmbedderType:          "onnx-in-process",
				IndexedTools:          63,
				EngineReady:           false,
				VectorReady:           &vectorReady,
				VectorSearchFallbacks: 3,
				VectorIndexFallbacks:  1,
				EngineErrorFallbacks:  2,
			}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/search", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "hybrid") {
		t.Fatalf("expected shared engine type in body")
	}
	if !strings.Contains(body, "onnx-in-process") {
		t.Fatalf("expected shared embedder type in body")
	}
	if !strings.Contains(body, "Vector Ready") {
		t.Fatalf("expected vector ready row in body")
	}
	if !strings.Contains(body, "Fallbacks (Vector Search)") {
		t.Fatalf("expected vector fallback stats in body")
	}
}

func TestHandleSearch_StoreErrors(t *testing.T) {
	synonymStore := &mockSynonymStore{listErr: errors.New("db down")}
	reindexStore := &mockReindexLogStore{recentErr: errors.New("db down")}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		SynonymStore:    synonymStore,
		ReindexLogStore: reindexStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/search", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearch(w, req)

	// Should still render the page despite store errors.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even with store errors, got %d", w.Code)
	}
}

func TestHandleSearchTest_EmptyQuery(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodPost, "/admin/search/test", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearchTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Enter a query") {
		t.Fatalf("expected empty query message")
	}
}

func TestHandleSearchTest_NilDiscoveryIndex(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	form := url.Values{"query": {"kubernetes"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearchTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Discovery index not available") {
		t.Fatalf("expected discovery index not available message")
	}
}

func TestHandleSearchTest_WithDiscoveryIndex(t *testing.T) {
	cfg := &config.Config{ProgressiveDiscovery: true, SearchEngine: "bm25"}
	ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), cfg, nil, 0, nil)
	di := ps.DiscoveryIndex()

	handler := setupTestHandlerWithStores(t, AdminDeps{
		DiscoveryIndex:   di,
		SearchEngineType: "bm25",
	})

	form := url.Values{"query": {"test-tool"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearchTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "search-test-results") {
		t.Fatalf("expected search results container in body")
	}
	if !strings.Contains(body, "test-tool") {
		t.Fatalf("expected query echoed in body")
	}
}

func TestHandleSearchTest_WithResults(t *testing.T) {
	cfg := &config.Config{ProgressiveDiscovery: true, SearchEngine: "bm25"}
	index := registration.NewCapabilityIndex()
	index.Add(types.ServerRecord{
		ID:   "srv-1",
		Name: "test-server",
		Capabilities: types.Capability{
			Tools: []string{"list_pods"},
		},
		Status: types.StatusActive,
	})
	ps := proxy.NewProxyServer(index, cfg, nil, 0, nil)
	di := ps.DiscoveryIndex()

	handler := setupTestHandlerWithStores(t, AdminDeps{
		DiscoveryIndex:   di,
		SearchEngineType: "bm25",
	})

	form := url.Values{"query": {"list_pods"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearchTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "list_pods") {
		t.Fatalf("expected tool name in results, got: %s", body)
	}
}

func TestHandleSearchTest_WithEngineResults(t *testing.T) {
	cfg := &config.Config{ProgressiveDiscovery: true, SearchEngine: "bm25"}
	ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), cfg, nil, 0, nil)
	di := ps.DiscoveryIndex()

	// Set engine first, then index tools so the engine gets populated.
	bm25 := search.NewBM25Engine()
	di.SetEngine(bm25)
	di.Index([]proxy.ToolDefinition{
		{Name: "create_issue", Description: "Create a GitHub issue"},
	})

	handler := setupTestHandlerWithStores(t, AdminDeps{
		DiscoveryIndex:   di,
		SearchEngineType: "bm25",
	})

	form := url.Values{"query": {"create_issue"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSearchTest(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Raw engine scores") {
		t.Fatalf("expected raw engine scores section, got: %s", body)
	}
}

func TestHandleSynonymList_NilStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodGet, "/admin/search/synonyms", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No custom synonyms") {
		t.Fatalf("expected no synonyms message")
	}
}

func TestHandleSynonymList_WithEntries(t *testing.T) {
	synonymStore := &mockSynonymStore{
		entries: []types.SynonymEntry{
			{Term: "deploy", Synonyms: []string{"ship"}, UpdatedAt: time.Now()},
		},
	}

	handler := setupTestHandlerWithStores(t, AdminDeps{
		SynonymStore: synonymStore,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/search/synonyms", nil)
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "deploy") {
		t.Fatalf("expected synonym term in body")
	}
}

func TestHandleSynonymCreate_NilStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	form := url.Values{"term": {"deploy"}, "synonyms": {"ship,release"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymCreate(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleSynonymCreate_MissingFields(t *testing.T) {
	synonymStore := &mockSynonymStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{SynonymStore: synonymStore})

	// Missing synonyms field.
	form := url.Values{"term": {"deploy"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymCreate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSynonymCreate_Success(t *testing.T) {
	synonymStore := &mockSynonymStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{SynonymStore: synonymStore})

	form := url.Values{"term": {"Deploy"}, "synonyms": {"Ship, Release, Launch"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymCreate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(synonymStore.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(synonymStore.upserted))
	}
	if synonymStore.upserted[0].Term != "deploy" {
		t.Fatalf("expected lowercased term 'deploy', got %q", synonymStore.upserted[0].Term)
	}
	if len(synonymStore.upserted[0].Synonyms) != 3 {
		t.Fatalf("expected 3 synonyms, got %d", len(synonymStore.upserted[0].Synonyms))
	}
}

func TestHandleSynonymCreate_UpsertError(t *testing.T) {
	synonymStore := &mockSynonymStore{upsertErr: errors.New("db error")}
	handler := setupTestHandlerWithStores(t, AdminDeps{SynonymStore: synonymStore})

	form := url.Values{"term": {"deploy"}, "synonyms": {"ship"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymCreate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleSynonymDelete_NilStore(t *testing.T) {
	handler := setupTestHandlerWithStores(t, AdminDeps{})

	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms/deploy/delete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("term", "deploy")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymDelete(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleSynonymDelete_MissingTerm(t *testing.T) {
	synonymStore := &mockSynonymStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{SynonymStore: synonymStore})

	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms//delete", nil)
	// No chi URL param set — term will be empty.
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSynonymDelete_Success(t *testing.T) {
	synonymStore := &mockSynonymStore{}
	handler := setupTestHandlerWithStores(t, AdminDeps{SynonymStore: synonymStore})

	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms/deploy/delete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("term", "deploy")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymDelete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(synonymStore.deleted) != 1 || synonymStore.deleted[0] != "deploy" {
		t.Fatalf("expected deploy deleted, got %v", synonymStore.deleted)
	}
}

func TestHandleSynonymDelete_Error(t *testing.T) {
	synonymStore := &mockSynonymStore{deleteErr: errors.New("db error")}
	handler := setupTestHandlerWithStores(t, AdminDeps{SynonymStore: synonymStore})

	req := httptest.NewRequest(http.MethodPost, "/admin/search/synonyms/deploy/delete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("term", "deploy")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = withSession(req, defaultSession())
	w := httptest.NewRecorder()

	handler.HandleSynonymDelete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestParseSynonymList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"simple", "a, b, c", []string{"a", "b", "c"}},
		{"trims whitespace", "  foo ,  bar  , baz ", []string{"foo", "bar", "baz"}},
		{"lowercases", "Deploy, SHIP, Release", []string{"deploy", "ship", "release"}},
		{"skips empty", "a,,b, ,c", []string{"a", "b", "c"}},
		{"single item", "solo", []string{"solo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSynonymList(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d items, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Fatalf("item %d: expected %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestItoa(t *testing.T) {
	if itoa(42) != "42" {
		t.Fatalf("expected '42', got %q", itoa(42))
	}
	if itoa(0) != "0" {
		t.Fatalf("expected '0', got %q", itoa(0))
	}
}

func TestFtoa(t *testing.T) {
	result := ftoa(3.14159)
	if result != "3.1416" {
		t.Fatalf("expected '3.1416', got %q", result)
	}
	if ftoa(0) != "0.0000" {
		t.Fatalf("expected '0.0000', got %q", ftoa(0))
	}
}
