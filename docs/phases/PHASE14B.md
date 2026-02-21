# Phase 14B: Production Hardening — ECS Logging, NATS TLS, CI, Environments

## Overview

Complete all remaining production maturity items: ECS structured logging for observability pipelines, NATS TLS for transport security, container image scanning in CI, staging/production ArgoCD environments, and Dependabot for dependency updates.

## Scope

### ECS Structured Logging (`internal/logging/`, `cmd/mcpgw/serve.go`)

Implement ECS (Elastic Common Schema) logging using zap + ecszap with a slog bridge. This keeps the existing `*slog.Logger` interface (used in 40+ files) while producing ECS-compliant JSON when enabled.

- `internal/logging/ecs_handler.go`:
  - Custom `slog.Handler` wrapper that remaps field names to ECS standard before passing to the inner handler.
  - Field remapping:

    | Current | ECS |
    |---------|-----|
    | `method` | `http.request.method` |
    | `path` | `url.path` |
    | `status` | `http.response.status_code` |
    | `duration` | `event.duration` |
    | `request_id` | `http.request.id` |
    | `error` | `error.message` |
    | `client_id` | `client.id` |
    | `tool_name` | `event.action` |
    | `server_id` | `server.id` |

  - Handler intercepts `Handle()`, walks attributes, remaps keys, delegates to inner handler.
  - Groups nested ECS dotted fields into slog groups (`http.request.method` -> group `http` -> group `request` -> attr `method`).

- `cmd/mcpgw/serve.go` — extend `newLogger()`:
  - When `ecsEnabled`:
    1. Create zap core via `ecszap.NewCore()`.
    2. Wrap as `zapslog.NewHandler()`.
    3. Wrap again with `ECSRemapHandler`.
    4. Return `slog.New(handler)`.
  - When disabled: keep current `slog.JSONHandler`/`slog.TextHandler`.

- Config: `MCPGW_ECS_LOGGING` (default `false`).
- go.mod: add `go.uber.org/zap`, `go.uber.org/zap/exp/zapslog`, `go.elastic.co/ecszap`.

### NATS TLS (`internal/config/config.go`, `cmd/mcpgw/serve.go`)

Wire NATS TLS from config into the events client.

- Config fields:
  - `MCPGW_NATS_TLS_ENABLED` (default `false`)
  - `MCPGW_NATS_TLS_CERT` (client cert path)
  - `MCPGW_NATS_TLS_KEY` (client key path)
  - `MCPGW_NATS_TLS_CA` (CA bundle path)
- In `serve.go`: when NATS TLS enabled, build `tls.Config` from cert/key/CA, pass `events.WithTLS(tlsCfg)` when creating the NATS client.
- Validate: all three paths must exist and be readable when TLS enabled.

### Container Image Scanning (`.github/workflows/`)

Add Trivy scan step to image build workflows.

- Add to `image-dev.yml` (and future staging/prod workflows):
  ```yaml
  - name: Run Trivy vulnerability scanner
    uses: aquasecurity/trivy-action@master
    with:
      image-ref: '${{ env.IMAGE_NAME }}:${{ github.sha }}'
      format: 'sarif'
      output: 'trivy-results.sarif'
      severity: 'CRITICAL,HIGH'
      exit-code: '1'
  
  - name: Upload Trivy scan results
    uses: github/codeql-action/upload-sarif@v3
    if: always()
    with:
      sarif_file: 'trivy-results.sarif'
  ```

### Staging and Production Environments

Expand ArgoCD ApplicationSet and AppProject.

- `deploy/argocd/project.yaml`: add `cruvero-staging` and `cruvero-prod` namespaces.
- `deploy/argocd/applicationset.yaml`: add staging and prod to list generator:
  ```yaml
  elements:
    - env: dev
      namespace: cruvero-dev
      valuesFile: values-dev.yaml
      targetRevision: main
      autoSync: "true"
      prune: "true"
      selfHeal: "true"
    - env: staging
      namespace: cruvero-staging
      valuesFile: values-staging.yaml
      targetRevision: main
      autoSync: "false"
      prune: "false"
      selfHeal: "false"
    - env: prod
      namespace: cruvero-prod
      valuesFile: values-prod.yaml
      targetRevision: main
      autoSync: "false"
      prune: "false"
      selfHeal: "false"
  ```
- Create `charts/mcpgateway/values-staging.yaml` and `values-prod.yaml`.
- Create `.github/workflows/image-staging.yml` and `image-prod.yml`.

### Dependabot Configuration

```yaml
# .github/dependabot.yml
version: 2
updates:
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 10
    labels:
      - "dependencies"
      - "go"
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 5
    labels:
      - "dependencies"
      - "ci"
  - package-ecosystem: "docker"
    directory: "/"
    schedule:
      interval: "weekly"
    open-pull-requests-limit: 3
    labels:
      - "dependencies"
      - "docker"
```

### SonarQube Configuration (Optional)

Create `sonar-project.properties`:
```properties
sonar.projectKey=cruvero-mcp-gateway
sonar.organization=cruvero
sonar.sources=cmd,internal
sonar.tests=.
sonar.test.inclusions=**/*_test.go
sonar.go.coverage.reportPaths=coverage.out
sonar.go.tests.reportPaths=test-report.json
```

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/logging/ecs_handler.go` | ECS field remapping slog.Handler |
| `internal/logging/ecs_handler_test.go` | Tests |
| `internal/config/config.go` | ECS, NATS TLS config fields |
| `cmd/mcpgw/serve.go` | Logger factory, NATS TLS wiring |
| `cmd/mcpgw/main.go` | `--ecs-logging` CLI flag |
| `.github/workflows/image-dev.yml` | Add Trivy scan step |
| `.github/workflows/image-staging.yml` | New staging image workflow |
| `.github/workflows/image-prod.yml` | New prod image workflow |
| `.github/dependabot.yml` | Dependency update automation |
| `deploy/argocd/project.yaml` | Add staging/prod namespaces |
| `deploy/argocd/applicationset.yaml` | Add staging/prod entries |
| `charts/mcpgateway/values-staging.yaml` | Staging environment values |
| `charts/mcpgateway/values-prod.yaml` | Prod environment values |
| `charts/mcpgateway/values.yaml` | Add ECS, NATS TLS config blocks |
| `charts/mcpgateway/templates/configmap.yaml` | Add new env vars |
| `go.mod` | Add zap, ecszap, zapslog |
| `sonar-project.properties` | SonarQube config (optional) |

## Testing Requirements

- ECS handler: test field remapping (method -> http.request.method), test unmapped fields pass through, test nested group creation
- ECS logging: test JSON output contains ECS-standard fields
- NATS TLS: test config loading, test TLS config build from cert/key/CA
- Helm: lint and template for all three environments (dev, staging, prod)
- CI: verify Trivy action syntax is valid
- Coverage: >=80% on new packages
