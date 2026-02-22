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

	"github.com/cruvero/mcp-gateway/internal/identity"
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

// RateLimitMiddleware enforces per-(client,route) rate limits via the pluggable backend.
func RateLimitMiddleware(
	backend LimiterBackend,
	resolver ProfileResolver,
	logger *slog.Logger,
) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	rl := &rateLimitEnforcer{backend: backend, resolver: resolver, logger: logger}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rl.serve(w, r, next)
		})
	}
}

// rateLimitEnforcer holds rate limit evaluation state for the middleware.
type rateLimitEnforcer struct {
	backend  LimiterBackend
	resolver ProfileResolver
	logger   *slog.Logger
}

func (rl *rateLimitEnforcer) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	ctx, span := otel.Tracer("mcpgw/ratelimit").Start(r.Context(), "ratelimit.check")
	defer span.End()
	r = r.WithContext(ctx)

	if rl.backend == nil {
		next.ServeHTTP(w, r)
		return
	}

	clientID := resolveClientID(r)
	limit, burst := rl.resolveProfile(r)

	key := LimiterKey{
		ClientID: clientID,
		Route:    resolveRouteKey(r),
	}
	span.SetAttributes(
		attribute.String("client.id", clientID),
		attribute.String("route", key.Route),
	)

	allowed, remaining, retryAfter, err := rl.backend.Allow(ctx, key, limit, burst)
	if err != nil {
		rl.logger.ErrorContext(ctx, "rate limit backend error", slog.String("error", err.Error()))
		next.ServeHTTP(w, r)
		return
	}

	setBackendHeaders(w.Header(), limit, remaining)

	if allowed {
		next.ServeHTTP(w, r)
		return
	}
	span.SetAttributes(attribute.Bool("rate_limited", true))

	rl.writeLimitedResponse(w, r, retryAfter)
	notifyRateLimited(clientID, key.Route)
}

// resolveClientID extracts the client identity from the request context.
func resolveClientID(r *http.Request) string {
	id, ok := identity.FromContext(r.Context())
	if ok && strings.TrimSpace(id.ID) != "" {
		return strings.TrimSpace(id.ID)
	}
	return "anonymous"
}

// resolveProfile resolves rate limit and burst from the identity's policy profile.
func (rl *rateLimitEnforcer) resolveProfile(r *http.Request) (float64, int) {
	if rl.resolver == nil {
		return 0, 0
	}
	id, _ := identity.FromContext(r.Context())
	profile := rl.resolver.Resolve(id)
	if profile == nil {
		return 0, 0
	}
	var limit float64
	var burst int
	if profile.RateLimit > 0 {
		limit = float64(profile.RateLimit)
	}
	if profile.RateBurst > 0 {
		burst = profile.RateBurst
	}
	return limit, burst
}

// writeLimitedResponse writes the 429 Too Many Requests response.
func (rl *rateLimitEnforcer) writeLimitedResponse(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	retryAfterSecs := int(math.Ceil(retryAfter.Seconds()))
	if retryAfterSecs <= 0 {
		retryAfterSecs = 1
	}
	w.Header().Set(headerRetryAfter, strconv.Itoa(retryAfterSecs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	payload := map[string]any{
		"error":       "rate limit exceeded",
		"retry_after": retryAfterSecs,
	}
	if encErr := json.NewEncoder(w).Encode(payload); encErr != nil {
		rl.logger.ErrorContext(r.Context(), "write ratelimit response failed", slog.String("error", encErr.Error()))
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

func setBackendHeaders(headers http.Header, limit float64, remaining int) {
	if headers == nil {
		return
	}

	limitPerSecond := limit
	if limitPerSecond <= 0 {
		limitPerSecond = defaultRateLimit
	}

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
