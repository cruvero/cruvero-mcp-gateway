# Phase 16A Implementation Prompts

## Prompt 1 of 3: ArgoCD AppProject Multi-Environment

### Required Reading (read these files before writing code)

- `docs/phases/PHASE16A.md` (full scope)
- `deploy/argocd/project.yaml` (current AppProject with single `cruvero-dev` destination)

### Task

Extend the ArgoCD AppProject to allow deployments to staging and production namespaces.

1. **Update description** (line 7):
   ```yaml
   # Before:
   description: GitOps boundaries for dev deployment of cruvero-mcp-gateway.
   # After:
   description: GitOps boundaries for cruvero-mcp-gateway across dev, staging, and production environments.
   ```

2. **Add staging and production destinations** (after line 12):
   ```yaml
   destinations:
     - server: https://kubernetes.default.svc
       namespace: cruvero-dev
     - server: https://kubernetes.default.svc
       namespace: cruvero-staging
     - server: https://kubernetes.default.svc
       namespace: cruvero-prod
   ```

3. **Add Ingress to namespaceResourceWhitelist** (at the end, after the VaultStaticSecret entry at line 42):
   ```yaml
   - group: networking.k8s.io
     kind: Ingress
   ```

4. **Do NOT change** `clusterResourceWhitelist`, `orphanedResources`, or any other existing configuration.

### Verification

```bash
# Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('deploy/argocd/project.yaml'))"
# Check destinations count
grep -c 'namespace: cruvero-' deploy/argocd/project.yaml  # Should be 3
```

### Acceptance Criteria

- AppProject has three destinations: `cruvero-dev`, `cruvero-staging`, `cruvero-prod`
- Ingress resource type is whitelisted
- All existing configuration is preserved unchanged
- YAML is valid

---

## Prompt 2 of 3: ArgoCD ApplicationSet Multi-Environment

### Required Reading (read these files before writing code)

- `docs/phases/PHASE16A.md` (ApplicationSet section)
- `deploy/argocd/applicationset.yaml` (current ApplicationSet with single dev element)

### Task

Extend the ApplicationSet list generator to create Applications for staging and production.

1. **Add staging and production list elements** (after line 19):
   ```yaml
   generators:
     - list:
         elements:
           - env: dev
             namespace: cruvero-dev
             valuesFile: values-dev.yaml
             targetRevision: dev
           - env: staging
             namespace: cruvero-staging
             valuesFile: values-staging.yaml
             targetRevision: main
           - env: prod
             namespace: cruvero-prod
             valuesFile: values-prod.yaml
             targetRevision: main
   ```

2. **Add environment-conditional sync policy** using `templatePatch`:

   The current `templatePatch` (lines 59-70) only handles image updater for dev. Extend it to:
   - Dev: keep current automated sync + image updater (unchanged)
   - Staging: automated sync + image updater with staging tag pattern
   - Prod: **remove automated sync** (manual promotion)

   ```yaml
   templatePatch: |
     {{- if eq .env "dev" }}
     metadata:
       labels:
         image-updater: mcpgateway
       annotations:
         argocd-image-updater.argoproj.io/image-list: "mcpgateway=harbor.dev.gchinfo.com/cruvero/cruvero-mcp-gateway:dev"
         argocd-image-updater.argoproj.io/mcpgateway.update-strategy: digest
         argocd-image-updater.argoproj.io/mcpgateway.force-update: "true"
         argocd-image-updater.argoproj.io/mcpgateway.helm.image-name: image.repository
         argocd-image-updater.argoproj.io/mcpgateway.helm.image-tag: image.tag
     {{- else if eq .env "staging" }}
     metadata:
       labels:
         image-updater: mcpgateway
       annotations:
         argocd-image-updater.argoproj.io/image-list: "mcpgateway=harbor.dev.gchinfo.com/cruvero/cruvero-mcp-gateway"
         argocd-image-updater.argoproj.io/mcpgateway.update-strategy: semver
         argocd-image-updater.argoproj.io/mcpgateway.allow-tags: "regexp:^staging-"
         argocd-image-updater.argoproj.io/mcpgateway.helm.image-name: image.repository
         argocd-image-updater.argoproj.io/mcpgateway.helm.image-tag: image.tag
     {{- else if eq .env "prod" }}
     spec:
       syncPolicy:
         automated: null
         syncOptions:
           - CreateNamespace=true
           - ServerSideApply=true
           - RespectIgnoreDifferences=true
     {{- end }}
   ```

   The prod patch explicitly sets `automated: null` to remove auto-sync via strategic merge. Without the explicit null, the `automated` block from the base template would be preserved. The `syncOptions` are retained for manual sync operations.

### Verification

```bash
# Validate YAML syntax
python3 -c "import yaml; yaml.safe_load(open('deploy/argocd/applicationset.yaml'))"
# Check list elements count
grep -c 'env:' deploy/argocd/applicationset.yaml  # Should be 3 (in elements) + template refs
```

### Acceptance Criteria

