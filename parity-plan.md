# Phase Docs vs Implementation Parity Analysis

## Implementation Instructions

> These instructions are for Claude Code sessions working through the remediation checklist below.

### Workflow

1. **Read this file first** at the start of every session. Check the checklist to find the next unchecked item.
2. **One gap per branch.** Create a feature branch off `dev` for each gap: `fix/gap-N-short-description` (e.g., `fix/gap-1-configstore-wiring`).
3. **One commit per logical change** within a gap. Follow conventional commits with scope (see `CLAUDE.md`). Never mention AI in commits.
4. **Read before writing.** Always read the affected files listed in the gap's remediation section before making changes. Understand existing patterns — match them.
5. **Run quality gates after every code change:**
   ```bash
   go build ./cmd/mcpgw && go test -race ./... && make quality
   ```
   Do not consider a gap complete until all gates pass.
6. **Update the checklist** in this file after completing each sub-task (change `- [ ]` to `- [x]`). This is how progress is tracked across sessions.
7. **Do not open PRs automatically.** Leave branches ready for the user to review and PR themselves.

### Codebase Conventions (Quick Reference)

- **Module**: `github.com/cruvero/mcp-gateway`
- **Config**: All configuration via `MCPGW_*` env vars parsed in `internal/config/config.go`. No config files.
- **Config pattern**: Add field to `Config` struct → add `const default*` → parse in `Load()` with `parseInt`/`parseDuration`/`os.Getenv` → add to struct literal.
- **Server wiring**: `internal/server/server.go` → `New()` constructor. The `cmd/mcpgw/serve.go` `runServe()` function creates dependencies and passes them to the server.
- **Testing**: Table-driven with `t.Run()`. Use `go-sqlmock` for DB tests. Use `t.Setenv()` for env vars. Every error path needs a test case. Target 80% coverage per package.
- **NATS events**: `internal/events/client.go` for connection, options are functional (`ClientOption` pattern). `WithTLS()` already exists — just needs to be called.
- **DegradationManager**: `internal/events/degradation.go`. Constructor is `NewDegradationManager(client, configStore, subscriber, logger)`. `PostgresConfigStore` is in `internal/events/persistence.go`.
- **ArgoCD**: `deploy/argocd/` — ApplicationSet uses list generator, AppProject defines RBAC destinations.
- **Workflows**: `.github/workflows/` — `ci.yml` (test/lint/build), `image-dev.yml` (Docker build/push for dev). Self-hosted runners with `[self-hosted, cruvero]` labels.
- **Helm overlays**: `charts/mcpgateway/values-{dev,staging,prod}.yaml` all exist already. ArgoCD just doesn't reference staging/prod yet.
- **Harbor registry**: Images push to `harbor.dev.gchinfo.com/cruvero/cruvero-mcp-gateway`.

### Key Files Per Gap

| Gap | Primary files to read and modify |
|-----|----------------------------------|
| #1 | `internal/server/server.go`, `internal/events/degradation.go`, `internal/events/persistence.go` |
| #2 | `internal/config/config.go`, `internal/server/server.go`, `internal/events/client.go` |
| #3 | `deploy/argocd/applicationset.yaml`, `charts/mcpgateway/values-staging.yaml`, `charts/mcpgateway/values-prod.yaml` |
| #4 | `deploy/argocd/project.yaml` |
| #5 | `.github/workflows/image-dev.yml` (reference), create new workflow files |
| #6 | Create `.github/dependabot.yml` |
| #7 | `.github/workflows/image-dev.yml` (and any new workflow files from Gap #5) |
| #8 | Create `sonar-project.properties`, `.github/workflows/ci.yml` |
| #9 | `.devcontainer/devcontainer.json`, `docker-compose.yml`, `scripts/dev-setup.sh` (create), `cmd/mcpgw/serve.go`, `internal/config/config.go` |
| #10 | `charts/mcpgateway/values-dev.yaml`, `charts/mcpgateway/templates/ingress.yaml`, `charts/mcpgateway/templates/certificate.yaml`, `charts/mcpgateway/values.yaml` |

### Commit Message Examples

```
fix(events): wire PostgresConfigStore into DegradationManager

The DegradationManager was always receiving a nil ConfigStore, preventing
cached config loading when NATS is unavailable. Pass the Postgres-backed
store during server initialization.
```

