# cruvero-mcp-gateway

A pure-Go mTLS reverse proxy that acts as a gateway for MCP (Model Context Protocol) servers running on Kubernetes. The gateway auto-discovers MCP server deployments, registers them via a handshake protocol, and exposes their combined tool catalogs through a single MCP-compliant endpoint. Clients -- whether standalone CLI tools like Claude Code or integrated platforms like Cruvero -- connect to the gateway instead of individual backends.

The gateway is a standard MCP server itself. It proxies `tools/call` requests to the appropriate registered backend based on tool name, enforcing per-client rate limits, tool-level allow/deny policies, and circuit breakers on unhealthy backends. Authentication supports both API keys (for CLI usage) and OIDC tokens (for service-to-service flows), with mTLS providing transport-layer identity via SPIFFE IDs.

When paired with Cruvero, the gateway becomes a managed component: Cruvero's UI surfaces server health, connection status, and policy configuration, while a NATS event bus synchronizes state between the gateway and Cruvero's control plane. The gateway runs independently of Cruvero -- it maintains its own Postgres database and can operate in standalone mode with no NATS dependency.

## Key Features

- **MCP-native proxy** -- presents a unified tool catalog from multiple backend MCP servers
- **mTLS with SPIFFE** -- client and server identity via X.509 certificates and SPIFFE URI SANs
- **Auto-registration** -- backend MCP servers register via handshake with heartbeat keepalive
- **Dual auth modes** -- API key authentication for CLI tools, OIDC for service-to-service
- **Policy enforcement** -- per-tool and per-client allow/deny lists with risk classification
- **Rate limiting** -- token-bucket rate limiter with per-identity configuration
- **Circuit breakers** -- automatic backend isolation on repeated failures with configurable recovery
- **Cruvero integration** -- optional NATS-based event sync for centralized management
- **Kubernetes-native** -- Helm chart, health probes, Prometheus metrics, OTel tracing
- **Single binary** -- one `mcpgw` binary with subcommands (serve, migrate, register, health)

## Quick Start

```bash
# Build
go build -o mcpgw ./cmd/mcpgw

# Run migrations
export MCPGW_DB_URL="postgres://user:pass@localhost:5432/mcpgw?sslmode=disable"
./mcpgw migrate up

# Generate TLS certs (dev only)
./scripts/gen-dev-certs.sh

# Start the gateway
export MCPGW_TLS_CERT=certs/server.crt
export MCPGW_TLS_KEY=certs/server.key
export MCPGW_TLS_CA=certs/ca.crt
./mcpgw serve
```

## Development Requirements

- Go `1.25.7` (pinned in `go.mod` and CI)
- `golangci-lint`, `staticcheck`, `govulncheck`, `gosec`, and `dupl` for local quality checks

Run the full local quality profile before opening a PR:

```bash
go test -race ./...
go vet ./...
golangci-lint run ./...
staticcheck ./...
govulncheck ./...
./scripts/check-gosec.sh
./scripts/check-dupl.sh
./scripts/check-godoc.sh
./scripts/check-coverage.sh
```

## Local Devcontainer Workflow

Use a repository `.devcontainer` so development and verification run in a reproducible environment before any Kubernetes deployment.

1. Create `.devcontainer/devcontainer.json` with Go, Helm, `kubectl`, and Argo CD CLI tooling.
2. Open the repository in the devcontainer and run all quality gates there.
3. Validate locally before cluster sync:
   - `go test ./...`
   - `go vet ./...`
   - `golangci-lint run ./...`
   - `make chart-validate`

Local-first validation in the devcontainer is required before Argo CD sync or deployment PRs.

### Helm Validation Matrix

Use these targets for environment render checks:

- `make chart-lint`
- `make chart-render-base`
- `make chart-render-dev`
- `make chart-render-staging`
- `make chart-render-prod`
- `make chart-validate`

## GitOps Operations

For rollout validation and rollback steps, use:

- [docs/GITOPS-ROLLOUT.md](docs/GITOPS-ROLLOUT.md)

## GitHub Actions Standards

Any GitHub Actions workflow in this repository must follow these rules:

- Use `cruvero-org-runners` for `runs-on`.
- Publish/pull container images from Harbor using org secrets:
  - `HARBOR_URL`
  - `HARBOR_TOKEN`
- Sonar workflows must use org secrets:
  - `SONAR_HOST_URL`
  - `SONAR_TOKEN`
- Sonar workflows must use repo secret:
  - `SONAR_PROJECT_KEY`

## MCP Server Fleet Spec

For migrating standalone MCP servers to optional gateway integration (registration, heartbeat, Cruvero registry-managed non-secret settings, minimal containers, Vault-managed secrets, Helm/Argo GitOps), use:

- [docs/MCP-SERVER-FLEET-GATEWAY-INTEGRATION.md](docs/MCP-SERVER-FLEET-GATEWAY-INTEGRATION.md)

## Contributing

Contribution process, branch rules, and required local quality gates are documented in:

- [CONTRIBUTING.md](CONTRIBUTING.md)

## Repository Layout

