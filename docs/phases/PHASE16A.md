# Phase 16A: ArgoCD Multi-Environment & Image Workflows

## Overview

Extend the ArgoCD AppProject and ApplicationSet from single-environment (dev) to multi-environment (dev, staging, prod) and create image build workflows for staging and production.

## Scope

### ArgoCD AppProject Multi-Environment (`deploy/argocd/project.yaml`)

The current AppProject only allows deployment to `cruvero-dev` namespace (line 12). Add staging and production destinations:

- Add destination: `server: https://kubernetes.default.svc`, `namespace: cruvero-staging`
- Add destination: `server: https://kubernetes.default.svc`, `namespace: cruvero-prod`
- Update description to reflect multi-environment scope
- Keep existing `clusterResourceWhitelist` and `namespaceResourceWhitelist` — same resource types are needed in all environments
- Add Ingress to `namespaceResourceWhitelist`:
  ```yaml
  - group: networking.k8s.io
    kind: Ingress
  ```

### ArgoCD ApplicationSet Multi-Environment (`deploy/argocd/applicationset.yaml`)

The current ApplicationSet list generator has a single `dev` element (line 16-19). Add staging and production elements:

- **Staging element**: `env: staging`, `namespace: cruvero-staging`, `valuesFile: values-staging.yaml`, `targetRevision: main`
- **Production element**: `env: prod`, `namespace: cruvero-prod`, `valuesFile: values-prod.yaml`, `targetRevision: main`

**Sync policy differences by environment:**
- Dev: automated sync with prune + selfHeal (current behavior, unchanged)
- Staging: automated sync with prune + selfHeal (same as dev for continuous deployment)
- Production: **no automated sync** — manual promotion only

Update the `templatePatch` to include image updater annotations for staging:

```yaml
{{- if eq .env "staging" }}
metadata:
  labels:
    image-updater: mcpgateway
  annotations:
    argocd-image-updater.argoproj.io/image-list: "mcpgateway=harbor.dev.gchinfo.com/cruvero/cruvero-mcp-gateway"
    argocd-image-updater.argoproj.io/mcpgateway.update-strategy: semver
    argocd-image-updater.argoproj.io/mcpgateway.allow-tags: "regexp:^staging-"
    argocd-image-updater.argoproj.io/mcpgateway.helm.image-name: image.repository
    argocd-image-updater.argoproj.io/mcpgateway.helm.image-tag: image.tag
{{- end }}
```

Production does not use image updater — images are pinned via semver tags in values-prod.yaml.

### Staging Image Build Workflow (`.github/workflows/image-staging.yml`)

Create a new workflow mirroring `image-dev.yml` structure:

- **Trigger**: push to `main` branch
- **Tags**: `staging-${GITHUB_SHA}` and `staging-latest`
- **Runner**: `[self-hosted, cruvero]` (same as dev)
- **Registry**: same Harbor registry with same env vars
- **Build args**: same Dockerfile with same `DOCKER_PROXY`, `GCR_PROXY`, `GOPROXY` args

### Production Image Build Workflow (`.github/workflows/image-prod.yml`)

Create a new workflow for production releases:

- **Trigger**: tag push matching `v[0-9]+.[0-9]+.[0-9]+*` (semver tags)
- **Tags**: `${GITHUB_REF_NAME}` (the semver tag itself) and `latest`
- **Runner**: `[self-hosted, cruvero]`
- **Registry**: same Harbor registry
- **Build args**: same as dev/staging

### Helm Values Overlays

Both `values-staging.yaml` and `values-prod.yaml` **already exist** with vault, secrets, and monitoring configuration. These files need to be **extended** (not replaced) with image, TLS, and config sections.

**`charts/mcpgateway/values-staging.yaml`** (extend existing):
- Add: 2 replicas, autoscaling (min 2, max 5), image, TLS, config sections
- Keep: existing vault (`createAuth: true`, `authRef: mcpgw-vault-auth-staging`), secrets (`mcpgw-runtime-staging`), monitoring

**`charts/mcpgateway/values-prod.yaml`** (extend existing):
- Add: image (pinned semver tag), TLS, resources (1Gi memory), config sections
- Keep: existing autoscaling (3-20), rateLimit, dragonfly, vault (`createAuth: true`, `authRef: mcpgw-vault-auth-prod`), secrets (`mcpgw-runtime-prod`), monitoring

## Files Created or Modified

| File | Description |
|------|-------------|
| `deploy/argocd/project.yaml` | Add staging and prod namespace destinations, add Ingress to whitelist |
| `deploy/argocd/applicationset.yaml` | Add staging and prod list elements, update templatePatch |
| `.github/workflows/image-staging.yml` | New staging image build workflow |
| `.github/workflows/image-prod.yml` | New production image build workflow |
| `charts/mcpgateway/values-staging.yaml` | Extend with image, TLS, config sections (preserve existing vault, secrets, monitoring) |
| `charts/mcpgateway/values-prod.yaml` | Extend with image, TLS, resources, config sections (preserve existing vault, dragonfly, secrets, monitoring) |

## Testing Requirements

- `helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml` renders cleanly
- `helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-staging.yaml` renders cleanly
- `helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml` renders cleanly
- ArgoCD manifests are valid YAML with correct apiVersions
- Image workflows use correct trigger patterns and tag formats
- Production ApplicationSet entry has no `automated` sync policy