- ApplicationSet list generator has three elements (dev, staging, prod)
- Dev sync policy unchanged (automated prune + selfHeal)
- Staging has automated sync with staging-specific image updater
- Production has no automated sync (manual promotion only)
- All environments target the correct namespace and values file
- Staging and prod both target `main` branch; dev targets `dev`
- YAML is valid with goTemplate syntax

---

## Prompt 3 of 3: Staging & Production Image Build Workflows

### Required Reading (read these files before writing code)

- `docs/phases/PHASE16A.md` (image workflows section)
- `.github/workflows/image-dev.yml` (reference for structure, env vars, runner labels)

### Task

Create staging and production image build workflows mirroring the dev workflow structure.

1. **Create `.github/workflows/image-staging.yml`:**
   ```yaml
   name: Build And Push Staging Image

   on:
     push:
       branches:
         - main
     workflow_dispatch:

   permissions:
     contents: read

   env:
     REGISTRY: ${{ secrets.HARBOR_URL || vars.HARBOR_URL || 'harbor.dev.gchinfo.com' }}
     HARBOR_PROJECT: cruvero
     DOCKER_PROXY: docker-cache.dev.gchinfo.com
     GCR_PROXY: gcr-cache.dev.gchinfo.com
     GOPROXY: https://nexus.dev.gchinfo.com/repository/go-proxy/,direct
     DOCKER_API_VERSION: "1.41"

   jobs:
     docker-build-push:
       runs-on: [self-hosted, cruvero]
       steps:
         - uses: actions/checkout@v4

         - name: Normalize Harbor Registry
           shell: bash
           run: |
             set -euo pipefail
             registry="${REGISTRY:-harbor.dev.gchinfo.com}"
             registry="${registry#https://}"
             registry="${registry#http://}"
             registry="${registry%/}"
             echo "REGISTRY_NORM=${registry}" >> "$GITHUB_ENV"

         - name: Login To Harbor
           uses: docker/login-action@v3
           with:
             registry: ${{ env.REGISTRY_NORM }}
             username: ${{ secrets.HARBOR_USERNAME }}
             password: ${{ secrets.HARBOR_PASSWORD || secrets.HARBOR_TOKEN }}

         - name: Build Image
           shell: bash
           run: |
             set -euo pipefail
             docker build \
               -f ./Dockerfile \
               --build-arg DOCKER_PROXY="${DOCKER_PROXY}" \
               --build-arg GCR_PROXY="${GCR_PROXY}" \
               --build-arg GOPROXY="${GOPROXY}" \
               -t "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:staging-${GITHUB_SHA}" \
               -t "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:staging-latest" \
               .

         - name: Push Image
           shell: bash
           run: |
             set -euo pipefail
             docker push "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:staging-${GITHUB_SHA}"
             docker push "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:staging-latest"
   ```

2. **Create `.github/workflows/image-prod.yml`:**
   ```yaml
   name: Build And Push Production Image

   on:
     push:
       tags:
         - 'v[0-9]+.[0-9]+.[0-9]+*'
     workflow_dispatch:

   permissions:
     contents: read

   env:
     REGISTRY: ${{ secrets.HARBOR_URL || vars.HARBOR_URL || 'harbor.dev.gchinfo.com' }}
     HARBOR_PROJECT: cruvero
     DOCKER_PROXY: docker-cache.dev.gchinfo.com
     GCR_PROXY: gcr-cache.dev.gchinfo.com
     GOPROXY: https://nexus.dev.gchinfo.com/repository/go-proxy/,direct
     DOCKER_API_VERSION: "1.41"

   jobs:
     docker-build-push:
       runs-on: [self-hosted, cruvero]
       steps:
         - uses: actions/checkout@v4

         - name: Normalize Harbor Registry
           shell: bash
           run: |
             set -euo pipefail
             registry="${REGISTRY:-harbor.dev.gchinfo.com}"
             registry="${registry#https://}"
             registry="${registry#http://}"
             registry="${registry%/}"
             echo "REGISTRY_NORM=${registry}" >> "$GITHUB_ENV"

         - name: Login To Harbor
           uses: docker/login-action@v3
           with:
             registry: ${{ env.REGISTRY_NORM }}
             username: ${{ secrets.HARBOR_USERNAME }}
             password: ${{ secrets.HARBOR_PASSWORD || secrets.HARBOR_TOKEN }}

         - name: Build Image
           shell: bash
           run: |
             set -euo pipefail
             docker build \
               -f ./Dockerfile \
               --build-arg DOCKER_PROXY="${DOCKER_PROXY}" \
               --build-arg GCR_PROXY="${GCR_PROXY}" \
               --build-arg GOPROXY="${GOPROXY}" \
               -t "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:${GITHUB_REF_NAME}" \
               -t "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:latest" \
               .

         - name: Push Image
           shell: bash
           run: |
             set -euo pipefail
             docker push "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:${GITHUB_REF_NAME}"
             docker push "${REGISTRY_NORM}/${HARBOR_PROJECT}/cruvero-mcp-gateway:latest"
   ```

