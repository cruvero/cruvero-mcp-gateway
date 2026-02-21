# Copilot Instructions — cruvero-mcp-gateway

## Project Overview

Go 1.25.7 mTLS reverse-proxy gateway for MCP (Model Context Protocol) servers on Kubernetes.

- **Module**: `github.com/cruvero/mcp-gateway`
- **Configuration**: All configuration is via `MCPGW_*` environment variables. There are no YAML/TOML/JSON config files.
- **Admin Dashboard**: OIDC AuthCode+PKCE login, AES-256-GCM encrypted sessions, CSRF protection, HTMX/Alpine.js templates for managing servers, API keys, policies, and tool classifications.
- **Device Code Flow**: OAuth2 Device Authorization Grant for CLI authentication (`mcpgw auth login`), with secure token caching.
- **Branch strategy**: `dev` is the integration branch. `main` is production.

## PR Rules

- All PRs target the `dev` branch. Never push directly to `dev` or `main`.
- One logical change per PR. Reference issues with `Closes #<number>`.
- Conventional commits are required: `type(scope): description`
  - **Types**: feat, fix, docs, test, chore, refactor, perf, ci, build
  - **Scopes**: proxy, auth, admin, store, config, events, identity, policy, ratelimit, registration, resilience, server, types, testutil, migrations, helm, ci, readme

## Code Standards

- No hardcoded secrets, credentials, or connection strings.
- No command injection, SQL injection, XSS, or other OWASP top-10 vulnerabilities.
- Validate at system boundaries (user input, external APIs); trust internal code.
- Prefer `log/slog` structured logging.
- All exported symbols must have godoc comments.

## Quality Gates

Every PR must pass before merge:

```
go build ./cmd/mcpgw
go test -race ./...
make quality   # vet, lint, staticcheck, govulncheck, gosec, dupl, godoc-check, coverage-check
```

## What NOT to Include

- Never mention AI assistants in commits, code comments, or documentation.
- No TODO comments without an associated issue number: `// TODO(#123): ...`
