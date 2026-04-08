package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
)

type serverScopeKey struct{}

// ContextWithServerScope attaches a server scope allowlist to context.
func ContextWithServerScope(ctx context.Context, scope []string) context.Context {
	return context.WithValue(ctx, serverScopeKey{}, scope)
}

// ServerScopeFromContext retrieves the server scope allowlist from context.
// Returns nil when no scope was set (gateway-wide access).
func ServerScopeFromContext(ctx context.Context) []string {
	scope, _ := ctx.Value(serverScopeKey{}).([]string)
	return scope
}

// ServerScopeMiddleware enforces server-level access restrictions for API keys.
// It reads server scope from context (set during authentication) and checks it
// against the target server. For per-server endpoints the server name comes from
// the chi URL parameter {serverName}. For the unified /mcp endpoint the scope is
// stored in context for deferred post-resolution checking.
//
// An empty scope grants gateway-wide access.
func ServerScopeMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope := ServerScopeFromContext(r.Context())

			// Empty scope = gateway-wide access, pass through.
			if len(scope) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Per-server endpoint: extract {serverName} and check immediately.
			serverName := strings.TrimSpace(chi.URLParam(r, "serverName"))
			if serverName != "" {
				if !serverInScope(serverName, scope) {
					logger.WarnContext(r.Context(), "server scope denied",
						slog.String("server", serverName),
						slog.Any("scope", scope),
					)
					writeScopeDenied(w)
					return
				}
			}

			// For unified endpoint (no serverName param), scope stays in context
			// for post-resolution checking in the proxy router.
			next.ServeHTTP(w, r)
		})
	}
}

// CheckServerScope validates a resolved server name against the scope stored
// in context. Returns true when access is allowed (empty scope = gateway-wide).
func CheckServerScope(ctx context.Context, serverName string) bool {
	scope := ServerScopeFromContext(ctx)
	if len(scope) == 0 {
		return true
	}
	return serverInScope(serverName, scope)
}

// serverInScope returns true if the given server name matches any entry in the
// scope list. Matching is case-insensitive and also tries stripping the common
// "mcp-" prefix from the server name.
func serverInScope(serverName string, scope []string) bool {
	name := strings.TrimSpace(serverName)
	if name == "" {
		return false
	}
	stripped := strings.TrimPrefix(name, "mcp-")
	for _, allowed := range scope {
		a := strings.TrimSpace(allowed)
		if strings.EqualFold(name, a) || strings.EqualFold(stripped, a) {
			return true
		}
	}
	return false
}

func writeScopeDenied(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Denied-Reason", "server_scope")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "forbidden",
		"reason": "server_scope",
	})
}
