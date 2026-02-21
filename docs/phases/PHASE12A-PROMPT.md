# Phase 12A Implementation Prompts

## Prompt 1 of 4: Device Code Flow Handler

### Required Reading (read these files before writing code)
- docs/phases/PHASE12A.md (full scope, device flow architecture)
- internal/auth/oidc.go (existing OIDC validator pattern)
- internal/server/server.go (existing route mounting pattern)
- internal/config/config.go (existing config pattern)

### Task

Implement the Device Code flow proxy endpoints that forward requests to the IdP.

1. **Config** in `internal/config/config.go`:
   - Add to Config struct:
     ```go
     DeviceFlowEnabled     bool   `json:"device_flow_enabled"`
     DeviceFlowIDPDeviceURL string `json:"device_flow_idp_device_url"`
     DeviceFlowIDPTokenURL  string `json:"device_flow_idp_token_url"`
     DeviceFlowClientID     string `json:"device_flow_client_id"`
     DeviceFlowClientSecret string `json:"device_flow_client_secret"`
     ```
   - Parse `MCPGW_DEVICE_FLOW_ENABLED`, `MCPGW_DEVICE_FLOW_IDP_DEVICE_URL`, `MCPGW_DEVICE_FLOW_IDP_TOKEN_URL`, `MCPGW_DEVICE_FLOW_CLIENT_ID`, `MCPGW_DEVICE_FLOW_CLIENT_SECRET`.
   - Validate: if `DeviceFlowEnabled`, require non-empty `IDPDeviceURL`, `IDPTokenURL`, and `ClientID`.

2. `internal/auth/device_flow.go`:
   ```go
   type DeviceFlowConfig struct {
       IDPDeviceURL string
       IDPTokenURL  string
       ClientID     string
       ClientSecret string
   }
   
   type DeviceFlowHandler struct {
       cfg        DeviceFlowConfig
       httpClient *http.Client
       validator  *OIDCValidator // reuse existing OIDC validator for id_token verification
       logger     *slog.Logger
   }
   
   func NewDeviceFlowHandler(cfg DeviceFlowConfig, validator *OIDCValidator, logger *slog.Logger) *DeviceFlowHandler {
       return &DeviceFlowHandler{
           cfg: cfg,
           httpClient: &http.Client{Timeout: 10 * time.Second},
           validator: validator,
           logger: logger,
       }
   }
   ```

   - `HandleDeviceCode(w http.ResponseWriter, r *http.Request)`:
     1. Only accept POST.
     2. Read request body (max 4KB via `http.MaxBytesReader`).
     3. Build form-encoded POST to `cfg.IDPDeviceURL` with:
        - `client_id` from config (always use config value, not user-provided).
        - `scope` from request body (validate: must include `openid`, reject dangerous scopes).
     4. If `ClientSecret` is set, add `client_secret`.
     5. Forward to IdP, read response.
     6. Proxy the IdP response body and status code back to the client.
     7. Log: client IP, requested scopes, success/failure.

   - `HandleDeviceToken(w http.ResponseWriter, r *http.Request)`:
     1. Only accept POST.
     2. Read request body.
     3. Extract `device_code` and `grant_type` (must be `urn:ietf:params:oauth:grant-type:device_code`).
     4. Build form-encoded POST to `cfg.IDPTokenURL` with `device_code`, `grant_type`, `client_id`.
     5. Forward to IdP.
     6. If IdP returns success (200 with tokens):
        a. Optionally validate the `id_token` using `s.validator.Validate()`.
        b. Proxy full response to client.
        c. Log success (identity from id_token sub claim, NOT the tokens).
     7. If IdP returns `authorization_pending` or `slow_down`:
        a. Return 428 with the error body.
     8. If IdP returns other errors: proxy as-is.

   - `HandleDeviceVerify(w http.ResponseWriter, r *http.Request)`:
     1. Simple HTML page showing instructions.
     2. Read `user_code` from query parameter.
     3. Render: "Enter this code at your identity provider" with a link to the IdP's device verification URL.
     4. Use Go `html/template` with auto-escaping (the user_code is user input, must be escaped).

3. **Mount routes** in `internal/server/server.go`:
   - When device flow is enabled in config:
     ```go
     if cfg.DeviceFlowEnabled {
         dfHandler := auth.NewDeviceFlowHandler(...)
         r.Post("/device/code", dfHandler.HandleDeviceCode)
         r.Post("/device/token", dfHandler.HandleDeviceToken)
         r.Get("/device/verify", dfHandler.HandleDeviceVerify)
     }
     ```
   - These endpoints are **unauthenticated** (no Bearer token required — that's the point, the user doesn't have a token yet).
   - They MUST still be rate-limited (use IP-based rate limiting, not identity-based).

