package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// HandleUsers renders the users list page.
func (h *AdminHandler) HandleUsers(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)
	filter := buildUserFilter(r, page)
	users, total := h.searchUsers(r.Context(), filter)

	h.renderListPage(w, r, total, page, map[string]any{
		"PageTitle":  "Users",
		"Users":      users,
		"Query":      filter.Query,
		"RoleFilter": string(filter.Role),
	}, "users_table", "users.html")
}

func buildUserFilter(r *http.Request, page int) types.UserFilter {
	filter := types.UserFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  defaultPageSize,
		Offset: (page - 1) * defaultPageSize,
	}
	if rl := types.UserRole(strings.TrimSpace(r.URL.Query().Get("role"))); rl.IsValid() {
		filter.Role = rl
	}
	return filter
}

func (h *AdminHandler) searchUsers(ctx context.Context, filter types.UserFilter) ([]types.User, int) {
	if h.userStore == nil {
		return nil, 0
	}
	result, count, err := h.userStore.Search(ctx, filter)
	if err != nil {
		h.logger.ErrorContext(ctx, "search users failed", slog.String("error", err.Error()))
		return nil, 0
	}
	return result, count
}

// HandleUserDetail renders the user detail page with tool permissions.
func (h *AdminHandler) HandleUserDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		h.renderError(w, r, http.StatusBadRequest, "A user ID is required.")
		return
	}

	if h.userStore == nil {
		h.renderError(w, r, http.StatusInternalServerError, "User store is not configured.")
		return
	}

	user, err := h.userStore.Get(r.Context(), id)
	if err != nil {
		h.logger.Error("get user failed", slog.String("id", id), slog.String("error", err.Error()))
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load user.")
		return
	}
	if user == nil {
		h.renderError(w, r, http.StatusNotFound, "The requested user could not be found.")
		return
	}

	perms, err := h.userStore.GetToolPermissions(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("get user permissions failed", slog.String("id", id), slog.String("error", err.Error()))
	}
	permSet := make(map[string]bool, len(perms))
	for _, p := range perms {
		permSet[p.ToolName] = true
	}

	// Fetch all classified tools for the available tools panel.
	var allTools []types.ToolClassification
	if h.classificationStore != nil {
		tools, err := h.classificationStore.GetAll(r.Context())
		if err != nil {
			h.logger.Error("get all tools for user detail failed", slog.String("error", err.Error()))
		} else {
			allTools = tools
		}
	}

	// Split tools into allowed (user has permission) and available (user does not).
	type toolEntry struct {
		Name      string
		RiskLevel string
		Server    string
	}
	type serverGroup struct {
		Server string
		Tools  []toolEntry
	}

	allowedByServer := make(map[string][]toolEntry)
	availableByServer := make(map[string][]toolEntry)

	for _, tc := range allTools {
		server, tool := splitFederatedName(tc.ToolName)
		entry := toolEntry{
			Name:      tc.ToolName,
			RiskLevel: string(tc.RiskLevel),
			Server:    server,
		}
		if tool == "" {
			entry.Server = "other"
		}
		if permSet[tc.ToolName] {
			allowedByServer[entry.Server] = append(allowedByServer[entry.Server], entry)
		} else {
			availableByServer[entry.Server] = append(availableByServer[entry.Server], entry)
		}
	}

	// Also include permissions for tools that are no longer classified (deregistered).
	for _, p := range perms {
		if _, inClassified := findTool(allTools, p.ToolName); !inClassified {
			server, _ := splitFederatedName(p.ToolName)
			if server == "" {
				server = "other"
			}
			allowedByServer[server] = append(allowedByServer[server], toolEntry{
				Name:      p.ToolName,
				RiskLevel: "offline",
				Server:    server,
			})
		}
	}

	toGroups := func(m map[string][]toolEntry) []serverGroup {
		groups := make([]serverGroup, 0, len(m))
		for s, tools := range m {
			groups = append(groups, serverGroup{Server: s, Tools: tools})
		}
		return groups
	}

	h.render(w, r, "user_detail.html", map[string]any{
		"PageTitle":       "User Detail",
		"User":            user,
		"Permissions":     permSet,
		"PermissionCount": len(perms),
		"AllowedGroups":   toGroups(allowedByServer),
		"AvailableGroups": toGroups(availableByServer),
	})
}

// HandleUserRoleUpdate processes role changes.
func (h *AdminHandler) HandleUserRoleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}

	role := types.UserRole(strings.TrimSpace(r.FormValue("role")))
	if !role.IsValid() {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	if h.userStore == nil {
		http.Error(w, "user store not configured", http.StatusInternalServerError)
		return
	}

	// Get old role for audit.
	oldRole := ""
	user, err := h.userStore.Get(r.Context(), id)
	if err == nil && user != nil {
		oldRole = string(user.Role)
	}

	if err := h.userStore.UpdateRole(r.Context(), id, role); err != nil {
		h.logger.Error("update user role failed", slog.String("error", err.Error()))
		http.Error(w, "failed to update role", http.StatusInternalServerError)
		return
	}

	session, _ := SessionFromContext(r.Context())
	changedBy := "admin"
	if session != nil {
		changedBy = session.Email
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(r.Context(), &types.AuditEntry{
			EventType:  "user.role_changed",
			ClientID:   changedBy,
			ServerName: id,
			Details: map[string]any{
				"user_id":  id,
				"email":    emailFromUser(user),
				"old_role": oldRole,
				"new_role": string(role),
			},
		})
	}

	http.Redirect(w, r, "/admin/users/"+id, http.StatusFound)
}

