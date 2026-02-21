# MCP Server Fleet Integration Standard

## Purpose

This document defines the full set of changes required to migrate Go-based MCP servers (for example `mcp-todoist`) to a shared architecture that supports:

- Standalone local usage by default.
- Optional integration with `cruvero-mcp-gateway`.
- Kubernetes deployment with minimal containers.
- Vault-operator managed secrets.
- 12-factor compliance.
- Helm + Argo CD ApplicationSet GitOps rollout.

This is intentionally server-agnostic and can be reused across all MCP servers with the same architecture.

## Decisions Locked

- Gateway authentication: mTLS SPIFFE identity only.
- Default runtime mode: standalone (`stdio`).
- Fleet rollout strategy: shared reusable core module for common behavior.

## Current Baseline Pattern

Existing MCP servers (example: `mcp-todoist`) generally have:

- A single `main.go` with MCP server initialization and tool registration.
- Standalone `stdio` serving only.
- Provider token loaded from env.
- Distroless runtime image or near-minimal container.
- Tool middleware for timeout/logging/concurrency.

Missing for gateway integration:

- Runtime mode switch.
- HTTP serving path for gateway mode.
- Gateway registration + heartbeat lifecycle.
- Gateway-mode readiness semantics.
- Standardized env contract and deployment manifests.

## Target Runtime Architecture

### Modes

- `standalone` (default):
  - `stdio` MCP transport.
  - No gateway registration.
  - Local env-driven token usage.

- `gateway`:
  - HTTP MCP transport for backend routing via gateway.
  - Startup registration to gateway.
  - Periodic heartbeat check-in.
  - Best-effort deregistration on shutdown.
  - Health/readiness endpoints.

### Required startup behavior

1. Load config from env only.
2. Validate config based on mode.
3. Initialize logger + secret redaction.
4. Build tool catalog and MCP server.
5. Start transport:
   - standalone: stdio
   - gateway: HTTP
6. In gateway mode: register, then heartbeat loop, applying non-secret runtime settings from gateway responses.
7. On signal: graceful shutdown, stop heartbeat, deregister best effort.

## Standard Environment Contract

### Common (all servers)

- `MCP_SERVER_MODE` (`standalone` default, `gateway` optional)
- `MCP_SERVER_NAME` (default from binary/service name)
- `MCP_SERVER_VERSION` (ldflags-injected)
- `MCP_SERVER_LOG_LEVEL` (`INFO` default)
- `MCP_SERVER_LOG_FORMAT` (`json` default)
- `MCP_SERVER_MAX_CONCURRENCY` (`10` default)
- `MCP_SERVER_TOOL_TIMEOUT` (`30s` default)

Mode resolution order (must be identical in all servers):

1. If `MCP_GATEWAY_ENABLED=true`, effective mode is `gateway`.
2. Else if `MCP_SERVER_MODE` is set, use its value.
3. Else default to `standalone`.

If both are set and conflict, `MCP_GATEWAY_ENABLED=true` wins and a warning log is emitted.

### Gateway mode

- `MCP_GATEWAY_ENABLED` (`false` default)
- `MCP_GATEWAY_URL` (required in gateway mode)
- `MCP_GATEWAY_HEARTBEAT_INTERVAL` (optional override)
- `MCP_SERVER_LISTEN_ADDR` (required in gateway mode, e.g. `:8080`)
- `MCP_SERVER_PUBLIC_HOST` (required when listen host differs from routeable host)
- `MCP_SERVER_PUBLIC_PORT` (required when listen port differs from routeable port)
- `MCP_SERVER_PROTOCOL` (`http` default; `https` when server terminates TLS itself)

### Gateway mTLS

- `MCP_GATEWAY_TLS_CERT` (client cert path)
- `MCP_GATEWAY_TLS_KEY` (client key path)
- `MCP_GATEWAY_TLS_CA` (gateway CA bundle path)

Startup must validate:

- all three paths exist and are readable.
- certificate has URI SAN suitable for SPIFFE identity.

### Provider-specific secrets

Keep existing provider env names. Example:

- `TODOIST_API_TOKEN`

Rules:

- Local standalone: provider token via local env.
- Kubernetes: provider token via Vault-synced secret mapped to env var.
- No plaintext runtime secret manifests in git.

