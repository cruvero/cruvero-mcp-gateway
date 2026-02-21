# Phase 7B: Helm Chart, Security Hardening, Alerting

## Overview

Create the Helm chart for Kubernetes deployment with hardened security configuration, cert-manager integration, and Prometheus-based alerting rules.

## Scope

### Helm Chart (`charts/mcpgateway/`)
- Chart.yaml: name, version, appVersion, description
- values.yaml: comprehensive defaults for all configurable values
- Templates:
  - deployment.yaml: gateway Deployment with security context, probes, resources
  - service.yaml: ClusterIP service exposing 8443 (HTTPS) and 9090 (metrics)
  - hpa.yaml: HorizontalPodAutoscaler with CPU target and stabilization
  - pdb.yaml: PodDisruptionBudget (maxUnavailable: 1)
  - serviceaccount.yaml: dedicated ServiceAccount
  - configmap.yaml: non-secret configuration
  - runtime-secret.yaml: references to a pre-synced runtime secret (for env wiring)
  - vault-auth.yaml: VaultAuth resource (optional creation)
  - vault-secrets.yaml: VaultStaticSecret resource for secret sync
  - validate-secrets.yaml: preflight checks to fail fast when required secret refs are missing
  - networkpolicy.yaml: default deny + explicit ingress/egress
  - certificate.yaml: cert-manager Certificate for gateway TLS
  - servicemonitor.yaml: Prometheus ServiceMonitor CRD
  - prometheusrule.yaml: Prometheus alerting rules

### Vault Operator Secret Management

- Runtime application secrets must be sourced from Vault via Vault Secrets Operator.
- Chart values must use secret references (`existingSecret`) rather than inline secret literals.
- No committed plaintext Kubernetes Secret manifests for runtime credentials.

### Security Hardening
- SecurityContext on pod and container level:
  - runAsNonRoot: true
  - runAsUser: 65532, runAsGroup: 65532
  - readOnlyRootFilesystem: true
  - allowPrivilegeEscalation: false
  - capabilities.drop: ["ALL"]
  - seccompProfile.type: RuntimeDefault
- Volumes: emptyDir for /tmp only
- Resource limits: configurable with sensible defaults (100m/128Mi request, 500m/512Mi limit)

### NetworkPolicy
- Default deny all ingress and egress for labeled pods
- Explicit ingress: port 8443 from specified namespaces/selectors
- Explicit egress: Postgres (5432), NATS (4222), MCP backend pods, DNS (kube-dns)
- Configurable source selectors in values.yaml

### cert-manager Integration
- Certificate resource for gateway server cert:
  - issuerRef configurable (ClusterIssuer or Issuer)
  - secretName for TLS secret
  - dnsNames and ipAddresses configurable
- Certificate resource for MCP server client certs (template)

### Alerting Rules (PrometheusRule CRD)
- High 5xx rate: >2% of requests are 5xx over 5 minutes
- Registration failures: >5 failed registrations in 5 minutes
- Policy denial surge: >10 denials/sec over 5 minutes
- Circuit breaker open: any backend circuit open for >5 minutes
- Rate limit saturation: >20% of requests rate limited over 5 minutes
- NATS disconnected: gateway disconnected from NATS for >2 minutes
- Each rule: alert name, expr (PromQL), for duration, severity label, summary/description annotations

### ServiceMonitor
- Match gateway pods by label
- Scrape /metrics on port 9090
- Interval: 15s
- Path: /metrics

## Files Created

| File | Description |
|------|-------------|
| charts/mcpgateway/Chart.yaml | Chart metadata |
| charts/mcpgateway/values.yaml | Default values |
| charts/mcpgateway/templates/deployment.yaml | Gateway Deployment |
| charts/mcpgateway/templates/service.yaml | Service |
| charts/mcpgateway/templates/hpa.yaml | HPA |
| charts/mcpgateway/templates/pdb.yaml | PDB |
| charts/mcpgateway/templates/serviceaccount.yaml | ServiceAccount |
| charts/mcpgateway/templates/configmap.yaml | ConfigMap |
| charts/mcpgateway/templates/runtime-secret.yaml | Runtime secret wiring |
| charts/mcpgateway/templates/vault-auth.yaml | VaultAuth |
| charts/mcpgateway/templates/vault-secrets.yaml | VaultStaticSecret |
| charts/mcpgateway/templates/validate-secrets.yaml | Secret preflight validation |
| charts/mcpgateway/templates/networkpolicy.yaml | NetworkPolicy |
| charts/mcpgateway/templates/certificate.yaml | cert-manager Certificate |
| charts/mcpgateway/templates/servicemonitor.yaml | Prometheus ServiceMonitor |
| charts/mcpgateway/templates/prometheusrule.yaml | Alerting rules |
| charts/mcpgateway/templates/_helpers.tpl | Template helpers |

## Testing Requirements

- Helm lint passes
- helm template renders valid YAML
- Security context present on all containers
- NetworkPolicy rules are correct
- Alerting rules have valid PromQL expressions
- values.yaml documents all configurable fields
- Vault operator templates render correctly when enabled
- Runtime credentials are never hardcoded into chart-managed Secret resources
