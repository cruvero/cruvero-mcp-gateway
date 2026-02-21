# Phase 2: Identity & Auth

## Goal

Implement mTLS authentication for MCP server pods and API key + OIDC authentication for CLI users. Establish a unified auth middleware that routes to the correct authentication method based on request characteristics.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [2A](PHASE2A.md) | mTLS, SPIFFE ID Extraction, Cert Management | [4 prompts](PHASE2A-PROMPT.md) | identity |
| [2B](PHASE2B.md) | API Keys, OIDC, Auth Middleware | [4 prompts](PHASE2B-PROMPT.md) | auth |

## Dependencies

- Phase 1 (Core Foundation) — requires types, config, server, store packages

## Deliverables

- TLS config builder with mTLS support (RequireAndVerifyClientCert)
- SPIFFE ID extraction from URI SAN on peer certificates
- Identity middleware injecting authenticated identity into request context
- API key generation, deterministic lookup hashing (SHA-256), bcrypt verification, and validation
- OIDC token validation via go-oidc
- Unified auth middleware routing to mTLS / API key / OIDC based on request
- RBAC: scoped access control (read/write/admin)

## Packages Created

- `internal/identity` — mTLS configuration, SPIFFE ID extraction, cert validation
- `internal/auth` — API key management, OIDC validation, unified auth middleware

## Success Criteria

- mTLS handshake succeeds with valid client cert, fails with invalid
- SPIFFE ID correctly extracted from URI SAN
- API key creation, validation, and revocation work correctly
- OIDC token validation works with mock provider
- Unified middleware correctly routes to the right auth method
- >=80% test coverage per package
