package ratelimit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestRateLimitMiddlewareAllowsWithinLimit(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	handler := RateLimitMiddleware(store, nil, testRateLimitLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{ID: "client-a"}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if rec.Header().Get(headerRateLimitLimit) == "" {
		t.Fatal("expected X-RateLimit-Limit header")
	}
	if rec.Header().Get(headerRateLimitRemaining) == "" {
		t.Fatal("expected X-RateLimit-Remaining header")
	}
	if rec.Header().Get(headerRateLimitReset) == "" {
		t.Fatal("expected X-RateLimit-Reset header")
	}
}

func TestRateLimitMiddlewareReturns429WhenExceeded(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(1, 1)
	handler := RateLimitMiddleware(store, nil, testRateLimitLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ctx := identity.WithIdentity(context.Background(), &identity.Identity{ID: "client-a"})
	firstReq := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", firstRec.Code)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", secondRec.Code)
	}

	if secondRec.Header().Get(headerRateLimitLimit) == "" {
		t.Fatal("expected X-RateLimit-Limit header")
	}
	if secondRec.Header().Get(headerRateLimitRemaining) == "" {
		t.Fatal("expected X-RateLimit-Remaining header")
	}
	if secondRec.Header().Get(headerRetryAfter) == "" {
		t.Fatal("expected Retry-After header")
	}

	var payload map[string]any
	if err := json.Unmarshal(secondRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["error"] != "rate limit exceeded" {
		t.Fatalf("expected rate limit exceeded error, got %#v", payload)
	}
}

func TestRateLimitMiddlewareUsesResolvedProfile(t *testing.T) {
	t.Parallel()

	defaultProfile := &types.PolicyProfile{Name: "default", RateLimit: 10, RateBurst: 20}
	premiumProfile := &types.PolicyProfile{Name: "premium", RateLimit: 50, RateBurst: 100}
	resolver := NewDefaultProfileResolver(map[string]*types.PolicyProfile{
		"default": defaultProfile,
		"premium": premiumProfile,
	}, defaultProfile)

	store := NewLimiterStore(1, 1)
	handler := RateLimitMiddleware(store, resolver, testRateLimitLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{
		ID: "client-premium",
		Metadata: map[string]string{
			"policy_profile": "premium",
		},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get(headerRateLimitLimit) != "50" {
		t.Fatalf("expected profile rate header 50, got %q", rec.Header().Get(headerRateLimitLimit))
	}
}

func TestResolveRouteKeyUsesMCPMethodWhenPresent(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call"}`))
	req.Header.Set("Content-Type", "application/json")
	key := resolveRouteKey(req)
	if key != "mcp/tools/call" {
		t.Fatalf("expected mcp/tools/call route key, got %q", key)
	}
}

func testRateLimitLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
