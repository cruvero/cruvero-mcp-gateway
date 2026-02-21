# Phase 9B Implementation Prompts

## Prompt 1 of 3: AppProject Definition

### Required Reading (read these files before writing code)
- docs/phases/PHASE9B.md
- docs/OVERVIEW.md (GitOps deployment section)

### Task

Create `deploy/argocd/project.yaml` for mcpgateway GitOps boundaries.

Use this in-repo structural pattern:
- AppProject is least-privilege: explicit `sourceRepos`, explicit destination namespaces, and bounded resource scopes.
- Staging/prod remain gated by sync windows until intentionally opened.
- Orphaned resources policy must be explicit (do not rely on Argo defaults).

1. Define `AppProject` in `argocd` namespace.
2. Configure `sourceRepos` for the gateway repository.
3. Configure allowed destinations (dev/staging/prod namespaces in-cluster).
4. Add sync window policy that keeps staging/prod blocked by default until explicitly opened.
5. Configure orphaned resources handling appropriate for deployment controllers.

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria

- Project validates in Argo CD
- Repo and namespace boundaries are explicit and least-privilege
- Staging/prod gating policy is enforced

---

## Prompt 2 of 3: ApplicationSet Definition

### Required Reading (read these files before writing code)
- docs/phases/PHASE9B.md
- charts/mcpgateway/values.yaml

### Task

Create `deploy/argocd/applicationset.yaml` using a list generator and Helm overlays.

Use this in-repo structural pattern:
- List generator defines one entry per environment with explicit promotion/sync semantics.
- Application template renders `charts/mcpgateway` using base `values.yaml` plus environment overlay.
- Dev uses auto-sync by default; staging/prod are gated.

1. Define per-environment list entries with:
   - `env`, `namespace`, `valuesFile`, `targetRevision`, `autoSync`, `prune`, `selfHeal`
2. Set chart source:
   - `path: charts/mcpgateway`
   - value files: `values.yaml` + selected environment file
3. Configure sync options:
   - `CreateNamespace=true`
   - `ServerSideApply=true`
4. Add ignore-differences rules for expected mutable fields.
5. Enable auto-sync for dev only.

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria

- Generated applications point to correct namespaces and values files
- Dev autosync enabled; staging/prod gated
- Drift rules reduce false OutOfSync noise without masking real changes

---

## Prompt 3 of 3: Rollout Validation and Rollback Playbook

### Required Reading (read these files before writing code)
- docs/phases/PHASE9B.md
- deploy/argocd/project.yaml
- deploy/argocd/applicationset.yaml

### Task

Document and validate rollout operations.

1. Add deployment checklist for dev rollout validation:
   - Argo app health and sync status
   - pod readiness
   - service reachability
   - metrics endpoint availability
2. Add staged promotion checklist for staging/prod enablement.
3. Add rollback procedure:
   - revert Git change
   - sync in Argo
   - verify recovery metrics and health

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria

- Operators have deterministic rollout and rollback steps
- Deployment checks are explicit and repeatable
- Playbook aligns with AppProject/ApplicationSet policy
