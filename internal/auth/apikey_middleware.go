package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/store"
)

// APIKeyMiddleware authenticates requests using gateway API keys.
func APIKeyMiddleware(store store.APIKeyStore, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := extractBearerAPIKey(r.Header.Get("Authorization"))
			if !ok {
				writeAuthJSONError(w, http.StatusUnauthorized, "missing or invalid api key")
				return
			}

			lookupHash := LookupHashAPIKey(key)
			record, err := store.GetByLookupHash(r.Context(), lookupHash)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					writeAuthJSONError(w, http.StatusUnauthorized, "invalid api key")
					return
				}
				logger.ErrorContext(r.Context(), "api key lookup failed", slog.String("error", err.Error()))
				writeAuthJSONError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			if record == nil {
				writeAuthJSONError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			if record.KeyLookupHash != "" && record.KeyLookupHash != lookupHash {
				writeAuthJSONError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			if !VerifyAPIKey(key, record.KeyBcryptHash) {
				writeAuthJSONError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			if record.ExpiresAt != nil && record.ExpiresAt.Before(time.Now().UTC()) {
				writeAuthJSONError(w, http.StatusForbidden, "api key is expired")
				return
			}

			id := &identity.Identity{
				Type:   identity.IdentityAPIKey,
				ID:     record.ClientID,
				Scopes: append([]string(nil), record.Scopes...),
				Metadata: map[string]string{
					"auth_method": "apikey",
				},
			}

			next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), id)))
		})
	}
}

func extractBearerAPIKey(header string) (string, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" || !strings.HasPrefix(token, APIKeyPrefix) {
		return "", false
	}
	return token, true
}

func writeAuthJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
