# Phase 7: Kubernetes & Observability

## Goal

Prepare the gateway for production Kubernetes deployment with full observability: Prometheus metrics, OpenTelemetry tracing, structured logging, Helm chart, security hardening, and alerting rules.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [7A](PHASE7A.md) | Dockerfile, Prometheus Metrics, OTel Tracing | [3 prompts](PHASE7A-PROMPT.md) | (instrumentation code) |
| [7B](PHASE7B.md) | Helm Chart, Security Hardening, Alerting | [4 prompts](PHASE7B-PROMPT.md) | (deployment artifacts) |

## Dependencies

- Phase 1–6 (all Go code complete before deployment packaging)

## Deliverables

- Multi-stage Dockerfile producing minimal distroless image
- Prometheus metrics endpoint with gateway-specific counters/histograms
- OpenTelemetry tracing with spans around key operations
- Helm chart with Deployment, Service, HPA, PDB, NetworkPolicy
- Vault operator-based secret templates and runtime secret references
- Hardened SecurityContext (nonroot, read-only fs, drop capabilities, seccomp)
- cert-manager Certificate resources
- ServiceMonitor and PrometheusRule CRDs
- Alerting rules for SLO-based monitoring

## Packages Modified

- `internal/server` — metrics endpoint, tracing middleware
- `internal/proxy` — tracing spans on upstream calls
- `internal/ratelimit` — rate limit metrics
- `internal/policy` — policy decision metrics

## Success Criteria

- Docker image builds and runs successfully
- Prometheus metrics scraped correctly
- Traces visible in OTel collector/Jaeger
- Helm chart deploys to Kubernetes cluster
- Security hardening passes kube-bench/Trivy scan
- Alerting rules fire on simulated conditions
