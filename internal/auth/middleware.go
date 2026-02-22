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

const logKeyAuthType = "auth.type"

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
		d := &authDispatcher{
			opts:          opts,
			next:          next,
			apiKeyHandler: APIKeyMiddleware(opts.APIKeyStore, logger)(next),
			oidcHandler:   OIDCMiddleware(opts.OIDCValidator, logger)(next),
		}
		return http.HandlerFunc(d.serveHTTP)
	}
}

type authDispatcher struct {
	opts          AuthOptions
	next          http.Handler
	apiKeyHandler http.Handler
	oidcHandler   http.Handler
}

func (d *authDispatcher) serveHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("mcpgw/auth").Start(r.Context(), "auth.authenticate")
	defer span.End()
	r = r.WithContext(ctx)

	if _, ok := identity.FromContext(r.Context()); ok {
		span.SetAttributes(attribute.String(logKeyAuthType, "mtls"))
		d.next.ServeHTTP(w, r)
		return
	}

	token, ok := extractBearerToken(r.Header.Get("Authorization"))
	if !ok {
		d.handleXAPIKeyFallback(w, r, span)
		return
	}

	d.handleBearerToken(w, r, token, span)
}

func (d *authDispatcher) handleXAPIKeyFallback(w http.ResponseWriter, r *http.Request, span interface{ SetAttributes(...attribute.KeyValue) }) {
	xAPIKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if xAPIKey == "" {
		span.SetAttributes(attribute.String(logKeyAuthType, "missing"))
		writeAuthJSONError(w, http.StatusUnauthorized, "missing authorization header")
		return
	}

	span.SetAttributes(attribute.String(logKeyAuthType, "api_key"))
	if d.opts.APIKeyStore == nil {
		writeAuthJSONError(w, http.StatusUnauthorized, "api key store is not configured")
		return
	}
	cloned := r.Clone(r.Context())
	cloned.Header = r.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+xAPIKey)
	d.apiKeyHandler.ServeHTTP(w, cloned)
}

func (d *authDispatcher) handleBearerToken(w http.ResponseWriter, r *http.Request, token string, span interface{ SetAttributes(...attribute.KeyValue) }) {
	if isJWTToken(token) {
		span.SetAttributes(attribute.String(logKeyAuthType, "oidc"))
		if d.opts.OIDCValidator == nil {
			writeAuthJSONError(w, http.StatusUnauthorized, "oidc validator is not configured")
			return
		}
		d.oidcHandler.ServeHTTP(w, r)
		return
	}

	span.SetAttributes(attribute.String(logKeyAuthType, "api_key"))
	if d.opts.APIKeyStore == nil {
		writeAuthJSONError(w, http.StatusUnauthorized, "api key store is not configured")
		return
	}
	d.apiKeyHandler.ServeHTTP(w, r)
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
