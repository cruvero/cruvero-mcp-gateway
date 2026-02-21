# Production Readiness Assessment: MCP Gateway for Engineer IDE Access

## Context

The gateway is being prepared for production use where engineers connect their IDEs (Claude Code, etc.) to backend MCP servers providing tools like GitHub issue management, PR creation, and K8s pod reading. The initial rollout targets read-only/non-destructive operations, with write operations following.

Three specific requirements were raised:
1. OIDC login that prompts once and doesn't ask again for the day
2. Audit trail of all commands in Postgres
3. General production readiness

This assessment found **one critical bug** (audit logging is silently broken) and several gaps that need addressing before production traffic.

---

## P0 -- BLOCKERS (fix before any production traffic)

### P0-1: Audit Store Is Not Wired to the Policy Engine (BUG)

**The audit trail for tool calls is silently broken.** In `internal/server/server.go:83`, the policy engine is created with `nil` as the audit store:
```go
policyEngine := policy.NewEngine(profiles, nil, logger)
```

The audit store IS created in `cmd/mcpgw/serve.go:78` but only passed to the registration service. Since `LogDecision()` in `internal/policy/audit.go:19` returns nil when auditStore is nil, every `tools/call` policy decision is silently discarded.

**Fix**: Add a `SetAuditStore(store.AuditStore)` method to `*Server` that threads it into the policy engine. Call it from `serve.go` after creating the audit store.

**Files**: `internal/server/server.go`, `cmd/mcpgw/serve.go`
**Scope**: Small -- wiring fix, the Engine already has the field and uses it.

---

### P0-2: Postgres Connection Pool Not Configured

`cmd/mcpgw/serve.go:136-151` calls `sql.Open()` but never sets pool limits. Go defaults: unlimited open conns, 2 idle conns, no lifetime. With HPA scaling to 20 replicas under load, this will exhaust Postgres `max_connections`.

**Fix**: Add config fields (`MCPGW_DB_MAX_OPEN_CONNS`, `MCPGW_DB_MAX_IDLE_CONNS`, `MCPGW_DB_CONN_MAX_LIFETIME`) with sensible defaults (25, 10, 5m). Set them after `sql.Open()`.

**Files**: `internal/config/config.go`, `cmd/mcpgw/serve.go`
**Scope**: Small -- 3 config fields, 3 setter calls.

---

### P0-3: CORS Wildcard Origin

`internal/server/middleware.go:104` sets `Access-Control-Allow-Origin: *` when CORS is enabled. With bearer tokens, this is a credential-leaking vector. Any origin can make authenticated requests.

**Fix**: Add `MCPGW_CORS_ALLOWED_ORIGINS` config field. Replace `*` with origin checking against the allowlist.

**Files**: `internal/config/config.go`, `internal/server/middleware.go`, `charts/mcpgateway/values.yaml`
**Scope**: Medium -- origin matching logic + config + Helm values.

---

### P0-4: Audit Log Table Has No Retention

The `audit_log` table (migration 0003) has no retention policy. Under production load with every tool call logged, this table grows unboundedly until disk is full.

**Fix**: Add a background cleanup goroutine or a new migration with time-based partitioning. A configurable retention period (default 90 days) with a periodic `DELETE WHERE created_at < now() - interval` is the simplest approach.

**Files**: New migration in `migrations/`, new cleanup logic (goroutine or CronJob)
**Scope**: Medium.

---

## P1 -- SHOULD FIX (important for safety/reliability)

### P1-1: API Keys Don't Carry Policy Profile

When API keys authenticate (`internal/auth/apikey_middleware.go:63-65`), the identity metadata never sets `policy_profile`. The policy middleware (`internal/policy/middleware.go:118`) reads `id.Metadata["policy_profile"]` to select a profile. Result: ALL API key users get the `"default"` profile regardless of intended tier.

**Fix**: Add `policy_profile` column to `api_keys` table. Set `id.Metadata["policy_profile"]` from the key record. Accept `--profile` flag on `mcpgw apikey create`.

