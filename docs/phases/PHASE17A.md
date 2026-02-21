# Phase 17A: Container Scanning & Dependabot

## Overview

Add Trivy container vulnerability scanning to all image build workflows and configure Dependabot for automated dependency update proposals.

## Scope

### Trivy Container Scanning (`.github/workflows/image-dev.yml`)

Add a Trivy scan step to the dev image workflow between the Build and Push steps. The scan should:

- Use `aquasecurity/trivy-action` (latest stable version)
- Scan the locally built image (before push)
- Output format: `sarif` for GitHub Security integration
- Severity filter: `CRITICAL,HIGH`
- Exit code: `1` — fail the workflow on CRITICAL/HIGH vulnerabilities
- Upload SARIF results to GitHub Security tab via `github/codeql-action/upload-sarif`

**Workflow step placement:**
1. Checkout
2. Normalize Harbor Registry
3. Login To Harbor
4. Build Image
5. **Trivy Scan** ← new step
6. **Upload SARIF** ← new step
7. Push Image (only runs if Trivy passes)

The same Trivy step pattern should be documented for `image-staging.yml` and `image-prod.yml` (created in Phase 16A).

### Trivy Configuration Details

```yaml
- name: Scan Image with Trivy
  uses: aquasecurity/trivy-action@0.28.0
  with:
    image-ref: "${{ env.REGISTRY_NORM }}/${{ env.HARBOR_PROJECT }}/cruvero-mcp-gateway:dev-${{ github.sha }}"
    format: sarif
    output: trivy-results.sarif
    severity: CRITICAL,HIGH
    exit-code: "1"

- name: Upload Trivy SARIF
  uses: github/codeql-action/upload-sarif@v3
  if: always()
  with:
    sarif_file: trivy-results.sarif
```

Note: `if: always()` on the upload step ensures SARIF results are uploaded even when Trivy finds vulnerabilities (exit-code 1 would normally skip subsequent steps).

### Dependabot Configuration (`.github/dependabot.yml`)

Create a Dependabot configuration file with three ecosystems:

**Go modules (`gomod`):**
- Directory: `/`
- Schedule: weekly (Monday)
- Open PR limit: 10
- Labels: `dependencies`, `go`
- Reviewers: (leave empty for team default)

**GitHub Actions (`github-actions`):**
- Directory: `/`
- Schedule: weekly (Monday)
- Open PR limit: 5
- Labels: `dependencies`, `ci`

**Docker (`docker`):**
- Directory: `/`
- Schedule: weekly (Monday)
- Open PR limit: 5
- Labels: `dependencies`, `docker`

## Files Created or Modified

| File | Description |
|------|-------------|
| `.github/workflows/image-dev.yml` | Add Trivy scan + SARIF upload steps between build and push |
| `.github/dependabot.yml` | New Dependabot configuration for gomod, github-actions, docker |

## Testing Requirements

- Image workflow YAML is valid (no syntax errors)
- Trivy step references a pinned action version (not `@latest`)
- SARIF upload uses `if: always()` to upload even on scan failure
- Dependabot config is valid YAML with correct ecosystem names
- Push step only executes after successful Trivy scan
