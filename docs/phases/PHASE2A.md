# Phase 2A: mTLS, SPIFFE ID Extraction, Cert Management

## Overview

Implement the mTLS infrastructure for authenticating MCP server pods. The gateway requires client certificates and extracts SPIFFE IDs from URI SANs to identify workloads.

## Scope

### TLS Config Builder (`internal/identity/tls.go`)
- `TLSConfig` struct: CA bundle path, server cert/key paths, client CA path, min TLS version
- `BuildServerTLSConfig(cfg TLSConfig) (*tls.Config, error)` — creates tls.Config with:
  - MinVersion: tls.VersionTLS13
  - ClientAuth: tls.RequireAndVerifyClientCert
  - ClientCAs: loaded from CA bundle
  - Certificates: server cert/key pair
- `BuildClientTLSConfig(cfg TLSConfig) (*tls.Config, error)` — for gateway-to-backend connections
- Certificate loading with file watching (reload on change) via fsnotify or periodic check
- Error handling: clear errors for missing/invalid certs

### SPIFFE ID Extraction (`internal/identity/spiffe.go`)
- `ExtractSPIFFEID(certs []*x509.Certificate) (string, error)` — extract from URI SAN
- `ValidateSPIFFEID(id string, allowedPrefixes []string) error` — check against allowlist
- SPIFFE ID format: `spiffe://trust-domain/workload-identifier`
- Parse and validate trust domain, workload path components

### Identity Context (`internal/identity/context.go`)
- `Identity` struct: Type (mtls/apikey/oidc), ID string, Scopes []string, Metadata map[string]string
- Context helpers: `WithIdentity(ctx, identity)`, `FromContext(ctx) (*Identity, bool)`
- `IdentityType` enum: IdentityMTLS, IdentityAPIKey, IdentityOIDC

### Identity Middleware (`internal/identity/middleware.go`)
- `MTLSMiddleware(allowedPrefixes []string) func(http.Handler) http.Handler`
- Extracts peer certificates from TLS connection state
- Calls ExtractSPIFFEID, validates against allowed prefixes
- Injects Identity into request context
- Returns 401 if no valid client cert, 403 if SPIFFE ID not allowed

### cert-manager Integration (documentation only)
- Document Certificate resource YAML for gateway and MCP server pods
- Document CA issuer setup
- This is deployment config, not Go code

## Files Created

| File | Description |
|------|-------------|
| internal/identity/tls.go | TLS config builder |
| internal/identity/spiffe.go | SPIFFE ID extraction and validation |
| internal/identity/context.go | Identity type and context helpers |
| internal/identity/middleware.go | mTLS authentication middleware |
| internal/identity/tls_test.go | TLS config tests |
| internal/identity/spiffe_test.go | SPIFFE ID tests |
| internal/identity/context_test.go | Context helper tests |
| internal/identity/middleware_test.go | Middleware tests |

## Testing Requirements

- TLS config: test with valid certs, missing certs, invalid certs
- SPIFFE: test extraction from various cert configurations (with/without URI SAN, multiple SANs)
- SPIFFE validation: test allowed prefixes, rejected prefixes, empty prefixes
- Middleware: test with valid mTLS, invalid cert, no cert, forbidden SPIFFE ID
- Reuse `internal/testutil.GenerateTestCerts` from Phase 1 for cert fixtures
- Coverage: >=80% per package