## Required Code Changes Per Server

## 1. Config layer

Refactor config into mode-aware structs and validation:

- `RuntimeConfig`
- `GatewayConfig`
- `ProviderConfig`
- `Config.ValidateByMode()`

Validation requirements:

- standalone: provider config required.
- gateway: provider + gateway URL + listen addr + mTLS paths required.
- fail fast with descriptive errors.

## 2. Transport layer

Introduce a transport abstraction:

- `RunStdio(ctx, mcpServer) error`
- `RunHTTP(ctx, mcpServer, listenAddr, tlsCfg?) error`

Runtime selects transport by mode.

## 3. Gateway lifecycle client

Add reusable gateway client package with:

- `Register(ctx, req) (resp, error)`
- `Heartbeat(ctx, instanceID) error`
- `Deregister(ctx, instanceID) error`

Payloads align to gateway registration contract:

- request fields: `service_name`, `version`, `listen`, `capabilities`, `labels`
- response fields:
  - `instance_id`
  - `policy_snapshot`
  - `heartbeat_interval_seconds`
  - `status`
  - `config_version`
  - `effective_settings`
  - `settings_profile` (optional)

Loop behavior:

- startup registration retries with backoff.
- heartbeat interval from response unless overridden.
- settings refresh and apply on registration response and every heartbeat response.
- best-effort deregister on shutdown.

### Registration payload template (required shape)

```json
{
  "service_name": "mcp-todoist",
  "version": "1.2.3",
  "listen": {
    "host": "mcp-todoist.default.svc",
    "port": 8080,
    "protocol": "http"
  },
  "capabilities": {
    "tools": [],
    "resources": [],
    "prompts": []
  },
  "labels": {
    "environment": "dev",
    "team": "platform",
    "risk_tier": "medium",
    "provider": "todoist"
  }
}
```

Standard labels required for all servers:

- `environment`
- `team`
- `risk_tier`
- `provider`
- `service_class` (for example `mcp-server`)

## 3A. Cruvero Registry Non-Secret Settings Control

In gateway mode, Cruvero manages non-secret runtime settings through the gateway registry and config sync pipeline.

### Data flow

1. Cruvero updates MCP server non-secret settings.
2. Cruvero publishes update on `mcpgw.config.server_settings`.
3. Gateway validates, versions, persists, and computes effective per-server settings.
4. Gateway returns effective settings in registration and heartbeat responses.
5. Server applies valid hot-reload-safe keys on next heartbeat cycle.

### Authority and precedence

Gateway mode precedence:

1. Cruvero effective settings (authoritative)
2. Local env boot defaults (fallback only)

Standalone mode precedence:

1. Local env only

### Runtime setting classes

Hot-reload-safe keys (apply without restart):

- log level / verbosity
- tool timeout
- max concurrency
- non-secret tool exposure flags
- server metadata labels

Restart-required keys (not hot-reloaded in this phase):

- listen address / protocol
- TLS file paths
- provider endpoint transport-level changes

### Apply algorithm (server side)

1. Compare `config_version` with `last_applied_config_version`.
2. If unchanged: no-op.
3. Validate all incoming keys against known schema and class policy.
4. If valid:
   - apply hot-reload-safe keys atomically
   - persist `last_known_good_settings`
   - set `last_applied_config_version`
5. If invalid:
   - reject update
   - keep `last_known_good_settings`
   - report error status on next heartbeat

### Invalid config behavior

- Reject + keep last-known-good (no partial apply).
- Emit structured error logs and metrics.
- Continue serving traffic.

### Propagation SLA

- Settings take effect on next successful heartbeat response.

## 4. Capability export

Add capability exporter that derives registration payload from server tool definitions:

- export all tool names + descriptions + argument schemas.
- enforce tool name uniqueness inside one server.
- include resources/prompts if implemented.

## 5. Health/readiness behavior

Gateway mode HTTP server must expose:

- `/healthz` (process live)
- `/readyz` (ready only after successful registration and current heartbeat state is healthy)

Standalone mode does not require these endpoints unless HTTP mode is manually enabled.

## 6. Logging and redaction

Structured logs must include:

