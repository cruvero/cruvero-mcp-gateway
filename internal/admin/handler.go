package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

const headerHXRequest = "HX-Request"

// AdminHandler serves admin dashboard pages.
type AdminHandler struct {
	logger               *slog.Logger
	serverStore          store.ServerStore
	auditStore           store.AuditStore
	classificationStore  store.ToolClassificationStore
	userStore            store.UserStore
	broadcaster          registration.Broadcaster
	rateLimitBackend     ratelimit.LimiterBackend
	pages                map[string]*template.Template
	partials             *template.Template
	discoveryIndex       *proxy.DiscoveryIndex
	progressiveDiscovery bool
	devMode              bool
	synonymStore         store.SynonymStore
	reindexLogStore      store.ReindexLogStore
	searchEngineType     string
	embedderType         string
	searchStatusFunc     func() SearchStatusSnapshot
}

// NewAdminHandler creates a new admin handler with compiled templates.
//
// Each page template defines {{define "content"}}, so they cannot share a
// single template.Template (Go uses the last definition). We build per-page
// template sets by cloning the shared base+partials and parsing each page
// template into its own clone.
func NewAdminHandler(deps AdminDeps) *AdminHandler {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	shared := template.Must(template.ParseFS(templateFS,
		"templates/base.html",
		"templates/*_table.html",
	))

	pageFiles := []string{
		"templates/dashboard.html",
		"templates/servers.html",
		"templates/server_ratelimit.html",
		"templates/audit.html",
		"templates/ratelimits.html",
		"templates/tools.html",
		"templates/tool_edit.html",
		"templates/users.html",
		"templates/user_detail.html",
		"templates/error.html",
		"templates/search.html",
	}

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, pf := range pageFiles {
		clone := template.Must(shared.Clone())
		pages[pf] = template.Must(clone.ParseFS(templateFS, pf))
	}

	return &AdminHandler{
		logger:               deps.Logger,
		serverStore:          deps.ServerStore,
		auditStore:           deps.AuditStore,
		classificationStore:  deps.ClassificationStore,
		userStore:            deps.UserStore,
		broadcaster:          deps.Broadcaster,
		rateLimitBackend:     deps.RateLimitBackend,
		pages:                pages,
		partials:             shared,
		discoveryIndex:       deps.DiscoveryIndex,
		progressiveDiscovery: deps.ProgressiveDiscovery,
		devMode:              deps.DevMode,
		synonymStore:         deps.SynonymStore,
		reindexLogStore:      deps.ReindexLogStore,
		searchEngineType:     deps.SearchEngineType,
		embedderType:         deps.EmbedderType,
		searchStatusFunc:     deps.SearchStatusFunc,
	}
}

func (h *AdminHandler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	session, _ := SessionFromContext(r.Context())
	if session != nil {
		data["Email"] = session.Email
		data["CSRFToken"] = session.CSRFToken
	}
	data["DevMode"] = h.devMode

	// HTMX partial response.
	if r.Header.Get(headerHXRequest) == "true" {
		// Page templates define {{define "content"}} — render that block.
		if pageTmpl, ok := h.pages["templates/"+name]; ok {
			if err := pageTmpl.ExecuteTemplate(w, "content", data); err != nil {
				h.logger.Error("render partial failed", slog.String("template", name), slog.String("error", err.Error()))
				http.Error(w, "render failed", http.StatusInternalServerError)
			}
			return
		}
		// Table partials (e.g. servers_table) live in the shared set.
		if err := h.partials.ExecuteTemplate(w, name, data); err != nil {
			h.logger.Error("render partial failed", slog.String("template", name), slog.String("error", err.Error()))
			http.Error(w, "render failed", http.StatusInternalServerError)
		}
		return
	}

	// Full page — look up the per-page template set.
	pageTmpl, ok := h.pages["templates/"+name]
	if !ok {
		h.logger.Error("template not found", slog.String("template", name))
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	data["Title"] = data["PageTitle"]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := pageTmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		h.logger.Error("render page failed", slog.String("template", name), slog.String("error", err.Error()))
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func (h *AdminHandler) renderError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	data := map[string]any{
		"PageTitle":  http.StatusText(code),
		"Title":      http.StatusText(code),
		"StatusCode": code,
		"StatusText": http.StatusText(code),
		"Message":    msg,
		"DevMode":    h.devMode,
	}

	session, _ := SessionFromContext(r.Context())
	if session != nil {
		data["Email"] = session.Email
		data["CSRFToken"] = session.CSRFToken
	}

	pageTmpl, ok := h.pages["templates/error.html"]
	if !ok {
		http.Error(w, msg, code)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)

	if err := pageTmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		h.logger.Error("render error page failed", slog.String("error", err.Error()))
	}
}

