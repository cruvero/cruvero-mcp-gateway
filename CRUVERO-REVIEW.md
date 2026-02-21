# CRUVERO Review: MCP Gateway Integration Remediation

## Purpose & Scope

This review identifies the code, API, UI, and operational changes required to fully integrate `cruvero-mcp-gateway` with Cruvero in `../cruvero`.

Scope covered:
- Integration-focused runtime interoperability (gateway <-> Cruvero)
- Full MCP platform review (contracts, events, config, data model, runtime behavior)
- UI/API operator workflows for MCP + gateway administration

Evidence is code-referenced and prioritized as `P0` (must fix), `P1` (high-value), `P2` (hardening/optimization).

## System Map (Current State)

- Cruvero MCP runtime and transport stack:
  - `../cruvero/internal/mcp/config.go`
  - `../cruvero/internal/mcp/transport_gateway.go`
  - `../cruvero/internal/tools/mcp_bridge.go`
- Cruvero gateway bridge package:
  - `../cruvero/internal/mcpgw/types.go`
  - `../cruvero/internal/mcpgw/subscriber.go`
  - `../cruvero/internal/mcpgw/publisher.go`
- Cruvero UI/API MCP surfaces:
  - `../cruvero/cmd/ui/mcp_api.go`
  - `../cruvero/cmd/ui/mcpgw_api.go`
  - `../cruvero/cmd/ui/runtime_routes.go`
  - `../cruvero/cmd/ui/frontend/src/pages/McpStatusPage.tsx`
  - `../cruvero/cmd/ui/frontend/src/api/mcp.ts`
- Gateway contract source of truth in this repo:
  - `docs/OVERVIEW.md` section 11 (`mcpgw.{gateway_id}.events.*`, `mcpgw.{gateway_id}.config.*`)

## Findings Backlog (P0/P1/P2)

### P0-INT-001: NATS subject contract mismatch (gateway-id scoping)
- Priority: `P0`
- Area: `Backend`, `Events`
- Problem: Cruvero subscribes/publishes legacy global subjects (`mcpgw.events.*`, `mcpgw.config.*`) while gateway contract is gateway-id scoped (`mcpgw.{gateway_id}.events.*`, `mcpgw.{gateway_id}.config.*`).
- Evidence:
  - Gateway contract: `docs/OVERVIEW.md:485`, `docs/OVERVIEW.md:498`, `docs/OVERVIEW.md:515`
  - Cruvero legacy constants: `../cruvero/internal/mcpgw/types.go:13`, `../cruvero/internal/mcpgw/types.go:19`
  - Subscriber/publisher usage: `../cruvero/internal/mcpgw/subscriber.go:47`, `../cruvero/internal/mcpgw/publisher.go:43`
- Required Change:
  - Replace fixed constants with subject builders that include `gateway_id`.
  - Add dual-subscription compatibility window for legacy + scoped subjects.
  - Update publisher reconnect request handling to scoped request subjects.
- Public Interface Impact:
  - NATS subjects for all gateway lifecycle/config traffic.
- Tests Required:
  - Unit tests for subject builders.
  - Subscriber tests for scoped subject parsing.
  - Publisher tests for scoped publish targets.
- Verification:
  - `go test ./internal/mcpgw/...`
  - NATS integration test: publish scoped event and verify store/tracker update.
- Dependencies: None.
- Rollback/Compatibility:
  - Keep legacy subject listeners for one release; emit deprecation logs.

### P0-INT-002: Event payload and event-type mismatch (`health_changed` vs `server.health_changed`)
- Priority: `P0`
- Area: `Backend`, `Protocol`
- Problem: Cruvero expects legacy payloads (`server_name` etc.) and `health_changed`, but gateway contract uses `server.health_changed` and `ServerRecord`-style payloads.
- Evidence:
  - Gateway contract payload/event naming: `docs/OVERVIEW.md:487`
  - Cruvero event constants: `../cruvero/internal/mcpgw/types.go:29`
  - Cruvero payload structs: `../cruvero/internal/mcpgw/types.go:43`
  - Drop-on-empty behavior in subscriber: `../cruvero/internal/mcpgw/subscriber.go:116`