**Files**: New migration, `internal/store/apikey_store.go`, `internal/auth/apikey_middleware.go`, `cmd/mcpgw/apikey.go`

---

### P1-2: No Audit of Tool Call Results

Even with P0-1 fixed, the audit only logs the policy *decision* (allowed/denied + sanitized args). It does NOT log whether the tool call succeeded or failed, or what data was returned. For compliance you need the outcome, not just the authorization.

**Fix**: In `internal/proxy/server.go:264-286` (the tool handler closure), after `p.router.Route()` returns, log a `tool_call_result` audit entry with tool name, client_id, success/error, and optionally truncated response.

**Files**: `internal/proxy/server.go`, needs audit store threaded into `ProxyServer`

---

### P1-3: Shutdown Timeout (10s) Is Shorter Than MCP Heartbeat (15s)

`internal/server/server.go:26` sets `shutdownTimeout = 10 * time.Second`, but the MCP streamable HTTP handler uses `server.WithHeartbeatInterval(15*time.Second)`. Active IDE sessions get hard-cut during deploys.

**Fix**: Make shutdown timeout configurable (`MCPGW_SHUTDOWN_TIMEOUT`), default to 30s. Ensure it's less than Kubernetes `terminationGracePeriodSeconds`.

**Files**: `internal/config/config.go`, `internal/server/server.go`
**Scope**: Small.

---

### P1-4: Ingress Resource for External Access

No Ingress or Gateway API resource exists. Engineers' IDEs need a stable external endpoint. The service is ClusterIP-only.

**Fix**: Add `charts/mcpgateway/templates/ingress.yaml` with configurable hosts, TLS, and className. For mTLS passthrough, use an ingress controller that supports TLS passthrough or terminate TLS at ingress and re-encrypt to the pod.

**Files**: New Helm template + values entries

---

### P1-5: Staging/Prod ArgoCD + Image Workflows

Only dev is configured in ApplicationSet/AppProject. Only `image-dev.yml` exists in workflows. You can't deploy to staging or production.

**Files**: `deploy/argocd/applicationset.yaml`, `deploy/argocd/project.yaml`, `.github/workflows/`

---

### P1-6: Container Image Scanning

No Trivy/Grype scan step in any CI workflow. Production images aren't scanned for CVEs.

**Fix**: Add scan step to image build workflow.

---

## P2 -- NICE TO HAVE (production maturity)

### P2-1: OIDC Login Flow for IDE Engineers

**This is the biggest gap vs. your stated requirements.** The current OIDC implementation (`internal/auth/oidc.go`) is **token validation only**. There is no authorization code flow, no browser redirect, no refresh tokens, no session management. Engineers cannot "be prompted to login" through the gateway.

**Reality**: The MCP protocol (streamable HTTP) doesn't have a browser interaction point. Claude Code sends an `Authorization: Bearer <token>` header. There's no mechanism for the gateway to redirect an IDE to a browser login page.

**Practical options for "login once, good for the day"**:

| Option | How It Works | Effort |
|--------|-------------|--------|
| **A. API keys with 24h expiry** | `mcpgw apikey create --expires 24h --client-id alice@corp.com`. Engineer puts key in IDE config. Keys auto-expire. | Already works today. Just set `--expires`. |
| **B. Companion CLI for OIDC** | Build `mcpgw auth login` that opens a browser, runs OAuth2 PKCE flow, caches the JWT locally (~/.mcpgw/token). IDE MCP config uses a wrapper that injects the cached token. Refresh token auto-renews. | Large -- new package, CLI command, token cache, refresh logic. |
| **C. OAuth2 Device Code flow** | Gateway exposes a device authorization endpoint. IDE shows "Go to https://gw/device and enter code XXXX". Token issued after browser approval. | Large -- new endpoints, polling, device code grant. |

**Recommendation**: Start with **Option A** (API keys with 24h expiry) for initial launch. Plan **Option B** as a follow-up project. The 24h-expiry API key gives you the "don't ask again for the day" behavior with zero new code.

---

