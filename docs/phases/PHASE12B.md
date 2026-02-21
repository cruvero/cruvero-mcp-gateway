# Phase 12B: Admin Dashboard with Go Templates + HTMX + Alpine.js

## Overview

Build a minimal but functional admin dashboard as a Go-template-rendered web UI with HTMX for partial page updates and Alpine.js for client-side interactivity. Authenticated via OIDC Authorization Code + PKCE flow with encrypted session cookies. All admin actions audit-logged and classification changes broadcast to all pods.

## Scope

### Technology Stack

- **Server rendering**: Go `html/template` with a base layout and per-page templates
- **Partial updates**: HTMX (~14KB, CDN-loaded) for search, filtering, and form submission without full page reloads
- **Client interactivity**: Alpine.js (~15KB, CDN-loaded) for dropdowns, confirmations, toggle states
- **CSS**: Embedded minimal CSS — clean, functional, no framework. System font stack.
- **No build step**: All assets are either embedded via `//go:embed` or loaded from CDN

### OIDC Authorization Code + PKCE for Admin Auth (`internal/admin/auth.go`)

The admin UI runs in a browser, so it uses the standard Authorization Code flow (not Device Code):

1. User navigates to `/admin/`.
2. `AdminAuthMiddleware` checks for session cookie.
3. No session → generate PKCE code verifier + challenge, redirect to IdP authorize endpoint.
4. User authenticates at IdP → redirect back to `/admin/callback` with auth code.
5. Gateway exchanges code + verifier for tokens at IdP token endpoint.
6. Validate `id_token`, check for admin scope/group.
7. Create encrypted session cookie (AES-256-GCM).
8. Redirect to original URL.

```go
type AdminAuth struct {
    issuerURL    string
    clientID     string
    clientSecret string
    redirectURL  string // https://gateway/admin/callback
    requiredScope string // e.g., "admin" or "gateway-admin"
    sessionKey   [32]byte // AES-256-GCM key
    provider     *oidc.Provider
    logger       *slog.Logger
}

type Session struct {
    Subject   string    `json:"sub"`
    Email     string    `json:"email"`
    Name      string    `json:"name"`
    ExpiresAt time.Time `json:"expires_at"`
    CSRFToken string    `json:"csrf_token"`
}
```

Session cookie properties:
- Name: `__mcpgw_admin`
- `HttpOnly: true`
- `Secure: true`
- `SameSite: Strict`
- Payload: AES-256-GCM encrypted JSON of `Session` struct
- TTL: 8 hours
- CSRF: double-submit pattern — `csrf_token` in cookie matches hidden form field

### Template Structure (`internal/admin/templates/`)

Embedded via `//go:embed templates/*`:

```
internal/admin/
├── templates/
│   ├── base.html          # Base layout with nav, HTMX/Alpine.js script tags
│   ├── dashboard.html     # Overview page
│   ├── tools.html         # Tool catalog with risk editing
│   ├── tool_edit.html     # Tool classification edit form
│   ├── tools_table.html   # HTMX partial: tool table body
│   ├── ratelimits.html    # Rate limit state viewer
│   ├── ratelimits_table.html # HTMX partial
│   ├── audit.html         # Audit log viewer with filters
│   ├── audit_table.html   # HTMX partial
│   ├── servers.html       # Server status
│   ├── servers_table.html # HTMX partial
│   └── login.html         # Login required page
├── static/
│   └── style.css          # Minimal embedded CSS
├── handler.go             # HTTP handlers
├── auth.go                # OIDC auth + session management
├── middleware.go          # Auth middleware, CSRF protection
└── admin.go               # Router setup, template loading
```

### Base Layout (`templates/base.html`)

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>MCP Gateway Admin - {{block "title" .}}Dashboard{{end}}</title>
    <link rel="stylesheet" href="/admin/static/style.css">
    <script src="https://unpkg.com/htmx.org@2.0.4"></script>
    <script src="https://unpkg.com/alpinejs@3.14.8/dist/cdn.min.js" defer></script>
</head>
<body>
    <nav>
        <div class="nav-brand">MCP Gateway</div>
        <div class="nav-links">
            <a href="/admin/" hx-boost="true">Dashboard</a>
            <a href="/admin/tools" hx-boost="true">Tools</a>
            <a href="/admin/ratelimits" hx-boost="true">Rate Limits</a>
            <a href="/admin/audit" hx-boost="true">Audit Log</a>
            <a href="/admin/servers" hx-boost="true">Servers</a>
        </div>
        <div class="nav-user">{{.Session.Email}}</div>
    </nav>
    <main>
        {{block "content" .}}{{end}}
    </main>
