# Phase 7B Implementation Prompts

## Prompt 1 of 4: Helm Chart Foundation

### Required Reading (read these files before writing code)
- docs/phases/PHASE7B.md
- Dockerfile
- internal/config/config.go (env vars)
- docs/OVERVIEW.md (architecture reference)

### Task

Create the Helm chart foundation.

1. `charts/mcpgateway/Chart.yaml`:
   - apiVersion: v2
   - name: mcpgateway
   - description: MCP Gateway - mTLS reverse proxy for MCP servers
   - type: application
   - version: 0.1.0
   - appVersion: "0.1.0"

2. `charts/mcpgateway/templates/_helpers.tpl`:
   - mcpgateway.name, mcpgateway.fullname, mcpgateway.labels, mcpgateway.selectorLabels
   - mcpgateway.serviceAccountName

3. `charts/mcpgateway/values.yaml`:
   - replicaCount: 2
   - image: repository, tag, pullPolicy
   - serviceAccount: create, name, annotations
   - service: type (ClusterIP), ports (https: 8443, metrics: 9090)
   - resources: requests (100m/128Mi), limits (500m/512Mi)
   - autoscaling: enabled, minReplicas, maxReplicas, targetCPU
   - config: all MCPGW_* env vars with defaults
   - tls: enabled, certManager (enabled, issuerRef, dnsNames)
   - nats: url, enabled
   - database: url (from secret ref only)
   - vault: enabled, createAuth, authRef, mount, path, refreshAfter
   - secrets: existingSecret
   - monitoring: serviceMonitor (enabled), prometheusRule (enabled)
   - networkPolicy: enabled, ingress sources, egress destinations

4. `charts/mcpgateway/templates/serviceaccount.yaml`:
   - Conditional creation based on values

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- `helm lint charts/mcpgateway` passes
- values.yaml is comprehensive and well-commented

---

## Prompt 2 of 4: Deployment & Service Templates

### Required Reading (read these files before writing code)
- charts/mcpgateway/values.yaml
- charts/mcpgateway/templates/_helpers.tpl
- docs/phases/PHASE7B.md (security hardening section)

### Task

Create Deployment and Service templates.

1. `charts/mcpgateway/templates/deployment.yaml`:
   - Standard Deployment with selector labels
   - Pod securityContext: seccompProfile RuntimeDefault
   - Container securityContext: runAsNonRoot, runAsUser 65532, readOnlyRootFilesystem, drop ALL, no privilege escalation
   - Liveness probe: /healthz on port https, scheme HTTPS
   - Readiness probe: /readyz on port https, scheme HTTPS
   - Resources from values
   - Environment variables from configmap and secret refs
   - Volume mounts: tmp (emptyDir), tls-certs (from secret, if TLS enabled)

2. `charts/mcpgateway/templates/service.yaml`:
   - ClusterIP service
   - Ports: https (8443) and metrics (9090)
   - Selector labels

3. `charts/mcpgateway/templates/configmap.yaml`:
   - Non-secret config: MCPGW_LISTEN_ADDR, MCPGW_LOG_FORMAT, MCPGW_LOG_LEVEL, MCPGW_METRICS_ADDR, MCPGW_HEARTBEAT_TTL, rate limits, circuit breaker settings

4. `charts/mcpgateway/templates/runtime-secret.yaml`:
   - Wire app env vars to `secrets.existingSecret`
   - Fail template rendering if required `existingSecret` is missing

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Deployment has full security hardening
- Probes configured correctly
- Config separated into non-secret configmap and Vault-managed secret references

---

## Prompt 3 of 4: HPA, PDB, NetworkPolicy

### Required Reading (read these files before writing code)
- charts/mcpgateway/values.yaml
- charts/mcpgateway/templates/deployment.yaml

### Task

Create scaling, disruption, and network policy templates.

1. `charts/mcpgateway/templates/hpa.yaml`:
   - Conditional on autoscaling.enabled
   - autoscaling/v2 API
   - CPU utilization target (default 70%)
   - scaleDown stabilization: 300s
   - scaleDown policy: max 20% per 60s

2. `charts/mcpgateway/templates/pdb.yaml`:
   - maxUnavailable: 1
   - Selector matching deployment labels

3. `charts/mcpgateway/templates/networkpolicy.yaml`:
   - Conditional on networkPolicy.enabled
   - Default deny all
   - Ingress: allow port 8443 from configurable sources
   - Ingress: allow port 9090 from monitoring namespace (Prometheus)
   - Egress: allow to Postgres (5432), NATS (4222), DNS (53), MCP backend pods
   - Egress destinations configurable in values

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- HPA has stabilization behavior
- PDB protects availability
- NetworkPolicy follows least-privilege principle

---

## Prompt 4 of 4: cert-manager, ServiceMonitor, Alerting Rules

### Required Reading (read these files before writing code)
- charts/mcpgateway/values.yaml
- docs/phases/PHASE7B.md (alerting rules section)

### Task

Create cert-manager, monitoring, and alerting templates.

1. `charts/mcpgateway/templates/vault-auth.yaml` and `charts/mcpgateway/templates/vault-secrets.yaml`:
   - Conditional on `vault.enabled`
   - Create optional `VaultAuth` when `vault.createAuth=true`
   - Create `VaultStaticSecret` with mount/path/refresh settings
   - Destination secret name must match `secrets.existingSecret`

2. `charts/mcpgateway/templates/validate-secrets.yaml`:
   - Template guard checks for required secret references when vault is disabled
   - Produce clear `fail` message for missing secret configuration

3. `charts/mcpgateway/templates/certificate.yaml`:
   - Conditional on tls.certManager.enabled
   - cert-manager Certificate resource
   - issuerRef from values (name, kind, group)
   - secretName for TLS secret
   - dnsNames from values
   - Renewal: renewBefore 360h

4. `charts/mcpgateway/templates/servicemonitor.yaml`:
   - Conditional on monitoring.serviceMonitor.enabled
   - Match by release labels
   - Endpoint: port metrics, path /metrics, interval 15s

5. `charts/mcpgateway/templates/prometheusrule.yaml`:
   - Conditional on monitoring.prometheusRule.enabled
   - Alert rules:
     - MCPGatewayHighErrorRate: sum(rate(mcpgw_http_requests_total{status_code=~"5.."}[5m])) / sum(rate(mcpgw_http_requests_total[5m])) > 0.02, for 5m, severity critical
     - MCPGatewayRegistrationFailures: sum(increase(mcpgw_http_requests_total{path="/v1/registrations",status_code=~"4..|5.."}[5m])) > 5, for 5m, severity warning
     - MCPGatewayPolicyDenialSurge: sum(rate(mcpgw_policy_denied_total[5m])) > 10, for 5m, severity warning
     - MCPGatewayCircuitOpen: mcpgw_circuit_breaker_state{state="open"} > 0, for 5m, severity critical
     - MCPGatewayRateLimitSaturation: sum(rate(mcpgw_rate_limited_total[5m])) / sum(rate(mcpgw_http_requests_total[5m])) > 0.2, for 5m, severity warning
     - MCPGatewayNATSDisconnected: mcpgw_nats_connected == 0, for 2m, severity warning
   - Each rule: summary and description annotations

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Vault operator resources valid and render conditionally
- Secret wiring uses existing secret refs only (no plaintext runtime secrets)
- cert-manager Certificate resource valid
- ServiceMonitor targets correct port and path
- All alerting rules have valid PromQL
- Rules have appropriate severity and for durations
- All templates render with `helm template`