### P2-2: Tool Risk Classification

`PolicyProfile` only has binary allowlist/denylist. No per-tool risk levels (read/write/destructive). For "starting with read-only, graduating to write," you'd create separate policy profiles:

- `readonly` profile: `ToolAllowlist: ["mcp.github.list_issues", "mcp.k8s.get_pods", ...]`
- `standard` profile: broader allowlist including write tools
- Use P1-1 (API key profiles) to assign engineers to the right tier

This works today with the existing allowlist mechanism. A richer risk classification system is a future enhancement.

---

### P2-3: Rate Limiter Is Per-Pod (In-Memory)

Rate limits reset on pod restart and aren't shared across replicas. Acceptable for initial launch; document the behavior. Superseded by P1-8 for multi-replica deployments.

---

### P2-4: Persistent Policy Store

Profiles are hardcoded in `server.go:defaultProfiles()` (default/premium/admin). Only updateable via NATS from Cruvero. No `mcpgw policy` CLI or DB backing. Acceptable for initial launch if you pre-define profiles in code and redeploy to change them.

---

### P1-7: ECS Structured Logging (Feature Flag + Full Field Remapping)

**Priority**: P1 -- needed for production observability pipeline (ELK stack ingestion).

**Current state**:
- Logger: `log/slog` (Go stdlib), used in 40+ files via dependency injection
- Initialized in `cmd/mcpgw/serve.go:194` (`newLogger()` function)
- Already outputs JSON to stdout by default (`MCPGW_LOG_FORMAT=json`)
- Config/feature flag pattern exists: `MCPGW_CRUVERO_ENABLED`, `MCPGW_CORS_ENABLED`
- No ECS support today; field names are arbitrary (e.g., `method`, `status`, `duration`)

**Approach**: Keep `*slog.Logger` as the interface (zero changes to 40+ files). When ECS enabled, swap the slog handler backend to zap+ecszap via the `zapslog` bridge, plus a custom handler wrapper that remaps field names to ECS standard. This gives zap-level performance with full ECS compliance while preserving the existing slog API.

**Full ECS field remapping** (handler-level, not at call sites):
A custom `slog.Handler` wrapper intercepts attributes and remaps keys before passing to the ecszap backend. This centralizes the mapping, keeps all existing log call sites unchanged, and is toggled by the feature flag.

Key field mappings:

| Current field | ECS field |
|--------------|-----------|
| `method` | `http.request.method` |
| `path` | `url.path` |
| `status` | `http.response.status_code` |
| `duration` | `event.duration` |
| `request_id` | `http.request.id` |
| `error` | `error.message` |
| `client_id` | `client.id` |
| `tool_name` | `event.action` |
| `server_id` | `server.id` |

**Implementation**:

1. `go.mod` -- Add dependencies:
   - `go.uber.org/zap`
   - `go.uber.org/zap/exp/zapslog`
   - `go.elastic.co/ecszap`

2. `internal/config/config.go`:
   - Add `defaultECSLogging = false`
   - Add `ECSLogging bool` field to Config struct
   - Parse `MCPGW_ECS_LOGGING` in `Load()`

3. `internal/logging/ecs_handler.go` (new package):
   - `ECSRemapHandler` wrapping an inner `slog.Handler`
   - `fieldMap` for key remapping (configurable)
   - Intercepts `Handle()`, walks attributes, remaps keys, delegates to inner handler
   - Handles nested grouping for ECS dotted fields (`http.request.method` -> slog groups)

4. `cmd/mcpgw/serve.go` -- Extend `newLogger(format, level string)` to `newLogger(format, level string, ecsEnabled bool)`:
   - When `ecsEnabled`: create zap core via `ecszap.NewCore()`, wrap as `zapslog.NewHandler()`, wrap again with `ECSRemapHandler`, return `slog.New(handler)`
   - When disabled: keep current `slog.JSONHandler`/`slog.TextHandler` behavior
   - Map slog levels to zap levels for ECS mode

