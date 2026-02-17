package auth

import (
	"net/http"

	"github.com/cruvero/mcp-gateway/internal/identity"
)

// RequireScope enforces that the current identity has a specific scope.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := identity.FromContext(r.Context())
			if !ok || !id.HasScope(scope) {
				writeAuthJSONError(w, http.StatusForbidden, "insufficient scope")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyScope enforces that the current identity has at least one of the given scopes.
func RequireAnyScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(scopes) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			id, ok := identity.FromContext(r.Context())
			if !ok {
				writeAuthJSONError(w, http.StatusForbidden, "insufficient scope")
				return
			}

			for _, scope := range scopes {
				if id.HasScope(scope) {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeAuthJSONError(w, http.StatusForbidden, "insufficient scope")
		})
	}
}
