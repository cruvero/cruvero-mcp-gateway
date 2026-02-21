# Phase 8A: Admin CLI

## Overview

Implement the `mcpgw` CLI binary with subcommands for running the gateway server and managing registered servers, API keys, policies, health, and migrations.

## Scope

### CLI Framework (`cmd/mcpgw/main.go`)
- Use standard library (flag) or lightweight CLI library (e.g., cobra or urfavecli -- keep dependency minimal)
- Root command: mcpgw
- Version flag: --version (prints version, commit, build date from ldflags)
- Global flags: --config (unused, reserved), --log-level, --log-format

### Subcommands

**mcpgw serve**
- Start the gateway server
- Load config from environment
- Initialize all components: store, identity, auth, registration, proxy, rate limiter, policy, events
- Start HTTP server, metrics server, NATS client, background sweeper
- Graceful shutdown on SIGINT/SIGTERM
- This is the primary production entrypoint

**mcpgw server list**
- Connect to gateway's own API or directly to database
- List all registered MCP servers with: name, status, SPIFFE ID, last heartbeat, capabilities summary
- Flags: --status (filter), --format (table/json)

**mcpgw server inspect {name-or-id}**
- Show detailed info for a specific server
- Include: all fields, full capabilities, policy profile, registration time

**mcpgw server deregister {name-or-id}**
- Remove a server registration
- Prompt for confirmation (--force to skip)

**mcpgw apikey create**
- Generate a new API key
- Flags: --name (required), --scopes (comma-separated, default "read"), --expires (duration, e.g., "30d"), --client-id
- Output: plaintext key (shown once), key ID, expiration
- Store hash in database

**mcpgw apikey list**
- List all API keys: name, client_id, scopes, expires_at, created_at
- Never show the key hash or plaintext

**mcpgw apikey revoke {id}**
- Revoke an API key by ID
- Prompt for confirmation (--force to skip)

**mcpgw policy list**
- List all policy profiles with: name, rate_limit, rate_burst, enforcement_mode, tool_allowlist count, tool_denylist count

**mcpgw policy set {name}**
- Set or update a policy profile
- Flags: --rate-limit, --rate-burst, --enforcement-mode, --tool-allowlist, --tool-denylist

**mcpgw policy reset {name}**
- Reset a policy profile to defaults

**mcpgw health**
- Check gateway health
- Connect to /healthz and /readyz endpoints
- Display: status, version, uptime, registered servers count, NATS status
- Exit code: 0 if healthy, 1 if unhealthy

**mcpgw migrate**
- Run database migrations
- Flags: --direction (up/down, default up), --steps (number, default all)
- Uses golang-migrate with embedded migration files

## Files Created

| File | Description |
|------|-------------|
| cmd/mcpgw/main.go | CLI entrypoint and command routing |
| cmd/mcpgw/serve.go | serve subcommand |
| cmd/mcpgw/server.go | server list/inspect/deregister subcommands |
| cmd/mcpgw/apikey.go | apikey create/list/revoke subcommands |
| cmd/mcpgw/policy.go | policy list/set/reset subcommands |
| cmd/mcpgw/health.go | health check subcommand |
| cmd/mcpgw/migrate.go | migration subcommand |

## Testing Requirements

- Test each subcommand's flag parsing and validation
- Test serve starts and stops correctly (short-lived test)
- Test server list/inspect with mock store
- Test apikey create outputs key format, list output format, revoke confirmation
- Test migrate up/down execution
- Test health check with mock endpoints
- Coverage: >=80% for cmd/mcpgw