5. `cmd/mcpgw/main.go` -- Add `--ecs-logging` CLI flag (sets `MCPGW_ECS_LOGGING` env var)

6. `charts/mcpgateway/values.yaml` + configmap -- Add `MCPGW_ECS_LOGGING: "false"`

**Why zap + ecszap** (not pure slog or zerolog):
- zap is the fastest structured logger in Go benchmarks
- `go.elastic.co/ecszap` is Elastic's official ECS encoder for zap
- `zapslog` bridge gives zap performance without touching 40+ files that import `*slog.Logger`
- No `ecsslog` first-party package exists from Elastic for slog directly
- zerolog has no official ECS integration

**Files**: `go.mod`, `internal/config/config.go`, new `internal/logging/ecs_handler.go`, `cmd/mcpgw/serve.go`, `cmd/mcpgw/main.go`, `charts/mcpgateway/values.yaml`, `charts/mcpgateway/templates/configmap.yaml`

---

### P1-8: Distributed Rate Limiting (NATS KV + Redis/DragonflyDB Dual Backends)

**Priority**: P1 -- with `values-prod.yaml` at 3-20 replicas, per-pod rate limits are ineffective. Clients can bypass limits by distributing requests across pods.

**What breaks at 2+ replicas**:

| State | Location | Multi-Replica Impact |
|-------|----------|---------------------|
| Rate limiter buckets | `internal/ratelimit/store.go` (in-memory map) | Limits not enforced globally; client gets N*replicas throughput |
| Capability index | `internal/registration/index.go` (in-memory maps) | Pod A registers server; Pod B doesn't see it until DB poll |
| Circuit breakers | `internal/resilience/circuit.go` (per-pod state machine) | Pod A trips breaker; Pod B still routes to failing backend |
| Tool cache | `internal/proxy/server.go` (in-memory TTL cache) | Each pod caches independently; stale after backend changes |
| Backend connections | `internal/proxy/client.go` (per-pod HTTP pools) | Connection count multiplied by replica count |

**Current NATS usage** (control plane only):
- Pub/sub for Cruvero platform events (`mcpgw.<id>.events.*`)
- Config subscription (`mcpgw.<id>.config.*`)
- No inter-gateway coordination, no JetStream / KV store usage
- TLS supported (`events.WithTLS()`) but not currently wired (see P1-9)

**Architecture -- extract `LimiterBackend` interface with dual implementations**:

1. `internal/ratelimit/backend.go` -- Extract `LimiterBackend` interface:
   ```go
   type LimiterBackend interface {
       Allow(ctx context.Context, key LimiterKey, limit float64, burst int) (allowed bool, err error)
       Close() error
   }
   ```

2. `internal/ratelimit/memory_backend.go` -- Move current in-memory logic here (default, always available)

3. `internal/ratelimit/nats_backend.go` -- NATS JetStream KV implementation:
   - KV bucket: `ratelimits`
   - Key: `{client_id}:{route}`
   - Value: JSON with token count + last refill timestamp
   - Uses NATS KV atomic `Update()` for CAS consistency
   - Used when Cruvero platform integration is enabled (`MCPGW_CRUVERO_ENABLED=true`), since NATS is already deployed

4. `internal/ratelimit/redis_backend.go` -- Redis/DragonflyDB implementation:
   - Uses `github.com/redis/go-redis/v9` (works with DragonflyDB out of the box)
   - Sliding window counter via Lua script (atomic)
   - Key: `ratelimit:{client_id}:{route}`, TTL-based auto-expiry
   - Used in standalone mode. DragonflyDB is Redis-protocol-compatible with a BSD license (avoids Redis SSPL/RSALv2 licensing concerns)

5. `internal/config/config.go`:
   - Add `RateLimitBackend string` field (`"memory"`, `"nats"`, or `"redis"`, default `"memory"`)
   - Add `RedisURL string` field (required when backend is `"redis"`)
   - Parse `MCPGW_RATE_LIMIT_BACKEND` and `MCPGW_REDIS_URL`
   - Validation: `"nats"` requires `CruveroEnabled`, `"redis"` requires `RedisURL`

