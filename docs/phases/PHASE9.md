# Phase 9: GitOps Deployment

## Goal

Ship a production-ready GitOps deployment model for `cruvero-mcp-gateway` using devcontainer-first local validation, Helm environment overlays, Argo CD ApplicationSet, and Vault operator-managed runtime secrets.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [9A](PHASE9A.md) | Devcontainer Baseline, Helm Env Overlays, Vault Wiring | [3 prompts](PHASE9A-PROMPT.md) | (deployment artifacts) |
| [9B](PHASE9B.md) | Argo AppProject/ApplicationSet, Rollout Policy, Drift Handling | [3 prompts](PHASE9B-PROMPT.md) | (deployment artifacts) |

## Dependencies

- Phase 1--8 (all implementation and validation gates complete before rollout automation)

## Deliverables

- Repository `.devcontainer/` baseline for reproducible local build/test/packaging workflows
- Helm values overlay model (`values.yaml` + env overlays)
- Vault operator contract for runtime secrets (`VaultAuth`, `VaultStaticSecret`, and `existingSecret` references)
- Argo CD `AppProject` and `ApplicationSet` manifests in `deploy/argocd/`
- Environment-specific rollout policy (dev auto-sync; staging/prod gated)
- Drift handling rules (`ignoreDifferences`) for mutable Kubernetes fields
- Deployment validation and rollback playbook for cluster rollout

## Areas Modified

- `.devcontainer/` -- local reproducible developer environment
- `charts/mcpgateway/` -- env overlays and vault wiring values
- `deploy/argocd/` -- AppProject and ApplicationSet manifests
- `docs/` -- rollout and validation documentation

## Success Criteria

- Local verification can run end-to-end inside `.devcontainer`
- `helm lint` and environment-specific `helm template` pass for all defined overlays
- Argo CD ApplicationSet generates expected per-environment applications
- Secrets are sourced through Vault operator only (no committed runtime plaintext secrets)
- Dev environment can sync automatically; staging/prod remain policy-gated until approved
