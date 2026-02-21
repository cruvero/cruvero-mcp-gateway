# Phase 12B Implementation Prompts

## Prompt 1 of 4: Admin Auth — OIDC Authorization Code + PKCE & Session Management

### Required Reading (read these files before writing code)
- docs/phases/PHASE12B.md (full scope, auth section)
- internal/auth/oidc.go (existing OIDC validator for reference)
- internal/config/config.go (config pattern)
- LLM.md (conventions)

### Task

Implement OIDC Authorization Code + PKCE authentication and encrypted session management for the admin dashboard.

1. **Config** in `internal/config/config.go`:
   - Add fields:
     ```go
     AdminEnabled       bool          `json:"admin_enabled"`
     AdminOIDCClientID  string        `json:"admin_oidc_client_id"`
     AdminOIDCClientSecret string     `json:"admin_oidc_client_secret"`
     AdminRequiredScope string        `json:"admin_required_scope"`
     AdminSessionKey    [32]byte      `json:"-"` // Never serialized
     AdminSessionTTL    time.Duration `json:"admin_session_ttl"`
     ```
   - Parse `MCPGW_ADMIN_ENABLED`, `MCPGW_ADMIN_OIDC_CLIENT_ID`, `MCPGW_ADMIN_OIDC_CLIENT_SECRET`, `MCPGW_ADMIN_REQUIRED_SCOPE` (default `"admin"`), `MCPGW_ADMIN_SESSION_KEY` (64-char hex string → decode to `[32]byte`), `MCPGW_ADMIN_SESSION_TTL` (default `"8h"`).
   - Validate: if enabled, client ID, client secret, and session key are required. Session key must decode to exactly 32 bytes.

2. `internal/admin/auth.go`:
   ```go
   type AdminAuth struct {
       issuerURL     string
       clientID      string
       clientSecret  string
       redirectURL   string
       requiredScope string
       sessionKey    [32]byte
       sessionTTL    time.Duration
       provider      *oidc.Provider
       verifier      *oidc.IDTokenVerifier
       logger        *slog.Logger
   }
   
   type Session struct {
       Subject   string    `json:"sub"`
       Email     string    `json:"email"`
       Name      string    `json:"name"`
       Groups    []string  `json:"groups"`
       ExpiresAt time.Time `json:"expires_at"`
       CSRFToken string    `json:"csrf_token"`
   }
   ```
   
   - `NewAdminAuth(cfg config.Config, logger *slog.Logger) (*AdminAuth, error)`:
     - Discover OIDC provider from `cfg.OIDCIssuer` (reuse existing OIDC issuer URL).
     - Build redirect URL: `{cfg.ListenAddr}/admin/callback` (or from dedicated config if external URL differs).
   
   - `HandleLogin(w, r)`:
     1. Generate PKCE code verifier (32 random bytes, base64url-encoded).
     2. Compute code challenge (SHA-256 of verifier, base64url-encoded).
     3. Generate random `state` parameter (16 bytes, hex-encoded).
     4. Store verifier and state in a short-lived encrypted cookie (`__mcpgw_auth_state`, 10 minute TTL).
     5. Redirect to IdP authorize endpoint:
        ```
        {authorize_url}?client_id={id}&response_type=code&scope=openid+profile+email+{required_scope}
        &redirect_uri={redirect_url}&state={state}&code_challenge={challenge}&code_challenge_method=S256
        ```
   
   - `HandleCallback(w, r)`:
     1. Read `code` and `state` from query params.
     2. Load and decrypt `__mcpgw_auth_state` cookie. Verify `state` matches.
     3. Extract PKCE verifier from cookie.
     4. Exchange code for tokens at IdP token endpoint with `code_verifier`.
     5. Verify `id_token` using `oidc.IDTokenVerifier`.
     6. Extract claims (sub, email, name, groups).
     7. Check `groups` or `scope` contains `requiredScope`. If not → 403 with "insufficient permissions" page.
     8. Create `Session` with new `CSRFToken` (16 random bytes, hex).
     9. Encrypt session as AES-256-GCM cookie:
        ```go
        func encryptSession(key [32]byte, session *Session) (string, error) {
            plaintext, _ := json.Marshal(session)
            block, _ := aes.NewCipher(key[:])
            gcm, _ := cipher.NewGCM(block)
            nonce := make([]byte, gcm.NonceSize())
            io.ReadFull(rand.Reader, nonce)
            ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
            return base64.RawURLEncoding.EncodeToString(ciphertext), nil
        }
        ```
     10. Set cookie `__mcpgw_admin` with encrypted session. `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/admin/`.
     11. Delete `__mcpgw_auth_state` cookie.
     12. Redirect to `/admin/`.
   
   - `GetSession(r) (*Session, error)`:
     - Read `__mcpgw_admin` cookie.
     - Decrypt with AES-256-GCM.
     - Unmarshal JSON.
     - Check `ExpiresAt`. If expired, return error.
   
   - `HandleLogout(w, r)`:
     - Delete `__mcpgw_admin` cookie (set MaxAge=-1).
     - Redirect to `/admin/`.

