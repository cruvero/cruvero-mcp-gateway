# Phase 12A: OAuth2 Device Code Flow & CLI Auth Commands

## Overview

Implement RFC 8628 OAuth2 Device Authorization Grant so IDE/CLI users authenticate via a browser without the gateway needing to act as a full OAuth2 server. The gateway proxies the Device Code flow to the configured OIDC provider. Tokens are cached locally with a 24-hour lifetime governed by the IdP's refresh token policy.

## Scope

### Device Code Flow Architecture

The gateway does NOT issue tokens itself. It proxies the Device Code flow to the configured OIDC provider (Keycloak, Okta, Auth0, Azure AD). This keeps token issuance and revocation in the IdP where it belongs.

```
CLI                          Gateway                         IdP (Keycloak/Okta)
 |                              |                               |
 |-- POST /device/code -------->|                               |
 |                              |-- POST /device/authorization ->|
 |                              |<-- device_code, user_code -----|
 |<-- user_code, verify URL ----|                               |
 |                              |                               |
 | (user opens browser, enters code at IdP)                     |
 |                              |                               |
 |-- POST /device/token ------->|                               |
 |                              |-- POST /token (device_code) -->|
 |                              |<-- access_token, refresh -------|
 |<-- access_token, refresh ----|                               |
 |                              |                               |
 | (CLI caches tokens locally)  |                               |
```

### Device Code Endpoints

**POST `/device/code`** — Initiate device authorization:

Request:
```json
{"client_id": "mcpgw-cli", "scope": "openid profile email"}
```

Response (proxied from IdP):
```json
{
  "device_code": "...",
  "user_code": "ABCD-1234",
  "verification_uri": "https://idp.example.com/device",
  "verification_uri_complete": "https://idp.example.com/device?user_code=ABCD-1234",
  "expires_in": 900,
  "interval": 5
}
```

**POST `/device/token`** — Poll for token (called by CLI during polling):

Request:
```json
{"device_code": "...", "grant_type": "urn:ietf:params:oauth:grant-type:device_code"}
```

Response (proxied from IdP):
- `authorization_pending` → HTTP 428 with `{"error": "authorization_pending"}`
- `slow_down` → HTTP 428 with `{"error": "slow_down"}`
- Success → HTTP 200 with `{"access_token": "...", "refresh_token": "...", "id_token": "...", "expires_in": 3600}`

**GET `/device/verify`** — Optional convenience page that redirects to IdP's verification URI. Simple HTML page showing the user code and a link.

### Device Flow Handler (`internal/auth/device_flow.go`)

```go
type DeviceFlowHandler struct {
    idpDeviceURL  string // IdP's device authorization endpoint
    idpTokenURL   string // IdP's token endpoint
    clientID      string // OAuth2 client ID for CLI
    clientSecret  string // OAuth2 client secret (optional, for confidential clients)
    httpClient    *http.Client
    logger        *slog.Logger
}

func NewDeviceFlowHandler(cfg DeviceFlowConfig, logger *slog.Logger) *DeviceFlowHandler
```

- `HandleDeviceCode(w, r)`:
  1. Parse request body.
  2. Forward to `idpDeviceURL` with `client_id`, `scope`.
  3. Proxy response back to client.
  4. Log the initiation (client IP, requested scopes).

- `HandleDeviceToken(w, r)`:
  1. Parse request body (device_code, grant_type).
  2. Forward to `idpTokenURL` with `device_code`, `grant_type`, `client_id`.
  3. Proxy response back to client.
  4. On success: validate the returned `id_token` using existing OIDC validator.
  5. Log the result (success or still pending).

- Rate limiting: Apply gateway's rate limiter to device flow endpoints. Additionally, enforce the IdP's `interval` — reject polls faster than the interval with `slow_down`.

### CLI Auth Commands (`cmd/mcpgw/auth.go`)

**`mcpgw auth login`**:
1. POST to `{gateway_url}/device/code`.
2. Display: `"Go to {verification_uri} and enter code: {user_code}"`.
3. Attempt to open browser automatically with `os/exec` (platform-aware: `xdg-open`, `open`, `start`).
4. Poll `{gateway_url}/device/token` every `interval` seconds.
5. On success: write tokens to `~/.mcpgw/token.json` with `0600` permissions.
6. Display: `"Authenticated as {email}. Token expires at {time}."`.
7. Flags: `--gateway-url` (required unless `MCPGW_GATEWAY_URL` set), `--no-browser` (skip auto-open).

