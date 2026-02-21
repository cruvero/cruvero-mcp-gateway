# Phase 18B: Admin Dev Mode & Devcontainer Improvements

## Overview

Add a dev mode bypass for admin dashboard OIDC authentication to simplify local testing, and improve the devcontainer configuration with port forwarding and automated setup.

## Scope

### Admin Dev Mode Config (`internal/config/config.go`)

Add a new config field:

- `AdminDevMode bool` — parsed from `MCPGW_ADMIN_DEV_MODE` (default `false`)

**Safety guards in validation:**
- When `AdminDevMode` is `true` and `TLSCAPath` is set, log a warning if the CA path does not contain `dev` or `local` in the filename — this is a heuristic to discourage accidental dev mode in production (warning only, not a hard rejection)
- When `AdminDevMode` is `true`, `AdminEnabled` must also be `true` (dev mode without admin enabled is a no-op)
- Log a prominent startup warning: `"ADMIN DEV MODE ENABLED — OIDC authentication bypassed. Do not use in production."`

### Admin Auth Middleware Dev Mode Bypass (`internal/admin/middleware.go`)

Modify `AdminAuthMiddleware` (line 20-38) to check for dev mode before validating the session cookie:

```go
func AdminAuthMiddleware(auth *AdminAuth, devMode bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if devMode {
                // Inject synthetic dev session
                session := &AdminSession{
                    Subject:   "dev-user",
                    Email:     "dev@localhost",
                    Scopes:    []string{"admin"},
                    CSRFToken: "dev-csrf-token",
                    ExpiresAt: time.Now().Add(24 * time.Hour),
                }
                ctx := context.WithValue(r.Context(), sessionContextKey, session)
                next.ServeHTTP(w, r.WithContext(ctx))
                return
            }

            // ... existing cookie validation logic ...
        })
    }
}
```

**Key changes:**
- Add `devMode bool` parameter to `AdminAuthMiddleware`
- When `devMode` is true, inject a synthetic `AdminSession` with `dev@localhost` email
- The synthetic session uses `Subject` (not `Name` — `AdminSession` has no `Name` field) and includes `Scopes: []string{"admin"}`
- The synthetic session includes a static CSRF token so form submissions work
- All existing non-dev behavior is unchanged

### Wire Dev Mode through AdminDeps (`internal/admin/admin.go`)

Add `DevMode bool` to the `AdminDeps` struct (line 23). Update `NewRouter` to pass `deps.DevMode` to `AdminAuthMiddleware` at line 53.

### Skip OIDC Initialization in Dev Mode (`cmd/mcpgw/serve.go`)

In dev mode, `NewAdminAuth()` must be skipped because it performs OIDC discovery which requires a real issuer URL. Guard the call:
- If `AdminDevMode` is true, set `adminAuth = nil` and skip OIDC initialization
- Pass `nil` auth to `AdminDeps.Auth` — the dev mode middleware check runs before any auth operations
- Modify config `Validate()` to skip OIDC field requirements (`AdminOIDCClientID`, `OIDCIssuer`, `AdminSessionKey`) when `AdminDevMode` is true

### Devcontainer Port Forwarding (`.devcontainer/devcontainer.json`)

Add `forwardPorts` to the devcontainer config:

```json
"forwardPorts": [8443, 9090]
```

This allows VS Code and GitHub Codespaces to automatically forward the gateway (8443) and metrics (9090) ports to the host machine.

### Devcontainer Post-Create Command (`.devcontainer/devcontainer.json`)

Update `postCreateCommand` to run the dev setup script instead of just smoke checks:

```json
"postCreateCommand": "bash -lc 'set -euo pipefail; scripts/dev-setup.sh'"
```

This ensures the development environment is fully bootstrapped when the devcontainer is created, including certificates, database migrations, and binary build.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/config/config.go` | Add `AdminDevMode` field, parse `MCPGW_ADMIN_DEV_MODE`, safety validation |
| `internal/config/config_test.go` | Tests for AdminDevMode config parsing and validation |
| `internal/admin/middleware.go` | Add `devMode` parameter, synthetic session injection |
| `internal/admin/middleware_test.go` | Tests for dev mode bypass and normal mode unchanged behavior |
| `internal/server/server.go` | Pass `cfg.AdminDevMode` when mounting admin routes |
| `.devcontainer/devcontainer.json` | Add `forwardPorts`, update `postCreateCommand` |

## Testing Requirements

- Config: `MCPGW_ADMIN_DEV_MODE` defaults to `false`
- Config: `MCPGW_ADMIN_DEV_MODE=true` parses correctly
- Config: dev mode without admin enabled is rejected by validation
- Middleware: dev mode injects session with `Subject: "dev-user"`, `Email: "dev@localhost"`, `Scopes: ["admin"]`
- Middleware: dev mode session has valid CSRF token for form submissions
- Middleware: non-dev mode behavior unchanged — missing cookie redirects to `/admin/login`
- Middleware: non-dev mode with invalid cookie redirects to `/admin/login`
- Config: dev mode skips OIDC field validation (`AdminOIDCClientID`, `OIDCIssuer`, `AdminSessionKey` not required)
- Devcontainer: `forwardPorts` contains both 8443 and 9090
- Devcontainer: `postCreateCommand` calls `scripts/dev-setup.sh`
- Coverage: >=80% on `internal/config` and `internal/admin`