3. `internal/admin/middleware.go`:
   ```go
   func AdminAuthMiddleware(auth *AdminAuth) func(http.Handler) http.Handler {
       return func(next http.Handler) http.Handler {
           return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
               // Skip auth for login/callback/static routes
               if r.URL.Path == "/admin/login" || r.URL.Path == "/admin/callback" ||
                  strings.HasPrefix(r.URL.Path, "/admin/static/") {
                   next.ServeHTTP(w, r)
                   return
               }
               
               session, err := auth.GetSession(r)
               if err != nil {
                   // Redirect to login
                   auth.HandleLogin(w, r)
                   return
               }
               
               // Inject session into context
               ctx := context.WithValue(r.Context(), sessionContextKey, session)
               next.ServeHTTP(w, r.WithContext(ctx))
           })
       }
   }
   
   func CSRFMiddleware(next http.Handler) http.Handler {
       return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
               session := SessionFromContext(r.Context())
               formToken := r.FormValue("csrf_token")
               if session == nil || formToken != session.CSRFToken {
                   http.Error(w, "CSRF validation failed", http.StatusForbidden)
                   return
               }
           }
           next.ServeHTTP(w, r)
       })
   }
   ```

4. **Tests** `internal/admin/auth_test.go`:
   - Test PKCE: generate verifier → compute challenge → verify SHA-256 matches.
   - Test session encryption/decryption round-trip.
   - Test expired session returns error.
   - Test tampered cookie returns error (GCM auth tag fails).
   - Test middleware redirects unauthenticated users.
   - Test CSRF: POST without token → 403. POST with valid token → passes.
   - Test non-admin user (missing required scope) → 403 after callback.

### Verification
```bash
go test ./internal/admin/... ./internal/config/...
go vet ./...
```

### Acceptance Criteria
- PKCE flow generates valid code verifier and challenge
- Session cookie is encrypted with AES-256-GCM
- Tampered cookies are rejected (authentication tag failure)
- Expired sessions redirect to login
- CSRF protection blocks POST without valid token
- Non-admin users get 403 at callback
- State parameter prevents CSRF on OAuth callback

---

## Prompt 2 of 4: Admin Handler, Templates & CSS

### Required Reading (read these files before writing code)
- docs/phases/PHASE12B.md (template structure, pages)
- internal/admin/auth.go (session context, CSRF token)
- internal/store/interfaces.go (all store interfaces for data access)
- internal/ratelimit/backend.go (LimiterBackend)

### Task

Create the admin handler, templates, and CSS. Focus on the base layout, dashboard, tool catalog, and tool edit pages.

1. `internal/admin/admin.go`:
   ```go
   //go:embed templates/*
   var templateFS embed.FS
   
   //go:embed static/*
   var staticFS embed.FS
   
   type AdminDeps struct {
       ServerStore         store.ServerStore
       ClassificationStore store.ToolClassificationStore
       AuditStore          store.AuditStore
       RateLimitBackend    ratelimit.LimiterBackend
       Broadcaster         registration.Broadcaster
       Auth                *AdminAuth
       Logger              *slog.Logger
   }
   
   func NewRouter(deps AdminDeps) chi.Router {
       handler := NewAdminHandler(deps)
       
       r := chi.NewRouter()
       r.Use(AdminAuthMiddleware(deps.Auth))
       r.Use(CSRFMiddleware)
       
       // Static files (no auth needed, handled by middleware skip)
       r.Handle("/static/*", http.StripPrefix("/admin/static/",
           http.FileServer(http.FS(staticFS))))
       
       // Auth routes
       r.Get("/login", deps.Auth.HandleLogin)
       r.Get("/callback", deps.Auth.HandleCallback)
       r.Get("/logout", deps.Auth.HandleLogout)
       
       // Dashboard
       r.Get("/", handler.handleDashboard)
       
       // Tools
       r.Get("/tools", handler.handleTools)
       r.Get("/tools/{name}", handler.handleToolEdit)
       r.Post("/tools/{name}", handler.handleToolUpdate)
       r.Post("/tools/bulk", handler.handleToolBulkUpdate)
       
       // Rate Limits
       r.Get("/ratelimits", handler.handleRateLimits)
       
       // Audit
       r.Get("/audit", handler.handleAudit)
       r.Get("/audit/export", handler.handleAuditExport)
       
       // Servers
       r.Get("/servers", handler.handleServers)
       r.Post("/servers/{id}/deregister", handler.handleServerDeregister)
       
       return r
   }
   ```

