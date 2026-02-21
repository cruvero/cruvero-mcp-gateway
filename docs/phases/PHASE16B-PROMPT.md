# Phase 16B Implementation Prompts

## Prompt 1 of 2: Ingress Configuration in values-dev.yaml

### Required Reading (read these files before writing code)

- `docs/phases/PHASE16B.md` (full scope)
- `charts/mcpgateway/values.yaml` (lines 135-147: ingress section defaults)
- `charts/mcpgateway/values-dev.yaml` (current dev overlay — no ingress block)
- `charts/mcpgateway/templates/ingress.yaml` (existing Ingress template)

### Task

Enable and configure ingress in the dev environment for external admin dashboard access.

1. **Add ingress block** to `charts/mcpgateway/values-dev.yaml`:
   ```yaml
   ingress:
     enabled: true
     className: traefik
     host: cruvero-mcp-gateway.dev.gchinfo.com
     tls: true
     tlsSecretName: mcpgateway-dev-ingress-tls
     annotations:
       traefik.ingress.kubernetes.io/router.entrypoints: websecure
       traefik.ingress.kubernetes.io/router.tls: "true"
       traefik.ingress.kubernetes.io/service.serversscheme: https
       cert-manager.io/cluster-issuer: letsencrypt-prod
   ```

2. **Add NetworkPolicy ingress rule** for Traefik to `charts/mcpgateway/values-dev.yaml`:

   The NetworkPolicy template renders ingress rules from `networkPolicy.ingress.sources` (see `charts/mcpgateway/templates/networkpolicy.yaml` lines 17-30). Add the Traefik source to the existing `sources` list:

   ```yaml
   networkPolicy:
     ingress:
       sources:
         - namespaceSelector:
             matchLabels:
               kubernetes.io/metadata.name: traefik
           podSelector:
             matchLabels:
               app.kubernetes.io/name: traefik
     egress:
       otel:
         - namespaceSelector:
             matchLabels:
               kubernetes.io/metadata.name: cruvero-dev
           podSelector:
             matchLabels:
               app.kubernetes.io/instance: mcpgateway-dev
               app.kubernetes.io/component: otel-collector
   ```

   Note: The `egress.otel` block already exists in values-dev.yaml — include it in the final YAML to preserve it during the merge.

### Verification

```bash
# Render template and check Ingress resource
helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml | grep -A 30 'kind: Ingress'

# Verify no Ingress without the flag
helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml | grep 'kind: Ingress'  # Should return nothing

# Validate YAML
python3 -c "import yaml; yaml.safe_load(open('charts/mcpgateway/values-dev.yaml'))"
```

### Acceptance Criteria

- `helm template` with dev values renders an Ingress resource
- Ingress has `ingressClassName: traefik`
- Ingress host is `cruvero-mcp-gateway.dev.gchinfo.com`
- TLS section present with `tlsSecretName: mcpgateway-dev-ingress-tls`
- All four Traefik/cert-manager annotations are present
- Backend service port is 8443 (HTTPS)
- Without dev overlay, no Ingress resource is rendered (base values `enabled: false`)
- Existing dev values (image, TLS, config, vault) are unchanged

---

## Prompt 2 of 2: Validate Deployment & Document OIDC Redirect

### Required Reading (read these files before writing code)

- `docs/phases/PHASE16B.md` (OIDC section)
- `charts/mcpgateway/templates/ingress.yaml` (template structure)
- `charts/mcpgateway/templates/networkpolicy.yaml` (if it exists — check how ingress rules are rendered)

### Task

Validate the full Helm template output and document the OIDC redirect URI requirement.

1. **Validate all three environment templates:**
   ```bash
   # Dev (with ingress)
   helm template mcpgateway-dev charts/mcpgateway \
     -f charts/mcpgateway/values.yaml \
     -f charts/mcpgateway/values-dev.yaml \
     --set secrets.existingSecret=test \
     --set config.MCPGW_DB_URL=test

   # Staging (no ingress unless configured)
   helm template mcpgateway-staging charts/mcpgateway \
     -f charts/mcpgateway/values.yaml \
     -f charts/mcpgateway/values-staging.yaml \
     --set secrets.existingSecret=test \
     --set config.MCPGW_DB_URL=test

   # Prod (no ingress unless configured)
   helm template mcpgateway-prod charts/mcpgateway \
     -f charts/mcpgateway/values.yaml \
     -f charts/mcpgateway/values-prod.yaml \
     --set secrets.existingSecret=test \
     --set config.MCPGW_DB_URL=test
   ```

2. **Verify Ingress resource details:**
   - `metadata.annotations` includes all four Traefik/cert-manager annotations
   - `spec.ingressClassName` is `traefik`
   - `spec.tls[0].hosts[0]` is `cruvero-mcp-gateway.dev.gchinfo.com`
   - `spec.tls[0].secretName` is `mcpgateway-dev-ingress-tls`
   - `spec.rules[0].host` is `cruvero-mcp-gateway.dev.gchinfo.com`
   - `spec.rules[0].http.paths[0].backend.service.port.number` is `8443`

3. **Document OIDC redirect URI:**

   When the admin dashboard is accessible externally, the OIDC client registration in the IdP must include the callback URL. Add a comment in `values-dev.yaml` near the ingress or admin section:

   ```yaml
   # OIDC redirect URI for admin dashboard (configure in IdP):
   # https://cruvero-mcp-gateway.dev.gchinfo.com/admin/callback
   ```

### Verification

```bash
# Full template render for all environments should exit 0
helm template mcpgateway-dev charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml --set secrets.existingSecret=test --set config.MCPGW_DB_URL=test > /dev/null
helm template mcpgateway-staging charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-staging.yaml --set secrets.existingSecret=test --set config.MCPGW_DB_URL=test > /dev/null
helm template mcpgateway-prod charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml --set secrets.existingSecret=test --set config.MCPGW_DB_URL=test > /dev/null
echo "All templates render cleanly"
```

### Acceptance Criteria

- All three environment templates render without errors
- Dev environment includes Ingress resource with correct config
- Staging and prod do not include Ingress resource (unless explicitly configured)
- OIDC redirect URI is documented in values-dev.yaml as a comment
- NetworkPolicy allows Traefik ingress traffic
