package admin

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/search"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// HandleSearch renders the search management page.
func (h *AdminHandler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	engineType := h.searchEngineType
	if engineType == "" {
		engineType = "substring"
	}

	embedderType := h.embedderType
	engineReady := true
	indexedTools := 0
	var (
		vectorReady           bool
		hasVectorReady        bool
		vectorSearchFallbacks uint64
		vectorIndexFallbacks  uint64
		engineErrorFallbacks  uint64
	)
	if h.searchStatusFunc != nil {
		status := h.searchStatusFunc()
		if strings.TrimSpace(status.EngineType) != "" {
			engineType = status.EngineType
		}
		if strings.TrimSpace(status.EmbedderType) != "" {
			embedderType = status.EmbedderType
		}
		engineReady = status.EngineReady
		indexedTools = status.IndexedTools
		if status.VectorReady != nil {
			vectorReady = *status.VectorReady
			hasVectorReady = true
		}
		vectorSearchFallbacks = status.VectorSearchFallbacks
		vectorIndexFallbacks = status.VectorIndexFallbacks
		engineErrorFallbacks = status.EngineErrorFallbacks
	} else if h.discoveryIndex != nil {
		indexedTools = h.discoveryIndex.Stats().TotalTools
		engineErrorFallbacks = h.discoveryIndex.EngineErrorFallbacks()
		if engine := h.discoveryIndex.Engine(); engine != nil {
			engineReady = engine.Ready()
			if ve, ok := engine.(interface{ VectorReady() bool }); ok {
				vectorReady = ve.VectorReady()
				hasVectorReady = true
				engineReady = engineReady && vectorReady
			}
			if vf, ok := engine.(interface{ VectorSearchFallbacks() uint64 }); ok {
				vectorSearchFallbacks = vf.VectorSearchFallbacks()
			}
			if vf, ok := engine.(interface{ VectorIndexFallbacks() uint64 }); ok {
				vectorIndexFallbacks = vf.VectorIndexFallbacks()
			}
		}
	}

	var synonyms []types.SynonymEntry
	if h.synonymStore != nil {
		var err error
		synonyms, err = h.synonymStore.List(r.Context())
		if err != nil {
			h.logger.Error("list synonyms", slog.String("error", err.Error()))
		}
	}

	var reindexLogs []types.ReindexLogEntry
	if h.reindexLogStore != nil {
		var err error
		reindexLogs, err = h.reindexLogStore.Recent(r.Context(), 10)
		if err != nil {
			h.logger.Error("list reindex logs", slog.String("error", err.Error()))
		}
	}

	data := map[string]any{
		"PageTitle":    "Search",
		"EngineType":   engineType,
		"EngineReady":  engineReady,
		"IndexedTools": indexedTools,
		"EmbedderType": embedderType,
		"Synonyms":     synonyms,
		"ReindexLogs":  reindexLogs,

		"HasVectorReady":        hasVectorReady,
		"VectorReady":           vectorReady,
		"VectorSearchFallbacks": vectorSearchFallbacks,
		"VectorIndexFallbacks":  vectorIndexFallbacks,
		"EngineErrorFallbacks":  engineErrorFallbacks,
		"ShowFallbackStats":     hasVectorReady || vectorSearchFallbacks > 0 || vectorIndexFallbacks > 0 || engineErrorFallbacks > 0,
	}

	h.render(w, r, "search.html", data)
}

