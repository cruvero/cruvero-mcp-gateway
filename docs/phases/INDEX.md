# Phase Index

## Status Legend

- **Not Started**: Implementation has not begun
- **In Progress**: Currently being implemented
- **Complete**: Implementation finished and tested

## Phases

| Phase | Title | Status | Sub-phases | Dependencies |
|-------|-------|--------|------------|--------------|
| 1 | Core Foundation | Not Started | [1A](PHASE1A.md), [1B](PHASE1B.md) | -- |
| 2 | Identity & Auth | Not Started | [2A](PHASE2A.md), [2B](PHASE2B.md) | Phase 1 |
| 3 | Registration Protocol | Not Started | [3A](PHASE3A.md), [3B](PHASE3B.md) | Phase 2 |
| 4 | MCP Proxy & Routing | Not Started | [4A](PHASE4A.md), [4B](PHASE4B.md) | Phase 1, Phase 3 |
| 5 | Policy & Rate Limiting | Not Started | [5A](PHASE5A.md), [5B](PHASE5B.md) | Phase 2 |
| 6 | NATS & Cruvero Integration | Not Started | [6A](PHASE6A.md), [6B](PHASE6B.md) | Phase 3, 4, 5 |
| 7 | Kubernetes & Observability | Not Started | [7A](PHASE7A.md), [7B](PHASE7B.md) | Phase 1--6 |
| 8 | CLI & Testing | Not Started | [8A](PHASE8A.md), [8B](PHASE8B.md) | Phase 1--7 |
| 9 | GitOps Deployment | Not Started | [9A](PHASE9A.md), [9B](PHASE9B.md) | Phase 1--8 |

## Phase Details

### Phase 1: Core Foundation

Project skeleton, configuration loader, shared types, HTTP server with TLS and health probes, Postgres store interfaces, and database migrations.

- [Overview](PHASE1.md)
- [1A: Project Skeleton, Types, Config, HTTP Server](PHASE1A.md) -- [Prompts](PHASE1A-PROMPT.md)
- [1B: Postgres Store & Migrations](PHASE1B.md) -- [Prompts](PHASE1B-PROMPT.md)

### Phase 2: Identity & Auth

mTLS certificate validation, SPIFFE ID extraction and allowlist enforcement, API key store with deterministic lookup hash + bcrypt verification, OIDC token verification via go-oidc, and unified auth middleware that selects the correct method per request.

- [Overview](PHASE2.md)
- [2A: mTLS Identity & SPIFFE Validation](PHASE2A.md) -- [Prompts](PHASE2A-PROMPT.md)
- [2B: API Key Store, OIDC Validation, Unified Middleware](PHASE2B.md) -- [Prompts](PHASE2B-PROMPT.md)

### Phase 3: Registration Protocol

Server registration handshake endpoint, heartbeat processing, state machine (pending/approved/active/stale/expired), capability indexing, and background reaper for expired servers.

- [Overview](PHASE3.md)
- [3A: Registration Handshake & State Machine](PHASE3A.md) -- [Prompts](PHASE3A-PROMPT.md)
- [3B: Heartbeat Processing, Capability Index, Reaper](PHASE3B.md) -- [Prompts](PHASE3B-PROMPT.md)

### Phase 4: MCP Proxy & Routing

MCP protocol handler via mcp-go, tool catalog aggregation from active backends, tool-call routing by name, resource routing by URI prefix, and SSE streaming transport.

- [Overview](PHASE4.md)
- [4A: MCP Protocol Handler & Tool Catalog Aggregation](PHASE4A.md) -- [Prompts](PHASE4A-PROMPT.md)
- [4B: Tool-Call Routing, Resource Routing, SSE Transport](PHASE4B.md) -- [Prompts](PHASE4B-PROMPT.md)

### Phase 5: Policy & Rate Limiting

Token bucket rate limiter with per-profile configuration, tool allow/deny list evaluation, dangerous command pattern detection, argument schema validation, enforcement modes (enforce/audit), and audit logging.

- [Overview](PHASE5.md)
- [5A: Rate Limiting Engine](PHASE5A.md) -- [Prompts](PHASE5A-PROMPT.md)
- [5B: Tool Safety Guardrails & Audit Logging](PHASE5B.md) -- [Prompts](PHASE5B-PROMPT.md)

