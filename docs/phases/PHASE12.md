# Phase 12: OIDC Device Code Flow & Admin Dashboard

## Goal

Implement OAuth2 Device Code flow so IDE/CLI users can authenticate with a single browser login that lasts 24 hours. Build a minimal admin dashboard using Go templates + HTMX + Alpine.js with OIDC Authorization Code auth for observability and tool management.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [12A](PHASE12A.md) | OAuth2 Device Code Flow & CLI Auth Commands | [4 prompts](PHASE12A-PROMPT.md) | auth, config, cmd |
| [12B](PHASE12B.md) | Admin Dashboard with HTMX + Alpine.js | [4 prompts](PHASE12B-PROMPT.md) | admin (new), config |

## Dependencies

- Phase 10 (tool classification store, audit wiring — admin UI reads these)
- Phase 11 (distributed rate limiting, broadcaster — admin UI reads rate limit state, classification updates broadcast)
- Phase 2B (existing OIDC validation, API key auth)

## Deliverables

- `mcpgw auth login` — initiates Device Code flow, opens browser, caches tokens locally
- `mcpgw auth status` — shows current auth state (token expiry, identity)
- `mcpgw auth logout` — clears cached tokens
- `mcpgw mcp-proxy` — stdio-to-HTTP bridge that injects cached Bearer token for IDE integration
- Device authorization endpoints on the gateway (`/device/authorize`, `/device/token`, `/device/verify`)
- Admin dashboard at `/admin/` with OIDC Authorization Code + PKCE authentication
- Dashboard pages: overview, tool catalog with risk editing, rate limit state, audit log viewer, server status
- All admin actions audit-logged with the admin's OIDC `sub` claim
- Classification changes from admin UI broadcast to all pods via Phase 11 broadcaster

## Packages Created

- `internal/admin` — Admin dashboard handlers, templates, auth, middleware
- `internal/auth/device_flow.go` — Device Code flow endpoints and state management

## Packages Modified

- `internal/auth` — Device Code flow handlers
- `internal/config` — Device Code and admin config fields
- `internal/server` — Mount device and admin routes
- `cmd/mcpgw` — Auth subcommands, mcp-proxy subcommand

## Success Criteria

- Engineer runs `mcpgw auth login` → browser opens → authenticates at IdP → token cached → subsequent MCP requests use cached token
- Token auto-refreshes silently until refresh token expires (24h at IdP)
- `mcpgw mcp-proxy` works as stdio MCP bridge for IDEs that don't support custom auth headers
- Admin navigates to `/admin/` → redirected to IdP → authenticates → sees dashboard
- Admin can view and change tool risk classifications → change is persisted and broadcast
- Admin can view audit logs with date/tool/client filters
- Admin can view per-client rate limit state
- Non-admin OIDC users are rejected from `/admin/` with 403
- `go test ./...` passes with >=80% coverage on new packages
