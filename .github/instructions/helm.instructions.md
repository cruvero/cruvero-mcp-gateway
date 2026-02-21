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

## Anti-Patterns

- Flag any hardcoded namespace, image tag, or cluster-specific value in templates.