3. **Extend existing `charts/mcpgateway/values-staging.yaml`:**

   This file already exists with vault, secrets, and monitoring config. **Preserve all existing content** and merge these additions:

   ```yaml
   # --- ADD these sections (merge with existing file) ---

   replicaCount: 2

   autoscaling:
     enabled: true
     minReplicas: 2
     maxReplicas: 5
     targetCPU: 70

   image:
     repository: harbor.dev.gchinfo.com/cruvero/cruvero-mcp-gateway
     tag: staging-latest
     pullPolicy: Always

   imagePullSecrets:
     - name: harbor-pull-secret

   tls:
     certManager:
       enabled: true
       secretName: mcpgateway-staging-tls
       issuerRef:
         name: vault-mcp-gateway-server
         kind: ClusterIssuer
         group: cert-manager.io
       dnsNames:
         - mcpgateway-staging
         - mcpgateway-staging.cruvero-staging
         - mcpgateway-staging.cruvero-staging.svc
         - mcpgateway-staging.cruvero-staging.svc.cluster.local

   # Merge into existing config: block
   config:
     MCPGW_TLS_CERT: "/tls/tls.crt"
     MCPGW_TLS_KEY: "/tls/tls.key"
     MCPGW_TLS_CA: "/tls/ca.crt"
     MCPGW_SPIFFE_ALLOW_PREFIX: "spiffe://cruvero.dev/ns/cruvero-staging/sa/"
     MCPGW_CRUVERO_ENABLED: "true"
     MCPGW_GATEWAY_ID: "mcpgateway-staging"
     MCPGW_RATE_LIMIT_BACKEND: "memory"
     OTEL_EXPORTER_OTLP_ENDPOINT: "http://mcpgateway-staging-otel-collector:4318"

   tracing:
     enabled: true

   # --- KEEP existing vault, secrets, monitoring sections unchanged ---
   # vault:
   #   enabled: true
   #   createAuth: true
   #   authRef: mcpgw-vault-auth-staging
   #   mount: kubernetes
   #   path: secret/data/mcpgateway/staging/runtime
   #   ...
   ```

4. **Extend existing `charts/mcpgateway/values-prod.yaml`:**

   This file already exists with autoscaling, dragonfly, vault, secrets, and monitoring config. **Preserve all existing content** and merge these additions:

   ```yaml
   # --- ADD these sections (merge with existing file) ---

   image:
     repository: harbor.dev.gchinfo.com/cruvero/cruvero-mcp-gateway
     tag: v1.0.0  # Pinned to specific semver release
     pullPolicy: IfNotPresent

   imagePullSecrets:
     - name: harbor-pull-secret

   resources:
     requests:
       cpu: 250m
       memory: 256Mi
     limits:
       cpu: "1"
       memory: 1Gi

   tls:
     certManager:
       enabled: true
       secretName: mcpgateway-prod-tls
       issuerRef:
         name: vault-mcp-gateway-server
         kind: ClusterIssuer
         group: cert-manager.io
       dnsNames:
         - mcpgateway-prod
         - mcpgateway-prod.cruvero-prod
         - mcpgateway-prod.cruvero-prod.svc
         - mcpgateway-prod.cruvero-prod.svc.cluster.local

   # Merge into existing config: block
   config:
     MCPGW_TLS_CERT: "/tls/tls.crt"
     MCPGW_TLS_KEY: "/tls/tls.key"
     MCPGW_TLS_CA: "/tls/ca.crt"
     MCPGW_SPIFFE_ALLOW_PREFIX: "spiffe://cruvero.dev/ns/cruvero-prod/sa/"
     MCPGW_CRUVERO_ENABLED: "true"
     MCPGW_GATEWAY_ID: "mcpgateway-prod"
     MCPGW_RATE_LIMIT_BACKEND: "dragonfly"
     MCPGW_NATS_TLS_ENABLED: "true"
     OTEL_EXPORTER_OTLP_ENDPOINT: "http://mcpgateway-prod-otel-collector:4318"

   tracing:
     enabled: true

   # --- KEEP existing autoscaling, rateLimit, dragonfly, vault, secrets, monitoring ---
   # These sections already exist and must be preserved.
   ```

### Verification

```bash
# Validate workflow YAML
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/image-staging.yml'))"
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/image-prod.yml'))"

# Helm template validation
helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-staging.yaml
helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml
```

### Acceptance Criteria

- Staging workflow triggers on `main` push, tags images with `staging-${SHA}` and `staging-latest`
- Production workflow triggers on semver tag push, tags images with `${TAG}` and `latest`
- Both workflows use same registry, runner, and build args as dev
- `values-staging.yaml` renders clean Helm templates
- `values-prod.yaml` renders clean Helm templates
- Production uses DragonflyDB and NATS TLS
- Staging uses 2 replicas; prod uses 3 with autoscaling to 20