6. `cmd/mcpgw/serve.go` -- Select backend based on config; pass NATS JetStream or Redis client accordingly

**go.mod additions**: `github.com/redis/go-redis/v9`

**Files**: `internal/ratelimit/store.go` (refactor), new `internal/ratelimit/backend.go`, `internal/ratelimit/memory_backend.go`, `internal/ratelimit/nats_backend.go`, `internal/ratelimit/redis_backend.go`, `internal/config/config.go`, `cmd/mcpgw/serve.go`, `go.mod`

---

### P1-9: NATS TLS Encryption

**Priority**: P1 -- NATS TLS is already supported in the events client but not wired into production config.

Currently `events/client.go:115-117` handles `nats.Secure(cfg.tlsConfig)` when a TLS config is provided. The gap is purely configuration plumbing.

**Implementation**:
1. `internal/config/config.go` -- Add `NATSTLSEnabled bool`, `NATSTLSCert string`, `NATSTLSKey string`, `NATSTLSCA string` fields
2. `cmd/mcpgw/serve.go` -- Build `tls.Config` from cert/key/CA and pass `events.WithTLS(tlsCfg)` when creating the NATS client
3. `charts/mcpgateway/values.yaml` + configmap -- Add `MCPGW_NATS_TLS_*` env vars

**Files**: `internal/config/config.go`, `cmd/mcpgw/serve.go`, `charts/mcpgateway/values.yaml`, `charts/mcpgateway/templates/configmap.yaml`

---

### P2-5: 12-Factor Compliance Audit Results

**Priority**: P2 -- the application is already substantially compliant. Only minor fixes needed.

| Factor | Status | Notes |
|--------|--------|-------|
| 1. Codebase | Compliant | Single repo, proper git setup |
| 2. Dependencies | Compliant | `go.mod` pinned, multi-stage Docker build |
| 3. Config | Compliant | All config via `MCPGW_*` env vars through `config.Load()` |
| 4. Backing Services | Compliant | Postgres, NATS, OIDC all URL-configurable |
| 5. Build/Release/Run | Compliant | Multi-stage Dockerfile, CI validates before push |
| 6. Processes | Compliant* | In-memory rate limiter not shared across replicas (see P1-8) |
| 7. Port Binding | Compliant | Configurable `:8443` and `:9090` |
| 8. Concurrency | Compliant | HPA configured, process-based scaling |
| 9. Disposability | Compliant | Signal handling, graceful shutdown, fast startup |
| 10. Dev/Prod Parity | Compliant | Same Dockerfile, Helm overlays per env |
| 11. Logs | Compliant | Structured JSON to stdout only |
| 12. Admin Processes | Compliant | `mcpgw migrate`, `mcpgw apikey`, `mcpgw server` |

**Minor fixes identified**:

1. **Health check hardcoded default** (`cmd/mcpgw/health.go:27`): `--url` flag defaulted to `https://localhost:8443` instead of reading `MCPGW_LISTEN_ADDR`. **Fixed** -- now derives default from env var with hardcoded fallback.

2. **Factor 6 caveat**: Rate limiter, circuit breakers, and capability index are per-pod in-memory state. Acceptable for initial launch; remediation is P1-8.

**Files**: `cmd/mcpgw/health.go` (fixed), `PRODUCTION-PLANS.md` (this document)

---

### P2-6: Cross-Pod Index Sync via NATS

**Priority**: P2 -- fast follow after P1-8.

Broadcast registration/deregistration events so all gateway pods keep indexes consistent without waiting for DB poll intervals.

**Implementation**:
1. When `indexedRegistrationService.Register()` succeeds, publish to `mcpgw.registry.updated` NATS subject
2. All pods subscribe to `mcpgw.registry.updated` and refresh their index from DB for the affected server ID
3. Eliminates stale index windows between pods

**Files**: `internal/registration/service.go`, `cmd/mcpgw/serve.go`

---

### P2-7: NATS-Based Server Communication (Future Architecture)