- Required Change:
  - Introduce version-tolerant event parser handling both legacy and current payload shapes.
  - Normalize event names to canonical values in one place.
  - Update health-change handler to consume status transitions from gateway payload.
- Public Interface Impact:
  - Event envelope/payload decoding contract in `internal/mcpgw`.
- Tests Required:
  - Table-driven decode tests for old and new payloads.
  - Handler tests ensuring server records are not dropped.
- Verification:
  - `go test ./internal/mcpgw/... -run Event`
- Dependencies: `P0-INT-001`.
- Rollback/Compatibility:
  - Backward-compatible decoder retained until all gateways are upgraded.

### P0-UI-001: MCP UI gateway create flow is schema-incompatible (`endpoint` vs `gateway_url`)
- Priority: `P0`
- Area: `UI`, `API`
- Problem: Backend requires `gateway_url` for gateway transport, but frontend only collects/validates `endpoint`.
- Evidence:
  - Backend request schema and validation: `../cruvero/cmd/ui/mcp_api.go:300`, `../cruvero/cmd/ui/mcp_api.go:330`
  - Frontend validation currently endpoint-only: `../cruvero/cmd/ui/frontend/src/pages/McpStatusPage.tsx:52`
  - Frontend request type lacks `gateway_url`: `../cruvero/cmd/ui/frontend/src/api/mcp.ts:14`
- Required Change:
  - Add `gateway_url` and `gateway_auth` to frontend request model.
  - Render transport-specific inputs:
    - `stdio`: command/args/env
    - `http`: endpoint
    - `gateway`: gateway_url (+ optional gateway_auth)
  - Update server card display to show `gateway_url` for gateway transport.
- Public Interface Impact:
  - `/api/mcp/servers` POST payload used by frontend.
- Tests Required:
  - Frontend test for gateway transport create payload.
  - Backend test for `gateway` create success path and missing `gateway_url` path.
- Verification:
  - `go test ./cmd/ui/...`
  - `cd ../cruvero/cmd/ui/frontend && npm test -- McpStatusPage`
- Dependencies: None.
- Rollback/Compatibility:
  - Accept both `endpoint` and `gateway_url` for one release (prefer `gateway_url`).

### P0-INT-003: Missing `config.server_settings` publication path
- Priority: `P0`
- Area: `Backend`, `Config Sync`
- Problem: Gateway contract expects `config.server_settings`; Cruvero currently publishes only policy/servers/auth.
- Evidence:
  - Gateway contract includes server settings config scope: `docs/OVERVIEW.md:500`
  - Cruvero config subjects exclude server settings: `../cruvero/internal/mcpgw/types.go:19`
  - Publisher emits only policy/servers/auth: `../cruvero/internal/mcpgw/publisher.go:40`, `../cruvero/internal/mcpgw/publisher.go:64`
- Required Change:
  - Add `SubjectConfigServerSettings` and typed payload struct.
  - Extend `PublishAll` to include server settings payload.
  - Wire source-of-truth settings data (tenant settings store or dedicated config source).
- Public Interface Impact:
  - New NATS config subject and payload schema.
- Tests Required:
  - Publisher tests asserting 4-scope publish.
  - Integration test verifying gateway receives and applies settings.
- Verification:
  - `go test ./internal/mcpgw/...`
- Dependencies: `P0-INT-001`.
- Rollback/Compatibility:
  - If settings unavailable, publish empty versioned payload explicitly.

### P0-INT-004: Gateway integration startup is incorrectly coupled to MCP discovery mode
- Priority: `P0`
- Area: `Backend`, `Runtime Boot`
- Problem: `MCPGWEnabled` path runs inside `StartDiscovery`, but `StartDiscovery` exits early when discovery is `static`; gateway integration is silently disabled.
- Evidence:
  - Early return for static discovery: `../cruvero/internal/tools/mcp_bridge.go:217`
  - Gateway integration start below the return path: `../cruvero/internal/tools/mcp_bridge.go:275`
