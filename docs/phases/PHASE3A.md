# Phase 3A: Registration Endpoint, Handshake, Approval

## Overview

Implement the registration endpoint where MCP servers register with the gateway. The gateway validates the mTLS identity (SPIFFE ID), stores the registration, and returns the effective policy and operational parameters.

## Scope

### Registration Handler (`internal/registration/handler.go`)

**POST /v1/registrations**
- Request body: JSON with service_name, version, listen (host, port, protocol), capabilities (tools, resources, prompts), labels
- Validate mTLS identity from context (Identity must be present and type=mTLS)
- Validate SPIFFE ID against allowlist (from config MCPGW_SPIFFE_ALLOW_PREFIX)
- Validate registration payload (service_name required, capabilities non-empty, port valid)
- Upsert ServerRecord in store:
  - id: generated UUID
  - name: service_name
  - spiffe_id: from Identity
  - version: from request version
  - host: from listen.host
  - port: from listen.port
  - capabilities: from request
  - status: approved (auto-approve if identity matches policy)
  - policy_profile: default (or matched from config)
  - last_heartbeat: now
- Log registration to audit store
- Return RegistrationResponse: instance_id, policy_snapshot, heartbeat_interval (from config MCPGW_HEARTBEAT_TTL), status

**GET /v1/registrations**
- Requires admin scope
- Returns list of all ServerRecords
- Supports query params: status filter, name pattern

**DELETE /v1/registrations/{id}**
- Requires admin scope OR self (matching SPIFFE ID)
- Removes ServerRecord from store
- Logs deregistration to audit store
- Returns 204 on success

### Registration Types (`internal/registration/types.go`)
- `RegistrationRequest` -- service_name, version, listen, capabilities, labels (JSON tags)
- `RegistrationResponse` -- instance_id, policy_snapshot, heartbeat_interval_seconds, status
- `ListenConfig` -- host, port, protocol
- Request validation methods

### Registration Service (`internal/registration/service.go`)
- `Service` struct: store ServerStore, auditStore AuditStore, config *config.Config, logger *slog.Logger
- `Register(ctx context.Context, identity *Identity, req RegistrationRequest) (*RegistrationResponse, error)`
- `List(ctx context.Context, filter ServerFilter) ([]ServerRecord, error)`
- `Deregister(ctx context.Context, identity *Identity, id string) error`
- Encapsulates business logic separate from HTTP handlers

## Files Created

| File | Description |
|------|-------------|
| internal/registration/types.go | Registration request/response types |
| internal/registration/service.go | Registration business logic |
| internal/registration/handler.go | HTTP handlers |
| internal/registration/handler_test.go | Handler tests |
| internal/registration/service_test.go | Service tests |

## Testing Requirements

- Test successful registration with valid mTLS identity
- Test registration rejected with invalid SPIFFE ID (403)
- Test registration with missing required fields (400)
- Test list registrations with admin scope
- Test list registrations without admin scope (403)
- Test deregistration by admin
- Test deregistration by self (matching SPIFFE ID)
- Test deregistration by other (403)
- Test audit logging on register/deregister
- Coverage: >=80%
