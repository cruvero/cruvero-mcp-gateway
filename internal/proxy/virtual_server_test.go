package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// testServerRecord creates a ServerRecord for testing virtual server endpoints.
func testServerRecord(id, name string, status types.ServerStatus, host string, port int) types.ServerRecord {
	return types.ServerRecord{
		ID:       id,
		Name:     name,
		Host:     host,
		Port:     port,
		Protocol: "http",
		Status:   status,
		Capabilities: types.Capability{
			Tools: []string{"test-tool"},
		},
	}
}

// newTestRouter creates a chi router with the virtual server handler mounted
// at /mcp/servers/{serverName}/* for testing.
func newTestRouter(handler *VirtualServerHandler) chi.Router {
	r := chi.NewRouter()
	r.Route("/mcp/servers/{serverName}", func(sub chi.Router) {
		sub.HandleFunc("/*", handler.ServeHTTP)
		sub.HandleFunc("/", handler.ServeHTTP)
	})
	return r
}

func TestVirtualServerHandler_ValidServerName(t *testing.T) {
	t.Parallel()

	// Start a fake backend that responds to POST /mcp with a simple JSON body.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer backend.Close()

	// Parse the backend address to get host and port.
	backendURL := backend.URL
	hostPort := strings.TrimPrefix(backendURL, "http://")
	parts := strings.SplitN(hostPort, ":", 2)
	host := parts[0]
	port := 0
	if len(parts) == 2 {
		for _, c := range parts[1] {
			port = port*10 + int(c-'0')
		}
	}

	index := registration.NewCapabilityIndex()
	index.Add(testServerRecord("srv-1", "my-backend", types.StatusActive, host, port))

	handler := NewVirtualServerHandler(index, nil, 0, nil)
	router := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/mcp/servers/my-backend/", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestVirtualServerHandler_InvalidServerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverName string
	}{
		{name: "special chars", serverName: "my@server"},
		{name: "uppercase letters", serverName: "MyServer"},
		{name: "starts with hyphen", serverName: "-invalid"},
		{name: "starts with underscore", serverName: "_invalid"},
		{name: "contains dots", serverName: "my.server"},
		{name: "too long (64 chars)", serverName: strings.Repeat("a", 64)},
	}

	index := registration.NewCapabilityIndex()
	handler := NewVirtualServerHandler(index, nil, 0, nil)
	router := newTestRouter(handler)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := "/mcp/servers/" + tc.serverName + "/"
			req := httptest.NewRequest(http.MethodPost, path, nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest && rr.Code != http.StatusNotFound {
				t.Fatalf("expected 400 or 404 for server name %q, got %d", tc.serverName, rr.Code)
			}

			if rr.Code == http.StatusBadRequest {
				var body map[string]string
				if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
					t.Fatalf("expected JSON error body: %v", err)
				}
				if body["error"] == "" {
					t.Fatal("expected non-empty error message")
				}
			}
		})
	}
}

func TestVirtualServerHandler_NonexistentServer(t *testing.T) {
	t.Parallel()

	index := registration.NewCapabilityIndex()
	index.Add(testServerRecord("srv-1", "existing-server", types.StatusActive, "127.0.0.1", 9999))

	handler := NewVirtualServerHandler(index, nil, 0, nil)
	router := newTestRouter(handler)

	req := httptest.NewRequest(http.MethodPost, "/mcp/servers/nonexistent/", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("expected JSON error body: %v", err)
	}
	if !strings.Contains(body["error"], "not found") {
		t.Fatalf("expected 'not found' in error, got %q", body["error"])
	}
}

func TestVirtualServerHandler_ServerNotActive(t *testing.T) {
	t.Parallel()

	statuses := []struct {
		name   string
		status types.ServerStatus
	}{
		{name: "pending", status: types.StatusPending},
		{name: "stale", status: types.StatusStale},
		{name: "expired", status: types.StatusExpired},
	}

	for _, tc := range statuses {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Build a fresh index with an active server, then rebuild with non-active.
			// CapabilityIndex.Add() only adds routable (active) servers, so to test
			// non-active status we use LookupServer which reads from tools map.
			// A non-routable server won't appear in the index at all, so the handler
			// should return 404 (server not found in index).
			//
			// However, the real scenario is: a server was active but becomes stale.
			// In that case the index would have been rebuilt and the server removed.
			// We verify the handler returns 404 for non-routable servers.
			index := registration.NewCapabilityIndex()
			// Add the server as active first.
			index.Add(testServerRecord("srv-stale", "stale-server", types.StatusActive, "127.0.0.1", 9999))
			// Rebuild without this server (simulates it becoming non-routable).
			index.Rebuild([]types.ServerRecord{
				testServerRecord("srv-stale", "stale-server", tc.status, "127.0.0.1", 9999),
			})

			handler := NewVirtualServerHandler(index, nil, 0, nil)
			router := newTestRouter(handler)

			req := httptest.NewRequest(http.MethodPost, "/mcp/servers/stale-server/", nil)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			// Non-routable servers are excluded from the index entirely,
			// so LookupServer returns nil and we get 404.
			if rr.Code != http.StatusNotFound {
				t.Fatalf("expected status 404 for %s server, got %d: %s", tc.name, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestVirtualServerHandler_ValidServerNamePatterns(t *testing.T) {
	t.Parallel()

	validNames := []string{
		"a",
		"myserver",
		"my-server",
		"my_server",
		"server123",
		"0-starts-with-digit",
		strings.Repeat("a", 63), // max length
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !serverNamePattern.MatchString(name) {
				t.Fatalf("expected server name %q to be valid", name)
			}
		})
	}
}

func TestVirtualServerHandler_InvalidServerNamePatterns(t *testing.T) {
	t.Parallel()

	invalidNames := []string{
		"",
		"-starts-with-hyphen",
		"_starts-with-underscore",
		"UPPERCASE",
		"has spaces",
		"has.dots",
		"has@special",
		strings.Repeat("a", 64), // 1 char too long
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if serverNamePattern.MatchString(name) {
				t.Fatalf("expected server name %q to be invalid", name)
			}
		})
	}
}

func TestVirtualServerHandler_CachesInstances(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	hostPort := strings.TrimPrefix(backend.URL, "http://")
	parts := strings.SplitN(hostPort, ":", 2)
	host := parts[0]
	port := 0
	for _, c := range parts[1] {
		port = port*10 + int(c-'0')
	}

	index := registration.NewCapabilityIndex()
	index.Add(testServerRecord("srv-cache", "cache-test", types.StatusActive, host, port))

	handler := NewVirtualServerHandler(index, nil, 0, nil)
	router := newTestRouter(handler)

	// First request creates the instance.
	req1 := httptest.NewRequest(http.MethodPost, "/mcp/servers/cache-test/", strings.NewReader("{}"))
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)

	// Second request should reuse the cached instance.
	req2 := httptest.NewRequest(http.MethodPost, "/mcp/servers/cache-test/", strings.NewReader("{}"))
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)

	// Both should succeed (proxy to the backend).
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rr1.Code)
	}
	if rr2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rr2.Code)
	}

	// Verify the instance was cached.
	_, loaded := handler.instances.Load("srv-cache")
	if !loaded {
		t.Fatal("expected instance to be cached in sync.Map")
	}
}
