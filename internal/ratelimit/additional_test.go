package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestSetRateLimitedObserverInvoked(t *testing.T) {
	t.Parallel()

	defer SetRateLimitedObserver(nil)

	var gotClient string
	var gotRoute string
	SetRateLimitedObserver(func(clientID string, route string) {
		gotClient = clientID
		gotRoute = route
	})

	notifyRateLimited("client-1", "/mcp")
	if gotClient != "client-1" || gotRoute != "/mcp" {
		t.Fatalf("expected observer callback values, got client=%q route=%q", gotClient, gotRoute)
	}
}

func TestResolveRouteKeyUsesChiPattern(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/v1/registrations/abc", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.RoutePatterns = []string{"/v1/registrations/{id}"}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	req = req.WithContext(ctx)

	if got := resolveRouteKey(req); got != "/v1/registrations/{id}" {
		t.Fatalf("expected chi route pattern fallback, got %q", got)
	}
}

func TestReadMCPMethodGuardPaths(t *testing.T) {
	t.Parallel()

	if got := readMCPMethod(nil); got != "" {
		t.Fatalf("expected empty method for nil request, got %q", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Content-Type", "application/json")
	if got := readMCPMethod(req); got != "" {
		t.Fatalf("expected empty method for non-POST request, got %q", got)
	}
}

func TestSetRateLimitHeadersNilInputs(t *testing.T) {
	t.Parallel()

	setRateLimitHeaders(nil, nil)
	setRateLimitHeaders(http.Header{}, nil)
}

func TestLimiterStoreSetDefaults(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(1, 1)
	store.SetDefaults(25, 30)
	limiter := store.GetOrCreate(LimiterKey{ClientID: "a", Route: "/mcp"}, nil)
	if float64(limiter.Limit()) != 25 {
		t.Fatalf("expected updated default rate 25, got %v", limiter.Limit())
	}
	if limiter.Burst() != 30 {
		t.Fatalf("expected updated default burst 30, got %d", limiter.Burst())
	}

	store.SetDefaults(0, 0)
	limiter2 := store.GetOrCreate(LimiterKey{ClientID: "b", Route: "/mcp"}, nil)
	if float64(limiter2.Limit()) != defaultRateLimit {
		t.Fatalf("expected fallback default rate %v, got %v", defaultRateLimit, limiter2.Limit())
	}
	if limiter2.Burst() != defaultRateBurst {
		t.Fatalf("expected fallback default burst %d, got %d", defaultRateBurst, limiter2.Burst())
	}
}
