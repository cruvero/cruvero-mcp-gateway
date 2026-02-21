# Phase 17A Implementation Prompts

## Prompt 1 of 2: Container Image Scanning with Trivy

### Required Reading (read these files before writing code)

- `docs/phases/PHASE17A.md` (full scope, Trivy section)
- `.github/workflows/image-dev.yml` (current dev workflow — understand step order and env vars)

### Task

Add Trivy container vulnerability scanning to the dev image build workflow.

1. **Add Trivy scan step** to `.github/workflows/image-dev.yml` between the "Build Image" and "Push Image" steps:

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

2. **Add `security-events: write` permission** to the workflow's `permissions` block:
   ```yaml
   permissions:
     contents: read
     security-events: write
   ```

   This is required for the SARIF upload to the GitHub Security tab.

3. **Verify step order** in the final workflow:
   1. Checkout
   2. Normalize Harbor Registry
   3. Login To Harbor
   4. Build Image
   5. Scan Image with Trivy ← new
   6. Upload Trivy SARIF ← new
   7. Push Image (only runs if Trivy passes)

4. **Add the same Trivy steps** to `image-staging.yml` and `image-prod.yml` (if they were created in Phase 16A). Adjust the `image-ref` to match each workflow's tag:
   - Staging: `cruvero-mcp-gateway:staging-${GITHUB_SHA}`
   - Prod: `cruvero-mcp-gateway:${GITHUB_REF_NAME}`

### Verification

```bash
# Validate workflow YAML
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/image-dev.yml'))"

# Check named step count (should be 6 — checkout has no name: field)
grep -c '^\s*- name:' .github/workflows/image-dev.yml
```

### Acceptance Criteria

- Trivy scan step runs after build, before push
- CRITICAL/HIGH vulnerabilities fail the workflow (exit-code 1)
- SARIF results uploaded to GitHub Security tab even on scan failure (`if: always()`)
- Push step is skipped when Trivy finds vulnerabilities
- `security-events: write` permission is present
- Workflow YAML is valid

---

## Prompt 2 of 2: Dependabot Configuration

### Required Reading (read these files before writing code)

- `docs/phases/PHASE17A.md` (Dependabot section)
- `.github/` directory listing (confirm `dependabot.yml` does not exist)

### Task

Create a Dependabot configuration file for automated dependency updates.

1. **Create `.github/dependabot.yml`:**

   ```yaml
   version: 2
   updates:
     # Go modules
     - package-ecosystem: gomod
       directory: /
       schedule:
         interval: weekly
         day: monday
       open-pull-requests-limit: 10
       labels:
         - dependencies
         - go
       commit-message:
         prefix: "chore(deps)"

     # GitHub Actions
     - package-ecosystem: github-actions
       directory: /
       schedule:
         interval: weekly
         day: monday
       open-pull-requests-limit: 5
       labels:
         - dependencies
         - ci
       commit-message:
         prefix: "chore(ci)"

     # Docker
     - package-ecosystem: docker
       directory: /
       schedule:
         interval: weekly
         day: monday
       open-pull-requests-limit: 5
       labels:
         - dependencies
         - docker
       commit-message:
         prefix: "chore(docker)"
   ```

2. **Verify configuration:**
   - Three ecosystems: `gomod`, `github-actions`, `docker`
   - All use weekly schedule on Monday
   - Commit message prefixes follow the project's conventional commit format
   - Labels match the project's label conventions

### Verification

```bash
# Validate YAML
python3 -c "import yaml; yaml.safe_load(open('.github/dependabot.yml'))"

# Check ecosystem count
grep -c 'package-ecosystem:' .github/dependabot.yml  # Should be 3
```

### Acceptance Criteria

- `.github/dependabot.yml` exists and is valid YAML
- Three ecosystems configured: gomod, github-actions, docker
- Commit message prefixes follow conventional commit format (`chore(deps)`, `chore(ci)`, `chore(docker)`)
- Weekly schedule on Monday for all ecosystems
- Reasonable PR limits (10 for Go, 5 for Actions and Docker)
- Labels are applied for easy filtering
