package auth

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// OIDCMiddleware authenticates requests using OIDC bearer tokens.
func OIDCMiddleware(validator *OIDCValidator, logger *slog.Logger) func(http.Handler) http.Handler {
	return OIDCMiddlewareWithUserStore(validator, nil, nil, logger)
}

// OIDCMiddlewareWithUserStore authenticates OIDC tokens and auto-registers users.
func OIDCMiddlewareWithUserStore(validator *OIDCValidator, userStore store.UserStore, auditStore store.AuditStore, logger *slog.Logger) func(http.Handler) http.Handler {
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

			if userStore != nil {
				go autoRegisterOIDCUser(context.WithoutCancel(r.Context()), id, userStore, auditStore, logger)
			}

			next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), id)))
		})
	}
}

const autoRegisterTimeout = 5 * time.Second

func autoRegisterOIDCUser(parentCtx context.Context, id *identity.Identity, userStore store.UserStore, auditStore store.AuditStore, logger *slog.Logger) {
	if parentCtx == nil || id == nil || userStore == nil {
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, autoRegisterTimeout)
	defer cancel()

	user := &types.User{
		ID:          id.ID,
		OIDCSub:     id.ID,
		Email:       id.Metadata["email"],
		DisplayName: id.Metadata["display_name"],
		Role:        types.RoleUser,
	}

	if err := userStore.Upsert(ctx, user); err != nil {
		logger.Warn("auto-register oidc user failed",
			slog.String("oidc_sub", id.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	if auditStore != nil {
		_ = auditStore.Log(ctx, &types.AuditEntry{
			EventType: "user.registered",
			ClientID:  id.ID,
			Details: map[string]any{
				"oidc_sub": id.ID,
				"email":    id.Metadata["email"],
			},
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
