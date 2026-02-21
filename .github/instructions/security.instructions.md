---
applyTo: "internal/auth/**/*.go,internal/identity/**/*.go,internal/store/apikey_store*.go,internal/admin/**/*.go"
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

## OIDC AuthCode + PKCE

- PKCE must use S256 challenge method only; plain is not acceptable.
- Code verifiers must be 64 bytes from `crypto/rand`, base64url-encoded without padding.
- The OAuth state parameter must be encrypted or signed to prevent forgery.
- State cookies must be `HttpOnly`, `Secure`, `SameSite=Strict`, and short-lived.
- State and verifier must be cleared from cookies immediately after consumption.

## Session Encryption

- Sessions must be encrypted with AES-256-GCM using a fresh nonce per encryption.
- Session expiry must be checked server-side, not solely via cookie expiry.
- Session cookies must be `HttpOnly`, `Secure`, and `SameSite=Strict`.
- Flag any use of a static or reused nonce for GCM encryption.

## CSRF Protection

- CSRF tokens must be validated on all POST, PUT, and DELETE requests.
- GET, HEAD, and OPTIONS requests must be exempt from CSRF checks.
- Tokens must be transmitted via hidden form field (`_csrf`) or request header.

## Token Cache Security

- Token cache files must use `0600` permissions; directories must use `0700`.
- Writes must use atomic write-to-temp-then-rename to prevent partial reads.
- Refresh tokens must never be logged, even at debug level.

## Device Code Flow

- HTTP request bodies from external sources must be limited with `http.MaxBytesReader` (4KB max).
- IdP response bodies must be limited with `io.LimitReader`.
- Device codes, user codes, and refresh tokens must never appear in log output.
