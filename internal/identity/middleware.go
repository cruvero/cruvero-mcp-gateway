package identity

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
)

// MTLSMiddleware authenticates requests using mTLS client certificate identity.
func MTLSMiddleware(allowedPrefixes []string, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 {
				logger.WarnContext(r.Context(), "mtls authentication failed", slog.String("reason", "missing verified tls client certificate"))
				writeAuthError(w, http.StatusUnauthorized, "missing or invalid mTLS client certificate")
				return
			}

			spiffeID, err := ExtractSPIFFEID(r.TLS.VerifiedChains[0])
			if err != nil {
				logger.WarnContext(r.Context(), "mtls authentication failed", slog.String("reason", err.Error()))
				writeAuthError(w, http.StatusUnauthorized, "invalid mTLS client certificate identity")
				return
			}

			if err := ValidateSPIFFEID(spiffeID, allowedPrefixes); err != nil {
				logger.WarnContext(
					r.Context(),
					"mtls authorization denied",
					slog.String("spiffe_id", spiffeID),
					slog.String("reason", err.Error()),
				)
				writeAuthError(w, http.StatusForbidden, "spiffe identity is not allowed")
				return
			}

			identity := &Identity{
				Type:   IdentityMTLS,
				ID:     spiffeID,
				Scopes: []string{ScopeAdmin},
				Metadata: map[string]string{
					"auth_method": "mtls",
				},
			}

			logger.InfoContext(r.Context(), "mtls authentication succeeded", slog.String("spiffe_id", spiffeID))
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

func writeAuthError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
