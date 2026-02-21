package admin

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const sessionContextKey contextKey = "admin_session"

// SessionFromContext returns the admin session from the request context.
func SessionFromContext(ctx context.Context) (*AdminSession, bool) {
	session, ok := ctx.Value(sessionContextKey).(*AdminSession)
	return session, ok
}

// AdminAuthMiddleware checks for a valid admin session cookie.
// When devMode is true, authentication is bypassed with a synthetic session.
func AdminAuthMiddleware(auth *AdminAuth, devMode bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if devMode {
				session := &AdminSession{
					Subject:   "dev-user",
					Email:     "dev@localhost",
					Scopes:    []string{"admin"},
					CSRFToken: "dev-csrf-token",
					ExpiresAt: time.Now().Add(24 * time.Hour),
				}
				ctx := context.WithValue(r.Context(), sessionContextKey, session)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

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

// SecurityHeadersMiddleware adds standard security headers to all responses.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
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

		if strings.TrimSpace(csrfToken) == "" ||
			subtle.ConstantTimeCompare([]byte(csrfToken), []byte(session.CSRFToken)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