4. **Tests** `internal/auth/device_flow_test.go`:
   - Set up a mock IdP HTTP server using `httptest.NewServer`.
   - Test `HandleDeviceCode`:
     - Mock IdP returns device_code → verify response proxied correctly.
     - Mock IdP returns error → verify error proxied.
     - Test scope validation: reject request without `openid`.
   - Test `HandleDeviceToken`:
     - Mock IdP returns `authorization_pending` → verify 428 response.
     - Mock IdP returns tokens → verify 200 with tokens.
     - Test invalid grant_type → reject.
   - Test `HandleDeviceVerify`:
     - Verify HTML rendered with user_code.
     - Test XSS: user_code with `<script>` tags is escaped.

### Verification
```bash
go test ./internal/auth/... ./internal/config/...
go vet ./...
```

### Acceptance Criteria
- Device Code endpoints proxy to IdP correctly
- Client ID always from config (never user-provided)
- Tokens never logged
- Device verify page escapes user input
- Rate limiting applied to unauthenticated endpoints
- Config validation catches missing required fields

---

## Prompt 2 of 4: Token Cache & Auth CLI Commands

### Required Reading (read these files before writing code)
- docs/phases/PHASE12A.md (token cache and CLI sections)
- cmd/mcpgw/main.go (existing subcommand pattern)
- cmd/mcpgw/apikey.go (CLI output pattern reference)

### Task

Create the file-based token cache and the `mcpgw auth` CLI subcommands.

1. `internal/auth/token_cache.go`:
   ```go
   type CachedTokens struct {
       AccessToken  string    `json:"access_token"`
       RefreshToken string    `json:"refresh_token"`
       IDToken      string    `json:"id_token"`
       ExpiresAt    time.Time `json:"expires_at"`
       GatewayURL   string    `json:"gateway_url"`
   }
   ```
   
   - `TokenCachePath() string`:
     - Return `filepath.Join(os.UserHomeDir(), ".mcpgw", "token.json")`.
     - Handle `UserHomeDir` error gracefully.
   
   - `SaveTokens(tokens *CachedTokens) error`:
     - Ensure `~/.mcpgw/` exists with `os.MkdirAll(dir, 0700)`.
     - Marshal to JSON with indentation (human-readable for debugging).
     - Write to temp file in same directory, then `os.Rename()` for atomicity.
     - Set file permissions: `os.Chmod(path, 0600)`.
   
   - `LoadTokens() (*CachedTokens, error)`:
     - Read file, unmarshal.
     - Return `os.ErrNotExist` if file doesn't exist (caller can handle).
   
   - `DeleteTokens() error`:
     - `os.Remove(TokenCachePath())`.
     - Return nil if file doesn't exist.
   
   - `(t *CachedTokens) IsExpired() bool` — `time.Now().After(t.ExpiresAt)`.
   - `(t *CachedTokens) NeedsRefresh() bool` — access token expired but refresh token exists.
   - `(t *CachedTokens) RefreshAccessToken(tokenURL, clientID string) error`:
     - POST to `tokenURL` with `grant_type=refresh_token`, `refresh_token`, `client_id`.
     - On success: update `AccessToken` and `ExpiresAt`.
     - On failure: return error (caller should prompt re-login).

2. `cmd/mcpgw/auth.go`:
   
   **`mcpgw auth login`**:
   ```go
   func authLoginCmd() *cobra.Command {
       cmd := &cobra.Command{
           Use:   "login",
           Short: "Authenticate with the MCP gateway via browser",
           RunE: func(cmd *cobra.Command, args []string) error {
               gatewayURL, _ := cmd.Flags().GetString("gateway-url")
               noBrowser, _ := cmd.Flags().GetBool("no-browser")
               
               // 1. POST to /device/code
               resp, err := initiateDeviceCode(gatewayURL)
               // 2. Display user code
               fmt.Printf("\nTo authenticate, visit:\n  %s\n\nEnter code: %s\n\n",
                   resp.VerificationURI, resp.UserCode)
               // 3. Open browser (unless --no-browser)
               if !noBrowser {
                   openBrowser(resp.VerificationURIComplete)
               }
               // 4. Poll for token
               fmt.Print("Waiting for authentication...")
               tokens, err := pollForToken(gatewayURL, resp.DeviceCode, resp.Interval)
               // 5. Save tokens
               err = auth.SaveTokens(tokens)
               // 6. Display success
               fmt.Printf("\nAuthenticated as %s. Token expires at %s.\n",
                   extractEmail(tokens.IDToken), tokens.ExpiresAt.Format(time.RFC3339))
               return nil
           },
       }
       cmd.Flags().String("gateway-url", os.Getenv("MCPGW_GATEWAY_URL"), "Gateway URL")
       cmd.Flags().Bool("no-browser", false, "Don't auto-open browser")
       return cmd
   }
   ```
   - `openBrowser(url string)`: use `exec.Command` with `xdg-open` (Linux), `open` (macOS), `cmd /c start` (Windows). Log warning if fails (user can open manually).
   - `pollForToken()`: loop with `time.Sleep(interval)`, POST to `/device/token`, handle `authorization_pending` (continue), `slow_down` (increase interval), success (return tokens), other errors (return error). Timeout after `expires_in` seconds.
   
   **`mcpgw auth status`**:
   - Load tokens. Display identity, expiry, gateway URL.
   - If expired: suggest `mcpgw auth login`.
   
   **`mcpgw auth logout`**:
   - Delete tokens. Confirm.

