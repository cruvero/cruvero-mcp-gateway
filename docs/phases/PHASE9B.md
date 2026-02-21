# Phase 9B: Argo AppProject/ApplicationSet, Rollout Policy, Drift Handling

## Overview

Define GitOps rollout control using Argo CD AppProject/ApplicationSet with environment-aware sync policy and safe drift handling.

## Scope

### AppProject (`deploy/argocd/project.yaml`)
- Restrict source repositories and destination namespaces.
- Define namespace and cluster resource policy boundaries.
- Configure sync windows for staging/prod gating.

### ApplicationSet (`deploy/argocd/applicationset.yaml`)
- Use list generator entries for environments:
  - `env`
  - `namespace`
  - `valuesFile`
  - `targetRevision`
  - `autoSync`
  - `prune`
  - `selfHeal`
- Deploy chart from `charts/mcpgateway` with base values plus overlay.
- Enable auto-sync only for dev by default.

### Drift and Sync Behavior
- Configure `syncOptions` for namespace creation and server-side apply.
- Add `ignoreDifferences` for mutable controller-managed fields (for example replicas, service-assigned fields, immutable job template diffs).
- Keep drift noise low without masking real config changes.

### Rollout and Rollback Guidance
- Define deployment order: dev first, then gated staging/prod.
- Document rollback approach (Git revert + Argo sync).
- Include verification checks after sync: app health, pod readiness, metrics availability.

### Parent Platform Parity
- Mirror proven parent-platform structure where practical (naming, generator shape, policy semantics).
- Keep this phase self-contained: all required implementation context must come from files in this repository.

## Files Created or Updated

| File | Description |
|------|-------------|
| deploy/argocd/project.yaml | Argo CD AppProject boundaries and sync policy |
| deploy/argocd/applicationset.yaml | Multi-environment ApplicationSet definition |
| docs/OVERVIEW.md | GitOps deployment architecture and flow |

## Testing Requirements

- ApplicationSet renders expected applications for configured environments
- Argo project restrictions match intended namespaces and repos
- Dev auto-sync behavior works; staging/prod remain gated by default
- Ignore-differences rules suppress expected mutable-field drift only
- Rollback flow is documented and tested in dev
