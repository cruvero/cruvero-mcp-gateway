# Phase 18B Implementation Prompts

## Prompt 1 of 2: Admin Dev Mode Configuration & Middleware

### Required Reading (read these files before writing code)

- `docs/phases/PHASE18B.md` (full scope)
- `internal/config/config.go` (Config struct — see `AdminEnabled` at line 75, `AdminSessionKey` at line 79)
- `internal/admin/middleware.go` (AdminAuthMiddleware at lines 20-38, SessionFromContext, CSRFMiddleware)
- `internal/admin/auth.go` (AdminAuth type, AdminSession struct — fields: Subject, Email, Scopes, CSRFToken, ExpiresAt)
- `internal/admin/admin.go` (AdminDeps struct at line 23, NewRouter at line 34, AdminAuthMiddleware call at line 53)
- `cmd/mcpgw/serve.go` (where AdminDeps is constructed and NewAdminAuth is called)

### Task

Add a dev mode for the admin dashboard that bypasses OIDC authentication for local development.

1. **Add config field** to `internal/config/config.go`:
   - Add to the Config struct (near other Admin fields, around line 75):
     ```go
     AdminDevMode bool `json:"admin_dev_mode"`
     ```
   - In `Load()`, parse `MCPGW_ADMIN_DEV_MODE` as bool (default `false`), same pattern as `AdminEnabled`:
     ```go
     adminDevMode := os.Getenv("MCPGW_ADMIN_DEV_MODE") == "true"
     ```
   - In `Validate()`, add safety checks and relax OIDC requirements for dev mode:
     ```go
     if c.AdminDevMode {
         if !c.AdminEnabled {
             return fmt.Errorf("MCPGW_ADMIN_DEV_MODE requires MCPGW_ADMIN_ENABLED=true")
         }
     }
     ```
   - Modify the existing admin validation block so that OIDC fields (`AdminOIDCClientID`, `OIDCIssuer`, `AdminSessionKey`) are only required when `AdminDevMode` is `false`. In dev mode, these are not needed because OIDC is bypassed entirely:
     ```go
     if c.AdminEnabled && !c.AdminDevMode {
         // ... existing OIDC field validation ...
     }
     ```

2. **Modify AdminAuthMiddleware** in `internal/admin/middleware.go`:
   - Change the function signature to accept `devMode bool`:
     ```go
     func AdminAuthMiddleware(auth *AdminAuth, devMode bool) func(http.Handler) http.Handler {
     ```
   - Add dev mode bypass at the start of the handler:
     ```go
     return func(next http.Handler) http.Handler {
         return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
             if devMode {
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

             // ... existing cookie validation logic unchanged ...
         })
     }
     ```
   - Add necessary imports: `"time"` (if not already imported)

3. **Add `DevMode` to `AdminDeps`** in `internal/admin/admin.go`:
   - Add a `DevMode bool` field to the `AdminDeps` struct (line 23):
     ```go
     type AdminDeps struct {
         Auth                *AdminAuth
         DevMode             bool
         Logger              *slog.Logger
         // ... existing fields ...
     }
     ```

4. **Update AdminAuthMiddleware call site** in `internal/admin/admin.go`:
   - The call is at line 53: `r.Use(AdminAuthMiddleware(deps.Auth))`
   - Change to: `r.Use(AdminAuthMiddleware(deps.Auth, deps.DevMode))`

5. **Add startup warning and wire DevMode** in `cmd/mcpgw/serve.go`:
   - When constructing `admin.AdminDeps{}`, pass `DevMode: cfg.AdminDevMode`
   - Add the warning before server start:
     ```go
     if cfg.AdminDevMode {
         logger.Warn("ADMIN DEV MODE ENABLED — OIDC authentication bypassed. Do not use in production.")
     }
     ```

