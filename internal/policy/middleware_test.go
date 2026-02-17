package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestPolicyMiddlewareAllowedRequestPasses(t *testing.T) {
	t.Parallel()

	engine := NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			EnforcementMode: types.ModeEnforce,
		},
	}, nil, testPolicyLogger())

	called := false
	handler := PolicyMiddleware(engine, testPolicyLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"method":"tools/call",
		"params":{"name":"safe.tool","arguments":{"message":"hello"}}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(identity.WithIdentity(context.Background(), &identity.Identity{
		ID: "client-1",
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected next handler to be called")
	}
	if rec.Header().Get(headerPolicyDecision) != "allowed" {
		t.Fatalf("expected allowed header, got %q", rec.Header().Get(headerPolicyDecision))
	}
}

func TestPolicyMiddlewareDeniedReturns403(t *testing.T) {
	t.Parallel()

	engine := NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			ToolDenylist:    []string{"danger.tool"},
			EnforcementMode: types.ModeEnforce,
		},
	}, nil, testPolicyLogger())

	called := false
	handler := PolicyMiddleware(engine, testPolicyLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"method":"tools/call",
		"params":{"name":"danger.tool","arguments":{"message":"hello"}}
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
	if called {
		t.Fatal("expected next handler not to be called on deny")
	}
	if rec.Header().Get(headerPolicyDecision) != "denied" {
		t.Fatalf("expected denied header, got %q", rec.Header().Get(headerPolicyDecision))
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode denied response: %v", err)
	}
	if payload["error"] != "policy denied" {
		t.Fatalf("expected policy denied error, got %#v", payload)
	}
}

func TestPolicyMiddlewareAuditModePasses(t *testing.T) {
	t.Parallel()

	engine := NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			EnforcementMode: types.ModeEnforce,
		},
		"audit": {
			Name:            "audit",
			ToolDenylist:    []string{"danger.tool"},
			EnforcementMode: types.ModeAudit,
		},
	}, nil, testPolicyLogger())

	called := false
	handler := PolicyMiddleware(engine, testPolicyLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0",
		"method":"tools/call",
		"params":{"name":"danger.tool","arguments":{"message":"hello"}}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(identity.WithIdentity(context.Background(), &identity.Identity{
		ID: "client-audit",
		Metadata: map[string]string{
			"policy_profile": "audit",
		},
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 in audit mode, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected next handler to be called in audit mode")
	}
	if rec.Header().Get(headerPolicyDecision) != "allowed" {
		t.Fatalf("expected allowed header in audit mode, got %q", rec.Header().Get(headerPolicyDecision))
	}
}

func TestPolicyMiddlewareBypassesNonToolCalls(t *testing.T) {
	t.Parallel()

	engine := NewEngine(map[string]*types.PolicyProfile{
		"default": {Name: "default", EnforcementMode: types.ModeEnforce},
	}, nil, testPolicyLogger())

	called := false
	handler := PolicyMiddleware(engine, testPolicyLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected non-tool call to bypass policy and hit next")
	}
}

func TestPolicyMiddlewareNilEnginePassthrough(t *testing.T) {
	t.Parallel()

	called := false
	handler := PolicyMiddleware(nil, testPolicyLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"safe.tool"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected nil engine to pass through")
	}
}