3. **Register subcommands** in `cmd/mcpgw/main.go`:
   - Add `auth` command group with `login`, `status`, `logout` subcommands.

4. **Tests:**
   - `internal/auth/token_cache_test.go`:
     - Test SaveTokens creates file with 0600 permissions.
     - Test SaveTokens creates directory with 0700.
     - Test LoadTokens round-trip.
     - Test LoadTokens with missing file returns os.ErrNotExist.
     - Test DeleteTokens removes file.
     - Test IsExpired/NeedsRefresh logic.
     - Test atomic write (crash between write and rename doesn't corrupt).
   - `cmd/mcpgw/auth_test.go`:
     - Test login flow with mock gateway (httptest.Server).
     - Test status with valid/expired/missing cache.
     - Test logout deletes cache.

### Verification
```bash
go test ./internal/auth/... ./cmd/mcpgw/...
go vet ./...
```

### Acceptance Criteria
- Token cache file has 0600 permissions, directory has 0700
- Atomic write prevents corruption on crash
- Login flow polls correctly and handles all IdP responses
- Tokens never printed to stdout (only identity info)
- Browser auto-open works on Linux/macOS/Windows

---

## Prompt 3 of 4: MCP Proxy Bridge

### Required Reading (read these files before writing code)
- docs/phases/PHASE12A.md (MCP proxy bridge section)
- internal/auth/token_cache.go (token loading and refresh)
- README.md (MCP protocol, streamable HTTP transport)

### Task

Create the `mcpgw mcp-proxy` command that bridges stdin/stdout to the gateway's HTTP endpoint with automatic auth.

1. `cmd/mcpgw/proxy.go`:
   ```go
   func mcpProxyCmd() *cobra.Command {
       cmd := &cobra.Command{
           Use:   "mcp-proxy",
           Short: "Stdio-to-HTTP MCP bridge with automatic auth",
           Long:  "Reads MCP JSON-RPC from stdin, forwards to gateway with auth, writes responses to stdout. Use as MCP server command in IDE configs.",
           RunE:  runMCPProxy,
       }
       cmd.Flags().String("gateway-url", os.Getenv("MCPGW_GATEWAY_URL"), "Gateway URL")
       return cmd
   }
   ```

   - `runMCPProxy(cmd, args)`:
     1. Load gateway URL from flag or env.
     2. Load tokens from cache. If missing: print error, suggest `mcpgw auth login`, exit 1.
     3. If access token expired, attempt silent refresh. If refresh fails: exit 1 with suggestion.
     4. Create an HTTP client with the access token as Bearer header.
     5. Enter the proxy loop:
        - Read JSON-RPC messages from stdin (newline-delimited JSON).
        - For each message: POST to `{gatewayURL}/mcp` with `Content-Type: application/json` and `Authorization: Bearer {token}`.
        - Write response to stdout.
        - Handle SSE streaming: if response is `text/event-stream`, read events and write to stdout as they arrive.
     6. On stdin EOF or SIGINT: exit cleanly.

   - **Token refresh during proxy operation**:
     - Before each request, check if access token needs refresh.
     - If refresh needed: attempt refresh, update cache file.
     - If refresh fails (refresh token expired): write error to stderr, exit 1.
     - This ensures long-running proxy sessions stay authenticated.

   - **Error handling**:
     - HTTP 401 from gateway: attempt token refresh. If still 401: suggest re-login.
     - HTTP 429 (rate limited): include Retry-After in error response to client.
     - Network errors: retry with backoff (3 attempts), then return MCP error response.

2. **SSE handling** for streaming responses:
   ```go
   func handleSSEResponse(resp *http.Response, stdout io.Writer) error {
       scanner := bufio.NewScanner(resp.Body)
       for scanner.Scan() {
           line := scanner.Text()
           if strings.HasPrefix(line, "data: ") {
               data := strings.TrimPrefix(line, "data: ")
               fmt.Fprintln(stdout, data)
           }
       }
       return scanner.Err()
   }
   ```

3. **Register** `mcp-proxy` subcommand in `cmd/mcpgw/main.go`.

4. **Tests** `cmd/mcpgw/proxy_test.go`:
   - Test: stdin JSON-RPC → HTTP POST → stdout response.
   - Test: token refresh on 401.
   - Test: missing token cache → error message.
   - Test: SSE streaming response handled correctly.
   - Test: graceful exit on stdin EOF.
   - Use `httptest.Server` as mock gateway and `io.Pipe` for stdin/stdout.

### Verification
```bash
go test ./cmd/mcpgw/...
go vet ./...
```

### Acceptance Criteria
- MCP proxy reads JSON-RPC from stdin and writes to stdout
- Auth header injected automatically from cached token
- Token refresh happens transparently
- SSE streaming works for long-running tool calls
- IDE config example works: `{"command": "mcpgw", "args": ["mcp-proxy", "--gateway-url", "..."]}`

---

## Prompt 4 of 4: Integration Test & Documentation

### Required Reading (read these files before writing code)
- All Phase 12A files created in prompts 1-3
- README.md (existing documentation structure)
- docs/OVERVIEW.md (existing architecture docs)

### Task

Write an end-to-end integration test and update documentation.

1. **Integration test** `internal/auth/device_flow_integration_test.go`:
   - Set up:
     - Mock IdP server (httptest.NewServer) that implements device authorization and token endpoints.
     - Gateway server with device flow enabled, pointing to mock IdP.
   - Test the full flow:
     1. POST `/device/code` → get device_code and user_code.
     2. Simulate user authorizing at IdP (call mock IdP's approval endpoint).
     3. POST `/device/token` with device_code → get tokens.
     4. Use the access_token to make an authenticated MCP request to the gateway.
     5. Verify the request is authenticated and processed.
   - Test token refresh:
     1. Get tokens with a short-lived access token (5s).
     2. Wait for expiry.
     3. Use refresh token to get new access token.
     4. Verify new token works.

2. **Update docs/OVERVIEW.md** — add a section on device code authentication:
   - Add to section 8 (Authentication Modes):
     ```
     ### Device Code Flow (IDE/CLI users)
     
     For CLI tools and IDEs that cannot perform browser redirects, the gateway
     supports OAuth2 Device Authorization Grant (RFC 8628).
     
     1. CLI initiates flow via POST /device/code
     2. User authenticates in browser at the IdP
     3. CLI polls POST /device/token until authorized
     4. Tokens cached locally (~/.mcpgw/token.json)
     5. mcpgw mcp-proxy injects tokens automatically
     
     Token lifetime is controlled by the IdP's refresh token policy.
     Configure 24h refresh token lifetime at the IdP for "login once per day".
     ```

3. **Update README.md** — add IDE configuration examples:
   ```markdown
   ### IDE Configuration
   
   #### Claude Code (with Device Code auth)
   \```json
   {
     "mcpServers": {
       "cruvero": {
         "command": "mcpgw",
         "args": ["mcp-proxy", "--gateway-url", "https://gateway.corp.example.com"]
       }
     }
   }
   \```
   
   First run: `mcpgw auth login --gateway-url https://gateway.corp.example.com`
   
   #### Direct API key (no auth flow needed)
   \```json
   {
     "mcpServers": {
       "cruvero": {
         "url": "https://gateway.corp.example.com/mcp",
         "headers": {"Authorization": "Bearer mcpgw_<your-key>"}
       }
     }
   }
   \```
   ```

4. **Helm values** for device flow:
   ```yaml
   deviceFlow:
     enabled: false
     idpDeviceURL: ""
     idpTokenURL: ""
     clientID: ""
     # clientSecret via Vault secret, not values.yaml
   ```

### Verification
```bash
go test ./internal/auth/... ./cmd/mcpgw/...
go vet ./...
helm lint charts/mcpgateway
```

### Acceptance Criteria
- Integration test covers the full device code → token → authenticated request flow
- Documentation covers all IDE configuration patterns
- Helm values render correctly with device flow enabled/disabled
- All tests pass, >=80% coverage on new packages
