package auth

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// AuthOptions configures unified authentication middleware.
type AuthOptions struct {
	APIKeyStore   store.APIKeyStore `json:"api_key_store"`
	OIDCValidator *OIDCValidator    `json:"oidc_validator"`
	Logger        *slog.Logger      `json:"logger"`
}

// AuthMiddleware routes requests to mTLS, OIDC, or API key authentication paths.
func AuthMiddleware(opts AuthOptions) func(http.Handler) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		apiKeyHandler := APIKeyMiddleware(opts.APIKeyStore, logger)(next)
		oidcHandler := OIDCMiddleware(opts.OIDCValidator, logger)(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := otel.Tracer("mcpgw/auth").Start(r.Context(), "auth.authenticate")
			defer span.End()
			r = r.WithContext(ctx)

			if _, ok := identity.FromContext(r.Context()); ok {
				span.SetAttributes(attribute.String("auth.type", "mtls"))
				next.ServeHTTP(w, r)
				return
			}

			token, ok := extractBearerToken(r.Header.Get("Authorization"))
			if !ok {
				span.SetAttributes(attribute.String("auth.type", "missing"))
				writeAuthJSONError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			if isJWTToken(token) {
				span.SetAttributes(attribute.String("auth.type", "oidc"))
				if opts.OIDCValidator == nil {
					writeAuthJSONError(w, http.StatusUnauthorized, "oidc validator is not configured")
					return
				}
				oidcHandler.ServeHTTP(w, r)
				return
			}

			span.SetAttributes(attribute.String("auth.type", "api_key"))
			if opts.APIKeyStore == nil {
				writeAuthJSONError(w, http.StatusUnauthorized, "api key store is not configured")
				return
			}
			apiKeyHandler.ServeHTTP(w, r)
		})
	}
}

func isJWTToken(token string) bool {
	if strings.Count(token, ".") != 2 {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}
