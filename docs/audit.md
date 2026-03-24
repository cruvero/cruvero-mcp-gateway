# Release Audit

**Date**: 2026-03-07  
**Remediation branch**: `feat/audit-remediation-go1261`

## Status

All findings from the March 7, 2026 audit were addressed on this branch.

Resolved items:

| ID | Resolution |
|----|------------|
| AUD-001 | Restored staging/prod chart values and image workflows so `make chart-validate` can validate all overlays again. |
| AUD-002 | Upgraded the repo from Go `1.25.7` to `1.26.1` across build, CI, devcontainer, and docs. |
| AUD-003 | Reworked the two gosec-flagged goroutine paths to use detached request-derived contexts instead of `context.Background()`. |
| AUD-004 | Replaced placeholder migration Make targets with real `mcpgw migrate` wrappers. |
| AUD-005 | Reconciled the image workflow/docs posture by removing the in-workflow Trivy gate and updating docs to match the current release path. |
| AUD-006 | Removed dead `docs/phases/` references from active instruction docs. |
| AUD-007 | Retired `MISSING-FUNCTIONALITY.md` as a live backlog and redirected readers to this audit plus issue tracking. |
| AUD-008 | Updated README migration inventory and repository layout to match the current tree. |

## Validation

Validated successfully on this branch with:

```bash
go build ./cmd/mcpgw
go test ./...
go test -race ./...
make quality
make chart-validate
```

## Remaining Manual Checks

The original audit also recommended manual verification that is still worth running before a public release:

- Browser-smoke the admin UI end-to-end, including auth, CSRF-protected forms, HTMX updates, and CSV export.
- Run the integration and security-tagged test suites in a production-like environment with backing services.
- Confirm staging/prod deployment remains intentionally disabled in ArgoCD while their release assets stay versioned and validated in-repo.