2. **Templates** — create all template files listed in the scope. Key patterns:
   
   `templates/base.html`:
   - Include HTMX and Alpine.js from CDN (with SRI integrity hashes for security).
   - Navigation with `hx-boost="true"` for smooth page transitions.
   - Pass `{{.Session}}` for user display and CSRF token.
   
   `templates/tools.html`:
   ```html
   {{define "content"}}
   <h1>Tool Catalog</h1>
   <div class="filters" x-data="{search: '', risk: ''}">
       <input type="text" placeholder="Search tools..."
              x-model="search"
              hx-get="/admin/tools"
              hx-target="#tools-table"
              hx-trigger="input changed delay:300ms"
              hx-include="[name='risk']"
              name="q">
       <select name="risk" x-model="risk"
               hx-get="/admin/tools"
               hx-target="#tools-table"
               hx-include="[name='q']">
           <option value="">All risk levels</option>
           <option value="read_only">Read Only</option>
           <option value="write">Write</option>
           <option value="destructive">Destructive</option>
           <option value="unknown">Unknown</option>
       </select>
   </div>
   <div id="tools-table">
       {{template "tools_table.html" .}}
   </div>
   {{end}}
   ```
   
   `templates/tools_table.html` (HTMX partial):
   ```html
   <table>
       <thead>
           <tr>
               <th><input type="checkbox" x-on:click="toggleAll"></th>
               <th>Tool Name</th>
               <th>Server</th>
               <th>Risk Level</th>
               <th>Auto</th>
               <th>Updated By</th>
               <th>Actions</th>
           </tr>
       </thead>
       <tbody>
           {{range .Tools}}
           <tr>
               <td><input type="checkbox" name="tools" value="{{.ToolName}}"></td>
               <td>{{.ToolName}}</td>
               <td>{{.ServerName}}</td>
               <td><span class="badge badge-{{.RiskLevel}}">{{.RiskLevel}}</span></td>
               <td>{{if .AutoClassified}}Auto{{else}}Manual{{end}}</td>
               <td>{{.UpdatedBy}}</td>
               <td><a href="/admin/tools/{{.ToolName}}" hx-boost="true">Edit</a></td>
           </tr>
           {{end}}
       </tbody>
   </table>
   ```

3. `internal/admin/handler.go`:
   - `handleDashboard(w, r)`: query summary stats from stores, render `dashboard.html`.
   - `handleTools(w, r)`: query classifications with optional `q` (search) and `risk` (filter) params. If `HX-Request` header present, render `tools_table.html` partial. Otherwise render full `tools.html`.
   - `handleToolEdit(w, r)`: get classification by tool name from URL param, render `tool_edit.html`.
   - `handleToolUpdate(w, r)`:
     1. Parse form: `risk_level`, `reason`, `csrf_token`.
     2. Validate CSRF.
     3. Upsert classification with `auto_classified=false`, `updated_by` from session.
     4. Broadcast `ClassificationEvent` via broadcaster.
     5. Audit log the change.
     6. Redirect to `/admin/tools` (or HTMX response if HX-Request).