```
feat(config): add NATS TLS certificate environment variables

Add MCPGW_NATS_TLS_CERT, MCPGW_NATS_TLS_KEY, and MCPGW_NATS_TLS_CA
fields to enable encrypted NATS connections in production.
```

```
feat(ci): add staging and production image build workflows
```

```
chore(ci): add Dependabot configuration for Go, Actions, and Docker
```

```
feat(server): add dev-setup script and local admin bypass for devcontainer

Add scripts/dev-setup.sh to automate migrations, cert generation, session
key creation, and gateway startup. Support MCPGW_ADMIN_DEV_MODE to bypass
OIDC in local development.
```

```
feat(helm): enable ingress for admin UI on cruvero-mcp-gateway.dev.gchinfo.com

Enable Traefik ingress in values-dev.yaml with TLS via cert-manager
letsencrypt-dns01 ClusterIssuer. Adds /admin path routing alongside
the existing mTLS gateway endpoint.
```

---

## Executive Summary

Phases 1–9 of `cruvero-mcp-gateway` are **functionally complete**. Every core deliverable — types, config, HTTP server, Postgres store, mTLS/API-key/OIDC auth, SPIFFE identity, server registration, MCP proxy with SSE streaming, circuit breakers, policy engine, distributed rate limiting, NATS event bus, Kubernetes packaging, CLI, and GitOps — is implemented and tested.

All four **P0 blockers** from `PRODUCTION-PLANS.md` have been resolved:

| P0 Item | Fix |
|---------|-----|
| Audit store not wired to policy engine | `SetAuditStore()` called at startup (`serve.go:137`) |
| DB connection pool unconfigured | `MCPGW_DB_MAX_OPEN_CONNS/IDLE/LIFETIME` env vars applied (`serve.go:89-91`) |
| CORS wildcard origin | `CORSMiddleware` uses explicit allowlist matching (`middleware.go:102-129`) |
| Audit log unbounded growth | `StartAuditRetention()` goroutine with 90-day default (`serve.go:231`) |

**Remaining gaps fall into two categories:**

1. **Code-level issues (2)** — a nil ConfigStore in DegradationManager and unused NATS TLS wiring
2. **GitOps / CI gaps (5)** — staging/prod ArgoCD configs, image build workflows, Dependabot, container scanning

None of these block local development or dev-environment deployment. They block **staging/production promotion** and **supply-chain security hardening**.

---

## Phase-by-Phase Completion Status

