Validate the Helm chart across all environment overlays.

## Instructions

1. **Lint** the chart:
   ```bash
   helm lint charts/mcpgateway
   ```

2. **Render** the chart for each environment overlay and validate:

   | Environment | Command |
   |-------------|---------|
   | base | `helm template mcpgw charts/mcpgateway -f charts/mcpgateway/values.yaml` |
   | dev | `helm template mcpgw charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml` |
   | staging | `helm template mcpgw charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-staging.yaml` |
   | prod | `helm template mcpgw charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml` |

3. For each rendered output, check:
   - Template renders without errors
   - All required fields are populated (image, ports, env vars)
   - Resource limits/requests are set (especially prod)
   - No duplicate resource names

4. Report results per environment:

```
Helm Chart Validation
─────────────────────
Lint:     PASS
base:     PASS (12 resources)
dev:      PASS (14 resources)
staging:  PASS (14 resources)
prod:     FAIL — missing resource limits on deployment
```

5. If any environment fails, show the specific error and suggest a fix.

## Constraints

- Always validate ALL four environments — don't skip any.
- Never modify chart files without asking first.