- Required Change:
  - Decouple gateway integration boot from discovery mode.
  - Start gateway integration whenever `cfg.MCPGWEnabled` is true and event bus is available.
- Public Interface Impact:
  - Startup behavior of MCP bridge.
- Tests Required:
  - Test: `MCPDiscovery=static` + `MCPGWEnabled=true` starts subscriber/publisher/tracker.
- Verification:
  - `go test ./internal/tools/... -run Gateway`
- Dependencies: None.
- Rollback/Compatibility:
  - Guard with feature flag remains; only startup order changes.

### P0-SEC-001: Auth default drift can downgrade gateway auth to `none`
- Priority: `P0`
- Area: `Security`, `Config`
- Problem: Cruvero defaults gateway auth modes to `none` and publishes this at startup, potentially overriding secure gateway defaults.
- Evidence:
  - MCP transport auth default `none`: `../cruvero/internal/config/config.go:369`
  - MCPGW auth mode default `none`: `../cruvero/internal/config/config.go:451`
  - Publisher always publishes auth in `PublishAll`: `../cruvero/internal/mcpgw/publisher.go:75`
  - Phase doc still documents `none` default: `../cruvero/docs/phases/PHASE30.md:130`
- Required Change:
  - Set secure default to `jwt` for gateway-facing auth mode.
  - Validate supported modes explicitly (`jwt`, `oidc`, `apikey`, `none` only when explicitly configured).
  - Prevent implicit downgrade to `none` on empty values.
- Public Interface Impact:
  - Config defaults and emitted auth config payload.
- Tests Required:
  - Config loader default tests.
  - Publisher auth payload tests.
- Verification:
  - `go test ./internal/config/... ./internal/mcpgw/...`
- Dependencies: None.
- Rollback/Compatibility:
  - Add temporary env override to preserve current behavior during rollout.

### P1-UI-002: `/api/mcpgw/*` APIs are implemented but not surfaced in frontend
- Priority: `P1`
- Area: `UI`
- Problem: Gateway fleet APIs exist server-side but frontend has no API module/page consuming them.
- Evidence:
  - Backend routes exist: `../cruvero/cmd/ui/runtime_routes.go:131`
  - Backend handlers exist: `../cruvero/cmd/ui/mcpgw_api.go:30`
  - Frontend API module list has no `mcpgw.ts`: `../cruvero/cmd/ui/frontend/src/api`
  - Frontend usage search only shows MCP page gateway transport UI: `../cruvero/cmd/ui/frontend/src/pages/McpStatusPage.tsx:24`
- Required Change:
  - Add `cmd/ui/frontend/src/api/mcpgw.ts`.
  - Add gateway fleet panel/page (list/detail/sync/events).
  - Link panel from MCP admin route.
- Public Interface Impact:
  - New frontend API client and UI route.
- Tests Required:
  - Frontend query tests for gateways/sync/events.
- Verification:
  - `cd ../cruvero/cmd/ui/frontend && npm test`
- Dependencies: `P0-INT-001`.
- Rollback/Compatibility:
  - Read-only panel first, then add sync action.

### P1-DATA-001: Tenant handling is hardcoded to `default` in gateway bridge
- Priority: `P1`
- Area: `Backend`, `Multi-Tenant`
- Problem: Subscriber/publisher operations hardcode tenant to `default`, which breaks tenant isolation.
- Evidence:
  - Publisher list tenant hardcoded: `../cruvero/internal/mcpgw/publisher.go:51`
  - Subscriber upsert hardcoded tenant: `../cruvero/internal/mcpgw/subscriber.go:123`
  - Subscriber status/health updates hardcoded tenant: `../cruvero/internal/mcpgw/subscriber.go:163`, `../cruvero/internal/mcpgw/subscriber.go:184`
- Required Change:
  - Introduce tenant-aware envelope/config routing.
  - Map gateway instances to tenant IDs; apply store operations per tenant.
- Public Interface Impact:
  - Event payload schema may require tenant field.
- Tests Required:
  - Subscriber tests for two-tenant isolation.
- Verification:
  - `go test ./internal/mcpgw/... -run Tenant`
