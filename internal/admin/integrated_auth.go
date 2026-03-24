package admin

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

// Integrated admin delegated auth contract:
//
// Required request headers:
//   - Authorization: Bearer <platform service token>
//   - X-Cruvero-Subject
//   - X-Cruvero-Email
//   - X-Cruvero-Tenant-ID
//   - X-Cruvero-Role
//   - X-Cruvero-Gateway-Role (viewer|editor|admin)
//
// Service token validation method:
//   - Constant-time exact comparison against MCPGW_PLATFORM_SERVICE_TOKEN.
//   - No DB lookup and no JWT/OIDC verification for this token.
//
// Integrated mode rejects local admin cookie/session auth. Browser requests must
// be delegated by Cruvero Platform through the header contract above.

const (
	headerCruveroSubject     = "X-Cruvero-Subject"
	headerCruveroEmail       = "X-Cruvero-Email"
	headerCruveroTenantID    = "X-Cruvero-Tenant-ID"
	headerCruveroRole        = "X-Cruvero-Role"
	headerCruveroGatewayRole = "X-Cruvero-Gateway-Role"
)

type integratedIdentityContextKey string

const delegatedIdentityContextKey integratedIdentityContextKey = "integrated_admin_identity"

// IntegratedDelegatedIdentity captures delegated Platform caller context.
type IntegratedDelegatedIdentity struct {
	Subject     string
	Email       string
	TenantID    string
	Role        string
	GatewayRole string
}

func integratedIdentityFromContext(ctx context.Context) (*IntegratedDelegatedIdentity, bool) {
	if ctx == nil {
		return nil, false
	}
	identity, ok := ctx.Value(delegatedIdentityContextKey).(*IntegratedDelegatedIdentity)
	return identity, ok
}

// IntegratedDelegatedAuthMiddleware enforces delegated auth headers for
// /admin/api/v1/* routes in integrated mode.
func IntegratedDelegatedAuthMiddleware(platformServiceToken string) func(http.Handler) http.Handler {
	expectedToken := strings.TrimSpace(platformServiceToken)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(sessionCookieName); err == nil {
				writeIntegratedError(w, http.StatusForbidden, errCodeInvalidToken, "local admin cookie authentication is disabled in integrated mode")
				return
			}

			token, ok := extractBearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeIntegratedError(w, http.StatusUnauthorized, errCodeInvalidToken, "missing or invalid Authorization bearer token")
				return
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
				writeIntegratedError(w, http.StatusUnauthorized, errCodeInvalidToken, "invalid platform service token")
				return
			}

			subject := strings.TrimSpace(r.Header.Get(headerCruveroSubject))
			email := strings.TrimSpace(r.Header.Get(headerCruveroEmail))
			tenantID := strings.TrimSpace(r.Header.Get(headerCruveroTenantID))
			role := strings.TrimSpace(r.Header.Get(headerCruveroRole))
			gatewayRole := strings.ToLower(strings.TrimSpace(r.Header.Get(headerCruveroGatewayRole)))

			missingHeader := ""
			switch {
			case subject == "":
				missingHeader = headerCruveroSubject
			case email == "":
				missingHeader = headerCruveroEmail
			case tenantID == "":
				missingHeader = headerCruveroTenantID
			case role == "":
				missingHeader = headerCruveroRole
			case gatewayRole == "":
				missingHeader = headerCruveroGatewayRole
			}
			if missingHeader != "" {
				writeIntegratedError(w, http.StatusBadRequest, errCodeMissingHeader, "missing required header: "+missingHeader)
				return
			}

			if !isValidGatewayRole(gatewayRole) {
				writeIntegratedError(w, http.StatusForbidden, errCodeInvalidGatewayRole, "invalid X-Cruvero-Gateway-Role")
				return
			}

			identity := &IntegratedDelegatedIdentity{
				Subject:     subject,
				Email:       email,
				TenantID:    tenantID,
				Role:        role,
				GatewayRole: gatewayRole,
			}
			ctx := context.WithValue(r.Context(), delegatedIdentityContextKey, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearerToken(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if token == "" {
		return "", false
	}
	return token, true
}
