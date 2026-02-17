package ratelimit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	headerRateLimitLimit     = "X-RateLimit-Limit"
	headerRateLimitRemaining = "X-RateLimit-Remaining"
	headerRateLimitReset     = "X-RateLimit-Reset"
	headerRetryAfter         = "Retry-After"
)

var (
	rateLimitObserverMu sync.RWMutex
	rateLimitObserver   func(clientID string, route string)
)

// SetRateLimitedObserver sets an optional callback invoked when a request is limited.
func SetRateLimitedObserver(observer func(clientID string, route string)) {
	rateLimitObserverMu.Lock()
	defer rateLimitObserverMu.Unlock()
	rateLimitObserver = observer
}

// RateLimitMiddleware enforces per-(client,route) token bucket limits.
func RateLimitMiddleware(
	store *LimiterStore,
	resolver ProfileResolver,
	logger *slog.Logger,
) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := otel.Tracer("mcpgw/ratelimit").Start(r.Context(), "ratelimit.check")
			defer span.End()
			r = r.WithContext(ctx)

			if store == nil {
				next.ServeHTTP(w, r)
				return
			}

			id, ok := identity.FromContext(r.Context())
			clientID := "anonymous"
			if ok && strings.TrimSpace(id.ID) != "" {
				clientID = strings.TrimSpace(id.ID)
			}

			var profile *types.PolicyProfile
			if resolver != nil {
				profile = resolver.Resolve(id)
			}

			key := LimiterKey{
				ClientID: clientID,
				Route:    resolveRouteKey(r),
			}
			span.SetAttributes(
				attribute.String("client.id", clientID),
				attribute.String("route", key.Route),
			)

			limiter := store.GetOrCreate(key, profile)
			allowed := limiter.Allow()
			setRateLimitHeaders(w.Header(), limiter)

			if allowed {
				next.ServeHTTP(w, r)
				return
			}
			span.SetAttributes(attribute.Bool("rate_limited", true))

			retryAfter := retryAfterSeconds(limiter)
			w.Header().Set(headerRetryAfter, strconv.Itoa(retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			payload := map[string]any{
				"error":       "rate limit exceeded",
				"retry_after": retryAfter,
			}
			if err := json.NewEncoder(w).Encode(payload); err != nil {
				logger.ErrorContext(r.Context(), "write ratelimit response failed", slog.String("error", err.Error()))
			}
			notifyRateLimited(clientID, key.Route)
		})
	}
}

func notifyRateLimited(clientID string, route string) {
	rateLimitObserverMu.RLock()
	observer := rateLimitObserver
	rateLimitObserverMu.RUnlock()
	if observer != nil {
		observer(clientID, route)
	}
}

func resolveRouteKey(r *http.Request) string {
	if method := readMCPMethod(r); method != "" {
		return "mcp/" + method
	}

	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		pattern := strings.TrimSpace(routeContext.RoutePattern())
		if pattern != "" {
			return pattern
		}
	}

	path := strings.TrimSpace(r.URL.Path)
	if path != "" {
		return path
	}
	return "unknown"
}

func readMCPMethod(r *http.Request) string {
	if r == nil || r.Body == nil {
		return ""
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return ""
	}
	if r.Method != http.MethodPost {
		return ""
	}

	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil || len(body) == 0 {
		return ""
	}

	var envelope struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}

	return strings.TrimSpace(envelope.Method)
}

func setRateLimitHeaders(headers http.Header, limiter *rate.Limiter) {
	if headers == nil || limiter == nil {
		return
	}

	limitPerSecond := float64(limiter.Limit())
	if limitPerSecond <= 0 {
		limitPerSecond = 1
	}

	remaining := int(math.Floor(limiter.Tokens()))
	if remaining < 0 {
		remaining = 0
	}

	resetAt := time.Now().UTC()
	if remaining < 1 {
		resetAt = resetAt.Add(time.Duration(float64(time.Second) / limitPerSecond))
	}

	headers.Set(headerRateLimitLimit, fmt.Sprintf("%.0f", limitPerSecond))
	headers.Set(headerRateLimitRemaining, strconv.Itoa(remaining))
	headers.Set(headerRateLimitReset, strconv.FormatInt(resetAt.Unix(), 10))
}

func retryAfterSeconds(limiter *rate.Limiter) int {
	if limiter == nil {
		return 1
	}

	limitPerSecond := float64(limiter.Limit())
	if limitPerSecond <= 0 {
		return 1
	}

	needed := 1 - limiter.Tokens()
	if needed <= 0 {
		return 1
	}

	seconds := int(math.Ceil(needed / limitPerSecond))
	if seconds <= 0 {
		return 1
	}
	return seconds
}