### Phase 6: NATS & Cruvero Integration

NATS client wrapper, event publishing on lifecycle changes, config subscription from Cruvero, graceful degradation on disconnect, and Cruvero registry state sync.

- [Overview](PHASE6.md)
- [6A: NATS Client, Event Publishing, Config Subscription](PHASE6A.md) -- [Prompts](PHASE6A-PROMPT.md)
- [6B: Cruvero Registry Sync, Graceful Degradation](PHASE6B.md) -- [Prompts](PHASE6B-PROMPT.md)

### Phase 7: Kubernetes & Observability

Multi-stage Dockerfile, Helm chart with all supporting resources (Deployment, Service, HPA, PDB, NetworkPolicy, cert-manager Certificate, ServiceMonitor), Prometheus metrics instrumentation, and OpenTelemetry tracing.

- [Overview](PHASE7.md)
- [7A: Dockerfile, Prometheus Metrics, OTel Tracing](PHASE7A.md) -- [Prompts](PHASE7A-PROMPT.md)
- [7B: Helm Chart, Security Hardening, Alerting](PHASE7B.md) -- [Prompts](PHASE7B-PROMPT.md)

### Phase 8: CLI & Testing

mcpgw subcommands (serve, migrate, register, health), integration test suite covering all layers, coverage enforcement at 80% minimum, and CI pipeline configuration.

- [Overview](PHASE8.md)
- [8A: CLI Subcommands & Integration Tests](PHASE8A.md) -- [Prompts](PHASE8A-PROMPT.md)
- [8B: Coverage Gates, CI Pipeline, Final Hardening](PHASE8B.md) -- [Prompts](PHASE8B-PROMPT.md)

### Phase 9: GitOps Deployment

Devcontainer-first local validation, Helm environment overlays, Argo CD AppProject/ApplicationSet manifests, and Vault operator-managed runtime secrets for Kubernetes deployment.

- [Overview](PHASE9.md)
- [9A: Devcontainer Baseline, Helm Env Overlays, Vault Wiring](PHASE9A.md) -- [Prompts](PHASE9A-PROMPT.md)
- [9B: Argo AppProject/ApplicationSet, Rollout Policy, Drift Handling](PHASE9B.md) -- [Prompts](PHASE9B-PROMPT.md)

## Dependency Graph

```
Phase 1 (Core Foundation)
  |
  +---> Phase 2 (Identity & Auth) --+--> Phase 3 (Registration)
  |                                  |
  |                                  +--> Phase 5 (Policy & Rate Limiting)
  |
  +---> Phase 4 (MCP Proxy) <------------ Phase 3
  |
  Phase 3 + Phase 4 + Phase 5 ---------> Phase 6 (NATS & Cruvero)
  |
  Phase 1 through 6 --------------------> Phase 7 (Kubernetes & Observability)
  |
  Phase 1 through 7 --------------------> Phase 8 (CLI & Testing)
  |
  Phase 1 through 8 --------------------> Phase 9 (GitOps Deployment)
```

### Reading the Graph

- **Phase 1** has no dependencies and is the starting point.
- **Phase 2** depends on Phase 1 (needs config, types, and HTTP server).
- **Phase 3** depends on Phase 2 (registration requires mTLS identity validation).
- **Phase 4** depends on Phase 1 (HTTP server) and Phase 3 (capability index from registrations).
- **Phase 5** depends on Phase 2 (auth middleware provides client identity for rate limiting and policy scoping).
- **Phase 6** depends on Phases 3, 4, and 5 (publishes events from registration, proxy, and policy layers).
- **Phase 7** depends on all prior phases (packages the complete system for Kubernetes deployment).
- **Phase 8** depends on all prior phases (tests the full integrated system end-to-end).
- **Phase 9** depends on all prior phases (ships environment-aware GitOps deployment and rollout policy).

### Parallelism Opportunities

Within the dependency constraints:
- After Phase 2 completes, Phase 3 and Phase 5 can proceed in parallel.
- Phase 4 can begin after Phase 1, but its routing logic requires the capability index from Phase 3. The protocol handler and catalog aggregation (4A) can start early; routing (4B) waits for 3B.
- Phase 9 should start only after Phase 8 validation gates are stable, because Argo rollout policy depends on tested chart and image artifacts.
