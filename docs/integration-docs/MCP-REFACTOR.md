# MCP Refactor Playbook

This playbook standardizes how to migrate legacy MCP servers (stdio/local-only pattern) to the Cruvero gateway + platform model.

Scope: use this for each of the remaining MCP servers that currently look like `mcp-k8s` pre-refactor.

## 1. Target Architecture

Each MCP server must provide:

- Streamable HTTP MCP endpoint (`/mcp`)
- Health and readiness endpoints (`/healthz`, `/readyz`)
- Gateway lifecycle client with mTLS:
  - startup register with retry/backoff
  - heartbeat loop
  - auto re-register on drift/not-found/action requests
  - best-effort deregister on shutdown
- Env-only runtime config contract (no hardcoded credentials)
- Kubernetes deployment via Helm + Argo CD
- cert-manager issued gateway client certificate
- Optional Vault secret sync for provider credentials

## 2. Required Runtime Contract

## Required env vars

- `MCP_SERVER_NAME`
- `MCP_SERVER_VERSION`
- `MCP_SERVER_LISTEN_ADDR`
- `MCP_SERVER_MCP_PATH`
- `MCP_SERVER_PUBLIC_HOST`
- `MCP_SERVER_PUBLIC_PORT`
- `MCP_SERVER_PROTOCOL`
- `MCP_SERVER_ENVIRONMENT`
- `MCP_SERVER_TEAM`
- `MCP_SERVER_RISK_TIER`
- `MCP_SERVER_LOG_LEVEL`
- `MCP_SERVER_MAX_CONCURRENCY`
- `MCP_SERVER_TOOL_TIMEOUT`
- `MCP_GATEWAY_URL`
- `MCP_GATEWAY_HEARTBEAT_INTERVAL`
- `MCP_GATEWAY_REQUEST_TIMEOUT`
- `MCP_GATEWAY_TLS_CERT`
- `MCP_GATEWAY_TLS_KEY`
- `MCP_GATEWAY_TLS_CA`

Provider secrets are additional env vars and should come from Kubernetes Secret / VaultStaticSecret.

## Registration payload requirements

- `service_name`
- `version`
- `listen.host`
- `listen.port`
- `listen.protocol`
- `capabilities.tools` (must match actual server tool names)
- `labels.environment`
- `labels.team`
- `labels.risk_tier`
- `labels.provider`
- `labels.service_class=mcp-server`

## 3. Code Refactor Checklist (Per Server)

1. Add `gateway/client.go`
- Implement `Register`, `Heartbeat`, `Deregister`.
- Enforce mTLS cert/key/CA loading.
- Validate gateway URL.

2. Add `runtime.go`
- Start HTTP MCP transport.
- Register/retry before server is considered ready.
- Run heartbeat loop.
- On heartbeat failures or `re_register`/`resend_capabilities`, re-register.
- Set readiness from gateway status.

3. Replace stdio startup in `main.go`
- Keep existing tool handlers.
- Track registered tool names programmatically.
- Pass tool list into registration payload.
- Use signal-aware shutdown (`SIGINT`, `SIGTERM`).

4. Replace config model
- Validate all required env vars.
- Validate TLS file existence/readability.
- Validate URL and durations.
- Fail fast on invalid config.

5. Preserve and harden tooling behavior
- Keep tool names stable to avoid platform cache drift.
- Keep timeout + concurrency middleware.
- Add structured logs for tool execution and gateway lifecycle.

## 4. Helm Refactor Checklist (Per Server)

1. Create chart layout
- `charts/<server>/Chart.yaml`
- `charts/<server>/values.yaml`
- `charts/<server>/values-dev.yaml`
- templates: Deployment, Service, ConfigMap, ServiceAccount

2. Deployment template must include
- `envFrom` ConfigMap
- optional `envFrom` Secret for provider creds
- `MCP_SERVER_PUBLIC_HOST` and `MCP_SERVER_PUBLIC_PORT`
- mount gateway TLS secret at `/gateway-tls`
- liveness `/healthz` and readiness `/readyz`