4. `internal/admin/static/style.css`:
   Minimal, clean CSS:
   ```css
   :root {
       --bg: #f8f9fa; --fg: #212529; --primary: #0d6efd;
       --success: #198754; --warning: #ffc107; --danger: #dc3545;
       --border: #dee2e6; --radius: 4px;
   }
   *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
   body { font-family: system-ui, -apple-system, sans-serif; background: var(--bg); color: var(--fg); line-height: 1.5; }
   nav { display: flex; align-items: center; padding: 0.75rem 1.5rem; background: #fff; border-bottom: 1px solid var(--border); }
   .nav-brand { font-weight: 700; font-size: 1.1rem; margin-right: 2rem; }
   .nav-links a { margin-right: 1rem; text-decoration: none; color: var(--fg); }
   .nav-links a:hover { color: var(--primary); }
   main { max-width: 1200px; margin: 1.5rem auto; padding: 0 1.5rem; }
   h1 { margin-bottom: 1rem; }
   table { width: 100%; border-collapse: collapse; background: #fff; border-radius: var(--radius); overflow: hidden; }
   th, td { padding: 0.5rem 0.75rem; text-align: left; border-bottom: 1px solid var(--border); }
   th { background: #f1f3f5; font-weight: 600; font-size: 0.85rem; text-transform: uppercase; }
   .badge { padding: 0.15rem 0.5rem; border-radius: var(--radius); font-size: 0.8rem; font-weight: 600; }
   .badge-read_only { background: #d1e7dd; color: var(--success); }
   .badge-write { background: #fff3cd; color: #856404; }
   .badge-destructive { background: #f8d7da; color: var(--danger); }
   .badge-unknown { background: #e2e3e5; color: #6c757d; }
   .filters { display: flex; gap: 1rem; margin-bottom: 1rem; }
   input[type="text"], select { padding: 0.4rem 0.6rem; border: 1px solid var(--border); border-radius: var(--radius); }
   button, .btn { padding: 0.4rem 0.8rem; border: none; border-radius: var(--radius); cursor: pointer; font-size: 0.9rem; }
   .btn-primary { background: var(--primary); color: #fff; }
   .btn-danger { background: var(--danger); color: #fff; }
   .card { background: #fff; border: 1px solid var(--border); border-radius: var(--radius); padding: 1rem; }
   .grid-4 { display: grid; grid-template-columns: repeat(4, 1fr); gap: 1rem; margin-bottom: 1.5rem; }
   .stat-value { font-size: 2rem; font-weight: 700; }
   .stat-label { font-size: 0.85rem; color: #6c757d; }
   ```

5. **Tests** `internal/admin/handler_test.go`:
   - Test each handler renders templates without error (pass mock data).
   - Test HTMX partial detection (HX-Request header → only partial rendered).
   - Test tool update persists to store and publishes broadcast event.
   - Test dashboard queries return valid data.

### Verification
```bash
go test ./internal/admin/...
go vet ./...
```

### Acceptance Criteria
- All templates render without errors
- HTMX partials returned when HX-Request header present
- Tool classification update persists to DB and broadcasts
- CSS produces a clean, readable UI
- CSRF token included in all forms
- No XSS: all user data goes through template auto-escaping

---

## Prompt 3 of 4: Audit Log, Rate Limits, Servers Pages & CSV Export

### Required Reading (read these files before writing code)
- internal/admin/handler.go (existing handler pattern from Prompt 2)
- internal/admin/templates/ (existing template pattern)
- internal/store/interfaces.go (AuditStore, ServerStore)
- internal/ratelimit/backend.go (LimiterBackend interface)

### Task

Implement the remaining admin pages: audit log with filters and export, rate limit viewer, and server management.

1. **Audit log handler** in `internal/admin/handler.go`:
   - `handleAudit(w, r)`:
     - Parse query params: `from` (date), `to` (date), `client_id`, `tool_name`, `decision` (allowed/denied), `page` (default 1).
     - Build query with filters. Page size: 50.
     - Calculate total pages for pagination.
     - Render `audit.html` (full) or `audit_table.html` (HTMX partial).
   
   - Audit store query helper: The existing `AuditStore` may not have a filter method. Add one:
     ```go
     // In internal/store/interfaces.go:
     type AuditFilter struct {
         ClientID  string
         ToolName  string
         EventType string
         Decision  string    // "allowed" or "denied"
         From      time.Time
         To        time.Time
         Limit     int
         Offset    int
     }
     
     // Add to AuditStore interface:
     List(ctx context.Context, filter AuditFilter) ([]types.AuditEntry, int, error) // returns entries and total count
     ```
   - Implement `List()` in the Postgres audit store with dynamic WHERE clause building (use parameterized queries, NOT string concatenation):
     ```go
     func (s *PostgresAuditStore) List(ctx context.Context, filter AuditFilter) ([]types.AuditEntry, int, error) {
         var conditions []string
         var args []interface{}
         argIdx := 1
         
         if filter.ClientID != "" {
             conditions = append(conditions, fmt.Sprintf("client_id = $%d", argIdx))
             args = append(args, filter.ClientID)
             argIdx++
         }
         // ... similar for other filters ...
         
         where := ""
         if len(conditions) > 0 {
             where = "WHERE " + strings.Join(conditions, " AND ")
         }
         // Count query
         // Data query with LIMIT/OFFSET
     }
     ```

