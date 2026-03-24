# Agent Instructions

## Navigation

- Start here. Read `README.md` for the repository map, environment variables, and dependency list.
- For implementation and validation tasks, verify `.devcontainer/` setup first so local checks run in the same toolchain used by CI/deployment.
- Use the package source and tests as the package map; the repo does not maintain per-package README files.
- Do not load the entire repo. Scope reads to the packages and files relevant to your current task.
- Historical phase-planning docs are not present in this repo. Start with `README.md` and `docs/OVERVIEW.md` instead.
- Architecture reference: `docs/OVERVIEW.md`.
- `docs/integration-docs/MCP-SERVER-FLEET-GATEWAY-INTEGRATION.md` is fleet-migration reference material and is not required for core gateway work unless the task explicitly targets fleet migration.
- GitOps deployment manifests live in `deploy/argocd/` (AppProject + ApplicationSet).

## Project Structure

```
cmd/           Single CLI binary (mcpgw) with subcommands (serve, migrate, register, health)
internal/      14 packages:
               - auth         API key + OIDC authentication
               - config       Environment-based configuration (MCPGW_* vars)
               - events       NATS client, event types, pub/sub
               - identity     mTLS, SPIFFE ID extraction, cert validation
               - policy       Tool safety classification, allowlist/denylist
               - proxy        MCP protocol handler, tool routing, catalog merge
               - ratelimit    Token bucket limiter, per-identity limits
               - registration Server registration handshake, heartbeat monitor
               - resilience   Circuit breaker, retry logic, connection pooling
               - server       HTTP server, chi router, middleware chain
               - store        Postgres store interfaces + implementations
               - testutil     Test helpers, fixtures, mock builders
               - types        Shared types (ServerRecord, Capability, ToolEntry, etc.)
migrations/    SQL migrations (0001_description.up.sql / .down.sql)
docs/          Architecture reference, integration docs, audit, and notes
.devcontainer/ Reproducible local dev/test environment
charts/        Helm chart (charts/mcpgateway/)
deploy/        GitOps manifests (deploy/argocd/)
scripts/       CI/CD helpers, coverage checks
```

## Conventions

### Types and Naming

- Types live in `types.go` per package. Input/Output struct pairs for request/response flows.
- Interfaces use descriptive nouns or `-er` suffix: `Store`, `Validator`, `Limiter`, `Authenticator`.
- Policy structs follow the pattern `XyzPolicy`, nested in their parent config structs.
- JSON tags on all exported struct fields -- required for serialization and API responses.
- Enum-like constants use `type Xyz string` with package-level `const` blocks.

### Code Patterns

- **Stores**: `XyzStore` interface + `PostgresXyzStore` implementation. Constructor: `NewPostgresXyzStore(db *sql.DB) *PostgresXyzStore`.
- **Config**: Pure environment variables with `MCPGW_*` prefix. No config files. `config.Load()` reads all vars at startup and returns a validated `Config` struct.
- **Migrations**: Sequential `0001_description.up.sql` / `.down.sql` pairs in `migrations/`. Never reuse or modify an existing migration number.
- **HTTP router**: chi with middleware chain defined in `server/`. Middleware order matters -- auth before rate limiting before proxy.
- **Structured logging**: `slog` with JSON output by default. Use `slog.With()` for request-scoped fields.
- **Errors**: Return errors, do not panic. Wrap with `fmt.Errorf("operation: %w", err)` for context.
- **Context**: `context.Context` as the first parameter for any function that does I/O or calls external services.

### Testing

- Run all tests: `go test ./...`
- Static checks: `go vet ./...` and `golangci-lint run ./...`
- Coverage quality gate: 80% minimum per package.
- Run coverage gate locally: `./scripts/check-coverage.sh`
- Use table-driven tests with descriptive subtest names: `t.Run("returns error when cert has no URI SAN", ...)`.
- Mock external dependencies via interfaces. Use `testutil` package for shared helpers.
- Use `sqlmock` for store tests, `httptest` for handler tests.

### Commits

- Branch from `dev`, open PRs against `dev`.
- Conventional commit format: `feat(package): description`, `fix(package): description`, `test(package): description`.
- One concern per commit -- separate refactors, features, and test additions.
- Never include Co-authored-by trailers for AI or LLM assistants.
- Never reference AI tooling in commits, PRs, or code comments.

## Key Entry Points by Task

| Task | Start At | Key Files |
|------|----------|-----------|
| Add a new env var | `internal/config/` | `config.go`, `config_test.go` |
| Add a new store table | `migrations/`, `internal/store/` | Latest migration number, `store.go`, `types.go` |
| Add an HTTP endpoint | `internal/server/` | `routes.go`, `middleware.go`, handler in relevant package |
| Add a new MCP tool proxy | `internal/proxy/` | `handler.go`, `catalog.go`, `router.go` |
| Modify auth logic | `internal/auth/` | `apikey.go`, `oidc.go`, `middleware.go` |
| Change rate limits | `internal/ratelimit/` | `limiter.go`, `config.go` |
| Add a NATS event | `internal/events/` | `types.go`, `publisher.go`, `subscriber.go` |
| Modify registration flow | `internal/registration/` | `handshake.go`, `heartbeat.go` |
| Add circuit breaker logic | `internal/resilience/` | `breaker.go`, `pool.go` |
| Add a CLI subcommand | `cmd/mcpgw/` | `main.go`, new subcommand file |
| Write a new migration | `migrations/` | Check latest number, create `NNNN_description.{up,down}.sql` |
| Update Helm chart | `charts/mcpgateway/` | `values.yaml`, relevant template |
| Update Argo deployment | `deploy/argocd/` | `project.yaml`, `applicationset.yaml` |

## Common Pitfalls

- **mTLS config**: Always validate client certificates. Never set `tls.Config.ClientAuth` to anything less than `tls.RequireAndVerifyClientCert` in non-test code. Test helpers may use `tls.NoClientCert` explicitly.
- **Migration numbering**: Always check `migrations/` for the highest existing number before creating a new one. Never reuse or backfill numbers.
- **JSON tags**: Every exported struct field needs a `json:"snake_case"` tag. Missing tags break API serialization silently.
- **Config loading**: All config flows through `config.Load()` reading env vars. Do not read `os.Getenv` directly outside the config package.
- **Rate limiter cleanup**: The token bucket limiter creates per-client state. Always clean up expired entries to prevent memory leaks. The cleanup goroutine runs on a configurable interval.
- **SPIFFE IDs**: Extract from the URI SAN of the client certificate (`x509.Certificate.URIs`). Never extract identity from the CN field.
- **Chi middleware order**: Auth middleware must run before rate limiting, which must run before the proxy handler. Incorrect ordering causes auth bypasses or untracked requests.
- **Context propagation**: Always pass the request context through to store calls and external requests. Do not create background contexts in request handlers.
- **NATS subjects**: Use dot-separated hierarchical subjects prefixed with the gateway ID: `mcpgw.{gateway_id}.events.{event_type}` for events and `mcpgw.{gateway_id}.config.{scope}` for config. This enables subject-based filtering in multi-gateway deployments.
- **Connection pooling**: The resilience package manages backend connections. Do not create ad-hoc HTTP clients in other packages -- use the pool.
- **Secrets in Kubernetes**: Runtime secrets must come from Vault operator-managed resources. Do not commit plaintext app secrets into chart `Secret` manifests.
