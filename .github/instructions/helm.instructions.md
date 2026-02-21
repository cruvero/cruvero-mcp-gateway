---
applyTo: "charts/**"
---

# Helm Chart Review

## Environment Overlays

Four environment overlays exist: base (`values.yaml`), dev, staging, prod. Changes must be validated across all overlays.

## Conventions

- New env vars must be added to `templates/configmap.yaml` and base `values.yaml`.
- Secrets must never appear in values files; use `existingSecret` references.
- Template helpers must be in `_helpers.tpl`.
- Resource naming must use chart helper functions (e.g., `include "mcpgateway.fullname" .`).
- Labels must use `include "mcpgateway.labels" .`.

## Admin Dashboard Config

- Admin dashboard settings must be gated by `admin.enabled`.
- `admin.clientSecret` and `admin.sessionKey` must be stored in Kubernetes secrets only, never in values files.
- `admin.sessionTTL` must be a valid Go duration string (e.g., `24h`, `30m`).

## Device Flow Config

- Device flow settings must be gated by `deviceFlow.enabled`.
- `deviceFlow.clientSecret` must be stored in Kubernetes secrets only, never in values files.

## Cross-Overlay Consistency

- New values keys must appear in all four overlay files: base (`values.yaml`), dev, staging, and prod.
- Flag any variable that exists in one overlay but is missing from the others.

## Anti-Patterns

- Flag any hardcoded namespace, image tag, or cluster-specific value in templates.
