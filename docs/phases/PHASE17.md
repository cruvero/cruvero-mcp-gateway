# Phase 17: CI/CD Supply Chain Hardening

## Goal

Add container image vulnerability scanning to the CI pipeline, configure automated dependency update proposals via Dependabot, and establish SonarQube integration for continuous code quality analysis.

Covers parity audit gaps: Gap #6 (no container scanning in image workflows), Gap #7 (no Dependabot configuration), and Gap #8 (no `sonar-project.properties`).

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [17A](PHASE17A.md) | Container Scanning & Dependabot | [2 prompts](PHASE17A-PROMPT.md) | .github/workflows, .github |
| [17B](PHASE17B.md) | SonarQube Integration | [1 prompt](PHASE17B-PROMPT.md) | sonar-project.properties, .github/workflows |

## Dependencies

- Phase 7 (Kubernetes & Observability) — provides Dockerfile and image build infrastructure
- Phase 8B (CI Pipeline) — provides `.github/workflows/ci.yml` where SonarQube step is added

## Deliverables

- Trivy container scanning runs after image build in all image workflows (dev, staging, prod)
- CRITICAL and HIGH vulnerabilities fail the workflow (exit-code 1)
- SARIF reports uploaded to GitHub Security tab
- Dependabot configured for Go modules, GitHub Actions, and Docker ecosystems
- `sonar-project.properties` with correct source, test, and coverage paths
- Optional SonarQube scanner step in CI workflow

## Packages Modified

- `.github/workflows/image-dev.yml` — add Trivy scan step
- `.github/workflows/image-staging.yml` — add Trivy scan step (if created in Phase 16A)
- `.github/workflows/image-prod.yml` — add Trivy scan step (if created in Phase 16A)
- `.github/dependabot.yml` — new Dependabot configuration
- `sonar-project.properties` — new SonarQube project configuration
- `.github/workflows/ci.yml` — optional SonarQube scanner step

## Success Criteria

- Trivy scan blocks image push on CRITICAL/HIGH CVEs
- Dependabot creates PRs for outdated Go modules, Actions, and Docker images
- `sonar-project.properties` correctly maps source and test directories
- SonarQube scanner step runs without configuration errors (when enabled)
- Existing CI jobs unaffected by new additions
