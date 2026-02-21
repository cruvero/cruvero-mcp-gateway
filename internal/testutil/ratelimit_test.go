package testutil

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestConcurrentRateLimiting(t *testing.T) {
	t.Parallel()

	ratePerSecond := 50
	burst := 50
	backend := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer backend.Close()
	backend.SetDefaults(float64(ratePerSecond), burst)
	resolver := ratelimit.NewDefaultProfileResolver(map[string]*types.PolicyProfile{
		"default": {Name: "default", RateLimit: ratePerSecond, RateBurst: burst},
	}, &types.PolicyProfile{Name: "default", RateLimit: ratePerSecond, RateBurst: burst})

	handler := ratelimit.RateLimitMiddleware(backend, resolver, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	start := time.Now()
	var success int64
	var limited int64
	var wg sync.WaitGroup
	for worker := 0; worker < 10; worker++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
				req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Type: identity.IdentityAPIKey, ID: "client-shared", Metadata: map[string]string{"policy_profile": "default"}}))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				switch rec.Code {
				case http.StatusOK:
					atomic.AddInt64(&success, 1)
				case http.StatusTooManyRequests:
					atomic.AddInt64(&limited, 1)
				}
			}
		}(worker)
	}
	wg.Wait()

	duration := time.Since(start).Seconds()
	maxExpectedSuccess := int(float64(ratePerSecond)*duration*1.2) + burst
	if int(success) > maxExpectedSuccess {
		t.Fatalf("expected successes <= %d, got %d", maxExpectedSuccess, success)
	}
	if limited == 0 {
		t.Fatal("expected some rate-limited responses under concurrency")
	}
}

func TestPerClientIsolation(t *testing.T) {
	t.Parallel()

	backend := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer backend.Close()
	backend.SetDefaults(1, 1)
	resolver := ratelimit.NewDefaultProfileResolver(map[string]*types.PolicyProfile{
		"default": {Name: "default", RateLimit: 1, RateBurst: 1},
	}, &types.PolicyProfile{Name: "default", RateLimit: 1, RateBurst: 1})

	handler := ratelimit.RateLimitMiddleware(backend, resolver, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := func(clientID string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req = req.WithContext(identity.WithIdentity(req.Context(), &identity.Identity{Type: identity.IdentityAPIKey, ID: clientID, Metadata: map[string]string{"policy_profile": "default"}}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := request("client-a"); code != http.StatusOK {
		t.Fatalf("expected first request for client-a to pass, got %d", code)
	}
	if code := request("client-b"); code != http.StatusOK {
		t.Fatalf("expected first request for client-b to pass, got %d", code)
	}

	if code := request("client-a"); code != http.StatusTooManyRequests {
		t.Fatalf("expected second immediate request for client-a to be limited, got %d", code)
	}
	if code := request("client-b"); code != http.StatusTooManyRequests {
		t.Fatalf("expected second immediate request for client-b to be limited, got %d", code)
	}
}
