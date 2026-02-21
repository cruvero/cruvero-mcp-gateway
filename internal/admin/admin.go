package admin

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/go-chi/chi/v5"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// AdminDeps holds all dependencies required by the admin dashboard.
type AdminDeps struct {
	Auth                *AdminAuth
	DevMode             bool
	Logger              *slog.Logger
	ServerStore         store.ServerStore
	AuditStore          store.AuditStore
	ClassificationStore store.ToolClassificationStore
	Broadcaster         registration.Broadcaster
	RateLimitBackend    ratelimit.LimiterBackend
}

// NewRouter creates the admin dashboard chi router.
func NewRouter(deps AdminDeps) chi.Router {
	if deps.Auth == nil && !deps.DevMode {
		panic("admin: Auth must not be nil when DevMode is false")
	}

	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	handler := NewAdminHandler(deps)

	r := chi.NewRouter()

	// Unauthenticated routes.
	if deps.Auth != nil {
		r.Get("/login", deps.Auth.HandleLogin)
		r.Get("/callback", deps.Auth.HandleCallback)
	} else {
		r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/", http.StatusFound)
		})
		r.Get("/callback", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/", http.StatusFound)
		})
	}

	// Static files.
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/admin/static/", http.FileServer(http.FS(staticSub))))

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(AdminAuthMiddleware(deps.Auth, deps.DevMode))
		r.Use(CSRFMiddleware)

		r.Get("/", handler.HandleDashboard)
		r.Get("/tools", handler.HandleTools)
		r.Get("/tools/{name}", handler.HandleToolEdit)
		r.Post("/tools/{name}", handler.HandleToolUpdate)
		r.Get("/ratelimits", handler.HandleRateLimits)
		r.Get("/audit", handler.HandleAudit)
		r.Get("/audit/export", handler.HandleAuditExport)
		r.Get("/servers", handler.HandleServers)
		r.Post("/servers/{id}/deregister", handler.HandleServerDeregister)
	})

	return r
}