6. **Skip OIDC initialization in dev mode** in `cmd/mcpgw/serve.go`:
   - The current code calls `admin.NewAdminAuth(cfg, logger)` which performs OIDC discovery. In dev mode, this would fail without a real OIDC issuer.
   - Guard the call:
     ```go
     var adminAuth *admin.AdminAuth
     if cfg.AdminDevMode {
         // Dev mode: skip OIDC discovery, AdminAuth is nil
         adminAuth = nil
     } else {
         var authErr error
         adminAuth, authErr = admin.NewAdminAuth(cfg, logger)
         if authErr != nil {
             return fmt.Errorf("admin auth: %w", authErr)
         }
     }
     ```
   - Pass `adminAuth` (which may be nil in dev mode) to `AdminDeps.Auth`
   - The middleware dev mode check runs before any auth operations, so nil `auth` is safe

7. **Update all other AdminAuthMiddleware call sites** (tests, etc.):
   - Search for `AdminAuthMiddleware(` across the codebase
   - Add `false` as the second parameter for non-dev-mode call sites

### Verification

```bash
go build ./cmd/mcpgw
go test ./internal/config/... ./internal/admin/... ./internal/server/...
go vet ./...
```

### Acceptance Criteria

- `MCPGW_ADMIN_DEV_MODE=true` enables dev mode when admin is also enabled
- `MCPGW_ADMIN_DEV_MODE=true` without `MCPGW_ADMIN_ENABLED=true` fails validation
- Dev mode injects `dev@localhost` session — no OIDC redirect
- Dev mode session has a CSRF token so forms work
- Non-dev mode behavior is completely unchanged
- Startup log includes a warning when dev mode is enabled
- All existing `AdminAuthMiddleware` call sites compile with the new parameter

---

## Prompt 2 of 2: Devcontainer Improvements

### Required Reading (read these files before writing code)

- `docs/phases/PHASE18B.md` (devcontainer section)
- `.devcontainer/devcontainer.json` (current config — 23 lines)
- `scripts/dev-setup.sh` (created in Phase 18A — the new post-create command)
- `docker-compose.yml` (devcontainer service configuration)

### Task

Improve the devcontainer configuration with port forwarding and automated setup.

1. **Add `forwardPorts`** to `.devcontainer/devcontainer.json`:
   ```json
   "forwardPorts": [8443, 9090],
   ```
   - Port 8443: gateway HTTPS
   - Port 9090: Prometheus metrics
   - Add this after the `remoteUser` field (line 6)

2. **Update `postCreateCommand`** to run the dev setup script:

   Replace the current smoke check command (line 22):
   ```json
   // Before:
   "postCreateCommand": "bash -lc 'set -euo pipefail; go version; helm version --short; kubectl version --client --output=yaml >/dev/null; argocd version --client --short; golangci-lint version; staticcheck --version; govulncheck --version; go list ./... >/dev/null; echo \"devcontainer smoke check complete\"'"

   // After:
   "postCreateCommand": "bash -lc 'set -euo pipefail; scripts/dev-setup.sh && echo \"devcontainer setup complete\"'"
   ```

   The `dev-setup.sh` script already handles cert generation, DB wait, migrations, and binary build. The smoke checks for tool availability (helm, kubectl, etc.) can be moved inside `dev-setup.sh` if needed, or left as a separate validation.

   **Alternative**: Keep the smoke checks and add the setup script:
   ```json
   "postCreateCommand": "bash -lc 'set -euo pipefail; go version && helm version --short && scripts/dev-setup.sh && echo \"devcontainer ready\"'"
   ```

3. **Add `postAttachCommand`** for port forwarding notification (optional):
   ```json
   "postAttachCommand": "echo 'Gateway: https://localhost:8443 | Metrics: http://localhost:9090'"
   ```

### Verification

```bash
# Validate JSON
python3 -c "import json; json.load(open('.devcontainer/devcontainer.json'))"

# Check forwardPorts
grep 'forwardPorts' .devcontainer/devcontainer.json

# Check postCreateCommand references dev-setup.sh
grep 'dev-setup.sh' .devcontainer/devcontainer.json
```

### Acceptance Criteria

- `.devcontainer/devcontainer.json` is valid JSON
- `forwardPorts` includes 8443 and 9090
- `postCreateCommand` runs `scripts/dev-setup.sh`
- Port 8443 auto-forwards for gateway access
- Port 9090 auto-forwards for metrics access
- Existing devcontainer configuration (service, workspaceFolder, extensions) is unchanged
