# Phase 2A Implementation Prompts

## Prompt 1 of 4: TLS Config Builder

### Required Reading (read these files before writing code)
- docs/phases/PHASE2A.md
- internal/config/config.go
- internal/testutil/certs.go
- LLM.md

### Task

Create `internal/identity/tls.go` with TLS configuration builders.

1. Define `TLSConfig` struct with fields for CA bundle, cert, key, client CA paths, and min TLS version
2. Implement `BuildServerTLSConfig(cfg TLSConfig) (*tls.Config, error)`:
   - Load server certificate and key via tls.LoadX509KeyPair
   - Load CA bundle into x509.CertPool
   - Set ClientAuth to tls.RequireAndVerifyClientCert
   - Set MinVersion to tls.VersionTLS13
3. Implement `BuildClientTLSConfig(cfg TLSConfig) (*tls.Config, error)`:
   - Load client certificate and key
   - Load server CA bundle for verification
4. Implement `LoadCertPool(path string) (*x509.CertPool, error)` helper
5. Write `internal/identity/tls_test.go`:
   - Test successful config building with certs from `testutil.GenerateTestCerts`
   - Test error on missing cert file
   - Test error on invalid cert content
   - Test CA pool loading

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Both server and client TLS configs build correctly
- Errors are clear and descriptive
- No hardcoded paths or values

---

## Prompt 2 of 4: SPIFFE ID Extraction

### Required Reading (read these files before writing code)
- docs/phases/PHASE2A.md
- internal/identity/tls.go

### Task

Create `internal/identity/spiffe.go` for SPIFFE ID extraction and validation.

1. Implement `ExtractSPIFFEID(certs []*x509.Certificate) (string, error)`:
   - Iterate URI SANs on the leaf certificate
   - Find URI with scheme "spiffe"
   - Return the full SPIFFE ID string
   - Error if no SPIFFE URI SAN found
2. Implement `ParseSPIFFEID(raw string) (trustDomain, workloadID string, err error)`:
   - Validate format: spiffe://trust-domain/workload-path
   - Extract trust domain and workload path
3. Implement `ValidateSPIFFEID(id string, allowedPrefixes []string) error`:
   - Check if the SPIFFE ID starts with any allowed prefix
   - Return error with detail if no prefix matches
   - Empty allowedPrefixes means allow all (for development)
4. Write `internal/identity/spiffe_test.go`:
   - Test extraction from cert with SPIFFE URI SAN
   - Test extraction from cert without URI SAN (error)
   - Test extraction from cert with non-SPIFFE URI SAN (error)
   - Test parsing valid and invalid SPIFFE IDs
   - Test validation against various prefix combinations

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Correct SPIFFE ID extraction from X.509 certificates
- Robust validation with clear error messages
- Edge cases covered in tests

---

## Prompt 3 of 4: Identity Context

### Required Reading (read these files before writing code)
- docs/phases/PHASE2A.md
- internal/identity/spiffe.go

### Task

Create `internal/identity/context.go` with identity types and context helpers.

1. Define `IdentityType` string type with constants: IdentityMTLS, IdentityAPIKey, IdentityOIDC
2. Define `Identity` struct: Type IdentityType, ID string, Scopes []string, Metadata map[string]string
3. Implement context key and helpers:
   - unexported context key type
   - `WithIdentity(ctx context.Context, id *Identity) context.Context`
   - `FromContext(ctx context.Context) (*Identity, bool)`
4. Implement `(id *Identity) HasScope(scope string) bool`
5. Define scope constants: ScopeRead, ScopeWrite, ScopeAdmin
6. Write `internal/identity/context_test.go`:
   - Test round-trip: WithIdentity then FromContext
   - Test FromContext on empty context returns false
   - Test HasScope with various scope combinations

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Context helpers are type-safe
- HasScope correctly checks membership

---

## Prompt 4 of 4: mTLS Middleware

### Required Reading (read these files before writing code)
- docs/phases/PHASE2A.md
- internal/identity/context.go
- internal/identity/spiffe.go
- internal/server/server.go

### Task

Create `internal/identity/middleware.go` with mTLS authentication middleware.

1. Implement `MTLSMiddleware(allowedPrefixes []string, logger *slog.Logger) func(http.Handler) http.Handler`:
   - Check r.TLS is not nil and VerifiedChains is not empty
   - Extract SPIFFE ID using ExtractSPIFFEID
   - Validate against allowedPrefixes using ValidateSPIFFEID
   - Create Identity with Type=IdentityMTLS, ID=spiffeID, Scopes=[admin]
   - Inject into context with WithIdentity
   - Return 401 with JSON error if no valid TLS connection
   - Return 403 with JSON error if SPIFFE ID not allowed
   - Log authentication events
2. Write `internal/identity/middleware_test.go`:
   - Test with valid mTLS connection and allowed SPIFFE ID
   - Test with no TLS connection (401)
   - Test with TLS but no verified chains (401)
   - Test with valid cert but disallowed SPIFFE ID (403)
   - Use httptest and manually set TLS connection state on requests

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Middleware correctly authenticates mTLS connections
- Proper HTTP status codes for failure cases
- Identity injected into context for downstream handlers
- >=80% test coverage
