# cruvero-mcp-gateway

Go 1.25.7 mTLS reverse-proxy gateway for MCP (Model Context Protocol) servers on Kubernetes.

**Module**: `github.com/cruvero/mcp-gateway`

## Repository Layout

```
cmd/mcpgw/          CLI entry point (serve, server, apikey, policy, health, migrate, tool)
internal/
  auth/             mTLS + API-key + OIDC middleware
  config/           Env-var loader (MCPGW_* prefix)
  events/           NATS JetStream event bus
  identity/         SPIFFE identity extraction
  policy/           Per-server policy profiles
  proxy/            MCP-aware reverse proxy + SSE bridge
  ratelimit/        Token-bucket rate limiter
  registration/     Server registration protocol + leases
  resilience/       Circuit breaker, retry, health checks
  server/           HTTPS server lifecycle
  store/            Postgres data access (servers, keys, audit, config)
  testutil/         Integration, security, and load test suites
  types/            Shared domain types
migrations/         SQL migrations (golang-migrate, NNNN_name.{up,down}.sql)
charts/mcpgateway/  Helm chart + values-{dev,staging,prod}.yaml overlays
deploy/argocd/      GitOps Application manifests
scripts/            Quality-gate helper scripts
docs/phases/        Phase specs and prompts (gitignored)
```

## Commit Conventions

Conventional commits with scope:

```
feat(proxy): add longest-prefix resource routing guard
fix(auth): reject expired api keys in middleware
docs(readme): document dev image publish workflow
test(store): add server lease renewal edge cases
chore(ci): bump golangci-lint to v1.65
```

**Types**: feat, fix, docs, test, chore, refactor, perf, ci, build
**Scopes**: proxy, auth, store, config, events, identity, policy, ratelimit, registration, resilience, server, types, testutil, migrations, helm, ci, readme

### Strict Rules

- **NEVER mention AI, Claude, LLM, Copilot, or any AI assistant in commits, PRs, code comments, or documentation.**
- All merges go through PRs targeting `dev`. Never push directly to `dev` or `main`.
- Reference issues: `Closes #<number>`.

## Quality Gates

Run everything:

```bash
make quality    # vet → lint → staticcheck → govulncheck → gosec → dupl → godoc-check → coverage-check
```

Individual gates:

| Command | Tool | Purpose |
|---------|------|---------|
| `go vet ./...` | built-in | Suspicious constructs |
| `golangci-lint run ./...` | golangci-lint | Aggregated linting |
| `staticcheck ./...` | staticcheck | Advanced static analysis |
| `govulncheck ./...` | govulncheck | Known vulnerability scan |
| `./scripts/check-gosec.sh` | gosec | Security-focused scan |
| `./scripts/check-dupl.sh` | dupl | Duplicate code detection |
| `./scripts/check-godoc.sh` | check-godoc.go | Exported symbols must be documented |
| `./scripts/check-coverage.sh` | go test | Per-package coverage ≥ threshold (see `coverage-thresholds.json`) |

Always run before opening a PR:

```bash
go build ./cmd/mcpgw
go test -race ./...
make quality
```

## Testing Patterns

- **Table-driven tests** with `t.Run()` subtests.
- **go-sqlmock** for database tests — never hit a real DB in unit tests.
- **`t.Setenv()`** for env-var tests — no global mutation.
- **Build tags** for non-unit suites:
  - `go test -tags integration ./internal/testutil`
  - `go test -tags security ./internal/testutil`
  - `go test -tags load ./internal/testutil`
