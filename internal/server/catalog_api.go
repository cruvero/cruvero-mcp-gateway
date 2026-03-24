package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/policy"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

// CatalogEntry represents an enriched tool definition with server ownership
// metadata. Defined here to avoid an import cycle between proxy and server.
type CatalogEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Annotations any             `json:"annotations,omitempty"`
	ServerID    string          `json:"server_id"`
	ServerName  string          `json:"server_name"`
}

// CatalogToolLister abstracts the proxy method that returns enriched tool entries.
type CatalogToolLister interface {
	ListCatalogTools(ctx context.Context) ([]CatalogEntry, error)
}

// CatalogHandler serves the platform tool catalog endpoint.
type CatalogHandler struct {
	lister              CatalogToolLister
	classificationStore store.ToolClassificationStore
	serverStore         store.ServerStore
	gatewayID           string
	logger              *slog.Logger
}

// NewCatalogHandler creates a CatalogHandler.
func NewCatalogHandler(
	lister CatalogToolLister,
	classificationStore store.ToolClassificationStore,
	serverStore store.ServerStore,
	gatewayID string,
	logger *slog.Logger,
) *CatalogHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &CatalogHandler{
		lister:              lister,
		classificationStore: classificationStore,
		serverStore:         serverStore,
		gatewayID:           gatewayID,
		logger:              logger,
	}
}

// Routes returns a chi router for the catalog endpoint.
func (h *CatalogHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.handleList)
	return r
}

type catalogServerView struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	ToolCount int    `json:"tool_count"`
}

type catalogToolView struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Annotations any             `json:"annotations,omitempty"`
	ServerID    string          `json:"server_id"`
	ServerName  string          `json:"server_name"`
	RiskLevel   string          `json:"risk_level"`
}

type catalogResponse struct {
	Servers     []catalogServerView `json:"servers"`
	Tools       []catalogToolView   `json:"tools"`
	TotalTools  int                 `json:"total_tools"`
	GeneratedAt time.Time           `json:"generated_at"`
	GatewayID   string              `json:"gateway_id"`
}

func (h *CatalogHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entries, err := h.lister.ListCatalogTools(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "catalog list tools failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tools"})
		return
	}

	classificationMap := h.loadClassifications(ctx)
	serverToolCounts := make(map[string]int)
	tools := make([]catalogToolView, 0, len(entries))

	for _, entry := range entries {
		serverToolCounts[entry.ServerID]++

		riskLevel := h.resolveRiskLevel(entry, classificationMap)

		tools = append(tools, catalogToolView{
			Name:        entry.Name,
			Description: entry.Description,
			InputSchema: entry.InputSchema,
			Annotations: entry.Annotations,
			ServerID:    entry.ServerID,
			ServerName:  entry.ServerName,
			RiskLevel:   riskLevel,
		})
	}

	servers := h.buildServerViews(ctx, serverToolCounts)

	writeJSON(w, http.StatusOK, catalogResponse{
		Servers:     servers,
		Tools:       tools,
		TotalTools:  len(tools),
		GeneratedAt: time.Now().UTC(),
		GatewayID:   h.gatewayID,
	})
}

func (h *CatalogHandler) loadClassifications(ctx context.Context) map[string]types.RiskLevel {
	if h.classificationStore == nil {
		return nil
	}
	all, err := h.classificationStore.GetAll(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "catalog load classifications failed", slog.String("error", err.Error()))
		return nil
	}
	m := make(map[string]types.RiskLevel, len(all))
	for _, tc := range all {
		m[tc.ToolName] = tc.RiskLevel
	}
	return m
}

func (h *CatalogHandler) resolveRiskLevel(entry CatalogEntry, classificationMap map[string]types.RiskLevel) string {
	if classificationMap != nil {
		if level, ok := classificationMap[entry.Name]; ok {
			return level.String()
		}
	}
	level, _ := policy.AutoClassify(entry.Name, entry.Description)
	return level.String()
}

func (h *CatalogHandler) buildServerViews(ctx context.Context, toolCounts map[string]int) []catalogServerView {
	if h.serverStore == nil {
		return []catalogServerView{}
	}
	active := types.StatusActive
	records, err := h.serverStore.List(ctx, types.ServerFilter{Status: &active})
	if err != nil {
		h.logger.ErrorContext(ctx, "catalog list servers failed", slog.String("error", err.Error()))
		return []catalogServerView{}
	}

	views := make([]catalogServerView, 0, len(records))
	for _, rec := range records {
		views = append(views, catalogServerView{
			Name:      strings.TrimPrefix(rec.Name, "mcp-"),
			ID:        rec.ID,
			Version:   rec.Version,
			Status:    string(rec.Status),
			ToolCount: toolCounts[rec.ID],
		})
	}
	return views
}