**`mcpgw auth status`**:
1. Read `~/.mcpgw/token.json`.
2. Display: identity (from id_token claims), access token expiry, refresh token expiry.
3. If expired: display `"Token expired. Run 'mcpgw auth login' to re-authenticate."`.

**`mcpgw auth logout`**:
1. Delete `~/.mcpgw/token.json`.
2. Display: `"Logged out. Cached tokens removed."`.

### Token Cache (`internal/auth/token_cache.go`)

```go
type CachedTokens struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    IDToken      string    `json:"id_token"`
    ExpiresAt    time.Time `json:"expires_at"`
    GatewayURL   string    `json:"gateway_url"`
}

func TokenCachePath() string // ~/.mcpgw/token.json
func SaveTokens(tokens *CachedTokens) error  // write with 0600
func LoadTokens() (*CachedTokens, error)     // read and parse
func DeleteTokens() error                     // remove file
func (t *CachedTokens) IsExpired() bool
func (t *CachedTokens) NeedsRefresh() bool   // access token expired but refresh token valid
```

- Directory `~/.mcpgw/` created with `0700` permissions.
- File written atomically (write to temp, rename).
- Tokens never logged (even at debug level).

### MCP Proxy Bridge (`cmd/mcpgw/proxy.go`)

**`mcpgw mcp-proxy`** — local stdio-to-HTTP bridge for IDEs:

1. Read MCP JSON-RPC messages from stdin.
2. Load tokens from cache. If expired, attempt silent refresh via IdP token endpoint.
3. Forward each message to `{gateway_url}/mcp` with `Authorization: Bearer {access_token}`.
4. Write response to stdout.
5. Handle SSE streaming (for long-running tool calls).
6. Flags: `--gateway-url` (or `MCPGW_GATEWAY_URL`).

This allows any MCP client that supports `command` transport (Claude Code, Cursor) to use the gateway with automatic auth:

```json
{
  "mcpServers": {
    "cruvero": {
      "command": "mcpgw",
      "args": ["mcp-proxy", "--gateway-url", "https://gateway.corp.example.com"]
    }
  }
}
```

### Config Changes (`internal/config/config.go`)

- `MCPGW_DEVICE_FLOW_ENABLED` (default `false`)
- `MCPGW_DEVICE_FLOW_IDP_DEVICE_URL` (IdP's device authorization endpoint)
- `MCPGW_DEVICE_FLOW_IDP_TOKEN_URL` (IdP's token endpoint)
- `MCPGW_DEVICE_FLOW_CLIENT_ID` (OAuth2 client ID)
- `MCPGW_DEVICE_FLOW_CLIENT_SECRET` (optional, for confidential clients)
- Validate: if enabled, device URL and token URL and client ID must be set.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/auth/device_flow.go` | Device Code flow proxy handlers |
| `internal/auth/device_flow_test.go` | Tests with mock IdP |
| `internal/auth/token_cache.go` | File-based token cache |
| `internal/auth/token_cache_test.go` | Tests |
| `cmd/mcpgw/auth.go` | `mcpgw auth login/status/logout` subcommands |
| `cmd/mcpgw/auth_test.go` | CLI tests |
| `cmd/mcpgw/proxy.go` | `mcpgw mcp-proxy` stdio bridge |
| `cmd/mcpgw/proxy_test.go` | Tests |
| `cmd/mcpgw/main.go` | Register auth and proxy subcommands |
| `internal/config/config.go` | Device flow config fields |
| `internal/server/server.go` | Mount device flow endpoints |

## Testing Requirements

- Device flow handler: mock IdP HTTP responses, test device_code initiation, test token polling (pending → success), test rate limiting on polls
- Token cache: test save/load round-trip, test permissions (0600 file, 0700 dir), test atomic write, test expired token detection
- CLI auth login: test with mock gateway, verify token cache created
- CLI auth status: test with valid cache, expired cache, missing cache
- CLI auth logout: test cache file deleted
- MCP proxy: test stdin→HTTP→stdout round-trip, test auth header injection
- Coverage: >=80%