2. **Audit templates**:
   
   `templates/audit.html`:
   ```html
   {{define "content"}}
   <h1>Audit Log</h1>
   <div class="filters">
       <input type="date" name="from" hx-get="/admin/audit" hx-target="#audit-table" hx-include=".filters *">
       <input type="date" name="to" hx-get="/admin/audit" hx-target="#audit-table" hx-include=".filters *">
       <input type="text" name="client_id" placeholder="Client ID" hx-get="/admin/audit" hx-target="#audit-table" hx-trigger="input changed delay:300ms" hx-include=".filters *">
       <input type="text" name="tool_name" placeholder="Tool name" hx-get="/admin/audit" hx-target="#audit-table" hx-trigger="input changed delay:300ms" hx-include=".filters *">
       <select name="decision" hx-get="/admin/audit" hx-target="#audit-table" hx-include=".filters *">
           <option value="">All</option>
           <option value="allowed">Allowed</option>
           <option value="denied">Denied</option>
       </select>
       <a href="/admin/audit/export?{{.FilterQueryString}}" class="btn btn-primary" download>Export CSV</a>
   </div>
   <div id="audit-table">
       {{template "audit_table.html" .}}
   </div>
   {{end}}
   ```

3. **CSV export handler** `internal/admin/export.go`:
   ```go
   func (h *AdminHandler) handleAuditExport(w http.ResponseWriter, r *http.Request) {
       // Parse same filters as handleAudit
       filter := parseAuditFilter(r)
       filter.Limit = 10000 // Cap export size
       filter.Offset = 0
       
       entries, _, err := h.auditStore.List(r.Context(), filter)
       // ...
       
       w.Header().Set("Content-Type", "text/csv")
       w.Header().Set("Content-Disposition", "attachment; filename=audit_log.csv")
       
       writer := csv.NewWriter(w)
       writer.Write([]string{"Timestamp", "Client ID", "Tool Name", "Event Type", "Decision", "Reason", "Enforcement Mode"})
       for _, e := range entries {
           writer.Write([]string{
               e.CreatedAt.Format(time.RFC3339),
               e.ClientID,
               e.ToolName,
               e.EventType,
               // ... map fields ...
           })
       }
       writer.Flush()
   }
   ```

4. **Rate limit handler** in `internal/admin/handler.go`:
   - `handleRateLimits(w, r)`:
     - Need to expose current rate limit state from the backend.
     - Add `Snapshot() []RateLimitEntry` method to `LimiterBackend` interface (or as an optional interface):
       ```go
       type LimiterInspector interface {
           Snapshot() []RateLimitEntry
       }
       
       type RateLimitEntry struct {
           ClientID  string
           Route     string
           Current   int
           Limit     int
           Remaining int
       }
       ```
     - For MemoryBackend: iterate map, compute current tokens.
     - For RedisBackend: `SCAN` for `rl:*` keys, `ZCARD` each to get current count.
     - If backend doesn't implement `LimiterInspector`, show "Inspection not available for this backend".

5. **Server handler** in `internal/admin/handler.go`:
   - `handleServers(w, r)`: list all servers from store.
   - `handleServerDeregister(w, r)`:
     1. Get server ID from URL.
     2. CSRF check.
     3. Deregister via registration service (or directly via store).
     4. Broadcast deregistration event.
     5. Audit log the admin action.
     6. Redirect to `/admin/servers`.

6. **Tests:**
   - `internal/store/audit_store_test.go`: Test `List()` with various filters and pagination.
   - `internal/admin/handler_test.go`: Test audit page renders, CSV export produces valid CSV, rate limit page renders, server deregister works.

### Verification
```bash
go test ./internal/admin/... ./internal/store/...
go vet ./...
```

