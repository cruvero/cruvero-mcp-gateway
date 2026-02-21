# Phase 16: Multi-Environment GitOps & Ingress

## Goal

Extend the existing single-environment (dev) ArgoCD deployment to support staging and production environments with environment-specific image build workflows, and enable external ingress for the admin dashboard in the dev environment.

Covers parity audit gaps: Gap #3 (ArgoCD AppProject only has `cruvero-dev` destination), Gap #4 (ApplicationSet has single `dev` list element), Gap #5 (no staging/prod image build workflows), and Gap #10 (ingress values not configured for admin UI access).

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [16A](PHASE16A.md) | ArgoCD Multi-Environment & Image Workflows | [3 prompts](PHASE16A-PROMPT.md) | deploy/argocd, .github/workflows |
| [16B](PHASE16B.md) | Admin Dashboard Ingress | [2 prompts](PHASE16B-PROMPT.md) | charts/mcpgateway |

## Dependencies

- Phase 9 (GitOps Deployment) — provides ArgoCD project, applicationset, Helm chart structure
- Phase 12 (Admin Dashboard) — provides admin routes that need external access via ingress
- Phase 7B (Helm Chart) — provides ingress template already in `charts/mcpgateway/templates/ingress.yaml`

## Deliverables

- ArgoCD AppProject allows deployments to `cruvero-staging` and `cruvero-prod` namespaces
- ApplicationSet generates three Argo Applications (dev, staging, prod) with appropriate sync policies
- Staging image workflow triggered on `main` branch push, tags `staging-${SHA}`
- Production image workflow triggered on semver tag push (`v*`), tags `${TAG}`
- Dev ingress enabled with Traefik annotations, TLS termination, and cert-manager integration
- Helm values overlays extended for staging and production environments (preserve existing vault, secrets, monitoring)

## Packages Modified

- `deploy/argocd/project.yaml` — add staging and prod namespace destinations
- `deploy/argocd/applicationset.yaml` — add staging and prod list elements with sync policies
- `.github/workflows/image-staging.yml` — new staging image build workflow
- `.github/workflows/image-prod.yml` — new production image build workflow
- `charts/mcpgateway/values-dev.yaml` — add ingress configuration block
- `charts/mcpgateway/values-staging.yaml` — extend with image, TLS, config sections
- `charts/mcpgateway/values-prod.yaml` — extend with image, TLS, resources, config sections

## Success Criteria

- `helm template` renders cleanly for all three environments (dev, staging, prod)
- ArgoCD project allows all three namespace destinations
- ApplicationSet generates three distinct Application resources
- Staging and prod sync policies disable auto-sync (manual promotion)
- Dev ingress renders correct Traefik annotations and TLS config
- Image workflows use correct triggers and tag patterns
- No changes to existing dev deployment behavior
