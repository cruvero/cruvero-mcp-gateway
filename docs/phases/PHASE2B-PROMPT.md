# Phase 2B Implementation Prompts

## Prompt 1 of 4: API Key Generation & Hashing

### Required Reading (read these files before writing code)
- docs/phases/PHASE2B.md
- internal/store/interfaces.go (APIKeyStore)
- internal/types/types.go (APIKey type)
- internal/identity/context.go (Identity, scopes)

### Interface contract
```go
type APIKeyStore interface {
    GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error)
}

func GenerateAPIKey() (plaintext string, lookupHash string, bcryptHash string, err error)
func LookupHashAPIKey(plaintext string) string
func HashAPIKey(plaintext string) (string, error)
func VerifyAPIKey(plaintext, hash string) bool
```

### Task

Create `internal/auth/apikey.go` with API key generation and hashing.

1. Define key prefix constant: `mcpgw_`
2. Implement `GenerateAPIKey() (plaintext string, lookupHash string, bcryptHash string, err error)`:
   - Generate 32 random bytes with crypto/rand
   - Base64url encode (no padding)
   - Prepend prefix: `mcpgw_` + encoded
   - Generate deterministic lookup hash using SHA-256 of plaintext key
   - Hash with bcrypt (cost 12)
   - Return plaintext + lookup hash + bcrypt hash
3. Implement `LookupHashAPIKey(plaintext string) string` — deterministic SHA-256
4. Implement `HashAPIKey(plaintext string) (string, error)` — bcrypt hash
5. Implement `VerifyAPIKey(plaintext, hash string) bool` — bcrypt compare, return false on error
6. Write `internal/auth/apikey_test.go`:
   - Test GenerateAPIKey produces correct prefix and length
   - Test LookupHashAPIKey deterministic output for same input
   - Test hash/verify round trip succeeds
   - Test verify with wrong plaintext fails
   - Test generated keys are unique

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Keys have recognizable `mcpgw_` prefix
- Deterministic lookup hash generation
- bcrypt hashing at appropriate cost
- No plaintext key stored anywhere

---

## Prompt 2 of 4: API Key Middleware

### Required Reading (read these files before writing code)
- docs/phases/PHASE2B.md
- internal/auth/apikey.go
- internal/store/interfaces.go
- internal/identity/context.go

### Task

Create `internal/auth/apikey_middleware.go`.

1. Implement `APIKeyMiddleware(store store.APIKeyStore, logger *slog.Logger) func(http.Handler) http.Handler`:
   - Extract Authorization header, expect "Bearer mcpgw_..."
   - Compute lookup hash from provided key
   - Look up in store via GetByLookupHash
   - Verify provided plaintext key against stored bcrypt hash
   - Check expires_at has not passed
   - Create Identity with Type=IdentityAPIKey, ID=apikey.ClientID, Scopes=apikey.Scopes
   - Inject into context
   - 401 for missing/invalid key, 403 for expired
2. Write `internal/auth/apikey_middleware_test.go`:
   - Mock APIKeyStore interface
   - Test valid API key flows through
   - Test lookup-hash mismatch returns 401
   - Test bcrypt verification mismatch returns 401
   - Test missing Authorization header returns 401
   - Test invalid key format returns 401
   - Test expired key returns 403
   - Test key not found returns 401

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Correct token extraction and validation
- Proper status codes for each error case
- Identity correctly populated in context

---

## Prompt 3 of 4: OIDC Validation & Middleware

### Required Reading (read these files before writing code)
- docs/phases/PHASE2B.md
- internal/identity/context.go
- internal/auth/apikey_middleware.go (pattern reference)

### Task

Create OIDC validation and middleware.

1. `internal/auth/oidc.go`:
   - `OIDCValidator` struct: provider *oidc.Provider, verifier *oidc.IDTokenVerifier
   - `NewOIDCValidator(ctx context.Context, issuerURL, audience string) (*OIDCValidator, error)` — discover provider, create verifier
   - `Validate(ctx context.Context, rawToken string) (*Identity, error)`:
     - Verify token via verifier
     - Extract standard claims (sub, email, groups)
     - Map to Identity: Type=IdentityOIDC, ID=sub, Scopes from groups mapping
   - Define claims struct for extraction
2. `internal/auth/oidc_middleware.go`:
   - `OIDCMiddleware(validator *OIDCValidator, logger *slog.Logger) func(http.Handler) http.Handler`
   - Extract Bearer token, validate, inject Identity
   - 401 for invalid/expired tokens
3. `internal/auth/oidc_test.go`:
   - Use httptest to create mock OIDC discovery endpoint
   - Test successful token validation
   - Test invalid token
   - Test claims extraction

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- OIDC discovery and token validation work
- Claims correctly mapped to Identity
- Error cases return proper HTTP status codes

---

## Prompt 4 of 4: Unified Auth Middleware & RBAC

### Required Reading (read these files before writing code)
- docs/phases/PHASE2B.md
- internal/auth/apikey_middleware.go
- internal/auth/oidc_middleware.go
- internal/identity/middleware.go (mTLS middleware reference)
- internal/identity/context.go

### Task

Create unified auth middleware and RBAC.

1. `internal/auth/middleware.go`:
   - Define `AuthOptions` struct: APIKeyStore, OIDCValidator (pointer, nil = disabled), Logger
   - Implement `AuthMiddleware(opts AuthOptions) func(http.Handler) http.Handler`:
     - If request already has Identity in context (from mTLS middleware) -> pass through
     - If Authorization header contains "Bearer mcpgw_" -> delegate to API key validation
     - If Authorization header contains "Bearer " (non-mcpgw) -> delegate to OIDC if configured
     - If no Authorization header -> return 401
     - If OIDC requested but not configured -> return 401 with descriptive error
2. `internal/auth/rbac.go`:
   - `RequireScope(scope string) func(http.Handler) http.Handler` — check Identity.HasScope, 403 if not
   - `RequireAnyScope(scopes ...string) func(http.Handler) http.Handler` — check any scope matches
3. Tests:
   - `internal/auth/middleware_test.go`: Test routing logic for each auth type, test passthrough for mTLS
   - `internal/auth/rbac_test.go`: Test scope requirements with various Identity configurations

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Unified middleware correctly routes to the right auth method
- mTLS-authenticated requests pass through without re-auth
- RBAC middleware enforces scope requirements
- >=80% test coverage across auth package
