package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// Integrated admin API contract (/admin/api/v1/*):
//
// Required delegated headers:
//   - Authorization: Bearer <platform service token>
//   - X-Cruvero-Subject
//   - X-Cruvero-Email
//   - X-Cruvero-Tenant-ID
//   - X-Cruvero-Role
//   - X-Cruvero-Gateway-Role (viewer|editor|admin)
//
// RBAC matrix:
//   - viewer: all read/list endpoints, search test, and audit export/query
//   - editor: viewer + tool update/classification + server rate limits + search tuning update
//   - admin: editor + server deregister/prune + user permissions update
//
// Service token validation method:
//   - exact constant-time compare against MCPGW_PLATFORM_SERVICE_TOKEN.
//   - no DB lookup and no JWT verification.

const integratedManagedMessage = "Admin UI is managed by Cruvero Platform"

func newIntegratedRouter(handler *AdminHandler, deps AdminDeps) chi.Router {
	r := chi.NewRouter()
	r.Use(SecurityHeadersMiddleware)

	blocked := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, integratedManagedMessage, http.StatusForbidden)
	}

	// Disable standalone admin UI/auth routes in integrated mode.
	r.HandleFunc("/", blocked)
	r.HandleFunc("/login", blocked)
	r.HandleFunc("/callback", blocked)
	r.HandleFunc("/static/*", blocked)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(IntegratedDelegatedAuthMiddleware(deps.PlatformServiceToken))
		api.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeIntegratedError(w, http.StatusNotFound, errCodeNotFound, "admin API endpoint not found")
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
			writeIntegratedError(w, http.StatusMethodNotAllowed, errCodeMethodNotAllowed, "method not allowed")
		})

		// @route GET /admin/api/v1/servers
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"items":[ServerRecord], "total":int, "next_cursor":string}
		// @example {"items":[{"id":"srv-1","name":"server-1"}],"total":1,"next_cursor":""}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/servers", handler.HandleIntegratedServers)

		// @route GET /admin/api/v1/servers/{id}
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"item":ServerRecord}
		// @example {"item":{"id":"srv-1","name":"server-1"}}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/servers/{id}", handler.HandleIntegratedServerDetail)

		// @route POST /admin/api/v1/servers/{id}/deregister
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role admin
		// @response 200 {"status":"ok"}
		// @example {"status":"ok"}
		api.With(RequireGatewayRole(gatewayRoleAdmin)).Post("/servers/{id}/deregister", handler.HandleIntegratedServerDeregister)

		// @route PUT /admin/api/v1/servers/{id}/rate-limits
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role editor
		// @response 200 {"status":"ok"}
		// @example {"status":"ok"}
		api.With(RequireGatewayRole(gatewayRoleEditor)).Put("/servers/{id}/rate-limits", handler.HandleIntegratedServerRateLimits)

		// @route POST /admin/api/v1/servers/prune
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role admin
		// @response 200 {"status":"ok","pruned":int}
		// @example {"status":"ok","pruned":4}
		api.With(RequireGatewayRole(gatewayRoleAdmin)).Post("/servers/prune", handler.HandleIntegratedServerPrune)

		// @route GET /admin/api/v1/tools
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"items":[ToolClassification], "total":int, "next_cursor":string}
		// @example {"items":[{"tool_name":"mcp.github.search"}],"total":1,"next_cursor":""}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/tools", handler.HandleIntegratedTools)

		// @route GET /admin/api/v1/tools/{id}
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"item":ToolClassification}
		// @example {"item":{"tool_name":"mcp.github.search","risk_level":"read_only"}}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/tools/{id}", handler.HandleIntegratedToolDetail)

		// @route PUT /admin/api/v1/tools/{id}
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role editor
		// @response 200 {"status":"ok"}
		// @example {"status":"ok"}
		api.With(RequireGatewayRole(gatewayRoleEditor)).Put("/tools/{id}", handler.HandleIntegratedToolUpdate)

		// @route PUT /admin/api/v1/tools/{id}/classification
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role editor
		// @response 200 {"status":"ok"}
		// @example {"status":"ok"}
		api.With(RequireGatewayRole(gatewayRoleEditor)).Put("/tools/{id}/classification", handler.HandleIntegratedToolClassification)

		// @route GET /admin/api/v1/tools/{id}/schema
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"item":ToolDefinition}
		// @example {"item":{"name":"mcp.github.search","inputSchema":{}}}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/tools/{id}/schema", handler.HandleIntegratedToolSchema)

		// @route GET /admin/api/v1/tools/discovery-stats
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"total_tools":int,"categories":map[string]int}
		// @example {"total_tools":42,"categories":{"github":12}}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/tools/discovery-stats", handler.HandleIntegratedToolDiscoveryStats)

		// @route GET /admin/api/v1/users
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"items":[User], "total":int, "next_cursor":string}
		// @example {"items":[{"id":"user-1","email":"user@example.com"}],"total":1}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/users", handler.HandleIntegratedUsers)

		// @route PUT /admin/api/v1/users/{id}/permissions
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role admin
		// @response 200 {"status":"ok"}
		// @example {"status":"ok"}
		api.With(RequireGatewayRole(gatewayRoleAdmin)).Put("/users/{id}/permissions", handler.HandleIntegratedUserPermissions)

		// @route GET /admin/api/v1/audit
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"items":[AuditEntry], "total":int, "next_cursor":string}
		// @example {"items":[{"event_type":"tool_call"}],"total":1}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/audit", handler.HandleIntegratedAuditQuery)

		// @route GET /admin/api/v1/audit/export
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role, Accept
		// @role viewer
		// @response 200 JSON {"items":[AuditEntry]} or CSV attachment
		// @example {"items":[{"event_type":"tool_call"}]}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/audit/export", handler.HandleIntegratedAuditExport)

		// @route GET /admin/api/v1/search-tuning
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"engine_type":string,"engine_ready":bool,"indexed_tools":int,"synonyms":[]}
		// @example {"engine_type":"substring","engine_ready":true,"indexed_tools":12,"synonyms":[]}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Get("/search-tuning", handler.HandleIntegratedSearchTuningGet)

		// @route PUT /admin/api/v1/search-tuning
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role editor
		// @response 200 {"status":"ok","updated":int}
		// @example {"status":"ok","updated":1}
		api.With(RequireGatewayRole(gatewayRoleEditor)).Put("/search-tuning", handler.HandleIntegratedSearchTuningUpdate)

		// @route POST /admin/api/v1/search-tuning/test
		// @headers Authorization Bearer, X-Cruvero-Subject, X-Cruvero-Email, X-Cruvero-Tenant-ID, X-Cruvero-Role, X-Cruvero-Gateway-Role
		// @role viewer
		// @response 200 {"query":string,"total":int,"results":[],"tested_at":string}
		// @example {"query":"github issue","total":2,"results":[],"tested_at":"2026-02-26T12:00:00Z"}
		api.With(RequireGatewayRole(gatewayRoleViewer)).Post("/search-tuning/test", handler.HandleIntegratedSearchTuningTest)
	})

	r.NotFound(blocked)
	return r
}

