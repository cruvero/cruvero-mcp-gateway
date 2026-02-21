# Phase 2B: API Keys, OIDC, Auth Middleware

## Overview

Implement API key authentication for CLI users, OIDC token validation for enterprise users, and a unified auth middleware that routes to the correct authentication method based on request characteristics.

## Scope

### API Key Management (`internal/auth/apikey.go`)
- `GenerateAPIKey() (plaintext string, lookupHash string, bcryptHash string, err error)` — crypto/rand + SHA-256 + bcrypt
- `LookupHashAPIKey(plaintext string) string` — deterministic SHA-256 for DB lookup
- `HashAPIKey(plaintext string) (string, error)` — bcrypt hash
- `VerifyAPIKey(plaintext, hash string) bool` — bcrypt compare
- Key format: `mcpgw_` prefix + 32 random bytes base64-encoded

### API Key Middleware (`internal/auth/apikey_middleware.go`)
- Extract Bearer token from Authorization header
- Compute deterministic lookup hash from plaintext key
- Look up in APIKeyStore by lookup hash
- Verify plaintext against bcrypt hash from store
- Check expiration and scopes
- Inject Identity with Type=IdentityAPIKey into context
- Return 401 if invalid/expired, 403 if insufficient scope

### OIDC Validation (`internal/auth/oidc.go`)
- `OIDCValidator` struct wrapping go-oidc provider and verifier
- `NewOIDCValidator(issuerURL, audience string) (*OIDCValidator, error)`
- `Validate(ctx context.Context, rawToken string) (*Identity, error)` — verify token, extract claims
- Map OIDC claims to Identity (sub -> ID, groups -> Scopes)

### OIDC Middleware (`internal/auth/oidc_middleware.go`)
- Extract Bearer token from Authorization header
- Validate via OIDCValidator
- Inject Identity with Type=IdentityOIDC into context
- Return 401 if invalid token

### Unified Auth Middleware (`internal/auth/middleware.go`)
- `AuthMiddleware(opts AuthOptions) func(http.Handler) http.Handler`
- AuthOptions: APIKeyStore, OIDCValidator (optional), Logger
- Routing logic:
  - If request has TLS client cert -> skip (handled by mTLS middleware upstream)
  - If Authorization header starts with "Bearer mcpgw_" -> API key auth
  - If Authorization header starts with "Bearer " (other) -> OIDC auth
  - If no Authorization header -> 401
- Compose with mTLS middleware in the server's middleware chain

### RBAC (`internal/auth/rbac.go`)
- `RequireScope(scope string) func(http.Handler) http.Handler` — middleware that checks Identity.HasScope
- `RequireAnyScope(scopes ...string) func(http.Handler) http.Handler`
- Return 403 if insufficient scope

## Files Created

| File | Description |
|------|-------------|
| internal/auth/apikey.go | API key generation and hashing |
| internal/auth/apikey_middleware.go | API key validation middleware |
| internal/auth/oidc.go | OIDC token validation |
| internal/auth/oidc_middleware.go | OIDC middleware |
| internal/auth/middleware.go | Unified auth middleware |
| internal/auth/rbac.go | Scope-based access control |
| internal/auth/apikey_test.go | API key tests |
| internal/auth/apikey_middleware_test.go | API key middleware tests |
| internal/auth/oidc_test.go | OIDC tests |
| internal/auth/middleware_test.go | Unified middleware tests |
| internal/auth/rbac_test.go | RBAC tests |

## Testing Requirements

- API key: test generation format, hash/verify round trip, verify with wrong key
- API key middleware: test valid key, expired key, wrong scope, missing header
- OIDC: test with mock OIDC provider (httptest), valid token, expired token, wrong audience
- Unified middleware: test routing to correct auth method based on token format
- RBAC: test scope checking, multiple scopes, missing scope
- Coverage: >=80% per package
