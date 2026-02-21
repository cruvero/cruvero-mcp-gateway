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
| 10 | Production Hardening & Tool Risk Classification | Not Started | [10A](PHASE10A.md), [10B](PHASE10B.md) | Phase 1--9 |
| 11 | Distributed Rate Limiting | Not Started | [11A](PHASE11A.md), [11B](PHASE11B.md) | Phase 1--10 |
| 12 | OIDC Device Code Flow & Admin Dashboard | Not Started | [12A](PHASE12A.md), [12B](PHASE12B.md) | Phase 2, 10 |
| 14 | Code Mode & Production Hardening | Not Started | [14A](PHASE14A.md), [14B](PHASE14B.md) | Phase 1--12 |
| 15 | NATS Resilience & Security | Not Started | [15A](PHASE15A.md), [15B](PHASE15B.md) | Phase 6 |
| 16 | Multi-Environment GitOps & Ingress | Not Started | [16A](PHASE16A.md), [16B](PHASE16B.md) | Phase 9, 12 |
| 17 | CI/CD Supply Chain Hardening | Not Started | [17A](PHASE17A.md), [17B](PHASE17B.md) | Phase 7 |
| 18 | Developer Experience | Not Started | [18A](PHASE18A.md), [18B](PHASE18B.md) | Phase 12, 15 |

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

### Phase 10: Production Hardening & Tool Risk Classification

Fix P0 blockers (audit wiring, DB pool, CORS, retention) and implement a database-backed tool risk classification system.

- [Overview](PHASE10.md)
- [10A: P0 Bug Fixes: Audit Wiring, DB Pool, CORS, Retention](PHASE10A.md) -- [Prompts](PHASE10A-PROMPT.md)
- [10B: Tool Risk Classification & Policy Engine Integration](PHASE10B.md) -- [Prompts](PHASE10B-PROMPT.md)

### Phase 11: Distributed Rate Limiting

Enable multi-replica scaling via DragonflyDB distributed rate limiting and cross-pod state synchronization.

- [Overview](PHASE11.md)
- [11A: DragonflyDB Rate Limit Backend](PHASE11A.md) -- [Prompts](PHASE11A-PROMPT.md)
- [11B: NATS-Based State Sync](PHASE11B.md) -- [Prompts](PHASE11B-PROMPT.md)

### Phase 12: OIDC Device Code Flow & Admin Dashboard

OAuth2 Device Code flow for IDE/CLI authentication and a minimal admin dashboard with OIDC auth for observability and tool management.

- [Overview](PHASE12.md)
- [12A: Device Code Flow](PHASE12A.md) -- [Prompts](PHASE12A-PROMPT.md)
- [12B: Admin Dashboard](PHASE12B.md) -- [Prompts](PHASE12B-PROMPT.md)

### Phase 14: Code Mode & Production Hardening

Cloudflare-style Code Mode via goja JavaScript runtime, ECS structured logging, NATS TLS spec, container scanning spec, and staging/prod environment spec.

- [Overview](PHASE14.md)
- [14A: Code Mode Integration](PHASE14A.md) -- [Prompts](PHASE14A-PROMPT.md)
- [14B: ECS Logging, NATS TLS, Environments](PHASE14B.md) -- [Prompts](PHASE14B-PROMPT.md)

### Phase 15: NATS Resilience & Security

Wire PostgresConfigStore into DegradationManager for cached config recovery, and add TLS support for NATS connections.

- [Overview](PHASE15.md)
- [15A: ConfigStore Wiring & Degradation Recovery](PHASE15A.md) -- [Prompts](PHASE15A-PROMPT.md)
- [15B: NATS TLS Configuration & Connection Security](PHASE15B.md) -- [Prompts](PHASE15B-PROMPT.md)

### Phase 16: Multi-Environment GitOps & Ingress

Extend ArgoCD from dev-only to multi-environment (dev/staging/prod), create environment-specific image workflows, and enable admin dashboard ingress.

- [Overview](PHASE16.md)
- [16A: ArgoCD Multi-Environment & Image Workflows](PHASE16A.md) -- [Prompts](PHASE16A-PROMPT.md)
- [16B: Admin Dashboard Ingress](PHASE16B.md) -- [Prompts](PHASE16B-PROMPT.md)

### Phase 17: CI/CD Supply Chain Hardening

Add Trivy container scanning to image workflows, configure Dependabot for dependency updates, and establish SonarQube integration.

- [Overview](PHASE17.md)
- [17A: Container Scanning & Dependabot](PHASE17A.md) -- [Prompts](PHASE17A-PROMPT.md)
- [17B: SonarQube Integration](PHASE17B.md) -- [Prompts](PHASE17B-PROMPT.md)

### Phase 18: Developer Experience

Standalone dev scripts for certificates and environment setup, admin dashboard dev mode, and devcontainer improvements.

- [Overview](PHASE18.md)
- [18A: Dev Scripts: Certificate Generation & Environment Setup](PHASE18A.md) -- [Prompts](PHASE18A-PROMPT.md)
- [18B: Admin Dev Mode & Devcontainer Improvements](PHASE18B.md) -- [Prompts](PHASE18B-PROMPT.md)

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
  |
  Phase 1 through 9 --------------------> Phase 10 (Production Hardening)
  |
  Phase 10 -----------------------------> Phase 11 (Distributed Rate Limiting)
  |
  Phase 2 + Phase 10 -------------------> Phase 12 (OIDC Device Code & Admin)
  |
  Phase 1 through 12 -------------------> Phase 14 (Code Mode & Hardening)

  --- Gap-closure phases (can run in parallel) ---

  Phase 6 ------------------------------> Phase 15 (NATS Resilience & Security)
  Phase 9 + Phase 12 -------------------> Phase 16 (Multi-Env GitOps & Ingress)
  Phase 7 ------------------------------> Phase 17 (CI/CD Supply Chain)
  Phase 12 + Phase 15 ------------------> Phase 18 (Developer Experience)
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
- **Phase 10** depends on Phase 1–9 (fixes P0 blockers in the complete system).
- **Phase 11** depends on Phase 10 (distributed rate limiting builds on the rate limit backend).
- **Phase 12** depends on Phase 2 and 10 (admin dashboard needs auth and hardened foundation).
- **Phase 14** depends on Phase 1–12 (code mode needs the full platform).
- **Phase 15** depends on Phase 6 (NATS resilience builds on the events subsystem).
- **Phase 16** depends on Phase 9 and 12 (multi-env GitOps extends deployment; ingress exposes admin).
- **Phase 17** depends on Phase 7 (container scanning requires Dockerfile and image workflows).
- **Phase 18** depends on Phase 12 and 15 (dev mode needs admin dashboard; scripts need NATS TLS awareness).

### Parallelism Opportunities

Within the dependency constraints:
- After Phase 2 completes, Phase 3 and Phase 5 can proceed in parallel.
- Phase 4 can begin after Phase 1, but its routing logic requires the capability index from Phase 3. The protocol handler and catalog aggregation (4A) can start early; routing (4B) waits for 3B.
- Phase 9 should start only after Phase 8 validation gates are stable, because Argo rollout policy depends on tested chart and image artifacts.
- **Phases 15, 16, and 17 can proceed in parallel** — they address independent concerns (NATS resilience, GitOps, CI/CD) with no cross-dependencies.
- **Phase 18 can start after Phase 15** completes (needs NATS TLS config awareness for dev scripts), but is independent of Phases 16 and 17.
