package admin

import "net/http"

const (
	gatewayRoleViewer = "viewer"
	gatewayRoleEditor = "editor"
	gatewayRoleAdmin  = "admin"
)

func isValidGatewayRole(role string) bool {
	switch role {
	case gatewayRoleViewer, gatewayRoleEditor, gatewayRoleAdmin:
		return true
	default:
		return false
	}
}

func roleRank(role string) int {
	switch role {
	case gatewayRoleAdmin:
		return 3
	case gatewayRoleEditor:
		return 2
	default:
		return 1
	}
}

// RequireGatewayRole enforces minimum delegated gateway role for integrated API routes.
func RequireGatewayRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := integratedIdentityFromContext(r.Context())
			if !ok {
				writeIntegratedError(w, http.StatusUnauthorized, errCodeInvalidToken, "delegated identity is missing")
				return
			}
			if roleRank(identity.GatewayRole) < roleRank(minRole) {
				writeIntegratedError(w, http.StatusForbidden, errCodeInsufficientRole, "insufficient gateway role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
