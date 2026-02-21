# Phase 15B: NATS TLS Configuration & Connection Security

## Overview

Add TLS configuration fields for NATS connections and wire them into the events client, enabling encrypted mTLS communication between the gateway and NATS server.

## Scope

### NATS TLS Config Fields (`internal/config/config.go`)

Add four new fields to the `Config` struct for NATS TLS:

- `NATSTLSEnabled bool` — parsed from `MCPGW_NATS_TLS_ENABLED` (default `false`)
- `NATSTLSCert string` — parsed from `MCPGW_NATS_TLS_CERT` (TLS client certificate path)
- `NATSTLSKey string` — parsed from `MCPGW_NATS_TLS_KEY` (TLS client key path)
- `NATSTLSCa string` — parsed from `MCPGW_NATS_TLS_CA` (CA certificate for server verification)

**Validation rules:**
- When `NATSTLSEnabled` is `true`, all three paths (`NATSTLSCert`, `NATSTLSKey`, `NATSTLSCa`) must be non-empty
- When `NATSTLSEnabled` is `false`, the paths are ignored
- File existence is not validated at config load time (same pattern as `TLSCertPath`/`TLSKeyPath`)

### TLS Config Builder (`internal/server/server.go`)

Build a `*tls.Config` from the parsed certificate paths and pass it to the NATS client:

- Use `tls.LoadX509KeyPair(cfg.NATSTLSCert, cfg.NATSTLSKey)` for client cert
- Use `x509.NewCertPool()` + `AppendCertsFromPEM()` for CA verification
- Set `MinVersion: tls.VersionTLS12`
- Pass the TLS config to `events.NewClient` via an option parameter

### Events Client TLS Option (`internal/events`)

The `ClientOption` type, `WithTLS` function, and variadic `NewClient` signature **already exist** in `client.go` (lines 27-35, 64). The `WithTLS` option applies `nats.Secure(tlsConfig)` at lines 115-117. No changes needed in the events package — only the call site in `server.go` needs updating to pass the TLS option.

### Helm Values Update (`charts/mcpgateway/values.yaml`)

Add a `tls` sub-section to the **existing** `nats:` block (lines 96-100). Do NOT replace the existing `nats:` block:

```yaml
nats:
  enabled: false
  url: ""
  tls:
    enabled: false
    certSecretName: ""
    caSecretName: ""
```

**Note:** The Helm deployment template (`charts/mcpgateway/templates/deployment.yaml`) passes `MCPGW_*` config values via the `config:` map in values. Since NATS TLS env vars (`MCPGW_NATS_TLS_ENABLED`, etc.) are standard `MCPGW_*` config entries, they can be added directly to the `config:` map in environment-specific values overlays (e.g., `values-prod.yaml`) without template changes.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/config/config.go` | Add `NATSTLSEnabled`, `NATSTLSCert`, `NATSTLSKey`, `NATSTLSCa` fields, parse + validate |
| `internal/config/config_test.go` | Tests for NATS TLS config defaults, custom values, validation |
| `internal/server/server.go` | Build `*tls.Config` from NATS TLS fields, pass to events client |
| `internal/events/client.go` | Add `WithTLS` option (if not already present) |
| `internal/events/client_test.go` | Test TLS option wiring |
| `charts/mcpgateway/values.yaml` | Add `nats.tls` section |

## Testing Requirements

- Config: default `NATSTLSEnabled` is `false`, paths empty
- Config: custom values parse correctly
- Config: validation rejects `NATSTLSEnabled=true` with empty cert/key/ca
- Config: validation accepts `NATSTLSEnabled=true` with all three paths set
- Config: validation accepts `NATSTLSEnabled=false` regardless of paths
- TLS build: valid cert/key/CA files → `*tls.Config` created without error
- TLS build: missing cert file → returns descriptive error
- TLS build: invalid CA PEM → returns descriptive error
- Events client: `WithTLS` option applies `nats.Secure` to connection options
- Helm: `helm template` renders cleanly with and without NATS TLS enabled
- Coverage: >=80% on `internal/config` and `internal/events`
