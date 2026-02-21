# Phase 9A Implementation Prompts

## Prompt 1 of 3: Devcontainer Baseline

### Required Reading (read these files before writing code)
- README.md
- LLM.md
- docs/phases/PHASE9A.md

### Task

Create the repository devcontainer baseline for local parity.

1. Create `.devcontainer/devcontainer.json`:
   - Base image with Go toolchain matching repository requirements
   - Install Helm, `kubectl`, and Argo CD CLI
   - Include useful VS Code settings/extensions for Go and YAML/Helm editing
   - Define `postCreateCommand` to run lightweight setup checks
2. If needed, add `.devcontainer/Dockerfile` for extra tooling not available in base image.
3. Document required local validation commands in README if missing.

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria

- Devcontainer starts without manual host setup
- Tooling is available in-container (`go`, `helm`, `kubectl`, `argocd`)
- Contributors can run quality gates in-container before deployment work

---

## Prompt 2 of 3: Helm Environment Overlays and Vault Value Model

### Required Reading (read these files before writing code)
- docs/phases/PHASE9A.md
- charts/mcpgateway/values.yaml
- docs/phases/PHASE7B.md

### Task

Implement the environment overlay pattern and vault contract in chart values.

1. Ensure `charts/mcpgateway/values.yaml` contains shared defaults.
2. Add/update environment overlays:
   - `values-dev.yaml`
   - `values-staging.yaml`
   - `values-prod.yaml`
3. Add/update vault and secret reference values:
   - `vault.enabled`, `vault.createAuth`, `vault.authRef`, `vault.mount`, `vault.path`, `vault.refreshAfter`
   - `secrets.existingSecret`
4. Ensure runtime credential fields are sourced from secret references only.

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria

- Overlay files are minimal deltas and easy to review
- Secret handling is Vault-operator-compatible and reference-based
- Base and overlays are compatible with Argo ApplicationSet value file selection

---

## Prompt 3 of 3: Local Validation Matrix

### Required Reading (read these files before writing code)
- docs/phases/PHASE9A.md
- Makefile

### Task

Add a repeatable local validation matrix for deployment preparation.

1. Add or update Make targets (if missing) for:
   - chart linting
   - chart rendering per environment
2. Provide a documented command matrix for dev/staging/prod render checks.
3. Ensure checks can be run entirely inside `.devcontainer`.

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria

- Validation matrix is documented and executable
- Helm rendering passes for each environment overlay
- Local verification is reproducible across contributor machines
