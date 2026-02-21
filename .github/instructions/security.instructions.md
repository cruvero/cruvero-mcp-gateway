---
applyTo: "internal/auth/**/*.go,internal/identity/**/*.go,internal/store/apikey_store*.go"
---

# Security-Focused Review

## Authentication

- API key hashing uses bcrypt with a deterministic lookup hash — verify both are maintained.
- mTLS certificate validation must check SANs, expiry, and trust chain.
- SPIFFE ID prefixes must be validated against the allow list.
- OIDC token validation must verify issuer, audience, and expiration.

## Data Safety

- SQL queries must use parameterized statements — flag any string concatenation.
- TLS configs must not disable verification or use insecure cipher suites.
- Secrets must never be logged, even at debug level.

## Access Control

- Rate limiting must not be bypassable via header manipulation.
- Audit log entries must be created for security-relevant operations.