// HandleDashboard renders the dashboard summary page.
func (h *AdminHandler) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	serverCount := 0
	if h.serverStore != nil {
		status := types.StatusActive
		servers, err := h.serverStore.List(ctx, types.ServerFilter{Status: &status})
		if err == nil {
			serverCount = len(servers)
		}
	}

	toolCount := 0
	if h.classificationStore != nil {
		tools, err := h.classificationStore.GetAll(ctx)
		if err == nil {
			toolCount = len(tools)
		}
	}

	userCount := 0
	if h.userStore != nil {
		_, count, err := h.userStore.Search(ctx, types.UserFilter{Limit: 1})
		if err == nil {
			userCount = count
		}
	}

	auditCount := 0
	if h.auditStore != nil {
		since := time.Now().Add(-24 * time.Hour)
		entries, err := h.auditStore.Query(ctx, types.AuditFilter{Since: &since})
		if err == nil {
			auditCount = len(entries)
		}
	}

	h.render(w, r, "dashboard.html", map[string]any{
		"PageTitle":   "Dashboard",
		"ServerCount": serverCount,
		"ToolCount":   toolCount,
		"UserCount":   userCount,
		"AuditCount":  auditCount,
	})
}

const defaultPageSize = 50

func parsePage(r *http.Request) int {
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func pagination(total, pageSize int) (int, []int) {
	totalPages := (total + pageSize - 1) / pageSize
	pages := make([]int, 0, totalPages)
	for i := 1; i <= totalPages; i++ {
		pages = append(pages, i)
	}
	return totalPages, pages
}

func (h *AdminHandler) renderListPage(w http.ResponseWriter, r *http.Request, total, page int, data map[string]any, partial, fullPage string) {
	totalPages, pageNumbers := pagination(total, defaultPageSize)
	data["TotalPages"] = totalPages
	data["PageNumbers"] = pageNumbers
	data["CurrentPage"] = page
	if r.Header.Get(headerHXRequest) == "true" {
		h.render(w, r, partial, data)
		return
	}
	h.render(w, r, fullPage, data)
}

// HandleTools renders the tool classifications page.
func (h *AdminHandler) HandleTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	riskFilter := strings.TrimSpace(r.URL.Query().Get("risk"))
	page := parsePage(r)

	filter := types.ToolFilter{
		Query:  query,
		Limit:  defaultPageSize,
		Offset: (page - 1) * defaultPageSize,
	}
	if rl := types.RiskLevel(riskFilter); rl.IsValid() {
		filter.RiskLevel = rl
	}

	var tools []types.ToolClassification
	total := 0
	if h.classificationStore != nil {
		result, count, err := h.classificationStore.Search(ctx, filter)
		if err != nil {
			h.logger.ErrorContext(ctx, "search tool classifications failed", slog.String("error", err.Error()))
		} else {
			tools = result
			total = count
		}
	}

	h.renderListPage(w, r, total, page, map[string]any{
		"PageTitle":  "Tools",
		"Tools":      tools,
		"Query":      query,
		"RiskFilter": riskFilter,
	}, "tools_table", "tools.html")
}

// HandleToolEdit renders the tool edit form.
func (h *AdminHandler) HandleToolEdit(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if strings.TrimSpace(name) == "" {
		h.renderError(w, r, http.StatusBadRequest, "A tool name is required to load the editor.")
		return
	}

	var tool *types.ToolClassification
	if h.classificationStore != nil {
		result, err := h.classificationStore.Get(r.Context(), name)
		if err == nil {
			tool = result
		}
	}
	if tool == nil {
		tool = &types.ToolClassification{
			ToolName:  name,
			RiskLevel: types.RiskUnknown,
		}
	}

	h.render(w, r, "tool_edit.html", map[string]any{
		"PageTitle": "Edit Tool",
		"Tool":      tool,
	})
}