- `service_name`, `service_version`, `mode`
- `request_id`, `tool`, `duration_ms`
- registration lifecycle events (`register`, `heartbeat`, `deregister`)

Redaction list must include provider secrets and gateway TLS-sensitive values if logged.

## 7. Graceful shutdown

On SIGINT/SIGTERM:

1. stop accepting new work.
2. stop heartbeat goroutine.
3. call deregister (best effort, bounded timeout).
4. close server cleanly.

Shutdown time budget standard:

- total shutdown budget: 15 seconds.
- deregistration budget within shutdown: 3 seconds max.

## Shared Core Module Requirements

Create one shared Go module used by all MCP server repos:

- mode-aware config helpers
- transport runner
- gateway lifecycle client
- heartbeat scheduler with retries
- settings sync/apply manager (version tracking + validation + last-known-good state)
- shared health/readiness primitives
- shared logging/redaction helpers

Each server repo keeps:

- tool definitions and handlers
- provider API client and provider-specific validation
- provider-specific tests

## 12-Factor Compliance Requirements

All servers must satisfy:

- Config strictly from env.
- No runtime dependence on local files except explicit mounted cert/secret paths.
- Logs only to stdout/stderr.
- Stateless process.
- Build/release/run separation (immutable image + runtime env injection).
- Dev/prod parity via same binary + mode/env toggles.
- Secrets injected at runtime; never baked into image.
- Fast startup/shutdown and disposability.

Explicitly prohibited:

- mode-specific code paths requiring source changes per environment.
- secrets committed to `.env` files in repository history.
- writing runtime mutable state to local disk for coordination.
- secret values propagated through non-secret settings channels.

## Container Standard (Bare-Minimum K8s)

Use a standard multi-stage build:

1. Builder image (`golang:<version>-alpine` or pinned equivalent).
2. `CGO_ENABLED=0`, static binary, `-trimpath -ldflags "-s -w"`.
3. Runtime image: distroless static nonroot.

Runtime constraints:

- `USER nonroot:nonroot`
- read-only root filesystem compatible
- no shell required
- mount certs and runtime secrets as files/env only

## Helm Standard (Per Server Repo)

Each server gets chart at `charts/<server-name>/` with:

- `values.yaml` base.
- `values-dev.yaml`, `values-staging.yaml`, `values-prod.yaml`.
- Deployment/Service (+ optional HPA/PDB/NetworkPolicy).
- Env wiring for mode and gateway settings.
- Vault templates:
  - `vault-auth.yaml` (optional)
  - `vault-secrets.yaml` (`VaultStaticSecret`)
  - secret consumption via `secrets.existingSecret`
- Optional `validate-secrets.yaml` template guard.

Hard rule:

- No committed plaintext runtime `Secret` template containing provider tokens.

Required values keys (shared schema across all server charts):

- `mode`
- `gateway.enabled`
- `gateway.url`
- `gateway.tls.certPath`
- `gateway.tls.keyPath`
- `gateway.tls.caPath`
- `secrets.existingSecret`
- `vault.enabled`
- `vault.authRef`
- `vault.mount`
- `vault.path`
- `vault.refreshAfter`

## Argo CD ApplicationSet Standard

Each server repo gets `deploy/argocd/`:

- `project.yaml` (AppProject boundaries).
- `applicationset.yaml` with list generator fields:
  - `env`, `namespace`, `valuesFile`, `targetRevision`, `autoSync`, `prune`, `selfHeal`

Policy:

- dev auto-sync enabled by default.
- staging/prod gated by sync policy windows or explicit manual controls.
- `ignoreDifferences` added only for known mutable fields.

Required list generator fields for consistency:

- `env`
- `namespace`
- `valuesFile`
- `targetRevision`
- `autoSync`
- `prune`
- `selfHeal`

Default rollout policy:

- dev: `autoSync=true`, `prune=true`, `selfHeal=true`
- staging: gated/manual
- prod: gated/manual

## Gateway Config Subject Contract (Cruvero -> Gateway)

Use `mcpgw.config.server_settings` for non-secret MCP server runtime settings.

Expected payload shape:

