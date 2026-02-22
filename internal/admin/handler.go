package admin

import (
	"context"
	"encoding/json"
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
	broadcaster          registration.Broadcaster
	rateLimitBackend     ratelimit.LimiterBackend
	pages                map[string]*template.Template
	partials             *template.Template
	discoveryIndex       *proxy.DiscoveryIndex
	progressiveDiscovery bool
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
		"templates/audit.html",
		"templates/ratelimits.html",
		"templates/tools.html",
		"templates/tool_edit.html",
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
		broadcaster:          deps.Broadcaster,
		rateLimitBackend:     deps.RateLimitBackend,
		pages:                pages,
		partials:             shared,
		discoveryIndex:       deps.DiscoveryIndex,
		progressiveDiscovery: deps.ProgressiveDiscovery,
	}
}

func (h *AdminHandler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	session, _ := SessionFromContext(r.Context())
	if session != nil {
		data["Email"] = session.Email
		data["CSRFToken"] = session.CSRFToken
	}

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
		"AuditCount":  auditCount,
	})
}

// HandleTools renders the tool classifications page.
func (h *AdminHandler) HandleTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	riskFilter := strings.TrimSpace(r.URL.Query().Get("risk"))

	tools := h.fetchTools(ctx, riskFilter)
	tools = filterToolsByQuery(tools, query)

	data := map[string]any{
		"PageTitle": "Tools",
		"Tools":     tools,
	}

	if r.Header.Get(headerHXRequest) == "true" {
		h.render(w, r, "tools_table", data)
		return
	}

	h.render(w, r, "tools.html", data)
}

func (h *AdminHandler) fetchTools(ctx context.Context, riskFilter string) []types.ToolClassification {
	if h.classificationStore == nil {
		return nil
	}
	if riskFilter != "" {
		return h.fetchToolsByRisk(ctx, riskFilter)
	}
	result, err := h.classificationStore.GetAll(ctx)
	if err != nil {
		return nil
	}
	return result
}

func (h *AdminHandler) fetchToolsByRisk(ctx context.Context, riskFilter string) []types.ToolClassification {
	level := types.RiskLevel(riskFilter)
	if !level.IsValid() {
		return nil
	}
	result, err := h.classificationStore.GetByRiskLevel(ctx, level)
	if err != nil {
		return nil
	}
	return result
}

