//go:build load

package testutil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/resilience"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestSustainedThroughput(t *testing.T) {
	store := ratelimit.NewLimiterStore(100, 100)
	resolver := ratelimit.NewDefaultProfileResolver(map[string]*types.PolicyProfile{
		"default": {Name: "default", RateLimit: 100, RateBurst: 100},
	}, &types.PolicyProfile{Name: "default", RateLimit: 100, RateBurst: 100})

	handler := ratelimit.RateLimitMiddleware(store, resolver, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var success int64
	var limited int64
	var wg sync.WaitGroup

	duration := 10 * time.Second
	end := time.Now().Add(duration)
	for time.Now().Before(end) {
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
				req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Type: identity.IdentityAPIKey, ID: "load-client", Metadata: map[string]string{"policy_profile": "default"}}))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code == http.StatusOK {
					atomic.AddInt64(&success, 1)
				} else if rec.Code == http.StatusTooManyRequests {
					atomic.AddInt64(&limited, 1)
				}
			}()
		}
		time.Sleep(10 * time.Millisecond)
	}
	wg.Wait()

	expected := float64(100*10 + 100)
	tolerance := expected * 0.10
	if float64(success) < expected-tolerance || float64(success) > expected+tolerance {
		t.Fatalf("expected successes within %.0f +/- %.0f, got %d", expected, tolerance, success)
	}
	if limited == 0 {
		t.Fatal("expected some 429 responses under sustained overload")
	}
}

func TestBurst(t *testing.T) {
	store := ratelimit.NewLimiterStore(10, 20)
	resolver := ratelimit.NewDefaultProfileResolver(map[string]*types.PolicyProfile{
		"default": {Name: "default", RateLimit: 10, RateBurst: 20},
	}, &types.PolicyProfile{Name: "default", RateLimit: 10, RateBurst: 20})

	handler := ratelimit.RateLimitMiddleware(store, resolver, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var success int64
	var limited int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Type: identity.IdentityAPIKey, ID: "burst-client", Metadata: map[string]string{"policy_profile": "default"}}))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				atomic.AddInt64(&success, 1)
			} else if rec.Code == http.StatusTooManyRequests {
				atomic.AddInt64(&limited, 1)
			}
		}()
	}
	wg.Wait()

	if success < 18 || success > 22 {
		t.Fatalf("expected around 20 successful burst requests, got %d", success)
	}
	if limited == 0 {
		t.Fatal("expected burst overflow requests to be rate limited")
	}
}

func TestCircuitBreakerUnderLoad(t *testing.T) {
	breaker := resilience.NewCircuitBreaker("load-backend", 3, 200*time.Millisecond)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		err := breaker.Execute(ctx, func() error { return errors.New("backend unavailable") })
		if err == nil {
			t.Fatal("expected backend failure while opening circuit")
		}
	}

	if breaker.State() != resilience.StateOpen {
		t.Fatalf("expected open state after threshold failures, got %s", breaker.State())
	}

	err := breaker.Execute(ctx, func() error { return nil })
	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("expected fast-fail ErrCircuitOpen, got %v", err)
	}

	time.Sleep(250 * time.Millisecond)
	err = breaker.Execute(ctx, func() error { return nil })
	if err != nil {
		t.Fatalf("expected successful probe after timeout, got %v", err)
	}
	if breaker.State() != resilience.StateClosed {
		t.Fatalf("expected closed state after successful recovery, got %s", breaker.State())
	}
}
