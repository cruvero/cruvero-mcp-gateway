# Phase 18: Developer Experience

## Goal

Streamline local development setup with standalone scripts for certificate generation and environment bootstrapping, add an admin dashboard dev mode that bypasses OIDC for local testing, and improve the devcontainer configuration for a better onboarding experience.

Covers parity audit Gap #9 (devcontainer missing port forwards, setup automation, and admin dev mode).

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [18A](PHASE18A.md) | Dev Scripts: Certificate Generation & Environment Setup | [2 prompts](PHASE18A-PROMPT.md) | scripts |
| [18B](PHASE18B.md) | Admin Dev Mode & Devcontainer Improvements | [2 prompts](PHASE18B-PROMPT.md) | config, admin, .devcontainer |

## Dependencies

- Phase 12 (Admin Dashboard) — provides admin auth middleware that needs dev mode bypass
- Phase 15 (NATS Resilience) — scripts should account for optional NATS TLS certs
- Phase 9A (Devcontainer Baseline) — provides existing devcontainer configuration to improve

## Deliverables

- `scripts/gen-dev-certs.sh` — standalone script generating CA, server, and client TLS certificates
- `scripts/dev-setup.sh` — full local environment bootstrap (certs, DB wait, migrations, session key, build)
- `MCPGW_ADMIN_DEV_MODE` config field with safety guards against production use
- Admin auth middleware bypasses OIDC in dev mode, injecting synthetic `dev@localhost` session
- Devcontainer forwards ports 8443 and 9090 automatically
- Devcontainer runs `dev-setup.sh` as post-create command

## Packages Modified

- `scripts/gen-dev-certs.sh` — new certificate generation script
- `scripts/dev-setup.sh` — new environment setup script
- `internal/config/config.go` — add `AdminDevMode` field
- `internal/admin/middleware.go` — dev mode bypass logic with safety guard
- `.devcontainer/devcontainer.json` — add `forwardPorts` and update `postCreateCommand`

## Success Criteria

- `scripts/gen-dev-certs.sh` generates valid CA, server, and client certs (idempotent)
- `scripts/dev-setup.sh` completes successfully on a fresh devcontainer
- Admin dashboard accessible without OIDC when `MCPGW_ADMIN_DEV_MODE=true`
- Dev mode logs a warning when `MCPGW_TLS_CA` path does not contain `dev` or `local` (heuristic guard)
- Devcontainer auto-forwards gateway and metrics ports
- `go test ./...` passes with >=80% coverage on modified packages
- `go vet ./...` and `golangci-lint run` pass clean
