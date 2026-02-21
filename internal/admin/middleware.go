package admin

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const sessionContextKey contextKey = "admin_session"

// SessionFromContext returns the admin session from the request context.
func SessionFromContext(ctx context.Context) (*AdminSession, bool) {
	session, ok := ctx.Value(sessionContextKey).(*AdminSession)
	return session, ok
}

// AdminAuthMiddleware checks for a valid admin session cookie.
func AdminAuthMiddleware(auth *AdminAuth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			session, err := auth.DecryptSession(cookie.Value)
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFMiddleware validates CSRF tokens on state-changing requests.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		session, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "no session", http.StatusForbidden)
			return
		}

		csrfToken := r.FormValue("_csrf")
		if csrfToken == "" {
			csrfToken = r.Header.Get("X-CSRF-Token")
		}

		if strings.TrimSpace(csrfToken) == "" || csrfToken != session.CSRFToken {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