</body>
</html>
```

### Dashboard Pages

**Dashboard (`/admin/`):**
- Summary cards: total servers (active/stale/expired), total tools by risk level, recent rate limit hits (1h), recent denials (1h)
- Queries: `SELECT count(*), status FROM mcp_servers GROUP BY status`, `SELECT count(*), risk_level FROM tool_classifications GROUP BY risk_level`, `SELECT count(*) FROM audit_log WHERE created_at > now() - interval '1 hour' AND ...`

**Tool Catalog (`/admin/tools`):**
- Table with columns: tool name, server, risk level (color-coded badge), auto-classified, updated by, updated at
- HTMX search: `<input hx-get="/admin/tools?q={search}" hx-target="#tools-table" hx-trigger="input changed delay:300ms">`
- HTMX filter by risk level: `<select hx-get="/admin/tools?risk={level}" hx-target="#tools-table">`
- Alpine.js bulk select: `<input type="checkbox" x-model="selected" @click="toggleAll">`
- Bulk action: "Mark as destructive" / "Mark as read_only" — POST form with CSRF

**Tool Edit (`/admin/tools/{name}`):**
- Form: risk level dropdown, reason textarea, current classification display
- Alpine.js confirmation on destructive→read_only change: `x-on:submit="if (!confirm('Remove destructive block?')) return false"`
- POST handler: validate, upsert classification with `auto_classified=false`, `updated_by` from session email, broadcast `ClassificationEvent` via broadcaster

**Rate Limits (`/admin/ratelimits`):**
- Table: client ID, route, current usage, limit, remaining, window
- Data source: Redis `SCAN` for `rl:*` keys (parse sorted set size), or in-memory backend state
- Auto-refresh: `hx-trigger="every 10s"` on the table container
- For Redis backend: add a method to `RedisBackend` to scan and report current state
- For memory backend: expose a `Snapshot()` method

**Audit Log (`/admin/audit`):**
- Table: timestamp, client ID, tool name, decision, reason, enforcement mode
- HTMX filters: date range, client ID, tool name, decision (dropdowns that trigger GET with query params)
- Pagination: HTMX `hx-get="/admin/audit?page=2"` links
- Page size: 50
- CSV export: `<a href="/admin/audit/export?...filters..." download>Export CSV</a>`

**Servers (`/admin/servers`):**
- Table: server name, status (color badge), SPIFFE ID, last heartbeat (relative time), tool count
- Alpine.js deregister confirmation: `x-on:click="if (!confirm('Deregister server?')) return false"`
- HTMX refresh: `hx-trigger="every 30s"`

### Handler Implementation (`internal/admin/handler.go`)

```go
type AdminHandler struct {
    templates           *template.Template
    serverStore         store.ServerStore
    classificationStore store.ToolClassificationStore
    auditStore          store.AuditStore
    rateLimitBackend    ratelimit.LimiterBackend
    broadcaster         registration.Broadcaster
    logger              *slog.Logger
}

func NewAdminHandler(deps AdminDeps) *AdminHandler
func (h *AdminHandler) Routes() chi.Router
```

Handlers return HTMX partials when `HX-Request` header is present, full pages otherwise:

```go
func (h *AdminHandler) handleTools(w http.ResponseWriter, r *http.Request) {
    // ... fetch data ...
    if r.Header.Get("HX-Request") == "true" {
        h.templates.ExecuteTemplate(w, "tools_table.html", data)
    } else {
        h.templates.ExecuteTemplate(w, "tools.html", data)
    }
}
```

### Config Changes (`internal/config/config.go`)

- `MCPGW_ADMIN_ENABLED` (default `false`)
- `MCPGW_ADMIN_OIDC_CLIENT_ID` (required when admin enabled)
- `MCPGW_ADMIN_OIDC_CLIENT_SECRET` (required)
- `MCPGW_ADMIN_REQUIRED_SCOPE` (default `"admin"`)
- `MCPGW_ADMIN_SESSION_KEY` (32 bytes hex-encoded, for AES-256-GCM)
- `MCPGW_ADMIN_SESSION_TTL` (default `8h`)

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/admin/admin.go` | Package init, router, template loading |
| `internal/admin/handler.go` | HTTP handlers for all pages |
| `internal/admin/auth.go` | OIDC Authorization Code + PKCE, session management |
| `internal/admin/middleware.go` | Auth check, CSRF protection |
| `internal/admin/export.go` | CSV export handler for audit logs |
| `internal/admin/templates/base.html` | Base layout |
| `internal/admin/templates/dashboard.html` | Dashboard page |
| `internal/admin/templates/tools.html` | Tool catalog page |
| `internal/admin/templates/tool_edit.html` | Tool edit form |
| `internal/admin/templates/tools_table.html` | HTMX partial |
| `internal/admin/templates/ratelimits.html` | Rate limit page |
| `internal/admin/templates/ratelimits_table.html` | HTMX partial |
| `internal/admin/templates/audit.html` | Audit log page |
| `internal/admin/templates/audit_table.html` | HTMX partial |
| `internal/admin/templates/servers.html` | Server status page |
| `internal/admin/templates/servers_table.html` | HTMX partial |
| `internal/admin/templates/login.html` | Login redirect page |
| `internal/admin/static/style.css` | Minimal CSS |
| `internal/config/config.go` | Admin config fields |
| `internal/server/server.go` | Mount admin routes |
| `cmd/mcpgw/serve.go` | Wire admin handler |

## Testing Requirements

- Auth: test PKCE flow — generate verifier, challenge, verify exchange
- Auth: test session cookie encryption/decryption round-trip
- Auth: test expired session redirects to login
- Auth: test non-admin user gets 403
- CSRF: test POST without CSRF token returns 403
- Handlers: test each page renders without error (template execution)
- Handlers: test HTMX partial responses (HX-Request header detection)
- Tool edit: test classification update persists and broadcasts event
- Audit export: test CSV output format and content
- Coverage: >=80%