func delegatedActor(r *http.Request) string {
	identity, ok := integratedIdentityFromContext(r.Context())
	if !ok {
		return "platform"
	}
	if strings.TrimSpace(identity.Email) != "" {
		return strings.TrimSpace(identity.Email)
	}
	if strings.TrimSpace(identity.Subject) != "" {
		return strings.TrimSpace(identity.Subject)
	}
	return "platform"
}

func parseLimitOffset(values url.Values, defaultLimit int) (limit int, offset int, err error) {
	limit = defaultLimit
	offset = 0

	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			return 0, 0, fmt.Errorf("invalid limit")
		}
		if parsed > 1000 {
			parsed = 1000
		}
		limit = parsed
	}

	cursor := strings.TrimSpace(values.Get("cursor"))
	if cursor != "" {
		parsed, parseErr := strconv.Atoi(cursor)
		if parseErr != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("invalid cursor")
		}
		offset = parsed
	}

	return limit, offset, nil
}

func nextCursor(offset, limit, total int) string {
	if limit <= 0 || total <= 0 {
		return ""
	}
	next := offset + limit
	if next >= total {
		return ""
	}
	return strconv.Itoa(next)
}

// HandleIntegratedServers returns paginated server records for delegated admin callers.
func (h *AdminHandler) HandleIntegratedServers(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query(), defaultPageSize)
	if err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	var statusFilter *types.ServerStatus
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		status := types.ServerStatus(raw)
		statusFilter = &status
	}

	results := make([]types.ServerRecord, 0)
	if h.serverStore != nil {
		servers, listErr := h.serverStore.List(r.Context(), types.ServerFilter{Status: statusFilter})
		if listErr != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to list servers")
			return
		}

		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		for _, srv := range servers {
			if query != "" {
				name := strings.ToLower(strings.TrimSpace(srv.Name))
				id := strings.ToLower(strings.TrimSpace(srv.ID))
				if !strings.Contains(name, query) && !strings.Contains(id, query) {
					continue
				}
			}
			results = append(results, srv)
		}
	}

	total := len(results)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	items := results[offset:end]

	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"total":       total,
		"next_cursor": nextCursor(offset, limit, total),
	})
}

