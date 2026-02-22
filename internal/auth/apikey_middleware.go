package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

const errInvalidAPIKey = "invalid api key"

// APIKeyMiddleware authenticates requests using gateway API keys.
func APIKeyMiddleware(store store.APIKeyStore, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	v := &apiKeyValidator{store: store, logger: logger}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, statusCode, errMsg := v.validate(r)
			if id == nil {
				writeAuthJSONError(w, statusCode, errMsg)
				return
			}
			next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), id)))
		})
	}
}

type apiKeyValidator struct {
	store  store.APIKeyStore
	logger *slog.Logger
}

func (v *apiKeyValidator) validate(r *http.Request) (*identity.Identity, int, string) {
	key, ok := extractAPIKey(r.Header.Get("Authorization"), r.Header.Get("X-API-Key"))
	if !ok {
		return nil, http.StatusUnauthorized, "missing or invalid api key"
	}

	record, err := v.lookupRecord(r, key)
	if err != nil {
		return nil, http.StatusUnauthorized, err.Error()
	}

	if statusCode, msg, ok := v.verifyRecord(key, record); !ok {
		return nil, statusCode, msg
	}

	policyProfile := record.PolicyProfile
	if policyProfile == "" {
		policyProfile = "default"
	}

	id := &identity.Identity{
		Type:   identity.IdentityAPIKey,
		ID:     record.ClientID,
		Scopes: append([]string(nil), record.Scopes...),
		Metadata: map[string]string{
			"auth_method":    "apikey",
			"policy_profile": policyProfile,
		},
	}
	return id, 0, ""
}

func (v *apiKeyValidator) lookupRecord(r *http.Request, key string) (*types.APIKey, error) {
	lookupHash := LookupHashAPIKey(key)
	record, err := v.store.GetByLookupHash(r.Context(), lookupHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf(errInvalidAPIKey)
		}
		v.logger.ErrorContext(r.Context(), "api key lookup failed", slog.String("error", err.Error()))
		return nil, fmt.Errorf(errInvalidAPIKey)
	}
	if record == nil {
		return nil, fmt.Errorf(errInvalidAPIKey)
	}
	return record, nil
}

func (v *apiKeyValidator) verifyRecord(key string, record *types.APIKey) (int, string, bool) {
	lookupHash := LookupHashAPIKey(key)
	if record.KeyLookupHash != "" && record.KeyLookupHash != lookupHash {
		return http.StatusUnauthorized, errInvalidAPIKey, false
	}
	if !VerifyAPIKey(key, record.KeyBcryptHash) {
		return http.StatusUnauthorized, errInvalidAPIKey, false
	}
	if record.ExpiresAt != nil && record.ExpiresAt.Before(time.Now().UTC()) {
		return http.StatusForbidden, "api key is expired", false
	}
	return 0, "", true
}

func extractAPIKey(authHeader, xAPIKey string) (string, bool) {
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if token != "" && strings.HasPrefix(token, APIKeyPrefix) {
			return token, true
		}
	}
	xAPIKey = strings.TrimSpace(xAPIKey)
	if xAPIKey == "" || !strings.HasPrefix(xAPIKey, APIKeyPrefix) {
		return "", false
	}
	return xAPIKey, true
}

func writeAuthJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