**Priority**: P2 -- larger architectural change; requires design document before implementation.

Replace direct HTTP registration with NATS-mediated communication between MCP servers and gateways.

**Current flow**: MCP server -> HTTP POST `/register` -> specific gateway pod -> writes DB
**Proposed flow**: MCP server -> NATS publish `mcpgw.servers.register` -> all gateway pods subscribe -> each updates local index

**Benefits**:
- MCP servers don't need to know individual gateway pod addresses
- Registration is broadcast, not point-to-point
- Heartbeats flow through NATS (lower overhead than HTTP per-heartbeat)
- Natural load distribution
- Encrypted via NATS TLS (P1-9)

**Trade-offs**:
- Requires MCP servers to have NATS client dependency
- Need to handle registration persistence (one gateway writes to DB, others update index only)
- Leader election or idempotent DB writes needed to avoid duplicate inserts
- HTTP registration should remain as a fallback for servers without NATS connectivity

**Recommendation**: Keep as a separate design document. Implement P1-8 and P2-6 first, which provide most of the multi-replica coordination benefits without changing the server registration contract.

---

## Recommended Implementation Order

| # | Item | Scope | Blocks |
|---|------|-------|--------|
| 1 | P0-1: Wire audit store to policy engine | Small | Unblocks all audit logging |
| 2 | P0-2: DB connection pool limits | Small | Prevents production outage |
| 3 | P0-3: CORS origin allowlist | Medium | Security hardening |
| 4 | P0-4: Audit log retention | Medium | Prevents disk exhaustion |
| 5 | P1-1: API key policy_profile | Medium | Required for differentiated access |
| 6 | P1-2: Tool call result audit | Medium | Audit completeness |
| 7 | P1-3: Shutdown timeout | Small | Deploy safety |
| 8 | P1-4: Ingress resource | Medium | External access |
| 9 | P1-5: Staging/prod environments | Config | Deployment pipeline |
| 10 | P1-6: Container scanning | Small | CI security |
| 11 | P1-7: ECS structured logging | Medium | Observability pipeline |
| 12 | P1-8: Distributed rate limiting | Large | Multi-replica correctness |
| 13 | P1-9: NATS TLS encryption | Small | Transport security |
| 14 | P2-5: 12-Factor audit fixes | Small | Compliance (health.go fixed) |
| 15 | P2-6: Cross-pod index sync | Medium | Registration consistency |
| 16 | P2-7: NATS-based server comms | Large | Design doc first |

---

## Files Touched (Summary)

| File | Items |
|------|-------|
| `internal/server/server.go` | P0-1, P0-3, P1-3 |
| `cmd/mcpgw/serve.go` | P0-1, P0-2, P1-7, P1-8, P1-9 |
| `internal/config/config.go` | P0-2, P0-3, P1-3, P1-7, P1-8, P1-9 |
| `internal/server/middleware.go` | P0-3 |
| `internal/proxy/server.go` | P1-2 |
| `internal/auth/apikey_middleware.go` | P1-1 |
| `internal/store/apikey_store.go` | P1-1 |
| `cmd/mcpgw/apikey.go` | P1-1 |
| `cmd/mcpgw/main.go` | P1-7 |
| `cmd/mcpgw/health.go` | P2-5 |
| `migrations/` | P0-4, P1-1 |
| `charts/mcpgateway/` | P0-3, P1-4, P1-5, P1-7, P1-9 |
| `internal/logging/` (new) | P1-7 |
| `internal/ratelimit/` | P1-8 |
| `internal/registration/service.go` | P2-6 |
| `go.mod` | P1-7, P1-8 |

---

## Verification

After implementing P0 items:
- `make build && make test` -- compilation and unit tests pass
- `make chart-validate` -- Helm renders for all environments
- Manual test: start gateway with `MCPGW_DB_URL` set, create an API key, make a `tools/call` through `/mcp`, verify `audit_log` table has a `policy_decision` row
- Load test: verify DB connections stay within configured pool limits under concurrent requests
- CORS test: verify non-allowlisted origin gets rejected
