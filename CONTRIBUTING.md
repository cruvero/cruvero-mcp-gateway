# Contributing

Thanks for contributing to `cruvero-mcp-gateway`.

## Contribution Workflow

1. Create an issue first.
2. Fork the repository.
3. Create a branch from `dev`.
4. Implement the change and keep commits focused.
5. Run all required local quality gates.
6. Open a PR targeting `dev` and link the issue.

## Repository Access And Forking

Contributions are expected from forks.  
If GitHub organization policy has forking disabled for this repository, request maintainer guidance and use a contributor branch in the upstream repo instead.

## Branch And PR Rules

- Do not merge directly to `main` or `dev`.
- All merges into `main` and `dev` must go through pull requests.
- Open PRs against `dev`.
- Reference the related issue in the PR body (`Closes #<issue-number>`).
- Keep one logical change per PR.

## Commit Style

Use conventional commits.

Examples:

- `feat(proxy): add longest-prefix resource routing guard`
- `fix(auth): reject expired api keys in middleware`
- `docs(readme): document dev image publish workflow`

## Required Local Checks

Run these commands locally and ensure they all pass before opening a PR:

```bash
go build ./cmd/mcpgw
go test -race ./...
go vet ./...
golangci-lint run ./...
staticcheck ./...
govulncheck ./...
./scripts/check-gosec.sh
./scripts/check-dupl.sh
./scripts/check-godoc.sh
./scripts/check-coverage.sh
go test -tags security ./internal/testutil
```

Integration and load suites are environment-dependent and should be run when relevant:

```bash
go test -tags integration ./internal/testutil -run 'TestFullLifecycle|TestAuthFlow'
go test -tags load ./internal/testutil
```

## Quality Expectations

- Keep package coverage at or above the configured threshold.
- All exported declarations must be documented.
- No new linter, static analysis, vulnerability, or security scan failures.
- Keep changes compatible with the repository module path:
  `github.com/cruvero/mcp-gateway`.

## Pull Request Checklist

- Issue created and linked.
- Branch rebased on latest `dev`.
- Local checks passed.
- CI checks passed.
- Any docs or migration updates included when required.