// HandleSearchTest executes a test search and renders the results.
func (h *AdminHandler) HandleSearchTest(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.FormValue("query"))
	if query == "" {
		_, _ = w.Write([]byte(`<p class="text-muted">Enter a query to test search.</p>`))
		return
	}

	if h.discoveryIndex == nil {
		_, _ = w.Write([]byte(`<p class="text-muted">Discovery index not available.</p>`))
		return
	}

	results, total := h.discoveryIndex.Search(query, "", 0, 20)

	var engineResults []search.ScoredResult
	if engine := h.discoveryIndex.Engine(); engine != nil {
		engineResults, _ = engine.Search(context.Background(), query, 20)
	}

	esc := html.EscapeString

	var buf strings.Builder
	buf.WriteString(`<div class="search-test-results">`)
	buf.WriteString(`<p><strong>` + esc(query) + `</strong> — ` + itoa(total) + ` matches</p>`)

	if len(results) > 0 {
		buf.WriteString(`<table class="data-table"><thead><tr><th>#</th><th>Tool</th><th>Description</th></tr></thead><tbody>`)
		for i, res := range results {
			buf.WriteString(`<tr><td>` + itoa(i+1) + `</td><td><code>` + esc(res.Name) + `</code></td><td>` + esc(res.Description) + `</td></tr>`)
		}
		buf.WriteString(`</tbody></table>`)
	}

	if len(engineResults) > 0 {
		buf.WriteString(`<details style="margin-top: 0.5rem;"><summary>Raw engine scores (` + itoa(len(engineResults)) + ` results)</summary>`)
		buf.WriteString(`<table class="data-table"><thead><tr><th>Key</th><th>Score</th></tr></thead><tbody>`)
		for _, res := range engineResults {
			buf.WriteString(`<tr><td><code>` + esc(res.Key) + `</code></td><td>` + ftoa(res.Score) + `</td></tr>`)
		}
		buf.WriteString(`</tbody></table></details>`)
	}

	buf.WriteString(`</div>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(buf.String()))
}

// HandleSynonymList renders the synonym table partial (HTMX).
func (h *AdminHandler) HandleSynonymList(w http.ResponseWriter, r *http.Request) {
	var synonyms []types.SynonymEntry
	if h.synonymStore != nil {
		var err error
		synonyms, err = h.synonymStore.List(r.Context())
		if err != nil {
			h.logger.Error("list synonyms", slog.String("error", err.Error()))
		}
	}

	data := map[string]any{
		"Synonyms": synonyms,
	}
	session, _ := SessionFromContext(r.Context())
	if session != nil {
		data["CSRFToken"] = session.CSRFToken
	}

	if err := h.partials.ExecuteTemplate(w, "search_synonyms_table", data); err != nil {
		h.logger.Error("render synonyms table", slog.String("error", err.Error()))
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// HandleSynonymCreate adds or updates a synonym entry.
func (h *AdminHandler) HandleSynonymCreate(w http.ResponseWriter, r *http.Request) {
	if h.synonymStore == nil {
		http.Error(w, "synonym store not available", http.StatusServiceUnavailable)
		return
	}

	term := strings.TrimSpace(r.FormValue("term"))
	synonymsRaw := strings.TrimSpace(r.FormValue("synonyms"))

	if term == "" || synonymsRaw == "" {
		http.Error(w, "term and synonyms are required", http.StatusBadRequest)
		return
	}

	synonyms := parseSynonymList(synonymsRaw)
	entry := &types.SynonymEntry{
		Term:     strings.ToLower(term),
		Synonyms: synonyms,
	}

	if err := h.synonymStore.Upsert(r.Context(), entry); err != nil {
		h.logger.Error("upsert synonym", slog.String("error", err.Error()))
		http.Error(w, "failed to save synonym", http.StatusInternalServerError)
		return
	}

	h.HandleSynonymList(w, r)
}

// HandleSynonymDelete removes a synonym entry.
func (h *AdminHandler) HandleSynonymDelete(w http.ResponseWriter, r *http.Request) {
	if h.synonymStore == nil {
		http.Error(w, "synonym store not available", http.StatusServiceUnavailable)
		return
	}

	term := chi.URLParam(r, "term")
	if term == "" {
		http.Error(w, "term is required", http.StatusBadRequest)
		return
	}

	if err := h.synonymStore.Delete(r.Context(), term); err != nil {
		h.logger.Error("delete synonym", slog.String("error", err.Error()))
		http.Error(w, "failed to delete synonym", http.StatusInternalServerError)
		return
	}

	h.HandleSynonymList(w, r)
}

func parseSynonymList(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(strings.ToLower(p))
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func ftoa(f float64) string {
	return fmt.Sprintf("%.4f", f)
}