// HandleToolUpdate processes tool classification updates.
func (h *AdminHandler) HandleToolUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if strings.TrimSpace(name) == "" {
		http.Error(w, "tool name required", http.StatusBadRequest)
		return
	}

	riskLevel := types.RiskLevel(strings.TrimSpace(r.FormValue("risk_level")))
	if !riskLevel.IsValid() {
		http.Error(w, "invalid risk level", http.StatusBadRequest)
		return
	}

	reason := strings.TrimSpace(r.FormValue("reason"))
	session, _ := SessionFromContext(r.Context())
	updatedBy := "admin"
	if session != nil {
		updatedBy = session.Email
	}

	if err := h.upsertToolClassification(r.Context(), name, riskLevel, reason, updatedBy, "admin_dashboard"); err != nil {
		h.logger.Error("upsert tool classification failed", slog.String("error", err.Error()))
		http.Error(w, "failed to save classification", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/tools", http.StatusFound)
}

// HandleRateLimits renders the rate limit inspection page.
func (h *AdminHandler) HandleRateLimits(w http.ResponseWriter, r *http.Request) {
	inspector, ok := h.rateLimitBackend.(ratelimit.LimiterInspector)

	data := map[string]any{
		"PageTitle":           "Rate Limits",
		"InspectionAvailable": ok,
		"Entries":             []ratelimit.RateLimitEntry{},
	}

	if ok {
		data["Entries"] = inspector.Snapshot()
	}

	if r.Header.Get(headerHXRequest) == "true" {
		h.render(w, r, "ratelimits_table", data)
		return
	}

	h.render(w, r, "ratelimits.html", data)
}

// AuditEntryView is a template-friendly wrapper for audit entries.
type AuditEntryView struct {
	types.AuditEntry
	DetailsJSON   string
	DetailsPretty string
}

// HandleAudit renders the audit log page.
func (h *AdminHandler) HandleAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := buildAuditFilter(r)
	page := parsePage(r)
	filter.Limit = defaultPageSize
	filter.Offset = (page - 1) * defaultPageSize

	var entries []types.AuditEntry
	total := 0
	if h.auditStore != nil {
		result, err := h.auditStore.Query(ctx, filter)
		if err == nil {
			entries = result
		}
		count, err := h.auditStore.Count(ctx, filter)
		if err == nil {
			total = count
		}
	}

	views := make([]AuditEntryView, len(entries))
	for i, e := range entries {
		compactJSON, _ := json.Marshal(e.Details)
		prettyJSON, _ := json.MarshalIndent(e.Details, "", "  ")
		views[i] = AuditEntryView{
			AuditEntry:    e,
			DetailsJSON:   string(compactJSON),
			DetailsPretty: string(prettyJSON),
		}
	}

	h.renderListPage(w, r, total, page, map[string]any{
		"PageTitle":     "Audit Log",
		"Entries":       views,
		"ClientID":      filter.ClientID,
		"Username":      filter.Username,
		"ToolName":      filter.ServerName,
		"Decision":      filter.EventType,
		"DetailsSearch": filter.DetailsSearch,
		"SortBy":        filter.SortBy,
		"SortDir":       filter.SortDir,
		"Since":         r.URL.Query().Get("since"),
		"Until":         r.URL.Query().Get("until"),
	}, "audit_table", "audit.html")
}

// HandleAuditExport exports audit entries as CSV.
func (h *AdminHandler) HandleAuditExport(w http.ResponseWriter, r *http.Request) {
	filter := buildAuditFilter(r)
	filter.Limit = 10000
	filter.Offset = 0

	var entries []types.AuditEntry
	if h.auditStore != nil {
		result, err := h.auditStore.Query(r.Context(), filter)
		if err == nil {
			entries = result
		}
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_export.csv")
	writeAuditCSV(w, entries)
}

// HandleServers renders the servers page.
func (h *AdminHandler) HandleServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var servers []types.ServerRecord
	if h.serverStore != nil {
		result, err := h.serverStore.List(ctx, types.ServerFilter{})
		if err == nil {
			servers = result
		}
	}

	data := map[string]any{
		"PageTitle": "Servers",
		"Servers":   servers,
	}

	if r.Header.Get(headerHXRequest) == "true" {
		h.render(w, r, "servers_table", data)
		return
	}

	h.render(w, r, "servers.html", data)
}

// HandleServerRateLimitEdit renders the server rate limit edit form.
func (h *AdminHandler) HandleServerRateLimitEdit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		h.renderError(w, r, http.StatusBadRequest, "A server ID is required to load rate limit settings.")
		return
	}

	var server *types.ServerRecord
	if h.serverStore != nil {
		result, err := h.serverStore.Get(r.Context(), id)
		if err != nil {
			h.renderError(w, r, http.StatusNotFound, "The requested server could not be found.")
			return
		}
		server = result
	}
	if server == nil {
		h.renderError(w, r, http.StatusNotFound, "The requested server could not be found.")
		return
	}

	h.render(w, r, "server_ratelimit.html", map[string]any{
		"PageTitle": "Rate Limit: " + server.Name,
		"Server":    server,
	})
}

