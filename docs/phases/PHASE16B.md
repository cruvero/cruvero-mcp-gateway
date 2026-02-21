# Phase 16B: Admin Dashboard Ingress

## Overview

Enable Kubernetes ingress in the dev environment so the admin dashboard is accessible from outside the cluster via HTTPS, using Traefik as the ingress controller with cert-manager for TLS certificates.

## Scope

### Ingress Configuration in values-dev.yaml (`charts/mcpgateway/values-dev.yaml`)

The ingress template already exists at `charts/mcpgateway/templates/ingress.yaml` and renders conditionally on `ingress.enabled`. The base `values.yaml` has the ingress section (lines 135-147) with `enabled: false`. Enable and configure it in the dev overlay:

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

**Key considerations:**
- `serversscheme: https` is required because the gateway backend serves HTTPS (port 8443)
- The ingress terminates TLS at the edge and re-encrypts to the backend
- cert-manager handles the ingress TLS certificate lifecycle via the cluster-issuer annotation
- `tlsSecretName` is separate from the gateway's internal TLS secret (`mcpgateway-dev-tls`)

### OIDC Redirect URI Update

When the admin dashboard becomes accessible via ingress, the OIDC redirect URI must include the external hostname. Document that the OIDC client registration in the IdP must include:

```
https://cruvero-mcp-gateway.dev.gchinfo.com/admin/callback
```

This is a manual IdP configuration step, not automated by the chart.

### NetworkPolicy Ingress Rule

The existing NetworkPolicy template (`charts/mcpgateway/templates/networkpolicy.yaml`) renders ingress rules from `.Values.networkPolicy.ingress.sources` (lines 17-30). Add the Traefik ingress controller to the **existing `sources` list** in `values-dev.yaml`:

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
```

This adds Traefik as an allowed ingress source alongside any other sources defined in the base values. The template iterates over `ingress.sources` to build the NetworkPolicy rules for port 8443.

## Files Created or Modified

| File | Description |
|------|-------------|
| `charts/mcpgateway/values-dev.yaml` | Add ingress configuration block with Traefik annotations |
| `charts/mcpgateway/values-dev.yaml` | Add NetworkPolicy ingress rule for Traefik (if needed) |

## Testing Requirements

- `helm template -f values.yaml -f values-dev.yaml charts/mcpgateway` renders Ingress resource
- Ingress has correct className, host, TLS config, and annotations
- Ingress backend points to service port 8443 (HTTPS)
- Without ingress enabled (base values), no Ingress resource is rendered
- NetworkPolicy allows traffic from Traefik namespace