func filterToolsByQuery(tools []types.ToolClassification, query string) []types.ToolClassification {
	if query == "" || len(tools) == 0 {
		return tools
	}
	lowerQuery := strings.ToLower(query)
	var filtered []types.ToolClassification
	for _, t := range tools {
		if strings.Contains(strings.ToLower(t.ToolName), lowerQuery) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// HandleToolEdit renders the tool edit form.
func (h *AdminHandler) HandleToolEdit(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if strings.TrimSpace(name) == "" {
		http.Error(w, "tool name required", http.StatusBadRequest)
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

	classification := &types.ToolClassification{
		ToolName:  name,
		RiskLevel: riskLevel,
		Reason:    reason,
		UpdatedBy: updatedBy,
		UpdatedAt: time.Now().UTC(),
	}

	if h.classificationStore != nil {
		if err := h.classificationStore.Upsert(r.Context(), classification); err != nil {
			h.logger.Error("upsert tool classification failed", slog.String("error", err.Error()))
			http.Error(w, "failed to save classification", http.StatusInternalServerError)
			return
		}
	}

	// Broadcast classification change.
	if h.broadcaster != nil {
		evt := registration.NewClassificationEvent(name, string(riskLevel), updatedBy)
		data, err := json.Marshal(evt)
		if err == nil {
			_ = h.broadcaster.Publish(registration.SubjectClassificationUpdated, data)
		}
	}

	// Audit log.
	if h.auditStore != nil {
		_ = h.auditStore.Log(r.Context(), &types.AuditEntry{
			EventType:  "tool_classification_updated",
			ClientID:   updatedBy,
			ServerName: name,
			Details: map[string]any{
				"risk_level": string(riskLevel),
				"reason":     reason,
			},
		})
	}

	http.Redirect(w, r, "/admin/tools", http.StatusFound)
}

// HandleRateLimits renders the rate limit inspection page.
func (h *AdminHandler) HandleRateLimits(w http.ResponseWriter, r *http.Request) {
	inspector, ok := h.rateLimitBackend.(ratelimit.LimiterInspector)

	data := map[string]any{
		"PageTitle":            "Rate Limits",
		"InspectionAvailable":  ok,
		"Entries":              []ratelimit.RateLimitEntry{},
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
	DetailsJSON string
}

// HandleAudit renders the audit log page.
func (h *AdminHandler) HandleAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := buildAuditFilter(r)

	pageSize := 50
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize

	var entries []types.AuditEntry
	total := 0
	if h.auditStore != nil {
		result, err := h.auditStore.Query(ctx, filter)
		if err == nil {
			entries = result
		}
		// Get total count for pagination.
		countFilter := filter
		countFilter.Limit = 0
		countFilter.Offset = 0
		countResult, err := h.auditStore.Query(ctx, countFilter)
		if err == nil {
			total = len(countResult)
		}
	}

	views := make([]AuditEntryView, len(entries))
	for i, e := range entries {
		detailsJSON, _ := json.Marshal(e.Details)
		views[i] = AuditEntryView{
			AuditEntry:  e,
			DetailsJSON: string(detailsJSON),
		}
	}

	totalPages := (total + pageSize - 1) / pageSize
	var pageNumbers []int
	for i := 1; i <= totalPages; i++ {
		pageNumbers = append(pageNumbers, i)
	}

	data := map[string]any{
		"PageTitle":   "Audit Log",
		"Entries":     views,
		"TotalPages":  totalPages,
		"PageNumbers": pageNumbers,
		"CurrentPage": page,
	}

	if r.Header.Get(headerHXRequest) == "true" {
		h.render(w, r, "audit_table", data)
		return
	}

	h.render(w, r, "audit.html", data)
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

	_, _ = w.Write([]byte("time,event_type,client_id,server_name,details\n"))
	for _, e := range entries {
		detailsJSON, _ := json.Marshal(e.Details)
		line := e.CreatedAt.Format(time.RFC3339) + "," +
			csvEscape(e.EventType) + "," +
			csvEscape(e.ClientID) + "," +
			csvEscape(e.ServerName) + "," +
			csvEscape(string(detailsJSON)) + "\n"
		_, _ = w.Write([]byte(line))
	}
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

	if h.serverStore != nil {
		if err := h.serverStore.Delete(r.Context(), id); err != nil {
			h.logger.Error("deregister server failed", slog.String("error", err.Error()))
			http.Error(w, "failed to deregister server", http.StatusInternalServerError)
			return
		}
	}

	// Audit log.
	if h.auditStore != nil {
		_ = h.auditStore.Log(r.Context(), &types.AuditEntry{
			EventType:  "server_deregistered",
			ClientID:   deregisteredBy,
			ServerName: id,
			Details: map[string]any{
				"source": "admin_dashboard",
			},
		})
	}

	// Broadcast deregistration.
	if h.broadcaster != nil {
		evt := registration.NewRegistrationEvent("deregistered", id)
		data, err := json.Marshal(evt)
		if err == nil {
			_ = h.broadcaster.Publish(registration.SubjectRegistryUpdated, data)
		}
	}

	http.Redirect(w, r, "/admin/servers", http.StatusFound)
}

func buildAuditFilter(r *http.Request) types.AuditFilter {
	filter := types.AuditFilter{
		ClientID:   strings.TrimSpace(r.URL.Query().Get("client_id")),
		ServerName: strings.TrimSpace(r.URL.Query().Get("tool_name")),
		EventType:  strings.TrimSpace(r.URL.Query().Get("decision")),
	}

	if since := r.URL.Query().Get("since"); since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			filter.Since = &t
		}
	}
	if until := r.URL.Query().Get("until"); until != "" {
		if t, err := time.Parse("2006-01-02", until); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			filter.Until = &end
		}
	}

	return filter
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