- Dependencies: `P0-INT-001`.
- Rollback/Compatibility:
  - Default to `default` only when tenant missing and single-tenant mode is enabled.

### P1-DOC-001: Documentation drift (contracts and UI behavior)
- Priority: `P1`
- Area: `Documentation`
- Problem: Cruvero docs still document legacy subject scheme and outdated UI gateway field semantics.
- Evidence:
  - Legacy subject contract in phase docs: `../cruvero/docs/phases/PHASE30.md:34`
  - MCP manual describes gateway register via Endpoint: `../cruvero/docs/manual/mcp.md:226`
  - Config env manual lacks MCPGW variables: `../cruvero/docs/manual/config-env.md`
- Required Change:
  - Update phase and manual docs to scoped subject contract and `gateway_url` semantics.
  - Add MCPGW env var section to config env manual.
- Public Interface Impact:
  - Operator docs and runbooks.
- Tests Required:
  - Doc lint/link checks.
- Verification:
  - `make docs-lint` (or equivalent markdown/link check script)
- Dependencies: `P0-INT-001`, `P0-UI-001`.
- Rollback/Compatibility:
  - Include explicit “legacy contract” section with deprecation timeline.

### P2-TEST-001: End-to-end gateway integration test suite is incomplete
- Priority: `P2`
- Area: `Testing`
- Problem: Coverage exists for unit flows, but no contract-level integration suite validating gateway/Cruvero behavior under real NATS + scoped subjects + UI APIs.
- Evidence:
  - Existing tests are mostly unit/package-local across `internal/mcpgw` and `cmd/ui`.
- Required Change:
  - Add integration tests that boot Cruvero MCP bridge with NATS and simulate gateway events/config sync.
  - Include compatibility tests for old/new subject formats during migration window.
- Public Interface Impact:
  - Test harness additions only.
- Tests Required:
  - Integration tests under dedicated build tag.
- Verification:
  - `go test -tags integration ./internal/mcpgw/... ./internal/tools/... ./cmd/ui/...`
- Dependencies: `P0` items.
- Rollback/Compatibility:
  - Keep unit tests as fast path; integration tests in CI nightly + PR gating for touched areas.

## Interface/API/Schema Changes Required

1. NATS subject names
- From: `mcpgw.events.*` and `mcpgw.config.*`
- To: `mcpgw.{gateway_id}.events.*` and `mcpgw.{gateway_id}.config.*`

2. Event payload model
- Add compatibility parser for legacy (`server_name`) and canonical `ServerRecord` payloads.
- Normalize event type names to include `server.health_changed`.

3. MCP UI server create request
- Add fields:
  - `gateway_url` (required for `transport=gateway`)
  - `gateway_auth` (optional; validated enum)
- Keep `endpoint` required only for `transport=http`.

4. Config publish scopes
- Add `config.server_settings` payload publication.

5. Auth mode defaults
- Default gateway auth mode to `jwt` unless explicitly overridden.

## UI/UX Integration Gaps

- MCP register form transport branching is incorrect for gateway mode.
- Gateway URL/auth not visible in MCP server cards despite backend returning fields.
- No frontend panel for gateway fleet state (`/api/mcpgw/gateways`), manual sync, or events stream.

## Security & Auth Integration Gaps

- Defaulting to `none` can reduce effective security posture when config is published at startup.
- Mode validation is spread and inconsistent across MCP transport config and MCPGW config.
- Auth propagation semantics (JWT/API key/OIDC) are under-documented for operators.

## Operational Readiness Gaps

- Startup coupling to discovery mode can silently disable gateway integration.
- Tenant hardcoding prevents safe multi-tenant operation.
- Missing server-settings scope publication prevents full config parity with gateway runtime expectations.

## Test Plan & Acceptance Criteria

Acceptance criteria:
1. Cruvero successfully consumes scoped gateway lifecycle events with non-empty server updates.
2. Cruvero publishes all required scoped config subjects (`policy`, `servers`, `server_settings`, `auth`).
3. MCP UI can create and display gateway transport servers using `gateway_url`.
4. Gateway integration starts when enabled, regardless of MCP discovery mode.
5. Auth defaults align with secure baseline (`jwt`) and mode validation is enforced.