| Phase | Scope | Status | Notes |
|-------|-------|--------|-------|
| 1 | Core Foundation (types, config, server, store, migrations) | COMPLETE | 9 migrations, 16 packages |
| 2 | Identity & Auth (mTLS, API keys, OIDC, SPIFFE, device flow) | COMPLETE | 16 auth test files |
| 3 | Registration (lifecycle, heartbeat, capability index, sweeper) | COMPLETE | 12 test files |
| 4 | MCP Proxy (routing, SSE, circuit breaker, retry, pool) | COMPLETE | 7 proxy + 4 resilience test files |
| 5 | Policy & Rate Limiting (token bucket, NATS/Dragonfly backends, tool safety) | COMPLETE | 13 test files across 3 backends |
| 6 | NATS & Cruvero (event bus, config sync, degradation) | COMPLETE | 1 bug (see Gap #1) |
| 7 | K8s & Observability (Dockerfile, Helm, Prometheus, OTel) | COMPLETE | 4-environment overlays |
| 8 | CLI & Testing (8 subcommands, integration/security/load suites) | COMPLETE | 20+ CLI test files |
| 9 | GitOps (ArgoCD, devcontainer, Helm overlays) | PARTIAL | Dev only; staging/prod not wired |

---

## Detailed Gap Table

| # | Priority | Category | Description | Affected Files | Impact |
|---|----------|----------|-------------|----------------|--------|
| 1 | P1 | Code bug | `DegradationManager` always receives `nil` ConfigStore — cannot load cached config on startup if NATS is unavailable | `internal/server/server.go:125,143` | Readiness probe returns `not_ready` indefinitely when NATS is down and no cached config exists; graceful degradation is incomplete |
| 2 | P1 | Code bug | NATS TLS `WithTLS()` option exists but is never called — NATS connections are always unencrypted | `internal/events/client.go:30-35`, `internal/server/server.go:95-100`, `internal/config/config.go` | NATS traffic is plaintext; unacceptable for production clusters with network policy enforcement |
| 3 | P1 | GitOps | ArgoCD ApplicationSet list generator only includes `dev`; missing `staging` and `prod` elements | `deploy/argocd/applicationset.yaml:14-19` | Cannot deploy to staging or production via ArgoCD |
| 4 | P1 | GitOps | ArgoCD AppProject destinations restricted to `cruvero-dev` namespace; missing staging/prod | `deploy/argocd/project.yaml:10-12` | AppProject RBAC rejects deployments to other namespaces even if ApplicationSet is extended |
| 5 | P1 | CI | Only `image-dev.yml` workflow exists; no staging or production image build pipelines | `.github/workflows/` | No automated image builds for staging/prod environments |
| 6 | P2 | CI | No `.github/dependabot.yml` — dependency updates are manual | `.github/` | Security patches to `go.mod` and GitHub Actions must be discovered manually |
| 7 | P2 | CI | No container image scanning (Trivy/Grype) in any workflow | `.github/workflows/image-dev.yml` | Production images deployed without CVE assessment |
| 8 | P2 | Tooling | No `sonar-project.properties` — SonarQube integration missing | repo root | Lower visibility into code smells and cognitive complexity trends (mitigated by existing golangci-lint, staticcheck, gosec, dupl) |
| 9 | P1 | DX | Devcontainer not functional end-to-end — no auto-migration, no cert generation script, admin UI requires external OIDC provider with no local bypass, no `forwardPorts` in devcontainer.json | `.devcontainer/devcontainer.json`, `docker-compose.yml`, `cmd/mcpgw/serve.go`, `internal/admin/auth.go`, `internal/config/config.go` | Developers cannot open devcontainer and reach the admin UI without significant manual setup; blocks onboarding and local testing |
| 10 | P1 | Infra | Admin UI not exposed externally in Kubernetes — ingress disabled in `values-dev.yaml`, no cert-manager certificate for the public hostname, no Traefik annotations | `charts/mcpgateway/values-dev.yaml`, `charts/mcpgateway/templates/ingress.yaml`, `charts/mcpgateway/templates/certificate.yaml` | Admin dashboard is only reachable via `kubectl port-forward`; not accessible at `cruvero-mcp-gateway.dev.gchinfo.com` |

---

## Remediation Plan

### P1 — Required for Staging/Production

#### Gap #1: Wire ConfigStore into DegradationManager

**Problem**: Both call sites in `server.go` pass `nil` as the ConfigStore argument:

```go
// Line 125 (NATS available)
degradation := events.NewDegradationManager(natsClient, nil, srv.eventSubscriber, logger)

// Line 143 (NATS unavailable)
srv.degradation = events.NewDegradationManager(nil, nil, nil, logger)
```

The `PostgresConfigStore` implementation exists (`internal/events/persistence.go`) but is never instantiated in the startup path.

**Fix**:
1. In `internal/server/server.go`, after the database is available, create a `PostgresConfigStore` instance
2. Pass it as the second argument to `NewDegradationManager()` at both call sites
3. Call `LoadCachedConfig()` during startup when NATS is unavailable so the gateway can serve from cached config

**Files to modify**: `internal/server/server.go`

---

#### Gap #2: Wire NATS TLS Configuration

**Problem**: `WithTLS()` option exists in `internal/events/client.go:30-35` and `nats.Secure()` is conditionally applied at line 115-117, but no config fields or env vars exist to provide TLS material.

**Fix**:
1. Add three env vars to `internal/config/config.go`:
   - `MCPGW_NATS_TLS_CERT` — client certificate path
   - `MCPGW_NATS_TLS_KEY` — client key path
   - `MCPGW_NATS_TLS_CA` — CA certificate path
2. In `internal/server/server.go`, when creating the NATS client, build a `*tls.Config` from these paths and pass `events.WithTLS(tlsCfg)` to `NewClient()`
3. Add tests for the new config parsing and TLS wiring

**Files to modify**: `internal/config/config.go`, `internal/server/server.go`

---

#### Gap #3: Extend ArgoCD ApplicationSet

**Problem**: The list generator at `deploy/argocd/applicationset.yaml:14-19` only has one element:

```yaml
generators:
  - list:
      elements:
        - env: dev
          namespace: cruvero-dev
          valuesFile: values-dev.yaml
          targetRevision: dev
```

**Fix**: Add `staging` and `prod` elements:

```yaml
generators:
  - list:
      elements:
        - env: dev
          namespace: cruvero-dev
          valuesFile: values-dev.yaml
          targetRevision: dev
        - env: staging
          namespace: cruvero-staging
          valuesFile: values-staging.yaml
          targetRevision: main
        - env: prod
          namespace: cruvero-prod
          valuesFile: values-prod.yaml
          targetRevision: main
```

Also update the image updater patch to handle staging/prod tag patterns.

**Files to modify**: `deploy/argocd/applicationset.yaml`

---

#### Gap #4: Extend ArgoCD AppProject Destinations

**Problem**: `deploy/argocd/project.yaml:10-12` restricts to one namespace:

```yaml
destinations:
  - server: https://kubernetes.default.svc
    namespace: cruvero-dev
```

**Fix**: Add staging and prod namespaces:

```yaml
destinations:
  - server: https://kubernetes.default.svc
    namespace: cruvero-dev
  - server: https://kubernetes.default.svc
    namespace: cruvero-staging
  - server: https://kubernetes.default.svc
    namespace: cruvero-prod
```

Update the project description from "dev deployment only" to reflect multi-environment scope.

**Files to modify**: `deploy/argocd/project.yaml`

---

#### Gap #5: Staging/Production Image Build Workflows

**Problem**: Only `.github/workflows/image-dev.yml` exists. No image builds trigger for staging or production.

**Fix** (two options):

**Option A** — Parameterized single workflow with matrix:
- Rename `image-dev.yml` → `image-build.yml`
- Add a matrix strategy for `[dev, staging, prod]` with per-environment branch triggers and tag patterns

**Option B** — Separate workflows (simpler, more explicit):
- Create `image-staging.yml` triggered on `main` branch push, tagging `staging-${SHA}`
- Create `image-prod.yml` triggered on tag push (`v*`), tagging `${TAG}` and `latest`

Option A reduces duplication; Option B is easier to audit. Either way, add a Trivy scan step (see Gap #7).

**Files to create**: `.github/workflows/image-staging.yml`, `.github/workflows/image-prod.yml` (or refactor to single workflow)

---

### P2 — Hardening and Maturity

#### Gap #6: Dependabot Configuration

**Fix**: Create `.github/dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule:
      interval: weekly
    open-pull-requests-limit: 5
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
    open-pull-requests-limit: 3
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: weekly
    open-pull-requests-limit: 3
```

**Files to create**: `.github/dependabot.yml`

---

#### Gap #7: Container Image Scanning

**Fix**: Add a Trivy scan step to the image build workflow(s), after `docker build` and before `docker push`:

```yaml
- name: Scan image for vulnerabilities
  uses: aquasecurity/trivy-action@master
  with:
    image-ref: ${{ env.IMAGE_TAG }}
    format: table
    exit-code: 1
    severity: CRITICAL,HIGH
```

This fails the build on critical/high CVEs, preventing vulnerable images from reaching the registry.

**Files to modify**: `.github/workflows/image-dev.yml` (and staging/prod equivalents)

---

#### Gap #8: SonarQube Integration

**Fix**: Create `sonar-project.properties` with coverage and source paths. Add a `sonar-scanner` step to CI.

**Note**: This is lowest priority because the project already runs golangci-lint, staticcheck, gosec, dupl, and a custom godoc checker via `make quality`. SonarQube would add cognitive complexity tracking and a dashboard, but overlaps heavily with existing tools.

**Files to create**: `sonar-project.properties` (optional)

---

### P1 — Developer Experience

#### Gap #9: Functional Devcontainer with Local Admin UI Access

**Problem**: The devcontainer exists but a developer cannot open it and reach the admin UI without significant manual work. Multiple issues compound:

1. **No auto-migrations**: `postCreateCommand` in `devcontainer.json` runs a smoke check but does not run `mcpgw migrate`. The database is empty after first start.
2. **No cert generation script**: `docker-compose.yml` has a `certs` service that generates TLS certs, but it's gated behind the `gateway` profile — the `devcontainer` service mounts `/certs:ro` but certs are only generated when `docker compose --profile gateway up` is used. Running just the devcontainer leaves `/certs` empty.
3. **Admin UI requires external OIDC**: `MCPGW_ADMIN_ENABLED=true` requires a working OIDC issuer, client ID, and client secret. There is no local bypass for development. Without OIDC, the admin UI is inaccessible.
4. **No `forwardPorts`**: `devcontainer.json` does not declare forwarded ports — VS Code won't auto-forward 8443 (gateway) or 9090 (metrics) for browser access.
5. **No single launch command**: A developer must manually build, set env vars, run migrations, and start the gateway in separate steps.

**Fix** (multi-part):

**Part A — Dev setup script** (`scripts/dev-setup.sh`):
Create a single script that handles the full local startup sequence:
1. Generate self-signed TLS certs (CA, server, client) into a local `certs/` directory if they don't exist (replicate the `openssl` commands from the `certs` service in `docker-compose.yml`)
2. Wait for Postgres to be ready (poll `pg_isready`)
3. Run database migrations (`go run ./cmd/mcpgw migrate`)
4. Generate a random `MCPGW_ADMIN_SESSION_KEY` if not set
5. Optionally seed demo data (a few tool classifications, a test API key)
6. Build and start the gateway with dev-friendly defaults

**Part B — Admin dev mode** (`MCPGW_ADMIN_DEV_MODE`):
Add a `MCPGW_ADMIN_DEV_MODE=true` env var that, when set:
- Bypasses OIDC authentication in `AdminAuthMiddleware` (`internal/admin/middleware.go`)
- Creates a fake session with a hardcoded dev identity (e.g., `dev@localhost` with `admin` scope)
- Logs a clear warning at startup: `"admin dev mode enabled — authentication bypassed"`
- Is rejected if `MCPGW_TLS_CA` is set to a non-dev CA (safety guard against accidental production use)

This allows developers to access the admin UI at `https://localhost:8443/admin/` without configuring an OIDC provider.

**Part C — Devcontainer improvements** (`.devcontainer/devcontainer.json`):
- Add `forwardPorts: [8443, 9090]` for automatic port forwarding
- Update `postCreateCommand` to run `scripts/dev-setup.sh` (migrations + cert generation)
- Add a `postStartCommand` or VS Code task to optionally launch the gateway

**Part D — Cert generation script** (`scripts/gen-dev-certs.sh`):
Extract the OpenSSL commands from the `certs` service in `docker-compose.yml` into a standalone script that works both inside and outside the devcontainer. Output to a configurable directory (default `./certs/`).

**Files to create**: `scripts/dev-setup.sh`, `scripts/gen-dev-certs.sh`
**Files to modify**: `.devcontainer/devcontainer.json`, `internal/config/config.go`, `internal/admin/middleware.go`, `internal/admin/auth.go`, `docker-compose.yml`

---

#### Gap #10: Expose Admin UI on Kubernetes at cruvero-mcp-gateway.dev.gchinfo.com

**Problem**: The Helm chart has an ingress template (`charts/mcpgateway/templates/ingress.yaml`) and ingress values in `values.yaml`, but they are disabled by default (`ingress.enabled: false`) and `values-dev.yaml` does not override them. The `mcpgateway-dev` service is `ClusterIP` only — the admin dashboard is unreachable without `kubectl port-forward`.

The existing Cruvero deployments use a consistent pattern:
- **Ingress class**: `traefik`
- **Annotations**: `traefik.ingress.kubernetes.io/router.entrypoints: websecure` + `traefik.ingress.kubernetes.io/router.tls: "true"`
- **TLS**: cert-manager `Certificate` resource using the `letsencrypt-dns01` ClusterIssuer, with the secret referenced in the ingress `tls` block
- **Certificate DNS names**: Added to an existing or new cert-manager Certificate (the Cruvero chart bundles `cruvero.dev.gchinfo.com` and `cruvero-api.dev.gchinfo.com` on a single cert)

The mcpgateway chart already has a separate `certificate.yaml` template, but it's configured for the internal mTLS serving cert (Vault-issued, `vault-mcp-gateway-server` ClusterIssuer, cluster-local DNS names). The public ingress needs a **separate** Let's Encrypt certificate for `cruvero-mcp-gateway.dev.gchinfo.com`.

**Fix** (multi-part):

**Part A — Update ingress template** (`charts/mcpgateway/templates/ingress.yaml`):
The existing template routes all traffic (`path: /`) to port 8443. This is correct — the gateway serves both the mTLS API and the admin UI on the same port. The admin UI is at `/admin` and has its own auth middleware. No template changes are needed unless you want to restrict the ingress to only `/admin` (optional).

However, the gateway expects mTLS for API paths but the admin UI uses OIDC. Traefik terminates TLS at the ingress, so traffic from Traefik to the gateway pod arrives as HTTPS using the internal cert. This works because the Traefik backend protocol can be configured to trust the internal CA. Add an annotation for backend protocol:

```yaml
traefik.ingress.kubernetes.io/service.serversscheme: https
```

**Part B — Add ingress cert-manager Certificate** (`charts/mcpgateway/templates/ingress.yaml` or new template):
Add a second cert-manager Certificate (or extend ingress values) for the public hostname using `letsencrypt-dns01`:

```yaml
# Option 1: Use cert-manager annotation on the Ingress (simplest)
annotations:
  cert-manager.io/cluster-issuer: letsencrypt-dns01

# Option 2: Explicit Certificate resource (more control)
# Create charts/mcpgateway/templates/ingress-certificate.yaml
```

**Part C — Enable ingress in values-dev.yaml** (`charts/mcpgateway/values-dev.yaml`):
Add the following to `values-dev.yaml`:

```yaml
ingress:
  enabled: true
  className: traefik
  host: cruvero-mcp-gateway.dev.gchinfo.com
  tls: true
  tlsSecretName: mcpgateway-dev-ingress-tls
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
    traefik.ingress.kubernetes.io/service.serversscheme: https
    cert-manager.io/cluster-issuer: letsencrypt-dns01
```

**Part D — Update ArgoCD AppProject** (`deploy/argocd/project.yaml`):
The AppProject resource whitelist already includes `networking.k8s.io/Ingress` and `cert-manager.io/Certificate`, so no changes are needed there.

**Part E — Verify OIDC redirect URI**:
Once the ingress is live, the OIDC client in Keycloak must have `https://cruvero-mcp-gateway.dev.gchinfo.com/admin/callback` as an allowed redirect URI. This is an external configuration step — document it in the values file.

**Files to modify**: `charts/mcpgateway/values-dev.yaml`, `charts/mcpgateway/templates/ingress.yaml` (optional backend scheme annotation)
**External config**: Add redirect URI to Keycloak OIDC client for the admin dashboard

---

## Verification Steps

| Gap | Verification |
|-----|-------------|
| #1 ConfigStore wiring | Unit test: start DegradationManager with mock ConfigStore, assert `LoadCachedConfig()` loads data; integration test: start gateway with NATS down, verify readiness recovers from cached config |
| #2 NATS TLS | Unit test: `NewClient()` with `WithTLS()` option sets `nats.Secure()`; integration test: connect to NATS with TLS enabled, verify handshake |
| #3 ApplicationSet | `kubectl apply --dry-run=client -f deploy/argocd/applicationset.yaml` succeeds; verify 3 Application resources generated |
| #4 AppProject | `kubectl apply --dry-run=client -f deploy/argocd/project.yaml`; verify all 3 namespaces in destinations |
| #5 Image workflows | Trigger each workflow on the correct branch; verify images appear in Harbor with correct tags |
| #6 Dependabot | Merge to `dev`; verify Dependabot opens PRs within the configured schedule window |
| #7 Container scanning | Push a deliberately vulnerable image; verify Trivy step fails the workflow |
| #8 SonarQube | Run `sonar-scanner`; verify dashboard populates with coverage and code smell metrics |
| #9 Devcontainer & local UI | 1) Open fresh devcontainer — migrations should run automatically 2) Run `scripts/dev-setup.sh` — gateway starts with admin dev mode 3) Open `https://localhost:8443/admin/` in browser — dashboard loads without OIDC 4) Verify all admin pages render (tools, audit, servers, rate limits) 5) Confirm startup logs show dev mode warning |
| #10 K8s ingress for admin UI | 1) `helm template` with dev values — verify Ingress resource renders with correct host, class, annotations 2) Deploy to `cruvero-dev` — verify `kubectl get ingress -n cruvero-dev` shows `mcpgateway-dev` with host `cruvero-mcp-gateway.dev.gchinfo.com` 3) Verify cert-manager issues certificate — `kubectl get certificate -n cruvero-dev` shows Ready 4) `curl -sI https://cruvero-mcp-gateway.dev.gchinfo.com/admin/login` returns 200 or 302 to OIDC provider |

---

## Cross-Reference with Existing Gap Documents

| Document | Items Covered | Overlap with This Analysis |
|----------|---------------|---------------------------|
| `PRODUCTION-PLANS.md` | 4 P0, 10 P1, 5 P2 items | All 4 P0 items verified as FIXED; P1-5 (staging/prod ArgoCD), P1-6 (container scanning), P1-9 (NATS TLS) overlap with Gaps #2-7 here |
| `MISSING-FUNCTIONALITY.md` | 8 items (B1–B8) | B5 (Makefile migrate) and B8 (devcontainer Go version) already FIXED; B2 (SonarQube), B3 (ArgoCD), B4 (image workflows), B6 (Dependabot), B7 (scanning) overlap with Gaps #3-8 here; B1 (dev TLS cert script) is low priority and not included |
| `CRUVERO-REVIEW.md` | 6 P0, 3 P1, 1 P2 items | Focuses on Cruvero platform integration (NATS subject contract, event payloads, UI gaps, multi-tenant) — orthogonal to this phase-level parity analysis; these are integration-contract issues, not phase doc gaps |

**Items unique to `CRUVERO-REVIEW.md`** (not covered here because they are Cruvero platform integration issues, not phase implementation gaps):
- NATS subject naming contract mismatch
- Event payload schema incompatibility
- Missing `config.server_settings` publication scope
- Gateway integration nested inside `StartDiscovery()` (disabled in static mode)
- MCP UI missing `gateway_url` field
- Auth mode defaulting to `none` on startup publish
- Missing backend frontend panel/API module
- Hardcoded tenant `default`

These require coordinated changes between `cruvero-mcp-gateway` and the Cruvero platform — they are tracked separately in `CRUVERO-REVIEW.md`.

---

## Remediation Checklist

### P1 — Required for Staging/Production

- [ ] **Gap #1 — Wire ConfigStore into DegradationManager**
  - [ ] Instantiate `PostgresConfigStore` in `internal/server/server.go` after DB is available
  - [ ] Pass it to `NewDegradationManager()` at line 125 (NATS available path)
  - [ ] Pass it to `NewDegradationManager()` at line 143 (NATS unavailable path)
  - [ ] Call `LoadCachedConfig()` during startup when NATS is unavailable
  - [ ] Add unit test: DegradationManager with mock ConfigStore loads cached data
  - [ ] Verify readiness probe recovers from cached config when NATS is down

- [ ] **Gap #2 — Wire NATS TLS Configuration**
  - [ ] Add `NATSTLSCert`, `NATSTLSKey`, `NATSTLSCa` fields to `internal/config/config.go`
  - [ ] Parse `MCPGW_NATS_TLS_CERT`, `MCPGW_NATS_TLS_KEY`, `MCPGW_NATS_TLS_CA` env vars
  - [ ] Build `*tls.Config` from paths in `internal/server/server.go`
  - [ ] Pass `events.WithTLS(tlsCfg)` to `NewClient()` when TLS fields are set
  - [ ] Add config parsing tests for new env vars
  - [ ] Add unit test: `NewClient()` with `WithTLS()` sets `nats.Secure()`

- [ ] **Gap #3 — Extend ArgoCD ApplicationSet**
  - [ ] Add `staging` element (namespace: `cruvero-staging`, values: `values-staging.yaml`, revision: `main`)
  - [ ] Add `prod` element (namespace: `cruvero-prod`, values: `values-prod.yaml`, revision: `main`)
  - [ ] Update image updater patch to handle staging/prod tag patterns
  - [ ] Validate with `kubectl apply --dry-run=client`

- [ ] **Gap #4 — Extend ArgoCD AppProject Destinations**
  - [ ] Add `cruvero-staging` namespace to destinations
  - [ ] Add `cruvero-prod` namespace to destinations
  - [ ] Update project description from "dev deployment only" to multi-environment
  - [ ] Validate with `kubectl apply --dry-run=client`

- [ ] **Gap #5 — Staging/Production Image Build Workflows**
  - [ ] Decide approach: single parameterized workflow vs separate per-environment files
  - [ ] Create staging workflow (trigger: `main` branch push, tag: `staging-${SHA}`)
  - [ ] Create prod workflow (trigger: tag push `v*`, tag: `${TAG}` + `latest`)
  - [ ] Verify images appear in Harbor with correct tags

### P2 — Hardening and Maturity

- [ ] **Gap #6 — Dependabot Configuration**
  - [ ] Create `.github/dependabot.yml` with `gomod`, `github-actions`, and `docker` ecosystems
  - [ ] Set weekly schedule and PR limits
  - [ ] Verify Dependabot opens PRs after merge to `dev`

- [ ] **Gap #7 — Container Image Scanning**
  - [ ] Add Trivy scan step to `image-dev.yml` (after build, before push)
  - [ ] Add Trivy scan step to staging/prod workflows
  - [ ] Configure `exit-code: 1` for CRITICAL/HIGH severity
  - [ ] Verify scan fails on a known-vulnerable base image

- [ ] **Gap #8 — SonarQube Integration** *(optional)*
  - [ ] Create `sonar-project.properties` with source/test/coverage paths
  - [ ] Add `sonar-scanner` step to CI workflow
  - [ ] Verify dashboard populates with coverage and code smell metrics

### P1 — Developer Experience

- [ ] **Gap #9 — Functional Devcontainer with Local Admin UI Access**
  - [ ] **Part A: Dev setup script** (`scripts/dev-setup.sh`)
    - [ ] Generate self-signed TLS certs (CA + server + client) if not present
    - [ ] Wait for Postgres readiness (`pg_isready`)
    - [ ] Run database migrations (`go run ./cmd/mcpgw migrate`)
    - [ ] Generate random `MCPGW_ADMIN_SESSION_KEY` if not set
    - [ ] Seed demo data (tool classifications, test API key)
    - [ ] Build and start the gateway with dev-friendly env defaults
  - [ ] **Part B: Admin dev mode** (`MCPGW_ADMIN_DEV_MODE`)
    - [ ] Add `AdminDevMode` field to `internal/config/config.go`, parse `MCPGW_ADMIN_DEV_MODE`
    - [ ] Add dev-mode bypass in `AdminAuthMiddleware` (`internal/admin/middleware.go`) — inject fake `dev@localhost` session
    - [ ] Log startup warning when dev mode is active
    - [ ] Add safety guard: reject dev mode if `MCPGW_TLS_CA` points to a non-dev CA
    - [ ] Add tests for dev mode bypass and safety guard
  - [ ] **Part C: Devcontainer improvements** (`.devcontainer/devcontainer.json`)
    - [ ] Add `forwardPorts: [8443, 9090]`
    - [ ] Update `postCreateCommand` to run migrations and cert generation
  - [ ] **Part D: Cert generation script** (`scripts/gen-dev-certs.sh`)
    - [ ] Extract OpenSSL commands from `docker-compose.yml` `certs` service into standalone script
    - [ ] Support configurable output directory (default `./certs/`)
    - [ ] Make idempotent — skip generation if certs already exist
  - [ ] **Verification**
    - [ ] Open fresh devcontainer, confirm migrations run automatically
    - [ ] Run `scripts/dev-setup.sh`, confirm gateway starts
    - [ ] Open `https://localhost:8443/admin/` — dashboard loads without OIDC
    - [ ] Verify all admin pages render (tools, audit, servers, rate limits)
    - [ ] Confirm startup logs show dev mode warning

### P1 — Infrastructure

- [ ] **Gap #10 — Expose Admin UI on Kubernetes at cruvero-mcp-gateway.dev.gchinfo.com**
  - [ ] **Part A: Enable ingress in values-dev.yaml**
    - [ ] Add `ingress.enabled: true` with `className: traefik`
    - [ ] Set `host: cruvero-mcp-gateway.dev.gchinfo.com`
    - [ ] Add Traefik annotations: `router.entrypoints: websecure`, `router.tls: "true"`, `service.serversscheme: https`
    - [ ] Add cert-manager annotation: `cert-manager.io/cluster-issuer: letsencrypt-dns01`
    - [ ] Set `tlsSecretName: mcpgateway-dev-ingress-tls`
  - [ ] **Part B: Validate ingress template**
    - [ ] Run `helm template` with dev values — confirm Ingress resource renders correctly
    - [ ] Verify backend service name and port (8443) are correct
    - [ ] Verify TLS block includes correct host and secret name
  - [ ] **Part C: Deploy and verify**
    - [ ] ArgoCD sync or `helm upgrade` to deploy the ingress
    - [ ] `kubectl get ingress -n cruvero-dev` — confirm `mcpgateway-dev` with correct host
    - [ ] `kubectl get certificate -n cruvero-dev` — confirm cert-manager issues `mcpgateway-dev-ingress-tls` as Ready
    - [ ] `curl -sI https://cruvero-mcp-gateway.dev.gchinfo.com/admin/login` — returns 200 or 302 to OIDC
  - [ ] **Part D: OIDC redirect URI**
    - [ ] Add `https://cruvero-mcp-gateway.dev.gchinfo.com/admin/callback` as allowed redirect URI in Keycloak OIDC client