// HandleUserPermissionsUpdate replaces all tool permissions for a user.
func (h *AdminHandler) HandleUserPermissionsUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	toolNames := r.Form["tool_names"]

	if h.userStore == nil {
		http.Error(w, "user store not configured", http.StatusInternalServerError)
		return
	}

	session, _ := SessionFromContext(r.Context())
	grantedBy := "admin"
	if session != nil {
		grantedBy = session.Email
	}

	if err := h.replaceUserToolPermissions(r.Context(), id, toolNames, grantedBy, "admin_dashboard"); err != nil {
		h.logger.Error("set user permissions failed", slog.String("error", err.Error()))
		http.Error(w, "failed to update permissions", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/users/"+id, http.StatusFound)
}

// HandleUserToolsAdd adds tools to a user's permission set.
func (h *AdminHandler) HandleUserToolsAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	newTools := r.Form["tool_names"]

	if h.userStore == nil {
		http.Error(w, "user store not configured", http.StatusInternalServerError)
		return
	}

	// Merge with existing permissions.
	existing, err := h.userStore.GetToolPermissions(r.Context(), id)
	if err != nil {
		h.logger.Error("get existing permissions failed", slog.String("error", err.Error()))
		http.Error(w, "failed to read current permissions", http.StatusInternalServerError)
		return
	}

	merged := make(map[string]struct{}, len(existing)+len(newTools))
	for _, p := range existing {
		merged[p.ToolName] = struct{}{}
	}
	for _, name := range newTools {
		merged[strings.TrimSpace(name)] = struct{}{}
	}

	allNames := make([]string, 0, len(merged))
	for name := range merged {
		allNames = append(allNames, name)
	}

	session, _ := SessionFromContext(r.Context())
	grantedBy := "admin"
	if session != nil {
		grantedBy = session.Email
	}

	if err := h.userStore.SetToolPermissions(r.Context(), id, allNames, grantedBy); err != nil {
		h.logger.Error("add user tools failed", slog.String("error", err.Error()))
		http.Error(w, "failed to update permissions", http.StatusInternalServerError)
		return
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(r.Context(), &types.AuditEntry{
			EventType:  "user.tools_granted",
			ClientID:   grantedBy,
			ServerName: id,
			Details: map[string]any{
				"user_id":    id,
				"tools":      newTools,
				"granted_by": grantedBy,
			},
		})
	}

	http.Redirect(w, r, "/admin/users/"+id, http.StatusFound)
}

// HandleUserToolsRemove removes tools from a user's permission set.
func (h *AdminHandler) HandleUserToolsRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "user ID required", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	removeTools := r.Form["tool_names"]

	if h.userStore == nil {
		http.Error(w, "user store not configured", http.StatusInternalServerError)
		return
	}

	existing, err := h.userStore.GetToolPermissions(r.Context(), id)
	if err != nil {
		h.logger.Error("get existing permissions failed", slog.String("error", err.Error()))
		http.Error(w, "failed to read current permissions", http.StatusInternalServerError)
		return
	}

	removeSet := make(map[string]struct{}, len(removeTools))
	for _, name := range removeTools {
		removeSet[strings.TrimSpace(name)] = struct{}{}
	}

	remaining := make([]string, 0, len(existing))
	for _, p := range existing {
		if _, remove := removeSet[p.ToolName]; !remove {
			remaining = append(remaining, p.ToolName)
		}
	}

	session, _ := SessionFromContext(r.Context())
	revokedBy := "admin"
	if session != nil {
		revokedBy = session.Email
	}

	if err := h.userStore.SetToolPermissions(r.Context(), id, remaining, revokedBy); err != nil {
		h.logger.Error("remove user tools failed", slog.String("error", err.Error()))
		http.Error(w, "failed to update permissions", http.StatusInternalServerError)
		return
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(r.Context(), &types.AuditEntry{
			EventType:  "user.tools_revoked",
			ClientID:   revokedBy,
			ServerName: id,
			Details: map[string]any{
				"user_id":    id,
				"tools":      removeTools,
				"revoked_by": revokedBy,
			},
		})
	}

	http.Redirect(w, r, "/admin/users/"+id, http.StatusFound)
}

// splitFederatedName extracts (server, tool) from a federated tool name.
// Supports legacy "mcp.<server>.<tool>" and new "<displayName>.<tool>" formats.
func splitFederatedName(name string) (string, string) {
	// Legacy: mcp.<server>.<tool>
	rest, ok := strings.CutPrefix(name, "mcp.")
	if ok {
		server, tool, found := strings.Cut(rest, ".")
		if !found {
			return server, ""
		}
		return server, tool
	}
	// New: <displayName>.<tool>
	server, tool, found := strings.Cut(name, ".")
	if !found {
		return "", name
	}
	return server, tool
}

func emailFromUser(user *types.User) string {
	if user == nil {
		return ""
	}
	return user.Email
}

func (h *AdminHandler) replaceUserToolPermissions(ctx context.Context, userID string, toolNames []string, grantedBy, source string) error {
	if h.userStore != nil {
		if err := h.userStore.SetToolPermissions(ctx, userID, toolNames, grantedBy); err != nil {
			return err
		}
	}

	if h.auditStore != nil {
		_ = h.auditStore.Log(ctx, &types.AuditEntry{
			EventType:  "user.permissions_updated",
			ClientID:   grantedBy,
			ServerName: userID,
			Details: map[string]any{
				"user_id":    userID,
				"tools":      toolNames,
				"granted_by": grantedBy,
				"source":     source,
			},
		})
	}

	return nil
}

func findTool(tools []types.ToolClassification, name string) (types.ToolClassification, bool) {
	for _, t := range tools {
		if t.ToolName == name {
			return t, true
		}
	}
	return types.ToolClassification{}, false
}
