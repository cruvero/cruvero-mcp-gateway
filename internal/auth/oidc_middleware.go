package auth

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/identity"
)

// OIDCMiddleware authenticates requests using OIDC bearer tokens.
func OIDCMiddleware(validator *OIDCValidator, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawToken, ok := extractBearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeAuthJSONError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			if validator == nil {
				writeAuthJSONError(w, http.StatusUnauthorized, "oidc validator is not configured")
				return
			}

			id, err := validator.Validate(r.Context(), rawToken)
			if err != nil {
				logger.WarnContext(r.Context(), "oidc authentication failed", slog.String("error", err.Error()))
				writeAuthJSONError(w, http.StatusUnauthorized, "invalid oidc token")
				return
			}

			next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), id)))
		})
	}
}

func extractBearerToken(header string) (string, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		return "", false
	}
	return token, true
}