3. Gateway client cert
- Add cert-manager `Certificate` template
- SPIFFE URI: `spiffe://<TRUST_DOMAIN>/ns/<namespace>/sa/<serviceaccount>`

4. Vault integration (optional per server)
- Add `VaultStaticSecret` template gated by `vault.enabled`
- Destination secret feeds provider env vars

5. RBAC
- Add least-privileged Role/ClusterRole needed by tool set.
- For cluster-wide servers, add ClusterRoleBinding to ServiceAccount.

## 5. Argo CD Refactor Checklist (Per Server)

1. `deploy/argocd/project.yaml`
- Restrict source repo and namespace destinations
- Whitelist required resource kinds

2. `deploy/argocd/applicationset.yaml`
- Define dev generator entry
- Point to chart path and values files
- Enable automated sync + self heal
- Add image updater annotations for dev

3. Image updater resources
- `image-updater-<server>-dev.yaml`
- `image-updater-rbac.yaml` for pull secret access

## 6. CI/CD Checklist (Per Server)

- Add CI workflow: `vet`, `test -race`, `lint`, `govulncheck`
- Add `image-dev.yml` to publish `:dev` and `:dev-<sha>`
- Keep release workflow for tagged binaries if required
- Ensure Dockerfile injects `main.version` via ldflags

## 7. Validation Checklist (Per Server)

1. Build/test
- `go test ./...`
- `helm template <server> charts/<server> -f values.yaml -f values-dev.yaml`

2. Runtime registration
- Pod is Ready
- Gateway shows server as registered and active
- Tool list visible in Cruvero tool catalog (not only MCP server detail)

3. Functional tool execution
- Run at least one read tool and one write tool (if server supports writes)
- Confirm result is persisted in provider system

4. Drift recovery
- Restart gateway and confirm server auto re-registers
- Simulate transient network failure and confirm recovery

## 8. Rollout Sequence (Recommended)

1. Implement code + manifests in a single server repo
2. Deploy dev only
3. Validate registration + tool execution + resilience
4. Capture server-specific deltas in a short appendix
5. Repeat for next server

## 9. Copy/Paste Starter Values Template

```yaml
config:
  MCP_SERVER_NAME: <server-name>
  MCP_SERVER_LISTEN_ADDR: ":8080"
  MCP_SERVER_MCP_PATH: "/mcp"
  MCP_SERVER_PROTOCOL: "http"
  MCP_SERVER_ENVIRONMENT: "dev"
  MCP_SERVER_TEAM: "platform"
  MCP_SERVER_RISK_TIER: "medium"
  MCP_SERVER_LOG_LEVEL: "INFO"
  MCP_SERVER_MAX_CONCURRENCY: "10"
  MCP_SERVER_TOOL_TIMEOUT: "30s"
  MCP_GATEWAY_URL: "https://mcpgateway-dev.cruvero-dev.svc:8443"
  MCP_GATEWAY_HEARTBEAT_INTERVAL: ""
  MCP_GATEWAY_REQUEST_TIMEOUT: "15s"
  MCP_GATEWAY_TLS_CERT: "/gateway-tls/tls.crt"
  MCP_GATEWAY_TLS_KEY: "/gateway-tls/tls.key"
  MCP_GATEWAY_TLS_CA: "/gateway-tls/ca.crt"
```

## 10. Common Failure Modes

- Tools visible in server detail but missing on tools page:
  - stale registry projection or missed capability sync; verify heartbeat + re-register path.
- `tool not found` for existing tool:
  - registration payload tool names differ from runtime tool names.
- `unauthorized` on tool execution:
  - provider credential missing or invalid secret mapping.
- server stays `degraded`:
  - heartbeat failures, mTLS cert mismatch, or RBAC/API auth failures.

## 11. Standards for All 16 Migrations

- Do not hardcode provider credentials.
- Do not depend on local kubeconfig in cluster runtime.
- Keep one canonical registration path and one canonical tool name set.
- Ship with probe endpoints and auto-recovery behavior by default.
- Validate in dev before promoting to higher environments.