### Acceptance Criteria
- Audit log filters work independently and combined
- Pagination shows correct total pages
- CSV export produces valid CSV with all filtered entries (capped at 10K)
- Rate limit snapshot shows current per-client state
- Server deregister is audit-logged with admin identity
- HTMX partial updates work for all pages

---

## Prompt 4 of 4: Wiring, Helm Values & Integration Test

### Required Reading (read these files before writing code)
- All Phase 12B files from prompts 1-3
- cmd/mcpgw/serve.go (existing wiring pattern)
- charts/mcpgateway/values.yaml (existing Helm pattern)

### Task

Wire the admin dashboard into the gateway, add Helm values, and write integration tests.

1. **Wiring** in `cmd/mcpgw/serve.go`:
   ```go
   if cfg.AdminEnabled {
       adminAuth, err := admin.NewAdminAuth(cfg, logger)
       if err != nil {
           return fmt.Errorf("admin auth setup: %w", err)
       }
       
       adminRouter := admin.NewRouter(admin.AdminDeps{
           ServerStore:         serverStore,
           ClassificationStore: classificationStore,
           AuditStore:          auditStore,
           RateLimitBackend:    rlBackend,
           Broadcaster:         broadcaster,
           Auth:                adminAuth,
           Logger:              logger,
       })
       
       // Mount admin routes on the main router
       srv.MountAdmin(adminRouter)
       logger.Info("admin dashboard enabled at /admin/")
   }
   ```
   
   - Add `MountAdmin(r chi.Router)` method to `*Server` in `internal/server/server.go`:
     ```go
     func (s *Server) MountAdmin(adminRouter chi.Router) {
         s.router.Mount("/admin", adminRouter)
     }
     ```

2. **Helm values**:
   ```yaml
   admin:
     enabled: false
     oidc:
       clientID: ""
       # clientSecret via Vault secret
     requiredScope: admin
     sessionTTL: 8h
     # sessionKey via Vault secret
   ```
   
   Add to configmap template:
   ```yaml
   MCPGW_ADMIN_ENABLED: {{ .Values.admin.enabled | quote }}
   {{- if .Values.admin.enabled }}
   MCPGW_ADMIN_OIDC_CLIENT_ID: {{ .Values.admin.oidc.clientID | quote }}
   MCPGW_ADMIN_REQUIRED_SCOPE: {{ .Values.admin.requiredScope | quote }}
   MCPGW_ADMIN_SESSION_TTL: {{ .Values.admin.sessionTTL | quote }}
   {{- end }}
   ```
   
   Add secret references:
   ```yaml
   # In deployment template, env section:
   {{- if .Values.admin.enabled }}
   - name: MCPGW_ADMIN_OIDC_CLIENT_SECRET
     valueFrom:
       secretKeyRef:
         name: {{ include "mcpgateway.fullname" . }}-admin
         key: client-secret
   - name: MCPGW_ADMIN_SESSION_KEY
     valueFrom:
       secretKeyRef:
         name: {{ include "mcpgateway.fullname" . }}-admin
         key: session-key
   {{- end }}
   ```

3. **Integration test** `internal/admin/integration_test.go`:
   - Set up gateway with admin enabled, mock IdP, test DB.
   - Test full flow:
     1. Navigate to `/admin/` → redirect to IdP.
     2. Simulate IdP callback with valid code.
     3. Verify session cookie set.
     4. Access `/admin/tools` → page renders with tools.
     5. POST `/admin/tools/test-tool` to change classification → verify persisted.
     6. Verify broadcast event published.
     7. Access `/admin/audit` → page renders with entries.
     8. Export CSV → verify content.
   - Test non-admin user → 403 after callback.
   - Test expired session → redirect to login.

4. **Update serve.go** to ensure all admin dependencies are available and properly ordered in the initialization sequence.

### Verification
```bash
go test ./internal/admin/... ./cmd/mcpgw/...
go vet ./...
helm lint charts/mcpgateway
helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml --set admin.enabled=true --set admin.oidc.clientID=test
```

### Acceptance Criteria
- Admin dashboard accessible at `/admin/` when enabled
- Full OIDC login → session → pages → actions flow works
- Classification changes broadcast to all pods
- Helm renders correctly with admin enabled/disabled
- Admin secrets loaded from Kubernetes secrets, never from values.yaml
- Integration test covers the full admin workflow
