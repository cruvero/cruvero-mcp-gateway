package ratelimit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
	"golang.org/x/time/rate"
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

func TestResolveRouteKeyFallsBackToPath(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/v1/tools", nil)
	if got := resolveRouteKey(req); got != "/v1/tools" {
		t.Fatalf("expected path fallback, got %q", got)
	}
}

func TestReadMCPMethodNonJSONOrInvalidReturnsEmpty(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	if got := readMCPMethod(req); got != "" {
		t.Fatalf("expected empty method for invalid json, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"method":"tools/list"}`))
	req2.Header.Set("Content-Type", "text/plain")
	if got := readMCPMethod(req2); got != "" {
		t.Fatalf("expected empty method for non-json content type, got %q", got)
	}
}

func TestRateLimitHelpers(t *testing.T) {
	t.Parallel()

	limiter := rate.NewLimiter(rate.Limit(2), 1)
	headers := make(http.Header)
	setRateLimitHeaders(headers, limiter)
	if headers.Get(headerRateLimitLimit) != "2" {
		t.Fatalf("expected limit header 2, got %q", headers.Get(headerRateLimitLimit))
	}
	if _, err := strconv.Atoi(headers.Get(headerRateLimitRemaining)); err != nil {
		t.Fatalf("expected numeric remaining header, got %q", headers.Get(headerRateLimitRemaining))
	}
	if _, err := strconv.ParseInt(headers.Get(headerRateLimitReset), 10, 64); err != nil {
		t.Fatalf("expected unix reset header, got %q", headers.Get(headerRateLimitReset))
	}

	if retryAfter := retryAfterSeconds(nil); retryAfter != 1 {
		t.Fatalf("expected nil limiter retry-after 1, got %d", retryAfter)
	}
	if retryAfter := retryAfterSeconds(rate.NewLimiter(0, 1)); retryAfter != 1 {
		t.Fatalf("expected zero-limit retry-after 1, got %d", retryAfter)
	}
}

func testRateLimitLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