// HandleIntegratedServerDetail returns one server record by ID.
func (h *AdminHandler) HandleIntegratedServerDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "server id is required")
		return
	}
	if h.serverStore == nil {
		writeIntegratedError(w, http.StatusNotFound, errCodeNotFound, "server not found")
		return
	}

	record, err := h.serverStore.Get(r.Context(), id)
	if err != nil || record == nil {
		writeIntegratedError(w, http.StatusNotFound, errCodeNotFound, "server not found")
		return
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"item": record})
}

// HandleIntegratedServerDeregister marks a server as deregistered.
func (h *AdminHandler) HandleIntegratedServerDeregister(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "server id is required")
		return
	}
	if err := h.deregisterServer(r.Context(), id, delegatedActor(r), "integrated_api"); err != nil {
		writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to deregister server")
		return
	}
	writeIntegratedJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleIntegratedServerRateLimits updates per-server rate limit settings.
func (h *AdminHandler) HandleIntegratedServerRateLimits(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "server id is required")
		return
	}

	var body struct {
		RateLimit *int `json:"rate_limit"`
		RateBurst *int `json:"rate_burst"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid JSON body")
		return
	}
	if body.RateLimit != nil && *body.RateLimit < 0 {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid rate limit value")
		return
	}
	if body.RateBurst != nil && *body.RateBurst < 0 {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid rate burst value")
		return
	}
	if body.RateLimit == nil && body.RateBurst != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "rate burst requires rate limit")
		return
	}

	if err := h.updateServerRateLimit(r.Context(), id, body.RateLimit, body.RateBurst, delegatedActor(r), "integrated_api"); err != nil {
		writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to update server rate limits")
		return
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleIntegratedServerPrune removes orphaned tool-classification records.
func (h *AdminHandler) HandleIntegratedServerPrune(w http.ResponseWriter, r *http.Request) {
	pruned, err := h.pruneOrphanedToolClassifications(r.Context(), delegatedActor(r), "integrated_api")
	if err != nil {
		writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to prune orphaned tools")
		return
	}
	writeIntegratedJSON(w, http.StatusOK, map[string]any{"status": "ok", "pruned": pruned})
}

// HandleIntegratedTools returns paginated tool classifications.
func (h *AdminHandler) HandleIntegratedTools(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query(), defaultPageSize)
	if err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	filter := types.ToolFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  limit,
		Offset: offset,
	}

	risk := strings.TrimSpace(r.URL.Query().Get("classification"))
	if risk == "" {
		risk = strings.TrimSpace(r.URL.Query().Get("risk"))
	}
	if rl := types.RiskLevel(risk); rl.IsValid() {
		filter.RiskLevel = rl
	}

	items := make([]types.ToolClassification, 0)
	total := 0
	if h.classificationStore != nil {
		result, count, searchErr := h.classificationStore.Search(r.Context(), filter)
		if searchErr != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to list tools")
			return
		}
		items = result
		total = count
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"total":       total,
		"next_cursor": nextCursor(offset, limit, total),
	})
}

// HandleIntegratedToolDetail returns tool classification details by tool ID.
func (h *AdminHandler) HandleIntegratedToolDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "tool id is required")
		return
	}

	item := &types.ToolClassification{ToolName: id, RiskLevel: types.RiskUnknown}
	if h.classificationStore != nil {
		tool, err := h.classificationStore.Get(r.Context(), id)
		if err == nil && tool != nil {
			item = tool
		}
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"item": item})
}

type integratedToolUpdateBody struct {
	RiskLevel      string `json:"risk_level"`
	Classification string `json:"classification"`
	Risk           string `json:"risk"`
	Reason         string `json:"reason"`
}

// HandleIntegratedToolClassification updates a tool classification entry.
func (h *AdminHandler) HandleIntegratedToolClassification(w http.ResponseWriter, r *http.Request) {
	h.handleIntegratedToolMutation(w, r)
}

// HandleIntegratedToolUpdate updates tool metadata routed through the classification store.
func (h *AdminHandler) HandleIntegratedToolUpdate(w http.ResponseWriter, r *http.Request) {
	h.handleIntegratedToolMutation(w, r)
}

func (h *AdminHandler) handleIntegratedToolMutation(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "tool id is required")
		return
	}

	var body integratedToolUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid JSON body")
		return
	}

	rawRisk := strings.TrimSpace(body.RiskLevel)
	if rawRisk == "" {
		rawRisk = strings.TrimSpace(body.Classification)
	}
	if rawRisk == "" {
		rawRisk = strings.TrimSpace(body.Risk)
	}
	riskLevel := types.RiskLevel(rawRisk)
	if !riskLevel.IsValid() {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid risk level")
		return
	}

	if err := h.upsertToolClassification(r.Context(), id, riskLevel, strings.TrimSpace(body.Reason), delegatedActor(r), "integrated_api"); err != nil {
		writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to update tool classification")
		return
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleIntegratedToolSchema returns progressive-discovery schema details for a tool.
func (h *AdminHandler) HandleIntegratedToolSchema(w http.ResponseWriter, r *http.Request) {
	if !h.progressiveDiscovery || h.discoveryIndex == nil {
		writeIntegratedError(w, http.StatusNotImplemented, errCodeInvalidRequest, errProgressiveDiscoveryDisabled)
		return
	}

	name := strings.TrimSpace(chi.URLParam(r, "id"))
	if name == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "tool id is required")
		return
	}

	tools := h.discoveryIndex.GetTools([]string{name})
	if len(tools) == 0 {
		writeIntegratedError(w, http.StatusNotFound, errCodeNotFound, "tool not found")
		return
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"item": tools[0]})
}

// HandleIntegratedToolDiscoveryStats returns discovery and fallback counters.
func (h *AdminHandler) HandleIntegratedToolDiscoveryStats(w http.ResponseWriter, r *http.Request) {
	if !h.progressiveDiscovery || h.discoveryIndex == nil {
		writeIntegratedError(w, http.StatusNotImplemented, errCodeInvalidRequest, errProgressiveDiscoveryDisabled)
		return
	}

	stats := h.discoveryIndex.Stats()
	fullTokens := stats.TotalTools * tokensPerFullDefinition
	summaryTokens := stats.TotalTools * tokensPerSummary
	savingsPercent := 0.0
	if fullTokens > 0 {
		savingsPercent = float64(fullTokens-summaryTokens) / float64(fullTokens) * 100
	}

	vectorSearchFallbacks := uint64(0)
	vectorIndexFallbacks := uint64(0)
	if engine := h.discoveryIndex.Engine(); engine != nil {
		if provider, ok := engine.(interface{ VectorSearchFallbacks() uint64 }); ok {
			vectorSearchFallbacks = provider.VectorSearchFallbacks()
		}
		if provider, ok := engine.(interface{ VectorIndexFallbacks() uint64 }); ok {
			vectorIndexFallbacks = provider.VectorIndexFallbacks()
		}
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"total_tools":              stats.TotalTools,
		"categories":               stats.Categories,
		"progressive_discovery":    true,
		"estimated_full_tokens":    fullTokens,
		"estimated_summary_tokens": summaryTokens,
		"token_savings_percent":    savingsPercent,
		"vector_search_fallbacks":  vectorSearchFallbacks,
		"vector_index_fallbacks":   vectorIndexFallbacks,
		"engine_error_fallbacks":   h.discoveryIndex.EngineErrorFallbacks(),
	})
}

// HandleIntegratedUsers returns paginated gateway users and roles.
func (h *AdminHandler) HandleIntegratedUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query(), defaultPageSize)
	if err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	filter := types.UserFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  limit,
		Offset: offset,
	}
	if role := types.UserRole(strings.TrimSpace(r.URL.Query().Get("role"))); role.IsValid() {
		filter.Role = role
	}

	items, total := h.searchUsers(r.Context(), filter)

	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"total":       total,
		"next_cursor": nextCursor(offset, limit, total),
	})
}

// HandleIntegratedUserPermissions replaces user tool-permission assignments.
func (h *AdminHandler) HandleIntegratedUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "user id is required")
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid JSON body")
		return
	}

	toolNames := make([]string, 0)
	collect := func(value any) {
		list, ok := value.([]any)
		if !ok {
			return
		}
		for _, item := range list {
			v := strings.TrimSpace(fmt.Sprint(item))
			if v != "" {
				toolNames = append(toolNames, v)
			}
		}
	}
	collect(payload["tool_names"])
	collect(payload["tools"])
	collect(payload["permissions"])

	if err := h.replaceUserToolPermissions(r.Context(), id, toolNames, delegatedActor(r), "integrated_api"); err != nil {
		writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to update user permissions")
		return
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func buildIntegratedAuditFilter(r *http.Request) types.AuditFilter {
	q := url.Values{}
	for k, values := range r.URL.Query() {
		for _, value := range values {
			q.Add(k, value)
		}
	}

	if q.Get("client_id") == "" && q.Get("actor") != "" {
		q.Set("client_id", q.Get("actor"))
	}
	if q.Get("username") == "" && q.Get("actor") != "" {
		q.Set("username", q.Get("actor"))
	}
	if q.Get("tool_name") == "" && q.Get("resource") != "" {
		q.Set("tool_name", q.Get("resource"))
	}
	if q.Get("decision") == "" && q.Get("action") != "" {
		q.Set("decision", q.Get("action"))
	}
	if q.Get("details") == "" && q.Get("q") != "" {
		q.Set("details", q.Get("q"))
	}
	if q.Get("since") == "" && q.Get("from") != "" {
		q.Set("since", q.Get("from"))
	}
	if q.Get("until") == "" && q.Get("to") != "" {
		q.Set("until", q.Get("to"))
	}

	clone := *r.URL
	clone.RawQuery = q.Encode()
	proxyReq := r.Clone(r.Context())
	proxyReq.URL = &clone
	return buildAuditFilter(proxyReq)
}

// HandleIntegratedAuditQuery returns paginated audit events for delegated callers.
func (h *AdminHandler) HandleIntegratedAuditQuery(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query(), defaultPageSize)
	if err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	filter := buildIntegratedAuditFilter(r)
	filter.Limit = limit
	filter.Offset = offset

	entries := make([]types.AuditEntry, 0)
	total := 0
	if h.auditStore != nil {
		result, queryErr := h.auditStore.Query(r.Context(), filter)
		if queryErr != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to query audit entries")
			return
		}
		entries = result
		count, countErr := h.auditStore.Count(r.Context(), filter)
		if countErr != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to count audit entries")
			return
		}
		total = count
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"items":       entries,
		"total":       total,
		"next_cursor": nextCursor(offset, limit, total),
	})
}

func acceptWantsJSON(accept string) bool {
	accept = strings.ToLower(strings.TrimSpace(accept))
	if accept == "" {
		return false
	}
	return strings.Contains(accept, "application/json")
}

// HandleIntegratedAuditExport exports audit entries in JSON or CSV based on Accept header.
func (h *AdminHandler) HandleIntegratedAuditExport(w http.ResponseWriter, r *http.Request) {
	filter := buildIntegratedAuditFilter(r)
	filter.Limit = 10000
	filter.Offset = 0

	entries := make([]types.AuditEntry, 0)
	if h.auditStore != nil {
		result, err := h.auditStore.Query(r.Context(), filter)
		if err != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to export audit entries")
			return
		}
		entries = result
	}

	if acceptWantsJSON(r.Header.Get("Accept")) {
		writeIntegratedJSON(w, http.StatusOK, map[string]any{
			"items": entries,
		})
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_export.csv")
	writeAuditCSV(w, entries)
}

// HandleIntegratedSearchTuningGet returns search engine status and synonym tuning data.
func (h *AdminHandler) HandleIntegratedSearchTuningGet(w http.ResponseWriter, r *http.Request) {
	engineType := h.searchEngineType
	if engineType == "" {
		engineType = "substring"
	}

	engineReady := true
	indexedTools := 0
	if h.searchStatusFunc != nil {
		status := h.searchStatusFunc()
		if strings.TrimSpace(status.EngineType) != "" {
			engineType = status.EngineType
		}
		engineReady = status.EngineReady
		indexedTools = status.IndexedTools
	} else if h.discoveryIndex != nil {
		indexedTools = h.discoveryIndex.Stats().TotalTools
		if engine := h.discoveryIndex.Engine(); engine != nil {
			engineReady = engine.Ready()
		}
	}

	synonyms := make([]types.SynonymEntry, 0)
	if h.synonymStore != nil {
		entries, err := h.synonymStore.List(r.Context())
		if err != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to list search tuning")
			return
		}
		synonyms = entries
	}

	reindexLogs := make([]types.ReindexLogEntry, 0)
	if h.reindexLogStore != nil {
		entries, err := h.reindexLogStore.Recent(r.Context(), 10)
		if err != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to read search tuning logs")
			return
		}
		reindexLogs = entries
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"engine_type":   engineType,
		"engine_ready":  engineReady,
		"indexed_tools": indexedTools,
		"synonyms":      synonyms,
		"reindex_logs":  reindexLogs,
	})
}

// HandleIntegratedSearchTuningUpdate applies synonym tuning mutations.
func (h *AdminHandler) HandleIntegratedSearchTuningUpdate(w http.ResponseWriter, r *http.Request) {
	if h.synonymStore == nil {
		writeIntegratedError(w, http.StatusServiceUnavailable, errCodeInternalError, "synonym store is not configured")
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid JSON body")
		return
	}

	updates := 0
	if termRaw, ok := payload["term"]; ok {
		term := strings.ToLower(strings.TrimSpace(fmt.Sprint(termRaw)))
		synonyms := parseSynonymsPayload(payload["synonyms"])
		if term == "" || len(synonyms) == 0 {
			writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "term and synonyms are required")
			return
		}
		if err := h.synonymStore.Upsert(r.Context(), &types.SynonymEntry{Term: term, Synonyms: synonyms}); err != nil {
			writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to update search tuning")
			return
		}
		updates++
	}

	for _, key := range []string{"items", "entries", "synonyms"} {
		list, ok := payload[key].([]any)
		if !ok {
			continue
		}
		for _, entry := range list {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			term := strings.ToLower(strings.TrimSpace(fmt.Sprint(m["term"])))
			synonyms := parseSynonymsPayload(m["synonyms"])
			if term == "" || len(synonyms) == 0 {
				continue
			}
			if err := h.synonymStore.Upsert(r.Context(), &types.SynonymEntry{Term: term, Synonyms: synonyms}); err != nil {
				writeIntegratedError(w, http.StatusInternalServerError, errCodeInternalError, "failed to update search tuning")
				return
			}
			updates++
		}
	}

	if updates == 0 {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "no valid search tuning updates provided")
		return
	}

	writeIntegratedJSON(w, http.StatusOK, map[string]any{"status": "ok", "updated": updates})
}

func parseSynonymsPayload(value any) []string {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		return parseSynonymList(v)
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.ToLower(strings.TrimSpace(fmt.Sprint(item)))
			if s != "" {
				result = append(result, s)
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.ToLower(strings.TrimSpace(item))
			if s != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// HandleIntegratedSearchTuningTest runs a search query against discovery index state.
func (h *AdminHandler) HandleIntegratedSearchTuningTest(w http.ResponseWriter, r *http.Request) {
	if h.discoveryIndex == nil {
		writeIntegratedError(w, http.StatusServiceUnavailable, errCodeInternalError, "discovery index is not available")
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "invalid JSON body")
		return
	}
	query := strings.TrimSpace(fmt.Sprint(payload["query"]))
	if query == "" {
		writeIntegratedError(w, http.StatusBadRequest, errCodeInvalidRequest, "query is required")
		return
	}

	results, total := h.discoveryIndex.Search(query, "", 0, 20)
	writeIntegratedJSON(w, http.StatusOK, map[string]any{
		"query":     query,
		"total":     total,
		"results":   results,
		"tested_at": time.Now().UTC().Format(time.RFC3339),
	})
}
