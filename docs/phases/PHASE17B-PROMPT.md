# Phase 17B Implementation Prompts

## Prompt 1 of 1: SonarQube Project Configuration & CI Step

### Required Reading (read these files before writing code)

- `docs/phases/PHASE17B.md` (full scope)
- `.github/workflows/ci.yml` (current CI pipeline — jobs: test, lint, build, coverage, integration, security)
- `coverage-thresholds.json` (package coverage thresholds)
- Project root directory listing (confirm `sonar-project.properties` does not exist)

### Task

Create SonarQube project configuration and optionally add a CI scanner step.

1. **Create `sonar-project.properties`** at the repository root:

   ```properties
   # SonarQube project configuration for cruvero-mcp-gateway
   sonar.projectKey=cruvero_mcp-gateway
   sonar.projectName=cruvero-mcp-gateway
   sonar.organization=cruvero

   # Source directories
   sonar.sources=cmd,internal
   sonar.tests=internal
   sonar.test.inclusions=**/*_test.go

   # Coverage and test reports
   sonar.go.coverage.reportPaths=coverage.out
   sonar.go.tests.reportPaths=test-report.json

   # Exclusions
   sonar.exclusions=**/testdata/**,**/vendor/**,docs/**,charts/**,deploy/**,scripts/**,migrations/**
   sonar.coverage.exclusions=**/testutil/**,**/*_test.go,cmd/mcpgw/main.go

   # Quality gate
   sonar.qualitygate.wait=true
   ```

2. **Add optional SonarQube job** to `.github/workflows/ci.yml`:

   Add a new `sonar` job after the `coverage` job. This job should be conditional on the `SONAR_TOKEN` secret being set, so it doesn't break CI when SonarQube is not configured:

   ```yaml
   sonar:
     runs-on:
       group: cruvero-org-runners
     if: github.event_name == 'push' && vars.SONAR_ENABLED == 'true'
     needs: [test, coverage]
     steps:
       - uses: actions/checkout@v4
         with:
           fetch-depth: 0
       - uses: actions/setup-go@v5
         with:
           go-version: '1.25.7'
           cache: false
       - name: Generate Coverage Report
         run: go test -coverprofile=coverage.out -covermode=atomic ./...
       - name: Generate Test Report
         run: go test -json ./... > test-report.json || true
       - name: SonarQube Scan
         uses: SonarSource/sonarqube-scan-action@v5
         env:
           SONAR_TOKEN: ${{ secrets.SONAR_TOKEN }}
           SONAR_HOST_URL: ${{ vars.SONAR_HOST_URL }}
   ```

   **Key decisions:**
   - The job is gated on `vars.SONAR_ENABLED == 'true'` to make it opt-in
   - `fetch-depth: 0` is required for SonarQube to analyze git blame
   - Coverage report is regenerated in this job because coverage artifacts from other jobs may not be available
   - `|| true` on test report generation prevents failure if any test fails (coverage check already gates quality)

3. **Do NOT modify** any existing CI jobs. The sonar job is purely additive.

### Verification

```bash
# Validate properties file syntax (no YAML, just key=value)
grep -c '=' sonar-project.properties  # Should be ~10 lines

# Validate CI YAML
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"

# Verify source paths exist
ls -d cmd internal  # Both should exist
```

### Acceptance Criteria

- `sonar-project.properties` exists at repository root
- Source paths (`cmd`, `internal`) match actual project structure
- Test inclusion pattern (`**/*_test.go`) matches Go test file convention
- Coverage exclusions skip test utilities and the main entry point
- Exclusions skip non-source directories (docs, charts, deploy, scripts, migrations)
- CI sonar job is conditional and does not break existing CI when SonarQube is not configured
- CI sonar job depends on test and coverage jobs completing first
- `sonar.qualitygate.wait=true` blocks the job until analysis completes