// HandleServerRateLimitUpdate processes server rate limit updates.
func (h *AdminHandler) HandleServerRateLimitUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "server ID required", http.StatusBadRequest)
		return
	}

	rateLimit, rateBurst, err := parseRateLimitFormValues(r.FormValue("rate_limit"), r.FormValue("rate_burst"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	session, _ := SessionFromContext(r.Context())
	updatedBy := "admin"
	if session != nil {
		updatedBy = session.Email
	}

	if err := h.updateServerRateLimit(r.Context(), id, rateLimit, rateBurst, updatedBy, "admin_dashboard"); err != nil {
		h.logger.Error("update server rate limit failed", slog.String("error", err.Error()))
		http.Error(w, "failed to update rate limit", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/servers", http.StatusFound)
}

// HandleServerDeregister removes a server.
func (h *AdminHandler) HandleServerDeregister(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "server ID required", http.StatusBadRequest)
		return
	}

	session, _ := SessionFromContext(r.Context())
	deregisteredBy := "admin"
	if session != nil {
		deregisteredBy = session.Email
	}

	if err := h.deregisterServer(r.Context(), id, deregisteredBy, "admin_dashboard"); err != nil {
		h.logger.Error("deregister server failed", slog.String("error", err.Error()))
		http.Error(w, "failed to deregister server", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/servers", http.StatusFound)
}

// HandlePruneOrphanedTools deletes tool classifications not exposed by any active server.
func (h *AdminHandler) HandlePruneOrphanedTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	session, _ := SessionFromContext(ctx)
	prunedBy := "admin"
	if session != nil {
		prunedBy = session.Email
	}

	if h.serverStore == nil || h.classificationStore == nil {
		http.Redirect(w, r, "/admin/servers", http.StatusFound)
		return
	}
	if _, err := h.pruneOrphanedToolClassifications(ctx, prunedBy, "admin_dashboard"); err != nil {
		h.logger.Error("prune orphaned tools failed", slog.String("error", err.Error()))
		http.Error(w, "failed to prune orphaned tools", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/servers", http.StatusFound)
}

func buildAuditFilter(r *http.Request) types.AuditFilter {
	filter := types.AuditFilter{
		ClientID:      strings.TrimSpace(r.URL.Query().Get("client_id")),
		Username:      strings.TrimSpace(r.URL.Query().Get("username")),
		ServerName:    strings.TrimSpace(r.URL.Query().Get("tool_name")),
		EventType:     strings.TrimSpace(r.URL.Query().Get("decision")),
		DetailsSearch: strings.TrimSpace(r.URL.Query().Get("details")),
		SortBy:        types.NormalizeAuditSortBy(r.URL.Query().Get("sort_by")),
		SortDir:       types.NormalizeAuditSortDir(r.URL.Query().Get("sort_dir")),
	}

	if since := strings.TrimSpace(r.URL.Query().Get("since")); since != "" {
		if t, hasDateOnly, err := parseAuditTimeValue(since); err == nil {
			if hasDateOnly {
				t = t.UTC()
			}
			filter.Since = &t
		}
	}
	if until := strings.TrimSpace(r.URL.Query().Get("until")); until != "" {
		if t, hasDateOnly, err := parseAuditTimeValue(until); err == nil {
			if hasDateOnly {
				end := t.Add(24*time.Hour - time.Second)
				filter.Until = &end
			} else {
				filter.Until = &t
			}
		}
	}

	return filter
}

func parseAuditTimeValue(raw string) (time.Time, bool, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, true, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), false, nil
	}
	return time.Time{}, false, fmt.Errorf("invalid audit timestamp")
}

func csvEscape(s string) string {
	// Prevent CSV formula injection: prefix dangerous leading characters with a single quote.
	if len(s) > 0 {
		switch s[0] {
		case '=', '+', '-', '@', '\t', '\r':
			s = "'" + s
		}
	}
	if strings.ContainsAny(s, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func writeAuditCSV(w http.ResponseWriter, entries []types.AuditEntry) {
	_, _ = w.Write([]byte("time,event_type,client_id,username,server_name,details\n"))
	for _, e := range entries {
		detailsJSON, _ := json.Marshal(e.Details)
		line := e.CreatedAt.Format(time.RFC3339) + "," +
			csvEscape(e.EventType) + "," +
			csvEscape(e.ClientID) + "," +
			csvEscape(e.Username) + "," +
			csvEscape(e.ServerName) + "," +
			csvEscape(string(detailsJSON)) + "\n"
		_, _ = w.Write([]byte(line))
	}
}

func (h *AdminHandler) upsertToolClassification(ctx context.Context, name string, riskLevel types.RiskLevel, reason, updatedBy, source string) error {
	classification := &types.ToolClassification{
		ToolName:  name,
		RiskLevel: riskLevel,
		Reason:    reason,
		UpdatedBy: updatedBy,
		UpdatedAt: time.Now().UTC(),
	}

	if h.classificationStore != nil {
		if err := h.classificationStore.Upsert(ctx, classification); err != nil {
			return err
		}
	}

	if h.broadcaster != nil {
		evt := registration.NewClassificationEvent(name, string(riskLevel), updatedBy)
		data, err := json.Marshal(evt)
		if err == nil {
			_ = h.broadcaster.Publish(registration.SubjectClassificationUpdated, data)
		}
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(ctx, &types.AuditEntry{
			EventType:  "tool_classification_updated",
			ClientID:   updatedBy,
			ServerName: name,
			Details: map[string]any{
				"risk_level": string(riskLevel),
				"reason":     reason,
				"source":     source,
			},
		})
	}
	return nil
}

func parseRateLimitFormValues(rateLimitRaw, rateBurstRaw string) (*int, *int, error) {
	var rateLimit *int
	var rateBurst *int

	if v := strings.TrimSpace(rateLimitRaw); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, nil, fmt.Errorf("invalid rate limit value")
		}
		rateLimit = &n
	}

	if v := strings.TrimSpace(rateBurstRaw); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, nil, fmt.Errorf("invalid rate burst value")
		}
		rateBurst = &n
	}

	if rateLimit == nil && rateBurst != nil {
		return nil, nil, fmt.Errorf("rate burst requires rate limit")
	}
	return rateLimit, rateBurst, nil
}