Mandatory test scenarios:
- Subject scoping parser tests (legacy + scoped).
- Payload compatibility decode tests.
- UI create flow test for `transport=gateway`.
- Startup behavior test (`MCPDiscovery=static` + `MCPGWEnabled=true`).
- Integration test: event in -> store/tracker update; sync trigger -> all config scopes published.

## Decision Locks (Confirmed)

1. Release scope: all backlog items (`P0`, `P1`, `P2`) ship in the same release. No deferrals.
2. Auth compatibility: secure default shifts to `jwt`, but controlled by a temporary compatibility flag for rollout safety.
3. Tenancy posture: implement for current single-tenant operation while preserving explicit extension seams for multi-tenant support (no hardcoded assumptions that block tenant routing later).

## Execution Order (Milestones)

1. Milestone A (`P0-INT-001`, `P0-INT-002`): normalize subjects and payload parsing with backward compatibility.
2. Milestone B (`P0-INT-004`, `P0-INT-003`): fix startup semantics and complete config publication scopes.
3. Milestone C (`P0-UI-001`, `P0-SEC-001`): fix UI request schema and secure auth defaults.
4. Milestone D (`P1-*`): tenant correctness, gateway UI panel, docs refresh.
5. Milestone E (`P2-TEST-001`): end-to-end integration suite and CI gates.

## Risks, Dependencies, and Rollback Notes

- Primary risk: breaking existing gateways on legacy subjects.
  - Mitigation: complete subject/payload updates in the target release and verify using integration tests before rollout.
- Primary dependency: agreed canonical payload schema for gateway lifecycle events.
- Rollback strategy:
  - Preserve only the temporary auth compatibility flag (default behavior switch control).
  - Revert scoped publish first, keep scoped subscribe to avoid data loss.

## Appendix: Evidence Index

Gateway contract evidence (this repo):
- `docs/OVERVIEW.md:485`
- `docs/OVERVIEW.md:498`
- `docs/OVERVIEW.md:515`

Cruvero MCPGW bridge evidence:
- `../cruvero/internal/mcpgw/types.go:13`
- `../cruvero/internal/mcpgw/types.go:19`
- `../cruvero/internal/mcpgw/types.go:43`
- `../cruvero/internal/mcpgw/subscriber.go:47`
- `../cruvero/internal/mcpgw/subscriber.go:116`
- `../cruvero/internal/mcpgw/subscriber.go:123`
- `../cruvero/internal/mcpgw/subscriber.go:163`
- `../cruvero/internal/mcpgw/subscriber.go:184`
- `../cruvero/internal/mcpgw/publisher.go:40`
- `../cruvero/internal/mcpgw/publisher.go:64`
- `../cruvero/internal/mcpgw/publisher.go:75`

Cruvero startup/config evidence:
- `../cruvero/internal/tools/mcp_bridge.go:217`
- `../cruvero/internal/tools/mcp_bridge.go:275`
- `../cruvero/internal/config/config.go:369`
- `../cruvero/internal/config/config.go:451`

Cruvero UI/API evidence:
- `../cruvero/cmd/ui/mcp_api.go:300`
- `../cruvero/cmd/ui/mcp_api.go:330`
- `../cruvero/cmd/ui/runtime_routes.go:131`
- `../cruvero/cmd/ui/mcpgw_api.go:30`
- `../cruvero/cmd/ui/frontend/src/pages/McpStatusPage.tsx:52`
- `../cruvero/cmd/ui/frontend/src/api/mcp.ts:14`
- `../cruvero/cmd/ui/frontend/src/api/types.ts:210`
- `../cruvero/cmd/ui/frontend/src/api`

Cruvero docs drift evidence:
- `../cruvero/docs/phases/PHASE30.md:34`
- `../cruvero/docs/phases/PHASE30.md:130`
- `../cruvero/docs/manual/mcp.md:226`
- `../cruvero/docs/manual/config-env.md`