```json
{
  "service_name": "mcp-todoist",
  "environment": "dev",
  "version": "42",
  "settings": {
    "log_level": "DEBUG",
    "tool_timeout": "20s",
    "max_concurrency": 8
  },
  "updated_by": "user_or_service",
  "updated_at": "2026-02-17T12:00:00Z"
}
```

Optional per-instance override key:

- `spiffe_id`

## File-Level Change Template (Per Server Repo)

Minimum expected additions/updates:

- `main.go` (mode switch + lifecycle wiring)
- `config/config.go` + tests (mode-aware config)
- `internal/gatewayclient/*` (or shared module usage)
- `internal/runtime/*` (transport + lifecycle runner)
- `internal/health/*` (gateway-mode readiness)
- `Dockerfile` (standardized minimal image)
- `charts/<server-name>/*` (Helm)
- `deploy/argocd/project.yaml`
- `deploy/argocd/applicationset.yaml`
- `README.md` (standalone vs gateway configuration docs)

## Test Matrix (Required for Every Server)

## Unit

- config validation by mode.
- registration request generation from tools.
- heartbeat scheduler interval/retry behavior.
- redaction of provider secret in logs.
- runtime settings validation and version comparison.
- reject-invalid/keep-last-known-good behavior.

## Integration

- standalone mode tool invocation via stdio.
- gateway mode registration success + heartbeat loop.
- gateway mode startup retry when gateway unavailable.
- graceful shutdown with best-effort deregistration.
- mTLS failure scenarios (bad CA, missing cert, invalid URI SAN).
- settings update applies on next heartbeat.
- invalid settings update rejected and status reported.

## Container/K8s

- image runs non-root.
- read-only root filesystem compatibility.
- provider token loaded via env from secret.
- health/readiness behavior in gateway mode.

## Helm/GitOps

- `helm lint` passes.
- `helm template` passes for all environment overlays.
- ApplicationSet renders expected applications.
- dev sync policy and staging/prod gating validated.
- non-secret settings profile values render correctly in gateway mode overlays.

## CI gate (minimum)

- `go test ./...`
- `go vet ./...`
- `golangci-lint run ./...`
- `helm lint charts/<server-name>`
- `helm template charts/<server-name> -f charts/<server-name>/values.yaml -f charts/<server-name>/values-dev.yaml`

## Fleet Rollout Plan (20 Servers)

1. Build shared core module.
2. Pilot migration on one server (`mcp-todoist`).
3. Extract any remaining reusable pieces after pilot.
4. Migrate remaining servers in batches of 3-5.
5. Enable dev Argo apps first for each migrated server.
6. Validate operational stability and heartbeat/registration behavior.
7. Promote staged environments after acceptance.

## Acceptance Criteria

A server migration is complete only when:

- standalone mode still works with current local clients.
- gateway mode registers and heartbeats reliably using mTLS.
- server can run in minimal non-root container.
- secrets come from runtime env (Vault in k8s, local env in standalone).
- Helm + Argo manifests render and deploy successfully.
- automated tests cover mode switching and gateway lifecycle.
- no plaintext runtime secrets committed.
- gateway mode applies Cruvero non-secret settings with versioned, validated, last-known-good behavior.

## Operational Runbook (Required)

Each server repo must include an operator section in `README.md` that covers:

1. Standalone startup example.
2. Gateway mode startup example with required env vars.
3. Registration troubleshooting:
   - cert/CA mismatch
   - heartbeat failures
   - gateway unreachable
   - settings version mismatch / rejected update
4. Rollback:
   - disable gateway mode via env
   - redeploy last known good image/chart revision

## Per-Server Migration Checklist

Use this checklist per repo:

- [ ] Add mode-aware config and env docs
- [ ] Add transport abstraction
- [ ] Add gateway registration client integration
- [ ] Add heartbeat loop and shutdown deregister
- [ ] Add non-secret settings apply manager (versioned + last-known-good)
- [ ] Add gateway-mode health/readiness endpoints
- [ ] Standardize logging + redaction
- [ ] Update Dockerfile to minimal standard
- [ ] Add Helm chart with env overlays
- [ ] Add Vault operator secret wiring
- [ ] Add Argo AppProject/ApplicationSet manifests
- [ ] Add/extend unit + integration tests
- [ ] Validate standalone flow still unchanged by default
- [ ] Validate gateway mode end-to-end in dev
