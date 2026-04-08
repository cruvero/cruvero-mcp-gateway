# cruvero-mcp-gateway

[![CI](https://github.com/cruvero/cruvero-mcp-gateway/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/cruvero/cruvero-mcp-gateway/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/cruvero/cruvero-mcp-gateway/dev)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/cruvero/mcp-gateway)](https://goreportcard.com/report/github.com/cruvero/mcp-gateway)
[![License](https://img.shields.io/github/license/cruvero/cruvero-mcp-gateway)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/cruvero/cruvero-mcp-gateway?include_prereleases&sort=semver)](https://github.com/cruvero/cruvero-mcp-gateway/releases)

A pure-Go mTLS reverse proxy that acts as a unified gateway for MCP (Model Context Protocol) servers on Kubernetes. The gateway auto-discovers backend MCP servers via a registration handshake, aggregates their tool catalogs, and proxies requests with rate limiting, circuit breakers, and per-tool policy enforcement. Clients connect to one gateway instead of managing individual backends.

The gateway exposes **two endpoint modes** simultaneously:

- **Unified `/mcp/`** -- aggregates tools from every active backend into a single MCP endpoint. Used by LLM agents and the Cruvero platform.
- **Per-server `/mcp/servers/{name}/`** -- exposes each registered backend as an individual MCP-compliant endpoint with bare tool names. Used by IDE clients (Copilot, Cursor, Claude Code, Windsurf) that expect one URL per server. An auto-discovery document at `/.well-known/mcp.json` and a JSON listing at `GET /mcp/servers` enable zero-config IDE integration.

The gateway operates in two deployment modes: **standalone** with only PostgreSQL as a dependency, or **Cruvero-integrated** where a NATS event bus synchronizes state with the Cruvero control plane. Both modes use the same binary and configuration surface -- Cruvero features activate only when `MCPGW_CRUVERO_ENABLED=true`.

## Key Features

**Core**
- MCP-native reverse proxy with unified tool catalog aggregation
- **Per-server virtual endpoints** at `/mcp/servers/{name}/` for IDE-friendly individual MCP connections
- **Auto-discovery document** at `/.well-known/mcp.json` and server listing at `GET /mcp/servers`
- Auto-registration with heartbeat keepalive and server state machine
- Federated tool routing with round-robin or **session-affinity** routing (rendezvous hashing on `Mcp-Session-Id`)
- **Tool namespace isolation** with three modes (`reject`, `namespace_always`, `namespace_on_conflict`) for collision-free fleet scaling
- Resource routing by URI prefix across backends
- Progressive tool discovery with pluggable search engines (BM25, vector, hybrid)
- LLM-driven orchestrator meta-tool for multi-step tool planning and execution

**Security**
- mTLS with SPIFFE identity extraction
- Dual auth: API keys (CLI/headless) + OIDC JWT (service-to-service)
- **Server-scoped API keys** -- restrict keys to specific backend servers via `--server-scope`
- OAuth2 Device Code flow for IDE authentication
- Per-tool allow/deny policies with risk classification (read_only, write, destructive)
- Per-user tool permissions with OIDC auto-registration
- SSRF-protected server registration
- Security headers, constant-time CSRF protection, AES-GCM encrypted sessions

**Resilience**
- Token-bucket rate limiting with three backends (memory, DragonflyDB, NATS)
- Per-server rate limiting configurable via admin dashboard (block, throttle, or unlimited per backend)
- Circuit breakers with configurable failure threshold and recovery timeout
- Automatic retry with backoff on transient failures
- Graceful degradation when NATS or backends are unavailable

**Search & Discovery**
- Progressive tool discovery -- LLMs see meta-tools instead of the full catalog
- BM25 full-text search engine with TF-IDF ranking, **partitioned per server** for incremental reindex
- Vector search engine with in-process ONNX embedder (all-MiniLM-L6-v2) and **LRU embedding cache** keyed by content hash
- Hybrid search combining BM25 + vector via Reciprocal Rank Fusion
- **Realtime capability refresh** -- heartbeat hash detection + push endpoint trigger incremental search reindex when tool catalogs change
- Synonym expansion for search query augmentation
- Dynamic tool activation with per-session caps

**Operations**
- Admin dashboard (HTMX web UI) for server, tool, audit, user, and search management
- User access management with role-based tool permissions
- Audit log with username extraction, sorting, filtering, and CSV export
- Prometheus metrics + OpenTelemetry distributed tracing
- Helm chart with example values overlay
- ArgoCD GitOps deployment with ApplicationSet
- Single static binary, distroless nonroot container image

**Integration**
- Standalone mode (Postgres only) or Cruvero platform integration (NATS event bus)
- Integrated admin mode with delegated API for platform embedding
- stdio-to-HTTP bridge (`mcpgw mcp-proxy`) for IDE integration
- Device Code flow for browser-based CLI/IDE authentication

## Architecture Overview

```mermaid
graph TB
    subgraph Clients
        IDE["IDE Clients<br/>(Copilot · Cursor · Claude Code)<br/>per-server URLs"]
        LLM["LLM Agents<br/>(unified /mcp/)"]
        Platform["Cruvero Platform<br/>(HTTPS)"]
        CLI["CLI / Scripts<br/>(HTTPS)"]
    end

    subgraph Gateway["MCP Gateway :8443"]
        Discovery[".well-known/mcp.json<br/>+ GET /mcp/servers"]
        Virtual["Virtual Server Handler<br/>/mcp/servers/{name}/"]
        Unified["Unified Endpoint<br/>/mcp/ (aggregated)"]
        Auth["Auth Layer<br/>mTLS · OIDC · API Key"]
        Scope["Server Scope Middleware<br/>(scoped API keys)"]
        RL["Rate Limiter<br/>Memory · Dragonfly · NATS"]
        Policy["Policy Engine<br/>Classification · Audit"]
        Proxy["MCP Proxy<br/>Routing · Caching · Namespace"]
        Refresh["Capability Refresh<br/>Heartbeat hash + Push"]
        Admin["Admin Dashboard<br/>HTMX · OIDC Sessions"]
        Orch["Orchestrator<br/>LLM Plan & Execute"]
        Search["Search Engine<br/>BM25 · Vector · Hybrid<br/>(partitioned + LRU cache)"]
    end

    subgraph Storage
        PG[("PostgreSQL<br/>(required)")]
        NATS["NATS JetStream<br/>(optional)"]
        DF["DragonflyDB<br/>(optional)"]
    end

    subgraph Backends
        MCP1["MCP Server<br/>SonarQube"]
        MCP2["MCP Server<br/>Kubernetes"]
        MCP3["MCP Server<br/>GitHub"]
    end

    IDE --> Discovery
    IDE --> Virtual
    LLM --> Unified
    Platform --> Gateway
    CLI --> Gateway

    Virtual --> Auth
    Unified --> Auth
    Auth --> Scope --> RL --> Policy --> Proxy
    Proxy --> Search
    Proxy --> Orch
    Proxy --> Refresh
    Proxy --> MCP1 & MCP2 & MCP3
    Refresh --> Search

    Gateway --> PG
    Gateway -.-> NATS
    Gateway -.-> DF
```

### Request Flow

```mermaid
sequenceDiagram
    participant C as MCP Client
    participant G as Gateway
    participant A as Auth
    participant R as Rate Limiter
    participant P as Policy Engine
    participant B as Backend Server

    C->>G: POST /mcp (tools/call)
    G->>A: Authenticate (mTLS / OIDC / API Key)
    A-->>G: Identity + Permissions
    G->>R: Check rate limit (client + server)
    R-->>G: Allowed
    G->>P: Evaluate tool policy
    P-->>G: Classify (read_only / write / destructive)
    P->>P: Write audit log
    G->>B: Forward tool call (HTTPS + mTLS)
    B-->>G: Tool result
    G-->>C: JSON-RPC response
```

### Server Registration

```mermaid
sequenceDiagram
    participant B as Backend Server
    participant G as Gateway
    participant DB as PostgreSQL
    participant N as NATS

    B->>G: PUT /v1/registrations/{id} (mTLS)
    G->>G: Validate SPIFFE identity
    G->>DB: Store server record
    G->>G: Index capabilities (tools, resources)
    G->>N: Publish server.registered event
    G-->>B: 200 OK (lease epoch)

    loop Every MCPGW_HEARTBEAT_TTL
        B->>G: POST /v1/registrations/{id}/heartbeat
        G->>DB: Update last_heartbeat
        G-->>B: 200 OK
    end

    Note over G: Sweeper detects missed heartbeats
    G->>DB: Mark stale then deregister
    G->>N: Publish server.deregistered
```

### Progressive Tool Discovery

```mermaid
flowchart LR
    subgraph Client
        LLM["LLM / IDE"]
    end

    subgraph Gateway
        Meta["Meta-tools<br/>search_tools · get_tool_schema"]
        SE["Search Engine<br/>BM25 · Vector · Hybrid"]
        DI["Discovery Index"]
        Act["Session Activation"]
    end

    subgraph Backends
        Tools["Backend Tools<br/>(not enumerated upfront)"]
    end

    LLM -->|"tools/list → meta-tools only"| Meta
    LLM -->|"search_tools(query)"| SE
    SE --> DI
    DI -->|"Top-N results"| LLM
    LLM -->|"get_tool_schema(names)"| Act
    Act --> Tools
    Tools -->|"Full schema"| LLM
    LLM -->|"tools/call"| Tools
```

### Authentication

```mermaid
flowchart TD
    Req["Incoming Request"] --> TLS{"Client cert<br/>present?"}
    TLS -->|Yes| SPIFFE["Extract SPIFFE ID"]
    SPIFFE --> Prefix{"Matches allowed<br/>prefix?"}
    Prefix -->|Yes| OK["Authenticated"]
    Prefix -->|No| Deny["401 Unauthorized"]
    TLS -->|No| Bearer{"Authorization<br/>header?"}
    Bearer -->|"3-part JWT"| OIDC["Validate OIDC token"]
    OIDC --> Claims{"Valid issuer<br/>& audience?"}
    Claims -->|Yes| OK
    Claims -->|No| Deny
    Bearer -->|"Other token"| APIKey["Lookup API Key hash"]
    APIKey --> Found{"Key exists<br/>& active?"}
    Found -->|Yes| OK
    Found -->|No| Deny
    Bearer -->|None| XKey{"X-API-Key<br/>header?"}
    XKey -->|Yes| APIKey
    XKey -->|No| Deny
```

For the full architecture reference including protocol details and database schema, see [docs/OVERVIEW.md](docs/OVERVIEW.md).

## Prerequisites

| Component | Required | Purpose | Notes |
|-----------|----------|---------|-------|
| Go 1.26.1 | Build only | Compile from source | Not needed for container deployment |
| PostgreSQL 16+ | **Yes** | Server registry, API keys, audit log, users, search config | Single required runtime dependency |
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

The Helm chart is in `charts/mcpgateway/`. Install with the example values overlay:

```bash
helm install mcpgateway charts/mcpgateway -n myapp-dev \
  -f charts/mcpgateway/values.yaml \
  -f charts/mcpgateway/values-example.yaml
```

Copy `values-example.yaml` and customize for your environment. See `values.yaml` for all available keys and defaults.

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
| `applicationset.yaml` | ApplicationSet for multi-environment deployment |
| `image-updater-rbac.yaml` | RBAC for Image Updater to read registry pull secrets |

Sync policies:
- **Dev**: automated sync with prune and self-heal, image updater auto-deploys on push
- **Staging/Prod**: manual sync, version-pinned image tags

### Security Posture

- **Image**: `distroless/cc` -- minimal runtime with C library support for ONNX, no shell or package manager
- **User**: `nonroot:nonroot` (UID 65532)
- **SecurityContext**: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, all capabilities dropped
- **NetworkPolicy**: default-deny with explicit ingress/egress rules for Postgres, NATS, OTel, DNS, and backend servers
- **PodDisruptionBudget**: `minAvailable: 1` (base), scales with environment
- **HPA**: CPU-based autoscaling (target 70%)

## CLI Reference

| Command | Description |
|---------|-------------|
| `mcpgw serve` | Start the gateway server (default command) |
| `mcpgw migrate` | Run database migrations |
| `mcpgw server list` | List registered MCP servers |
| `mcpgw server inspect <id-or-name>` | Show server details |
| `mcpgw server deregister <id-or-name>` | Remove a server |
| `mcpgw apikey create` | Create a new API key (supports `--server-scope` to restrict to specific backends) |
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
| `MCPGW_ADMIN_MODE` | `standalone` | No | Admin mode: `standalone` (HTMX UI) or `integrated` (platform-delegated API) |
| `MCPGW_PLATFORM_SERVICE_TOKEN` | -- | If integrated mode | Bearer token for delegated `/admin/api/v1/*` auth |
| `MCPGW_PLATFORM_SPIFFE_PREFIXES` | -- | No | Comma-separated SPIFFE prefixes for platform catalog mTLS access |
| `MCPGW_ADMIN_OIDC_CLIENT_ID` | -- | If admin (non-dev) | OIDC client ID for admin auth |
| `MCPGW_ADMIN_OIDC_CLIENT_SECRET` | -- | No | OIDC client secret for admin auth |
| `MCPGW_ADMIN_REQUIRED_SCOPE` | `admin` | No | Required OIDC scope for admin access |
| `MCPGW_ADMIN_SESSION_KEY` | -- | If admin (non-dev) | 64-char hex string (32-byte AES-256-GCM key) |
| `MCPGW_ADMIN_SESSION_TTL` | `8h` | No | Admin session duration |
| `MCPGW_ADMIN_EXTERNAL_URL` | -- | No | Public base URL for OIDC callback redirect (e.g. `https://admin.example.com`) |
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

### Search & Discovery

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_PROGRESSIVE_DISCOVERY` | `false` | No | Enable progressive tool discovery (lazy-load tool schemas on first call) |
| `MCPGW_SEARCH_ENGINE` | -- | No | Search engine: `substring`, `bm25`, `vector`, or `hybrid` |
| `MCPGW_ONNX_RUNTIME_PATH` | `/usr/lib/libonnxruntime.so` | If vector/hybrid | Path to ONNX Runtime shared library |
| `MCPGW_ONNX_MODEL_PATH` | `/models/all-MiniLM-L6-v2.onnx` | If vector/hybrid | Path to ONNX embedding model |
| `MCPGW_TOKENIZER_PATH` | `/models/tokenizer.json` | If vector/hybrid | Path to HuggingFace tokenizer |
| `MCPGW_EMBEDDING_CACHE_MAX_SIZE` | `10000` | No | LRU cache size for ONNX embeddings (avoids re-embedding unchanged tools on incremental reindex) |

### Per-Server Endpoints & Discovery

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_PER_SERVER_ENDPOINTS` | `true` | No | Mount per-server virtual endpoints at `/mcp/servers/{name}/` |
| `MCPGW_WELL_KNOWN_ENABLED` | `true` | No | Serve `/.well-known/mcp.json` discovery document |
| `MCPGW_WELL_KNOWN_CACHE_TTL` | `5s` | No | TTL for the discovery document cache |
| `MCPGW_GATEWAY_BASE_URL` | -- | No | Public base URL used in discovery URLs (falls back to `X-Forwarded-Host` then request `Host`) |

### Capability Refresh

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_CAPABILITY_REFRESH_ENABLED` | `true` | No | Compare `capabilities_hash` on each heartbeat and trigger inline refresh on mismatch |
| `MCPGW_CAPABILITY_PUSH_ENABLED` | `true` | No | Enable `POST /v1/registrations/{id}/capabilities` push endpoint (10 req/min per server) |

### Server-Scoped API Keys

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_SERVER_SCOPE_ENFORCEMENT` | `true` | No | Enforce `server_scope` restrictions on API keys; empty scope = gateway-wide access |

### Routing & Namespacing

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_DEFAULT_ROUTING_STRATEGY` | `round_robin` | No | Default routing strategy: `round_robin` or `session_affinity` (per-server override via registration) |
| `MCPGW_TOOL_NAMESPACE_MODE` | `reject` | No | Tool name collision policy: `reject`, `namespace_always`, or `namespace_on_conflict` |
| `MCPGW_NAMESPACE_SEPARATOR` | `.` | No | Separator character for namespaced tool names |

### Orchestrator

| Variable | Default | Required | Description |
|----------|---------|----------|-------------|
| `MCPGW_ORCHESTRATE_ENABLED` | `false` | No | Enable the orchestrate meta-tool (LLM-driven tool planning/execution) |
| `MCPGW_LLM_PROVIDERS` | -- | If orchestrate | JSON array of LLM provider configs (`name`, `model`, `priority`, `base_url`) |
| `MCPGW_LLM_{NAME}_API_KEY` | -- | Per provider | API key for each LLM provider (e.g. `MCPGW_LLM_OPENAI_API_KEY`) |

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
- **User management** -- manage user roles and per-user tool permissions
- **Audit log viewer** -- paginated logs with filters by client, tool, username, decision, and date range
- **CSV export** -- export up to 10,000 audit records with CSV injection prevention
- **Per-server rate limiting** -- configure requests/second limits and burst capacity per backend server
- **Rate limit inspection** -- view current rate limit state (when backend supports inspection)
- **Search configuration** -- manage synonym groups, test search queries

### Admin Modes

- **`MCPGW_ADMIN_MODE=standalone` (default)**: `/admin/*` HTMX dashboard with local OIDC/session auth.
- **`MCPGW_ADMIN_MODE=integrated`**: disables standalone `/admin` login/UI routes and exposes delegated admin API at `/admin/api/v1/*` for platform embedding.

Integrated mode delegated auth contract:

- `Authorization: Bearer <platform service token>` (must match `MCPGW_PLATFORM_SERVICE_TOKEN`)
- `X-Cruvero-Subject`
- `X-Cruvero-Email`
- `X-Cruvero-Tenant-ID`
- `X-Cruvero-Role`
- `X-Cruvero-Gateway-Role` (`viewer|editor|admin`)

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

### Per-Server Rate Limits

In addition to per-client policy-based rate limiting, the gateway supports per-server rate limits configurable via the admin dashboard. These override the global defaults for individual backend servers:

| `rate_limit` Value | `rate_burst` Value | Behavior |
|--------------------|-------------------|----------|
| `NULL` | `NULL` | Use global defaults from policy profile |
| `0` | any | All requests to this server are blocked |
| `> 0` | `NULL` or `> 0` | Custom rate; burst defaults to rate if not set |

Database CHECK constraints enforce that rate values are non-negative and that `rate_burst` cannot be set without `rate_limit`. When a request is rate-limited, the client receives an MCP-level JSON-RPC error with a retry hint rather than a raw HTTP 429.

## Observability

### Prometheus Metrics

The gateway exposes metrics on `MCPGW_METRICS_ADDR` (default `:9090`). The Helm chart includes optional `ServiceMonitor` and `PrometheusRule` resources (enable via `monitoring.serviceMonitor.enabled` and `monitoring.prometheusRule.enabled`).

### OpenTelemetry Tracing

Configure distributed tracing by setting `OTEL_EXPORTER_OTLP_ENDPOINT` to your collector address. The Helm chart can deploy a dedicated OTel Collector when `tracing.enabled=true`.

### Structured Logging

All logs use Go's `slog` package. Configure format (`json`/`text`) via `MCPGW_LOG_FORMAT` and level via `MCPGW_LOG_LEVEL`.

## IDE Integration

The gateway supports two integration patterns for IDE clients:

1. **Per-server virtual endpoints** (recommended) -- IDE clients connect directly to `/mcp/servers/{name}/` URLs. Each backend appears as an individual MCP server in the IDE's configuration. Discoverable via `/.well-known/mcp.json`. Works with any client that supports HTTP MCP transport.
2. **Stdio bridge** (legacy / unified) -- IDEs invoke the `mcpgw mcp-proxy` binary, which translates stdin/stdout JSON-RPC to the gateway's unified `/mcp/` endpoint with automatic token management.

### Discovering Available Servers

Query the discovery document or server listing to find available per-server URLs:

```bash
# Discovery document (zero-config IDE integration)
curl https://gateway.example.com/.well-known/mcp.json
# {
#   "mcpServers": {
#     "todoist":  {"url": "https://gateway.example.com/mcp/servers/todoist/",  "name": "todoist",  "version": "1.2.0", "status": "active"},
#     "github":   {"url": "https://gateway.example.com/mcp/servers/github/",   "name": "github",   "version": "3.0.1", "status": "active"}
#   }
# }

# Authenticated listing with tool/resource counts
curl -H "X-API-Key: $MCPGW_API_KEY" https://gateway.example.com/mcp/servers
# [{"name":"todoist","url":"...","version":"1.2.0","status":"active","tool_count":8,"resource_count":2}, ...]
```

The discovery document is cached for `MCPGW_WELL_KNOWN_CACHE_TTL` (default 5s) and invalidated automatically when servers register or deregister.

### Token Lifespan

The gateway honors the OIDC provider's `expires_in` value for access tokens issued via the Device Code flow. To control how long IDE sessions remain valid before re-authentication is required, configure the access token lifespan on your identity provider (e.g., Keycloak realm or client settings). The gateway does not override or extend token lifetimes -- the IdP is the sole authority.

### Authentication Setup

Authenticate once via browser using the Device Code flow -- no API key management required:

```bash
# One-time login
mcpgw auth login --gateway-url https://gateway.example.com

# Check auth status
mcpgw auth status

# Logout
mcpgw auth logout
```

The `mcp-proxy` command reads stdin JSON-RPC, forwards to the gateway with automatic token refresh, and writes responses to stdout.

### Claude Code

**Per-server endpoints (recommended)** -- one IDE entry per backend, no stdio bridge required. Add to `.claude/mcp.json`:

```json
{
  "mcpServers": {
    "todoist": {
      "url": "https://gateway.example.com/mcp/servers/todoist/",
      "headers": {
        "Authorization": "Bearer mcpgw_YOUR_API_KEY"
      }
    },
    "github": {
      "url": "https://gateway.example.com/mcp/servers/github/",
      "headers": {
        "Authorization": "Bearer mcpgw_YOUR_API_KEY"
      }
    }
  }
}
```

**Unified endpoint via stdio bridge** -- one IDE entry that exposes every active backend as federated tools. Add to `.claude/mcp.json` (project) or `~/.claude/mcp.json` (global):

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

### GitHub Copilot (VS Code)

**Per-server endpoints (recommended)** -- add to `.vscode/mcp.json`:

```json
{
  "servers": {
    "todoist": {
      "type": "http",
      "url": "https://gateway.example.com/mcp/servers/todoist/",
      "headers": {
        "Authorization": "Bearer mcpgw_YOUR_API_KEY"
      }
    },
    "github": {
      "type": "http",
      "url": "https://gateway.example.com/mcp/servers/github/",
      "headers": {
        "Authorization": "Bearer mcpgw_YOUR_API_KEY"
      }
    }
  }
}
```

**Unified endpoint via stdio bridge**:

```json
{
  "servers": {
    "gateway": {
      "command": "mcpgw",
      "args": ["mcp-proxy", "--gateway-url", "https://gateway.example.com"]
    }
  }
}
```

### Cursor / Windsurf

Cursor and Windsurf both support HTTP MCP servers. Add per-server entries to your IDE's MCP configuration (URLs from `/.well-known/mcp.json`):

```json
{
  "mcpServers": {
    "todoist": {
      "url": "https://gateway.example.com/mcp/servers/todoist/",
      "headers": { "Authorization": "Bearer mcpgw_YOUR_API_KEY" }
    }
  }
}
```

### OpenAI Codex CLI

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

### Server-Scoped API Keys

For least-privilege access, create API keys restricted to specific backends:

```bash
# Key restricted to todoist + github only
mcpgw apikey create \
  --name frontend-team \
  --scopes "tools:call,tools:list" \
  --server-scope "todoist,github"

# Requests to /mcp/servers/slack/ with this key return 403
# Requests to /mcp/servers/todoist/ succeed
# Requests to the unified /mcp/ endpoint are allowed for tools resolved to todoist or github only
```

Empty `--server-scope` (or omitting the flag) creates a gateway-wide key, preserving backward compatibility with existing keys.

### API Key Mode (Headless/CI)

For environments without browser access (CI pipelines, headless servers), create an API key and use direct HTTP requests. The `mcp-proxy` bridge does not support API key authentication -- use the HTTP endpoint directly:

```bash
# Create an API key
mcpgw apikey create --name ci-pipeline --scopes read,write

# Call the gateway directly
curl -X POST https://gateway.example.com/mcp \
  -H "Authorization: Bearer <API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

### Federated Tool Routing (Unified Endpoint)

The unified `/mcp/` endpoint federates all registered MCP servers behind a single connection. One gateway entry exposes every active server's tools automatically -- ideal for LLM agents and the stdio bridge.

Tools on the unified endpoint use the pattern `<server-name>.<tool-name>`. When a client calls a federated tool, the gateway extracts the server name, looks up which backend exposes it, and forwards the request.

**Example**: With SonarQube, Kubernetes, Argo CD, and GitHub MCP servers registered:

| Federated Tool Name | Server | Description |
|----------------------|--------|-------------|
| `sonarqube.search_issues` | SonarQube | Search for code quality issues |
| `sonarqube.get_quality_gate` | SonarQube | Get project quality gate status |
| `k8s.list_pods` | Kubernetes | List pods in a namespace |
| `k8s.get_logs` | Kubernetes | Stream container logs |
| `argocd.list_applications` | Argo CD | List Argo CD applications |
| `argocd.sync_application` | Argo CD | Trigger an application sync |
| `github.search_code` | GitHub | Search code across repositories |
| `github.list_pull_requests` | GitHub | List PRs for a repository |

The gateway handles authentication, rate limiting, policy enforcement, and circuit breaking for all backends transparently. Backend servers must be registered and have `active` status to be routable -- manage server status via the admin dashboard or CLI (`mcpgw server list`).

> **Per-server endpoints** (`/mcp/servers/{name}/`) return **bare tool names** (no namespace prefix) since the scope is implicit in the URL. Use per-server endpoints for IDE clients; use the unified endpoint for LLM agents that benefit from a single tool catalog.

### Tool Namespace Collisions

When multiple backends register tools with the same name, behavior is controlled by `MCPGW_TOOL_NAMESPACE_MODE`:

| Mode | Behavior on Unified Endpoint |
|------|------------------------------|
| `reject` (default) | Registration fails if a tool name conflicts with an existing one |
| `namespace_always` | Every tool is prefixed: `<server>.<tool>` |
| `namespace_on_conflict` | Only conflicting names are prefixed; unique names stay bare |

Per-server endpoints are unaffected -- they always return bare names. The separator is configurable via `MCPGW_NAMESPACE_SEPARATOR` (default `.`).

### Routing Strategies

Each backend can opt into session-affinity routing for stateful workloads. The gateway uses **rendezvous (HRW) hashing** on the `Mcp-Session-Id` header to consistently map a session to the same replica. On replica removal, only sessions pinned to the removed replica redistribute.

Set `routing_strategy: "session_affinity"` in the backend's registration request, or change the global default via `MCPGW_DEFAULT_ROUTING_STRATEGY=session_affinity`. Backends without a session ID header fall back to round-robin.

### Realtime Capability Refresh

When a backend's tool catalog changes, the gateway can refresh its in-memory `CapabilityIndex` and search indexes without a restart:

- **Heartbeat hash detection** -- backends include a `capabilities_hash` (SHA-256 of their serialized catalog) in heartbeat payloads. On mismatch, the gateway fetches the updated catalog inline and reindexes only that server's partition (BM25 + vector). Embeddings for unchanged tools are reused via the LRU cache. Enabled by default; disable with `MCPGW_CAPABILITY_REFRESH_ENABLED=false`.
- **Push refresh endpoint** -- fleet servers can `POST /v1/registrations/{id}/capabilities` (mTLS, 10 req/min per server) to trigger immediate refresh without waiting for the next heartbeat. Enabled by default; disable with `MCPGW_CAPABILITY_PUSH_ENABLED=false`.

Capability changes broadcast a `server.capabilities_changed` event so other gateway pods stay synchronized.

## Deployment Modes

### Standalone Mode

The gateway runs as a single binary with PostgreSQL as its only required dependency.

- **Configuration**: environment variables only (`MCPGW_*`)
- **Rate limiting**: in-memory (single instance) or DragonflyDB (multi-replica)
- **Policy management**: static, configured via CLI (`mcpgw policy set`)
- **Server registration**: servers register directly, active immediately after approval
- **Config updates**: require process restart

### Cruvero Platform Integration

Enable with `MCPGW_CRUVERO_ENABLED=true` and `MCPGW_NATS_URL`.

What changes when Cruvero integration is enabled:

- **NATS event bus** publishes lifecycle events (`server.registered`, `server.deregistered`, `server.health_changed`, `policy.violated`)
- **Platform-driven config** -- Cruvero pushes policy profiles, auth config, and server settings updates
- **Multi-gateway coordination** via cross-pod NATS broadcast
- **Registration includes platform sync** with ack/nack from the control plane
- **Dynamic policy and rate limit updates** without gateway restart
- **Graceful degradation** -- gateway caches last-known config when NATS disconnects

### Feature Comparison

| Feature | Standalone | Cruvero |
|---------|-----------|---------|
| Server registration | Direct | Platform-synced |
| Policy management | CLI only | Platform UI + push |
| Rate limiting | Memory / DragonflyDB | Memory / DragonflyDB / NATS |
| Multi-gateway sync | DragonflyDB pub/sub | NATS broadcast |
| Config updates | Restart required | Dynamic via NATS |
| Observability | Prometheus + OTel | + Platform dashboard |

NATS subject pattern: `mcpgw.{gateway_id}.{category}.{scope}` -- categories: `events`, `config`, `acks`.

## API Reference

| Endpoint Group | Path | Auth | Description |
|---------------|------|------|-------------|
| MCP (unified) | `POST /mcp/` | API key / OIDC | Aggregated JSON-RPC endpoint (`tools/list`, `tools/call`, `resources/list`, `resources/read`) across all backends |
| MCP (per-server) | `POST /mcp/servers/{name}/` | API key / OIDC | Individual MCP-compliant endpoint scoped to a single backend (bare tool names) |
| MCP (per-server SSE) | `GET\|POST /mcp/servers/{name}/sse` | API key / OIDC | SSE transport variant for the per-server endpoint |
| Discovery | `GET /.well-known/mcp.json` | None | Auto-discovery document listing all active per-server endpoints |
| Discovery | `GET /mcp/servers` | API key / OIDC | JSON listing of active servers with `tool_count` and `resource_count` |
| Device Code | `POST /device/code` | None | Request device authorization code |
| Device Token | `POST /device/token` | None | Poll for / refresh access tokens |
| Device Verify | `GET /device/verify` | None | Browser verification page |
| Health | `GET /healthz` | None | Liveness probe (always 200) |
| Health | `GET /readyz` | None | Readiness probe (checks dependencies) |
| Metadata | `GET /meta` | None | Gateway metadata (`admin_mode`, `version`, `gateway_id`) |
| Registration | `PUT /v1/registrations/{id}` | mTLS | Register an MCP server |
| Registration | `POST /v1/registrations/{id}/heartbeat` | mTLS | Server keep-alive (accepts `capabilities_hash` for refresh detection) |
| Registration | `POST /v1/registrations/{id}/capabilities` | mTLS | Push capability refresh (10 req/min per server) |
| Registration | `DELETE /v1/registrations/{id}` | mTLS | Deregister server |
| Catalog | `GET /v1/catalog` | mTLS (platform) | Platform tool catalog |
| Admin | `GET /admin/*` | OIDC session | Web dashboard |
| Admin API | `/admin/api/v1/*` | Platform bearer | Integrated-mode admin API for servers/tools/users/audit/search |
| Metrics | `GET /metrics` | None | Prometheus metrics (separate port) |

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
| 0010 | Server rate limit columns + CHECK constraints |
| 0011 | pg_trgm search indexes for tools and audit |
| 0012 | User access management |
| 0013 | Search configuration (synonyms, reindex log) |
| 0014 | Audit log username and sort support |
| 0015 | Server capabilities hash column (refresh detection) |
| 0016 | API key server scope column (per-server access control) |
| 0017 | Server routing strategy column (round-robin / session affinity) |

## Development

### Requirements

- Go `1.26.1` (pinned in `go.mod`, CI, and container build)
- `golangci-lint`, `staticcheck`, `govulncheck`, `gosec`, `dupl` for quality gates

### Quality Gates

Run the full suite before opening a PR:

```bash
go build ./cmd/mcpgw
go test -race ./...
make quality    # vet -> lint -> staticcheck -> govulncheck -> gosec -> dupl -> godoc-check -> coverage-check
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
| `./scripts/check-coverage.sh` | Per-package coverage >= threshold |

Coverage thresholds are enforced per-package via `coverage-thresholds.json` (minimum 80%).

### Devcontainer

The repository includes a `.devcontainer` configuration with Go, Helm, `kubectl`, and Argo CD CLI tooling for a reproducible development environment.
The devcontainer image tracks the floating `1.26` minor line because the published base image is not patch-pinned; CI and release builds remain pinned to Go `1.26.1`, so local development can differ by patch level.

### Local Standalone Stack

The default local workflow runs the gateway inside the devcontainer in standalone mode with every dependency local:

- PostgreSQL
- Keycloak for admin OIDC and device-code authentication
- shared development TLS certificates
- a mock MCP backend that auto-registers with the gateway

Open the repo in the devcontainer, then start the gateway inside the container:

```bash
./scripts/dev-run.sh
```

Primary local URLs:

- Gateway: `https://gateway.localhost:8443`
- Keycloak: `https://keycloak.localhost:8444`

Run the local smoke check after the gateway is up:

```bash
./scripts/dev-smoke.sh
```

For the full local workflow, seeded credentials, and trust notes, see [docs/LOCAL_DEV.md](docs/LOCAL_DEV.md).

### Helm Validation

Validate chart rendering before deployment PRs:

```bash
make chart-lint
make chart-render-base
make chart-validate
```

## Repository Layout

```
cruvero-mcp-gateway/
├── .devcontainer/          Reproducible development environment
├── cmd/
│   └── mcpgw/              Single binary with subcommands
├── internal/
│   ├── admin/              Admin dashboard (OIDC auth, HTMX templates, integrated API)
│   ├── auth/               API key + OIDC + Device Code + ServerScope middleware
│   ├── config/             Environment-based configuration
│   ├── events/             NATS client, event types (incl. server.capabilities_changed), pub/sub
│   ├── identity/           mTLS, SPIFFE ID, cert validation
│   ├── llm/                LLM failover client (OpenAI-compatible providers)
│   ├── mockbackend/        Local mock MCP backend used by the dev stack
│   ├── orchestrator/       LLM-driven tool planning and execution
│   ├── policy/             Tool safety, allowlist/denylist, risk classification
│   ├── proxy/              MCP proxy: routing, caching, virtual server endpoints,
│   │                        discovery doc, server listing, namespace resolver,
│   │                        session affinity strategy
│   ├── ratelimit/          Token bucket with memory/dragonfly/NATS backends
│   ├── registration/       Server registration, heartbeat, capability refresh (hash + push)
│   ├── resilience/         Circuit breaker, retry, connection pool
│   ├── search/             Partitioned search engines (BM25, vector, hybrid),
│   │                        LRU embedding cache, ONNX embedder
│   ├── server/             HTTP server, router, middleware, catalog API
│   ├── store/              Postgres store interfaces + implementations
│   ├── testutil/           Integration, security, and load test suites
│   └── types/              Shared domain types
├── migrations/             SQL migrations (0001-0017)
├── charts/
│   └── mcpgateway/         Helm chart with values-example.yaml overlay
├── deploy/
│   ├── argocd/             GitOps manifests (AppProject, ApplicationSet)
│   └── pki/                Vault PKI + cert-manager bootstrap
├── docs/                   Architecture, integration, and audit docs
├── scripts/                Quality gate and CI helper scripts
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

## Troubleshooting

### Common Issues

**"run 'mcpgw auth login' first"**

The `mcp-proxy` stdio bridge requires Device Code authentication. Run:

```bash
mcpgw auth login --gateway-url https://your-gateway
```

**Token expired / refresh failed**

Clear stale tokens and re-authenticate:

```bash
mcpgw auth logout && mcpgw auth login --gateway-url https://your-gateway
```

**Connection refused on port 8443**

Ensure TLS certificates are configured. The gateway requires `MCPGW_TLS_CERT` + `MCPGW_TLS_KEY` to start with TLS. For local development without TLS, set `MCPGW_LISTEN_ADDR=:8080` (plain HTTP, no mTLS).

**Server shows "inactive" after registration**

Servers must send heartbeats within `MCPGW_HEARTBEAT_TTL` (default 30s). Check backend server logs for heartbeat failures. Inspect server state:

```bash
mcpgw server inspect <name>
```

**Rate limited unexpectedly**

Check per-server rate limits in the admin dashboard. Check client rate via policy profile:

```bash
mcpgw policy list
```

Default: 10 req/s, burst 20.

**NATS connection failed (Cruvero mode)**

The gateway degrades gracefully and continues with cached config. Check `MCPGW_NATS_URL` and NATS TLS settings. Verify connectivity:

```bash
mcpgw health
```

## Contributing

Contribution process, branch rules, and required local quality gates are documented in [CONTRIBUTING.md](CONTRIBUTING.md).

## License

This project is licensed under the [MIT License](LICENSE).