func (h *AdminHandler) updateServerRateLimit(ctx context.Context, id string, rateLimit, rateBurst *int, updatedBy, source string) error {
	if h.serverStore != nil {
		if err := h.serverStore.UpdateRateLimit(ctx, id, rateLimit, rateBurst); err != nil {
			return err
		}
	}

	if h.auditStore != nil {
		details := map[string]any{
			"source": source,
		}
		if rateLimit != nil {
			details["rate_limit"] = *rateLimit
		}
		if rateBurst != nil {
			details["rate_burst"] = *rateBurst
		}
		_ = h.auditStore.Log(ctx, &types.AuditEntry{
			EventType:  "server_rate_limit_updated",
			ClientID:   updatedBy,
			ServerName: id,
			Details:    details,
		})
	}

	if h.broadcaster != nil {
		evt := registration.NewRegistrationEvent("server_rate_limit_updated", id)
		data, err := json.Marshal(evt)
		if err == nil {
			_ = h.broadcaster.Publish(registration.SubjectRegistryUpdated, data)
		}
	}
	return nil
}

func (h *AdminHandler) deregisterServer(ctx context.Context, id, deregisteredBy, source string) error {
	if h.serverStore != nil {
		if err := h.serverStore.Delete(ctx, id); err != nil {
			return err
		}
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(ctx, &types.AuditEntry{
			EventType:  "server_deregistered",
			ClientID:   deregisteredBy,
			ServerName: id,
			Details: map[string]any{
				"source": source,
			},
		})
	}

	if h.broadcaster != nil {
		evt := registration.NewRegistrationEvent("deregistered", id)
		data, err := json.Marshal(evt)
		if err == nil {
			_ = h.broadcaster.Publish(registration.SubjectRegistryUpdated, data)
		}
	}
	return nil
}

func (h *AdminHandler) pruneOrphanedToolClassifications(ctx context.Context, prunedBy, source string) (int64, error) {
	status := types.StatusActive
	servers, err := h.serverStore.List(ctx, types.ServerFilter{Status: &status})
	if err != nil {
		return 0, err
	}

	seen := make(map[string]struct{})
	for _, srv := range servers {
		for _, tool := range srv.Capabilities.Tools {
			name := strings.TrimSpace(tool)
			if name == "" {
				continue
			}
			seen[name] = struct{}{}
		}
	}

	activeToolNames := make([]string, 0, len(seen))
	for name := range seen {
		activeToolNames = append(activeToolNames, name)
	}

	pruned, err := h.classificationStore.DeleteNotIn(ctx, activeToolNames)
	if err != nil {
		return 0, err
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(ctx, &types.AuditEntry{
			EventType:  "tools_pruned",
			ClientID:   prunedBy,
			ServerName: "",
			Details: map[string]any{
				"source":            source,
				"pruned_count":      pruned,
				"active_tool_count": len(activeToolNames),
			},
		})
	}

	if h.broadcaster != nil {
		evt := registration.NewClassificationEvent("*", "pruned", prunedBy)
		data, err := json.Marshal(evt)
		if err == nil {
			_ = h.broadcaster.Publish(registration.SubjectClassificationUpdated, data)
		}
	}

	return pruned, nil
}
