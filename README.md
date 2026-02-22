# cruvero-mcp-gateway

[![CI](https://github.com/cruvero/cruvero-mcp-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/cruvero/cruvero-mcp-gateway/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/cruvero/cruvero-mcp-gateway)](go.mod)


A pure-Go mTLS reverse proxy that acts as a unified gateway for MCP (Model Context Protocol) servers on Kubernetes. The gateway auto-discovers backend MCP servers via a registration handshake, aggregates their tool catalogs into a single MCP-compliant endpoint, and proxies requests with rate limiting, circuit breakers, and per-tool policy enforcement. Clients connect to one gateway instead of managing individual backends.

The gateway operates in two modes: **standalone** with only PostgreSQL as a dependency, or **Cruvero-integrated** where a NATS event bus synchronizes state with the Cruvero control plane. Both modes use the same binary and configuration surface -- Cruvero features activate only when `MCPGW_CRUVERO_ENABLED=true`.

## Key Features

**Core**
- MCP-native reverse proxy with unified tool catalog aggregation
- Auto-registration with heartbeat keepalive and server state machine
- Federated tool routing (`mcp.<server>.<tool>`) with round-robin load balancing
- Resource routing by URI prefix across backends

**Security**
- mTLS with SPIFFE identity extraction
- Dual auth: API keys (CLI/headless) + OIDC JWT (service-to-service)
- OAuth2 Device Code flow for IDE authentication
- Per-tool allow/deny policies with risk classification (read_only, write, destructive)
- SSRF-protected server registration
- Security headers, constant-time CSRF protection, AES-GCM encrypted sessions

**Resilience**
- Token-bucket rate limiting with three backends (memory, DragonflyDB, NATS)
- Circuit breakers with configurable failure threshold and recovery timeout
- Automatic retry with backoff on transient failures
- Graceful degradation when NATS is unavailable

**Operations**
- Admin dashboard (HTMX web UI) for server, tool, audit, and rate limit management
- Prometheus metrics + OpenTelemetry distributed tracing
- Helm chart with four environment overlays (base, dev, staging, prod)
- ArgoCD GitOps deployment with ApplicationSet
- Single static binary, distroless nonroot container image
- Audit log with configurable retention and CSV export

**Integration**
- Standalone mode (Postgres only) or Cruvero platform integration (NATS event bus)
- stdio-to-HTTP bridge (`mcpgw mcp-proxy`) for IDE integration
- Device Code flow for browser-based CLI/IDE authentication

## Architecture Overview

```
┌─────────────┐   ┌─────────────┐   ┌─────────────┐
│  Claude Code │   │  Cruvero UI │   │  Other MCP  │
│    (IDE)     │   │  (Platform) │   │   Clients   │
└──────┬───────┘   └──────┬───────┘   └──────┬───────┘
       │ stdio            │ HTTPS            │ HTTPS
       ▼                  ▼                  ▼
┌──────────────────────────────────────────────────┐
│                  MCP Gateway                      │
│  ┌──────────────────────────────────────────┐    │
│  │  mTLS → Identity → Auth → Rate Limit →   │    │
│  │  Policy → Circuit Breaker → Proxy/Route   │    │
│  └──────────────────────────────────────────┘    │
│  ┌────────────┐  ┌───────────┐  ┌────────────┐  │
│  │  Postgres   │  │   NATS    │  │ Prometheus  │  │
│  │  (required) │  │ (optional)│  │  + OTel     │  │
│  └────────────┘  └───────────┘  └────────────┘  │
└──────┬──────────────┬──────────────┬─────────────┘
       │              │              │
       ▼              ▼              ▼
┌────────────┐ ┌────────────┐ ┌────────────┐
│ MCP Server │ │ MCP Server │ │ MCP Server │
│ (SonarQube)│ │ (K8s)      │ │ (GitHub)   │
└────────────┘ └────────────┘ └────────────┘
```

For the full architecture reference including sequence diagrams, protocol details, and database schema, see [docs/OVERVIEW.md](docs/OVERVIEW.md).

## Prerequisites

| Component | Required | Purpose | Notes |
|-----------|----------|---------|-------|
| Go 1.25.7 | Build only | Compile from source | Not needed for container deployment |
| PostgreSQL 16+ | **Yes** | Server registry, API keys, audit log, config | Single required runtime dependency |
| Vault + Vault Operator | K8s only | Secret management (DB creds, TLS keys, session keys) | `VaultAuth` + `VaultStaticSecret` resources |
| cert-manager | K8s only | TLS certificate provisioning | Gateway server cert + client CA |
| NATS JetStream | Cruvero mode | Event bus for control plane sync | Not needed in standalone mode |
| DragonflyDB | Optional | Distributed rate limiting backend | Redis-compatible; alternative to memory or NATS backends |
| OTel Collector | Optional | Distributed tracing | Receives OTLP gRPC/HTTP exports |
| Prometheus | Optional | Metrics collection | Scrapes `/metrics` on metrics port (default `:9090`) |

## Quick Start

```bash
# 1. Build
go build -o mcpgw ./cmd/mcpgw

# 2. Start Postgres and run migrations
export MCPGW_DB_URL="postgres://user:pass@localhost:5432/mcpgw?sslmode=disable"
./mcpgw migrate

# 3. Generate TLS certificates (development only)
./scripts/gen-dev-certs.sh

# 4. Start the gateway
export MCPGW_TLS_CERT=certs/server.crt
export MCPGW_TLS_KEY=certs/server.key
export MCPGW_TLS_CA=certs/ca.crt
./mcpgw serve
```

Verify the gateway is running:

```bash
./mcpgw health --url https://localhost:8443
```

The dev certificate script generates a CA, server certificate (with localhost SANs), and a client certificate with a SPIFFE identity for testing.

## Kubernetes Deployment

### Helm Chart

The Helm chart is in `charts/mcpgateway/`. Install with environment-specific values:

```bash
# Dev
helm install mcpgateway charts/mcpgateway -n cruvero-dev \
  -f charts/mcpgateway/values.yaml \
  -f charts/mcpgateway/values-dev.yaml

# Staging
helm install mcpgateway charts/mcpgateway -n cruvero-staging \
  -f charts/mcpgateway/values.yaml \
  -f charts/mcpgateway/values-staging.yaml

# Production
helm install mcpgateway charts/mcpgateway -n cruvero-prod \
  -f charts/mcpgateway/values.yaml \
  -f charts/mcpgateway/values-prod.yaml
```

### Environment Overlays

| File | Replicas | Autoscaling | Rate Limit Backend | Image Tag | Key Differences |
|------|----------|-------------|-------------------|-----------|-----------------|
| `values.yaml` (base) | 2 | 2–10 | memory | latest | Base defaults |
| `values-dev.yaml` | 1 | disabled | memory | dev | Tracing enabled, ingress enabled |
| `values-staging.yaml` | 2 | 2–5 | memory | staging-latest | ServiceMonitor + PrometheusRule |
| `values-prod.yaml` | 3 | 3–20 | dragonfly | v1.0.0 | DragonflyDB, NATS TLS, increased resources |

### Secret Management

Secrets are managed via HashiCorp Vault Operator:

1. **VaultAuth** -- authenticates the gateway ServiceAccount with Vault
2. **VaultStaticSecret** -- syncs secrets (DB URL, TLS keys, session key, NATS creds) to a Kubernetes Secret
3. **Helm reference** -- `secrets.existingSecret` in values points to the synced Secret name

The Helm chart renders `vault-auth.yaml` and `vault-secrets.yaml` templates when `vault.enabled=true`.

### TLS with cert-manager

When `tls.certManager.enabled=true`, the chart creates a `Certificate` resource:

- **Issuer**: configurable `ClusterIssuer` (e.g. `vault-mcp-gateway-server`)
- **DNS names**: service FQDN within the cluster namespace
- **Secret**: `tls.secretName` (default: `mcpgw-tls`)

Client CA for mTLS verification is provided separately via Vault or manual Secret.

### GitOps with ArgoCD

The `deploy/argocd/` directory contains:

| File | Purpose |
|------|---------|
| `project.yaml` | AppProject scoping allowed namespaces and resource types |
| `applicationset.yaml` | ApplicationSet with list generator for dev, staging, prod |
| `image-updater-mcpgateway-dev.yaml` | ArgoCD Image Updater config for dev auto-deploy |
| `image-updater-rbac.yaml` | RBAC for Image Updater to read Harbor pull secrets |

Sync policies:
- **Dev**: automated sync with prune and self-heal, image updater auto-deploys on push to `dev`
- **Staging**: automated sync, tracks `main` branch, image updater with `staging-*` tag filter
- **Prod**: manual sync (automated sync disabled), tracks `main` branch with explicit version tags

### Security Posture

- **Image**: `distroless/static-debian12:nonroot` -- no shell, no package manager
- **SecurityContext**: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, all capabilities dropped
- **NetworkPolicy**: default-deny with explicit ingress/egress rules for Postgres, NATS, OTel, DNS, and backend servers
- **PodDisruptionBudget**: `minAvailable: 1` (base), scales with environment
- **HPA**: CPU-based autoscaling (target 70%)
- **Trivy scanning**: CI pipeline scans images for CRITICAL and HIGH CVEs before push

## CLI Reference

| Command | Description |
|---------|-------------|
| `mcpgw serve` | Start the gateway server (default command) |
| `mcpgw migrate` | Run database migrations |
| `mcpgw server list` | List registered MCP servers |
| `mcpgw server inspect <id-or-name>` | Show server details |
| `mcpgw server deregister <id-or-name>` | Remove a server |
| `mcpgw apikey create` | Create a new API key |
| `mcpgw apikey list` | List API keys |
| `mcpgw apikey revoke <id>` | Revoke an API key |
| `mcpgw policy list` | List policy profiles |
| `mcpgw policy set <name>` | Update a policy profile |
| `mcpgw policy reset <name>` | Reset a policy to defaults |
| `mcpgw tool list` | List known tools |
| `mcpgw tool classify <name>` | Set tool risk classification |
| `mcpgw tool auto-classify` | Auto-classify tools by pattern |
| `mcpgw auth login` | Device Code flow login |
| `mcpgw auth status` | Check auth status |
| `mcpgw auth logout` | Clear stored tokens |
| `mcpgw mcp-proxy` | stdio-to-HTTP bridge for IDEs |
| `mcpgw health` | Check gateway health |
| `mcpgw version` | Print build version |

Global flags: `--log-level`, `--log-format`, `--config` (reserved for future use; currently no config file is loaded)

Default policy profiles: `default` (10 req/s, burst 20), `premium` (50 req/s, burst 100), `admin` (100 req/s, burst 200).

## Configuration

All configuration is via environment variables with the `MCPGW_` prefix. No config files.

### Core

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_DB_URL` | -- | **Yes** | Postgres connection string |
| `MCPGW_LISTEN_ADDR` | `:8443` | No | Server listen address |
| `MCPGW_TLS_CERT` | -- | No | Server TLS certificate path (must pair with TLS_KEY) |
| `MCPGW_TLS_KEY` | -- | No | Server TLS private key path (must pair with TLS_CERT) |
| `MCPGW_TLS_CA` | -- | Yes (with TLS) | Client CA bundle for TLS/mTLS verification; required when `MCPGW_TLS_CERT` and `MCPGW_TLS_KEY` are set |
| `MCPGW_DB_MAX_OPEN_CONNS` | `25` | No | Max open database connections |
| `MCPGW_DB_MAX_IDLE_CONNS` | `10` | No | Max idle database connections |
| `MCPGW_DB_CONN_MAX_LIFETIME` | `5m` | No | Max connection lifetime |
| `MCPGW_SHUTDOWN_TIMEOUT` | `30s` | No | Graceful shutdown timeout (max 120s) |

### Authentication

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_OIDC_ISSUER` | -- | No | OIDC issuer URL |
| `MCPGW_OIDC_AUDIENCE` | -- | No | OIDC expected audience |
| `MCPGW_SPIFFE_ALLOW_PREFIX` | -- | No | Comma-separated allowed SPIFFE ID prefixes |

### Device Code Flow

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_DEVICE_FLOW_ENABLED` | `false` | No | Enable OAuth2 Device Code flow |
| `MCPGW_DEVICE_FLOW_IDP_DEVICE_URL` | -- | If device flow | IdP device authorization endpoint |
| `MCPGW_DEVICE_FLOW_IDP_TOKEN_URL` | -- | If device flow | IdP token endpoint |
| `MCPGW_DEVICE_FLOW_CLIENT_ID` | -- | If device flow | OAuth2 client ID |
| `MCPGW_DEVICE_FLOW_CLIENT_SECRET` | -- | No | OAuth2 client secret (optional) |

### Admin Dashboard

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_ADMIN_ENABLED` | `false` | No | Enable admin dashboard |
| `MCPGW_ADMIN_OIDC_CLIENT_ID` | -- | If admin (non-dev) | OIDC client ID for admin auth |
| `MCPGW_ADMIN_OIDC_CLIENT_SECRET` | -- | No | OIDC client secret for admin auth |
| `MCPGW_ADMIN_REQUIRED_SCOPE` | `admin` | No | Required OIDC scope for admin access |
| `MCPGW_ADMIN_SESSION_KEY` | -- | If admin (non-dev) | 64-char hex string (32-byte AES-256-GCM key) |
| `MCPGW_ADMIN_SESSION_TTL` | `8h` | No | Admin session duration |
| `MCPGW_ADMIN_DEV_MODE` | `false` | No | Bypass auth for local development (requires admin enabled) |

### Rate Limiting

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_RATE_DEFAULT` | `10` | No | Default requests/sec per client |
| `MCPGW_RATE_BURST` | `20` | No | Default burst token bucket size |
| `MCPGW_RATE_LIMIT_BACKEND` | `memory` | No | Backend: `memory`, `dragonfly`, or `nats` |
| `MCPGW_DRAGONFLY_URL` | -- | If dragonfly | DragonflyDB connection URL |

### Resilience

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_CIRCUIT_THRESHOLD` | `5` | No | Failures before circuit opens |
| `MCPGW_CIRCUIT_TIMEOUT` | `30s` | No | Circuit breaker recovery timeout |
| `MCPGW_RETRY_MAX` | `3` | No | Max retry attempts |
| `MCPGW_HEARTBEAT_TTL` | `30s` | No | Server heartbeat timeout |

### Cruvero Integration

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_CRUVERO_ENABLED` | `false` | No | Enable Cruvero platform integration |
| `MCPGW_GATEWAY_ID` | `auto` | No | Gateway instance ID (`auto` = random UUID) |
| `MCPGW_NATS_URL` | -- | If Cruvero | NATS server URL |
| `MCPGW_NATS_TLS_ENABLED` | `false` | No | Enable mTLS for NATS connections |
| `MCPGW_NATS_TLS_CERT` | -- | If NATS TLS | NATS client certificate path |
| `MCPGW_NATS_TLS_KEY` | -- | If NATS TLS | NATS client private key path |
| `MCPGW_NATS_TLS_CA` | -- | If NATS TLS | NATS CA certificate path |

### Observability

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_LOG_FORMAT` | `json` | No | Log format: `json` or `text` |
| `MCPGW_LOG_LEVEL` | `info` | No | Log level: `debug`, `info`, `warn`, `error` |
| `MCPGW_METRICS_ADDR` | `:9090` | No | Prometheus metrics listen address |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | -- | No | OpenTelemetry collector endpoint |
| `OTEL_SERVICE_NAME` | `mcpgw` | No | OpenTelemetry service name |

### Operations

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_CORS_ENABLED` | `false` | No | Enable CORS headers |
| `MCPGW_CORS_ALLOWED_ORIGINS` | -- | If CORS | Comma-separated allowed origins |
| `MCPGW_AUDIT_RETENTION_DAYS` | `90` | No | Days to retain audit log entries |
| `MCPGW_AUDIT_CLEANUP_INTERVAL` | `1h` | No | Interval between retention cleanup runs |

## Authentication

The gateway supports three authentication methods, evaluated in order:

1. **mTLS with SPIFFE** -- if the request presents a verified client certificate with a SPIFFE URI SAN, the gateway extracts the SPIFFE identity. Allowed prefixes are configured via `MCPGW_SPIFFE_ALLOW_PREFIX`.

2. **API Keys** -- clients send `X-API-Key` or `Authorization: Bearer <key>` headers. Keys are stored with SHA-256 lookup hashes and verified with bcrypt. Create and manage keys via `mcpgw apikey` CLI commands.

3. **OIDC JWT** -- Bearer tokens with three dot-separated segments are validated against the configured OIDC issuer. Used for service-to-service authentication.

### Device Code Flow

For IDE and CLI environments where browser-based login is preferred over static API keys:

```bash
mcpgw auth login --gateway-url https://gateway.example.com
```

The gateway presents a verification URL and code. After browser authorization, the CLI receives tokens and stores them locally with automatic refresh. See [IDE Integration](#ide-integration) for full setup.

## Admin Dashboard

Enable the admin dashboard with `MCPGW_ADMIN_ENABLED=true`. The dashboard provides:

- **Server management** -- view registered servers, deregister unhealthy backends
- **Tool classification** -- search, filter, and set risk levels (read_only, write, destructive)
- **Audit log viewer** -- paginated logs with filters by client, tool, decision, and date range
- **CSV export** -- export up to 10,000 audit records with CSV injection prevention
- **Rate limit inspection** -- view current rate limit state (when backend supports inspection)

### Production Setup

The dashboard requires OIDC authentication in production:

```bash
export MCPGW_ADMIN_ENABLED=true
export MCPGW_ADMIN_OIDC_CLIENT_ID=mcpgw-admin
export MCPGW_ADMIN_OIDC_CLIENT_SECRET=<secret>
export MCPGW_ADMIN_REQUIRED_SCOPE=admin
export MCPGW_ADMIN_SESSION_KEY=$(openssl rand -hex 32)  # 64-char hex string
export MCPGW_OIDC_ISSUER=https://idp.example.com
```

### Development Mode

For local development, bypass OIDC authentication:

```bash
export MCPGW_ADMIN_ENABLED=true
export MCPGW_ADMIN_DEV_MODE=true
```

## Rate Limiting

Token-bucket rate limiting with three interchangeable backends:

| Backend | Config Value | Dependency | Use Case |
|---------|-------------|------------|----------|
| Memory | `memory` (default) | None | Single-instance deployments |
| DragonflyDB | `dragonfly` | DragonflyDB (Redis-compatible) | Multi-replica coordination in production |
| NATS | `nats` | NATS JetStream | Environments already running NATS |

Set via `MCPGW_RATE_LIMIT_BACKEND`. DragonflyDB additionally requires `MCPGW_DRAGONFLY_URL`. The NATS backend requires `MCPGW_CRUVERO_ENABLED=true`.

Both DragonflyDB and NATS backends fall back to in-memory limiting when their backing service is unavailable. Policy profiles control per-client rate and burst limits -- manage profiles via `mcpgw policy set`.

## Observability

### Prometheus Metrics

The gateway exposes metrics on `MCPGW_METRICS_ADDR` (default `:9090`). The Helm chart includes optional `ServiceMonitor` and `PrometheusRule` resources (enable via `monitoring.serviceMonitor.enabled` and `monitoring.prometheusRule.enabled`).

### OpenTelemetry Tracing

Configure distributed tracing by setting `OTEL_EXPORTER_OTLP_ENDPOINT` to your collector address. The Helm chart can deploy a dedicated OTel Collector Deployment/Service and a Tempo instance when `tracing.enabled=true`.

### Structured Logging

All logs use Go's `slog` package. Configure format (`json`/`text`) via `MCPGW_LOG_FORMAT` and level via `MCPGW_LOG_LEVEL`.

## IDE Integration

The gateway supports two authentication modes for IDE integration: Device Code flow (browser-based) and API key (static credential).

### Device Code Flow (recommended)

Authenticate via browser -- no API key management required:

```bash
# One-time login
mcpgw auth login --gateway-url https://gateway.example.com

# Check auth status
mcpgw auth status

# Logout
mcpgw auth logout
```

IDE MCP server configuration (e.g. Claude Code `mcp_servers.json`):

```json
{
  "mcpServers": {
    "gateway": {
      "command": "mcpgw",
      "args": ["mcp-proxy", "--gateway-url", "https://gateway.example.com"]
    }
  }
}
```

The `mcp-proxy` command reads stdin JSON-RPC, forwards to the gateway with automatic token refresh, and writes responses to stdout.

### Federated Tool Routing

The gateway federates all registered MCP servers behind a single endpoint. You do **not** need a separate IDE entry per backend -- one gateway entry exposes every active server's tools automatically.

Tools are namespaced using the pattern `mcp.<server-name>.<tool-name>`. When the IDE calls a federated tool, the gateway extracts the server name, looks up which backend exposes it, and forwards the request.

**Example**: With SonarQube, Kubernetes, Argo CD, and GitHub MCP servers registered on the gateway, the single config above gives the IDE access to all of their tools:

| Federated Tool Name | Server | Description |
|----------------------|--------|-------------|
| `mcp.sonarqube.search_issues` | SonarQube | Search for code quality issues |
| `mcp.sonarqube.get_quality_gate` | SonarQube | Get project quality gate status |
| `mcp.k8s.list_pods` | Kubernetes | List pods in a namespace |
| `mcp.k8s.get_logs` | Kubernetes | Stream container logs |
| `mcp.argocd.list_applications` | Argo CD | List Argo CD applications |
| `mcp.argocd.sync_application` | Argo CD | Trigger an application sync |
| `mcp.github.search_code` | GitHub | Search code across repositories |
| `mcp.github.list_pull_requests` | GitHub | List PRs for a repository |

The gateway handles authentication, rate limiting, policy enforcement, and circuit breaking for all backends transparently. Backend servers must be registered and have `active` status to be routable -- manage server status via the admin dashboard or CLI (`mcpgw server list`).

If multiple servers expose the same tool name, the gateway uses round-robin selection. Use the federated prefix (`mcp.<server>.`) to target a specific backend.

### API Key Mode

For environments without browser access:

```json
{
  "mcpServers": {
    "gateway": {
      "command": "curl",
      "args": ["-s", "-X", "POST", "-H", "Authorization: Bearer <API_KEY>",
               "-H", "Content-Type: application/json",
               "https://gateway.example.com/mcp"]
    }
  }
}
```

## Database Migrations

The gateway ships SQL migration files (copied into the image) and applies them via `golang-migrate` from the filesystem:

```bash
# Run all pending migrations (default direction: up)
mcpgw migrate

# Run a specific number of steps
mcpgw migrate --steps 1

# Roll back all migrations
mcpgw migrate --direction down
```

Migration files follow the convention `NNNN_description.{up,down}.sql` in the `migrations/` directory. Current migrations:

| Migration | Description |
|-----------|-------------|
| 0001 | MCP servers table |
| 0002 | API keys table |
| 0003 | Audit log table |
| 0004 | Config cache table |
| 0005 | Server protocol fields |
| 0006 | Registration leases |
| 0007 | Audit log retention index |
| 0008 | API key policy profile |
| 0009 | Tool classifications |

## Development

### Requirements

- Go `1.25.7` (pinned in `go.mod` and CI)
- `golangci-lint`, `staticcheck`, `govulncheck`, `gosec`, `dupl` for quality gates

### Quality Gates

Run the full suite before opening a PR:

```bash
go build ./cmd/mcpgw
go test -race ./...
make quality    # vet → lint → staticcheck → govulncheck → gosec → dupl → godoc-check → coverage-check
```

Individual checks:

| Command | Purpose |
|---------|---------|
| `go vet ./...` | Suspicious constructs |
| `golangci-lint run ./...` | Aggregated linting |
| `staticcheck ./...` | Advanced static analysis |
| `govulncheck ./...` | Known vulnerability scan |
| `./scripts/check-gosec.sh` | Security-focused scan |
| `./scripts/check-dupl.sh` | Duplicate code detection |
| `./scripts/check-godoc.sh` | Exported symbols documented |
| `./scripts/check-coverage.sh` | Per-package coverage ≥ threshold |

Coverage thresholds are enforced per-package via `coverage-thresholds.json` (minimum 80%).

### Devcontainer

The repository includes a `.devcontainer` configuration with Go, Helm, `kubectl`, and Argo CD CLI tooling for a reproducible development environment.

### Helm Validation

Validate chart rendering for all environments before deployment PRs:

```bash
make chart-lint
make chart-render-base
make chart-render-dev
make chart-render-staging
make chart-render-prod
make chart-validate
```

### Development Phases

The gateway was developed across 18 sequential phases covering core infrastructure through production hardening.

## Repository Layout

```
cruvero-mcp-gateway/
├── .devcontainer/          Reproducible development environment
├── cmd/
│   └── mcpgw/              Single binary with subcommands
├── internal/
│   ├── admin/              Admin dashboard (OIDC auth, HTMX templates, handlers)
│   ├── auth/               API key + OIDC + Device Code flow authentication
│   ├── config/             Environment-based configuration
│   ├── events/             NATS client, event types, pub/sub
│   ├── identity/           mTLS, SPIFFE ID, cert validation
│   ├── policy/             Tool safety, allowlist/denylist, risk classification
│   ├── proxy/              MCP protocol handler, tool/resource routing
│   ├── ratelimit/          Token bucket with memory/dragonfly/NATS backends
│   ├── registration/       Server registration, handshake, heartbeat
│   ├── resilience/         Circuit breaker, retry, connection pool
│   ├── server/             HTTP server, router, middleware
│   ├── store/              Postgres store interfaces + implementations
│   ├── testutil/           Integration, security, and load test suites
│   └── types/              Shared domain types
├── migrations/             SQL migrations (0001–0009)
├── charts/
│   └── mcpgateway/         Helm chart + environment overlays
├── deploy/
│   └── argocd/             GitOps manifests (AppProject, ApplicationSet)
├── docs/
│   └── OVERVIEW.md          Architecture reference
├── scripts/                Quality gate and CI helper scripts
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

## Contributing

Contribution process, branch rules, and required local quality gates are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under the [MIT License](LICENSE).
