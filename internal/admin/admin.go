package admin

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	adminModeStandalone = "standalone"
	adminModeIntegrated = "integrated"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// AdminDeps holds all dependencies required by the admin dashboard.
type AdminDeps struct {
	Auth                 *AdminAuth
	DevMode              bool
	Mode                 string
	PlatformServiceToken string
	Logger               *slog.Logger
	ServerStore          store.ServerStore
	AuditStore           store.AuditStore
	ClassificationStore  store.ToolClassificationStore
	UserStore            store.UserStore
	Broadcaster          registration.Broadcaster
	RateLimitBackend     ratelimit.LimiterBackend
	DiscoveryIndex       *proxy.DiscoveryIndex
	ProgressiveDiscovery bool
	SynonymStore         store.SynonymStore
	ReindexLogStore      store.ReindexLogStore
	SearchEngineType     string
	EmbedderType         string
	SearchStatusFunc     func() SearchStatusSnapshot
}

// SearchStatusSnapshot captures current search health as shown in admin UI.
type SearchStatusSnapshot struct {
	EngineType            string
	EmbedderType          string
	IndexedTools          int
	EngineReady           bool
	VectorReady           *bool
	VectorSearchFallbacks uint64
	VectorIndexFallbacks  uint64
	EngineErrorFallbacks  uint64
}

func normalizeAdminMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case adminModeIntegrated:
		return adminModeIntegrated
	default:
		return adminModeStandalone
	}
}

// NewRouter creates the admin dashboard chi router.
func NewRouter(deps AdminDeps) chi.Router {
	mode := normalizeAdminMode(deps.Mode)
	if mode == adminModeStandalone && deps.Auth == nil && !deps.DevMode {
		panic("admin: Auth must not be nil when DevMode is false")
	}

	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	handler := NewAdminHandler(deps)

	if mode == adminModeIntegrated {
		return newIntegratedRouter(handler, deps)
	}

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
		r.Use(SecurityHeadersMiddleware)
		r.Use(AdminAuthMiddleware(deps.Auth, deps.DevMode))
		r.Use(CSRFMiddleware)

		r.Get("/", handler.HandleDashboard)
		r.Get("/tools", handler.HandleTools)
		r.Get("/tools/browse", handler.HandleToolBrowse)
		r.Get("/tools/discovery/stats", handler.HandleDiscoveryStats)
		r.Get("/tools/{name}", handler.HandleToolEdit)
		r.Get("/tools/{name}/schema", handler.HandleToolSchema)
		r.Post("/tools/{name}", handler.HandleToolUpdate)
		r.Get("/ratelimits", handler.HandleRateLimits)
		r.Get("/audit", handler.HandleAudit)
		r.Get("/audit/export", handler.HandleAuditExport)
		r.Get("/users", handler.HandleUsers)
		r.Get("/users/{id}", handler.HandleUserDetail)
		r.Post("/users/{id}/role", handler.HandleUserRoleUpdate)
		r.Post("/users/{id}/permissions", handler.HandleUserPermissionsUpdate)
		r.Post("/users/{id}/tools/add", handler.HandleUserToolsAdd)
		r.Post("/users/{id}/tools/remove", handler.HandleUserToolsRemove)

		r.Get("/search", handler.HandleSearch)
		r.Post("/search/test", handler.HandleSearchTest)
		r.Get("/search/synonyms", handler.HandleSynonymList)
		r.Post("/search/synonyms", handler.HandleSynonymCreate)
		r.Post("/search/synonyms/{term}/delete", handler.HandleSynonymDelete)

		r.Get("/servers", handler.HandleServers)
		r.Get("/servers/{id}/ratelimit", handler.HandleServerRateLimitEdit)
		r.Post("/servers/{id}/ratelimit", handler.HandleServerRateLimitUpdate)
		r.Post("/servers/prune-orphaned-tools", handler.HandlePruneOrphanedTools)
		r.Post("/servers/{id}/deregister", handler.HandleServerDeregister)

		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			handler.renderError(w, r, http.StatusNotFound,
				"The page you're looking for doesn't exist or has been moved.")
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		handler.renderError(w, r, http.StatusNotFound,
			"The page you're looking for doesn't exist or has been moved.")
	})

	return r
}
