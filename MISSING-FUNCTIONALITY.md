# Missing Functionality

Features documented in project files but not yet implemented. This serves as a backlog for tracking documentation-to-code gaps.

## B1: Dev TLS Certificate Generation Script

- **Documented in**: README.md Quick Start
- **Status**: `scripts/gen-dev-certs.sh` does not exist. The Quick Start has been updated to remove the reference.
- **Priority**: Low -- developers can generate certs manually or via `openssl` commands.
- **Action**: Create the script if a standardized dev cert workflow is needed.

## B2: SonarQube Integration

- **Documented in**: README.md GitHub Actions Standards
- **Status**: No `sonar-project.properties` file and no Sonar workflow in `.github/workflows/`. README references `SONAR_HOST_URL`, `SONAR_TOKEN`, and `SONAR_PROJECT_KEY` org/repo secrets.
- **Priority**: Medium -- static analysis coverage is valuable but can be deferred until CI matures.
- **Action**: Create `sonar-project.properties` with correct source/test/coverage paths, add a Sonar CI workflow.

## B3: Staging and Production ArgoCD Environments

- **Documented in**: docs/GITOPS-ROLLOUT.md (dev, staging, prod validation checklists)
- **Status**: `deploy/argocd/applicationset.yaml` only includes `dev` in the list generator. `project.yaml` only allows the `cruvero-dev` namespace.
- **Priority**: High -- required for promotion beyond dev.
- **Action**: Add staging and prod entries to the ApplicationSet list generator and expand AppProject namespace permissions.

## B4: Staging and Production Container Image Build Workflows

- **Documented in**: docs/GITOPS-ROLLOUT.md
- **Status**: Only `image-dev.yml` exists in `.github/workflows/`. No staging or prod image workflows.
- **Priority**: High -- blocked by B3 (no point building images for environments that don't exist).
- **Action**: Create `image-staging.yml` and `image-prod.yml` workflows (or parameterize `image-dev.yml`).

## B5: Makefile migrate-up / migrate-down Targets

- **Documented in**: Makefile
- **Status**: Fixed -- targets now delegate to `go run ./cmd/mcpgw migrate up/down`.
- **Priority**: Resolved.

## B6: Dependabot Configuration

- **Documented in**: CLAUDE.md global instructions (vulnerability scanning mention)
- **Status**: No `.github/dependabot.yml` exists. Go module and GitHub Actions dependency updates are not automated.
- **Priority**: Medium -- reduces manual effort for keeping dependencies current.
- **Action**: Create `.github/dependabot.yml` with Go modules and GitHub Actions ecosystems.

## B7: Container Image Scanning

- **Documented in**: README.md CI section implies security scanning
- **Status**: No container scan step (Trivy or equivalent) in any CI workflow.
- **Priority**: Medium -- important for production readiness.
- **Action**: Add a Trivy or Grype scan step to the image build workflow(s).

## B8: Devcontainer Go Version Alignment

- **Documented in**: README.md, `.devcontainer/Dockerfile`
- **Status**: Fixed -- devcontainer base image updated from Go 1.24 to 1.25 to match `go.mod`.
- **Priority**: Resolved.