```
cruvero-mcp-gateway/
├── .devcontainer/          Local reproducible development environment
├── cmd/
│   └── mcpgw/              Single binary with subcommands
├── internal/
│   ├── auth/               API key + OIDC authentication
│   ├── config/             Environment-based configuration
│   ├── events/             NATS client, event types, pub/sub
│   ├── identity/           mTLS, SPIFFE ID, cert validation
│   ├── policy/             Tool safety, allowlist/denylist
│   ├── proxy/              MCP protocol handler, tool routing
│   ├── ratelimit/          Token bucket, per-identity limits
│   ├── registration/       Server registration, handshake, heartbeat
│   ├── resilience/         Circuit breaker, retry, connection pool
│   ├── server/             HTTP server, router, middleware
│   ├── store/              Postgres store interfaces + implementations
│   ├── testutil/           Test helpers
│   └── types/              Shared types (ServerRecord, Capability, etc.)
├── migrations/             SQL migrations (0001_*.up.sql / .down.sql)
├── charts/
│   └── mcpgateway/         Helm chart
├── deploy/
│   └── argocd/             GitOps manifests (AppProject, ApplicationSet)
├── docs/
│   ├── OVERVIEW.md          Architecture reference
│   └── phases/             Phase documentation
├── scripts/                CI/CD, coverage checks
├── Dockerfile
├── Makefile
├── README.md
├── LLM.md
├── CLAUDE.md
├── go.mod
└── go.sum
```

## Environment Variables

All configuration is via environment variables with the `MCPGW_` prefix. No config files.

Canonical Go module path: `github.com/cruvero/mcp-gateway` (defined in `go.mod`).

| Variable | Default | Description |
|----------|---------|-------------|
| `MCPGW_LISTEN_ADDR` | `:8443` | Server listen address |
| `MCPGW_TLS_CERT` | -- | Server TLS certificate path |
| `MCPGW_TLS_KEY` | -- | Server TLS key path |
| `MCPGW_TLS_CA` | -- | Client CA bundle path |
| `MCPGW_DB_URL` | -- | Postgres connection string |
| `MCPGW_NATS_URL` | -- | NATS server URL (optional) |
| `MCPGW_OIDC_ISSUER` | -- | OIDC issuer URL (optional) |
| `MCPGW_OIDC_AUDIENCE` | -- | OIDC audience |
| `MCPGW_HEARTBEAT_TTL` | `30s` | Heartbeat timeout |
| `MCPGW_RATE_DEFAULT` | `10` | Default requests/sec per client |
| `MCPGW_RATE_BURST` | `20` | Default burst size |
| `MCPGW_CIRCUIT_THRESHOLD` | `5` | Failures before circuit opens |
| `MCPGW_CIRCUIT_TIMEOUT` | `30s` | Circuit breaker reset timeout |
| `MCPGW_RETRY_MAX` | `3` | Max retry attempts |
| `MCPGW_SPIFFE_ALLOW_PREFIX` | -- | Allowed SPIFFE ID prefixes (comma-separated) |
| `MCPGW_LOG_FORMAT` | `json` | Log format (`json` or `text`) |
| `MCPGW_LOG_LEVEL` | `info` | Log level |
| `MCPGW_METRICS_ADDR` | `:9090` | Prometheus metrics listen address |
| `MCPGW_CRUVERO_ENABLED` | `false` | Enable Cruvero integration |
| `MCPGW_GATEWAY_ID` | `auto` | Gateway instance ID (for NATS subjects) |

## Dependencies

Dependency versions are pinned in `go.mod` / `go.sum` and should be treated as the source of truth.

| Module | Purpose |
|--------|---------|
| `github.com/go-chi/chi/v5` | HTTP router |
| `github.com/mark3labs/mcp-go` | MCP protocol library |
| `github.com/lib/pq` | Postgres driver |
| `github.com/golang-migrate/migrate/v4` | Database migrations |
| `github.com/nats-io/nats.go` | NATS client |
| `golang.org/x/time` | Token bucket rate limiter |
| `github.com/coreos/go-oidc/v3` | OIDC token validation |
| `github.com/prometheus/client_golang` | Prometheus metrics |
| `go.opentelemetry.io/otel` | OTel tracing |
| `golang.org/x/crypto` | bcrypt verification for API keys (with deterministic lookup hash) |

## Phase Roadmap

Development is organized into nine sequential phases. See [docs/phases/INDEX.md](docs/phases/INDEX.md) for detailed specifications and implementation prompts.

| Phase | Scope |
|-------|-------|
| 1 | Core Foundation -- config, types, store, server skeleton, migrations |
| 2 | Identity and Auth -- mTLS, SPIFFE validation, API keys, OIDC |
| 3 | Registration Protocol -- server handshake, heartbeat, capability sync |
| 4 | MCP Proxy and Routing -- protocol handling, tool dispatch, catalog merge |
| 5 | Policy and Rate Limiting -- allow/deny lists, risk levels, token bucket |
| 6 | NATS and Cruvero Integration -- event bus, state sync, management API |
| 7 | Kubernetes and Observability -- Helm chart, probes, metrics, tracing |
| 8 | CLI and Testing -- subcommands, integration tests, coverage gates |
| 9 | GitOps Deployment -- devcontainer baseline, Helm env overlays, Argo ApplicationSet, Vault-managed secrets |

## License

Private. All rights reserved.