- **Coverage threshold**: 80% per package (enforced by `scripts/check-coverage.sh` + `coverage-thresholds.json`).

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MCPGW_DB_URL` | **yes** | — | Postgres connection string |
| `MCPGW_LISTEN_ADDR` | no | `:8443` | Server listen address |
| `MCPGW_TLS_CERT` | no | — | TLS certificate path |
| `MCPGW_TLS_KEY` | no | — | TLS private key path |
| `MCPGW_TLS_CA` | no | — | Client CA for mTLS verification |
| `MCPGW_NATS_URL` | if Cruvero | — | NATS server URL |
| `MCPGW_NATS_TLS_ENABLED` | no | `false` | Enable mTLS for NATS connections |
| `MCPGW_NATS_TLS_CERT` | if NATS TLS | — | Path to NATS client certificate |
| `MCPGW_NATS_TLS_KEY` | if NATS TLS | — | Path to NATS client private key |
| `MCPGW_NATS_TLS_CA` | if NATS TLS | — | Path to NATS CA certificate |
| `MCPGW_CRUVERO_ENABLED` | no | `false` | Enable Cruvero platform integration |
| `MCPGW_GATEWAY_ID` | no | `auto` | Gateway instance ID (auto = random UUID) |
| `MCPGW_OIDC_ISSUER` | no | — | OIDC issuer URL |
| `MCPGW_OIDC_AUDIENCE` | no | — | OIDC expected audience |
| `MCPGW_HEARTBEAT_TTL` | no | `30s` | Server heartbeat timeout |
| `MCPGW_RATE_DEFAULT` | no | `10` | Default requests/sec |
| `MCPGW_RATE_BURST` | no | `20` | Burst token bucket size |
| `MCPGW_CIRCUIT_THRESHOLD` | no | `5` | Circuit breaker failure threshold |
| `MCPGW_CIRCUIT_TIMEOUT` | no | `30s` | Circuit breaker recovery timeout |
| `MCPGW_RETRY_MAX` | no | `3` | Max retry attempts |
| `MCPGW_SPIFFE_ALLOW_PREFIX` | no | — | CSV of allowed SPIFFE ID prefixes |
| `MCPGW_LOG_FORMAT` | no | `json` | Log format (json/text) |
| `MCPGW_LOG_LEVEL` | no | `info` | Log level |
| `MCPGW_METRICS_ADDR` | no | `:9090` | Prometheus metrics address |
| `MCPGW_CORS_ENABLED` | no | `false` | Enable CORS headers |
| `MCPGW_CORS_ALLOWED_ORIGINS` | if CORS | — | Comma-separated allowed origins |
| `MCPGW_DB_MAX_OPEN_CONNS` | no | `25` | Max open database connections |
| `MCPGW_DB_MAX_IDLE_CONNS` | no | `10` | Max idle database connections |
| `MCPGW_DB_CONN_MAX_LIFETIME` | no | `5m` | Max connection lifetime |
| `MCPGW_AUDIT_RETENTION_DAYS` | no | `90` | Days to retain audit log entries |
| `MCPGW_AUDIT_CLEANUP_INTERVAL` | no | `1h` | Interval between retention cleanup runs |
| `MCPGW_SHUTDOWN_TIMEOUT` | no | `30s` | Graceful shutdown timeout (max 120s) |
| `MCPGW_RATE_LIMIT_BACKEND` | no | `memory` | Rate limit backend: memory, dragonfly, or nats |
| `MCPGW_DRAGONFLY_URL` | if dragonfly | — | DragonflyDB connection URL |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | no | — | OpenTelemetry collector endpoint |
| `OTEL_SERVICE_NAME` | no | `mcpgw` | OTEL service name |

## Migrations

- **Format**: `NNNN_descriptive_name.{up,down}.sql` (zero-padded 4 digits).
- **Current highest**: `0009` (tool_classifications).
- **Runtime**: `golang-migrate/migrate/v4` with `file://` source. The `migrationSourceURL()` helper resolves the embedded path at startup.
- **CLI**: `mcpgw migrate` subcommand or `make migrate-up`.

## CLI Subcommands

```
mcpgw serve                              Start the gateway (default command)
mcpgw server list|inspect|deregister     Manage registered MCP servers
mcpgw apikey create|list|revoke          Manage API keys
mcpgw policy list|set|reset              Manage policy profiles
mcpgw tool list|classify|auto-classify   Manage tool classifications
mcpgw health                             Check gateway health endpoint
mcpgw migrate                            Run database migrations
mcpgw version                            Print build version info
```

Global flags: `--log-level`, `--log-format`, `--config`

## Phase Development

See `docs/phases/INDEX-UPDATED.md` for the full phase roadmap.

- **Phases 1–10**: Complete (production hardening + tool risk classification).
- **Phase 10**: Production Hardening & Tool Risk Classification (done).
- **Phase 11–14**: Distributed rate limiting, OIDC device code, admin dashboard, code mode.

Each phase has sub-phases (A/B) with spec and prompt files: `PHASE{N}{A,B}.md` + `PHASE{N}{A,B}-PROMPT.md`.

## Gotchas

- **Go proxy**: Production builds use `nexus.dev.gchinfo.com/repository/go-proxy/` (see `Dockerfile` `GOPROXY` arg). Local dev uses the default proxy.
- **Docker proxies**: Production images pull from `docker-cache.dev.gchinfo.com` and `gcr-cache.dev.gchinfo.com`. Local dev uses Docker Hub.
- **Harbor registry**: Production images are pushed to `harbor.dev.gchinfo.com`.
- **Devcontainer Go version**: The `.devcontainer/Dockerfile` base image tag determines the Go version. Keep it aligned with `go.mod`.
- **mcp-go v0.44.0**: The MCP protocol library. Pin version — breaking changes are common upstream.
- **No config files**: All configuration is through environment variables. There is no YAML/TOML/JSON config file.
- **SPIFFE format**: Client identities use `spiffe://<trust-domain>/ns/<namespace>/sa/<service-account>`.
- **Helm overlays**: Four environments — base (`values.yaml`), dev, staging, prod. Always validate all four.
- **Branch strategy**: `dev` is the integration branch. PRs target `dev`. `main` is production.
- **CI runner group**: GitHub Actions use a self-hosted runner group (check `.github/workflows/` for label requirements).
