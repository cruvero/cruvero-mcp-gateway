package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	defaultBrowsePerPage = 50
	maxBrowsePerPage     = 100
	maxBrowsePage        = 10000

	// Approximate token estimates for tool definitions.
	tokensPerFullDefinition = 170
	tokensPerSummary        = 25
)

// HandleToolBrowse returns paginated tool summaries with optional search and category filter.
func (h *AdminHandler) HandleToolBrowse(w http.ResponseWriter, r *http.Request) {
	if !h.progressiveDiscovery || h.discoveryIndex == nil {
		writeAdminJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "progressive discovery is not enabled",
		})
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	page := parseIntParam(r, "page", 1)
	perPage := parseIntParam(r, "per_page", defaultBrowsePerPage)

	if perPage > maxBrowsePerPage {
		perPage = maxBrowsePerPage
	}
	if perPage <= 0 {
		perPage = defaultBrowsePerPage
	}
	if page <= 0 {
		page = 1
	}
	if page > maxBrowsePage {
		page = maxBrowsePage
	}

	offset := (page - 1) * perPage

	if query != "" {
		results, totalMatches := h.discoveryIndex.Search(query, category, offset, perPage)
		writeAdminJSON(w, http.StatusOK, map[string]any{
			"tools":    results,
			"total":    totalMatches,
			"page":     page,
			"per_page": perPage,
		})
		return
	}

	results, total := h.discoveryIndex.Browse(category, offset, perPage)
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"tools":    results,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// HandleToolSchema returns the full tool definition for a single tool by name.
func (h *AdminHandler) HandleToolSchema(w http.ResponseWriter, r *http.Request) {
	if !h.progressiveDiscovery || h.discoveryIndex == nil {
		writeAdminJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "progressive discovery is not enabled",
		})
		return
	}

	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeAdminJSON(w, http.StatusBadRequest, map[string]string{
			"error": "tool name is required",
		})
		return
	}

	tools := h.discoveryIndex.GetTools([]string{name})
	if len(tools) == 0 {
		writeAdminJSON(w, http.StatusNotFound, map[string]string{
			"error": "tool not found",
		})
		return
	}

	writeAdminJSON(w, http.StatusOK, tools[0])
}

// HandleDiscoveryStats returns aggregate statistics about the discovery index.
func (h *AdminHandler) HandleDiscoveryStats(w http.ResponseWriter, r *http.Request) {
	if !h.progressiveDiscovery || h.discoveryIndex == nil {
		writeAdminJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "progressive discovery is not enabled",
		})
		return
	}

	stats := h.discoveryIndex.Stats()

	fullTokens := stats.TotalTools * tokensPerFullDefinition
	summaryTokens := stats.TotalTools * tokensPerSummary
	savingsPercent := 0.0
	if fullTokens > 0 {
		savingsPercent = float64(fullTokens-summaryTokens) / float64(fullTokens) * 100
	}

	writeAdminJSON(w, http.StatusOK, map[string]any{
		"total_tools":              stats.TotalTools,
		"categories":              stats.Categories,
		"progressive_discovery":   true,
		"estimated_full_tokens":   fullTokens,
		"estimated_summary_tokens": summaryTokens,
		"token_savings_percent":   savingsPercent,
	})
}

func writeAdminJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func parseIntParam(r *http.Request, key string, defaultVal int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return defaultVal
	}
	return val
}
