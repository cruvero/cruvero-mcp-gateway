# Phase 9A: Devcontainer Baseline, Helm Env Overlays, Vault Wiring

## Overview

Create the local development baseline and environment-aware chart configuration needed for safe, repeatable deployment preparation.

## Scope

### Devcontainer Baseline (`.devcontainer/`)
- Define `.devcontainer/devcontainer.json` for reproducible local development and testing.
- Include required tooling: Go, Helm, `kubectl`, Argo CD CLI, and lint/test dependencies.
- Configure post-create validation command(s) so contributors can run quality gates immediately.

### Helm Environment Overlays (`charts/mcpgateway/`)
- Keep `values.yaml` as the base defaults.
- Add environment overlays:
  - `values-dev.yaml`
  - `values-staging.yaml` (gated defaults)
  - `values-prod.yaml` (gated defaults)
- Ensure overlays only contain environment deltas and inherit shared defaults from `values.yaml`.

### Vault Operator Contract
- Standardize chart values for vault integration:
  - `vault.enabled`
  - `vault.createAuth`
  - `vault.authRef`
  - `vault.mount`
  - `vault.path`
  - `vault.refreshAfter`
  - `secrets.existingSecret`
- Runtime credentials must come from Vault-synced destination secret.
- No runtime plaintext Kubernetes Secret manifests in chart templates.

### Parent Platform Parity
- Mirror parent-platform deployment conventions where practical (naming, overlay flow, rollout semantics) to simplify operations.
- Keep this phase self-contained: implementation instructions must be fully satisfiable from this repository without requiring sibling-repo file access.

## Files Created or Updated

| File | Description |
|------|-------------|
| .devcontainer/devcontainer.json | Reproducible local dev/test environment definition |
| charts/mcpgateway/values.yaml | Base configuration defaults |
| charts/mcpgateway/values-dev.yaml | Dev-specific chart overlay |
| charts/mcpgateway/values-staging.yaml | Staging-specific chart overlay |
| charts/mcpgateway/values-prod.yaml | Prod-specific chart overlay |

## Testing Requirements

- Devcontainer opens successfully and includes required CLI tools
- `go test ./...`, `go vet ./...`, and `golangci-lint run ./...` work in-container
- `helm lint charts/mcpgateway` passes
- `helm template` renders for each environment overlay without missing values
- Secret values resolve via `existingSecret` references (no inline runtime credentials)
