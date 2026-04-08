package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
)

// ServerListEntry describes a single server in the /mcp/servers listing.
type ServerListEntry struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	ToolCount     int    `json:"tool_count"`
	ResourceCount int    `json:"resource_count"`
}

// ServerListHandler serves the GET /mcp/servers endpoint, returning a JSON
// array of active server metadata from the capability index.
type ServerListHandler struct {
	index  *registration.CapabilityIndex
	config *config.Config
	logger *slog.Logger
}

// NewServerListHandler creates a handler that returns the active server list.
func NewServerListHandler(
	index *registration.CapabilityIndex,
	cfg *config.Config,
	logger *slog.Logger,
) *ServerListHandler {
	return &ServerListHandler{
		index:  index,
		config: cfg,
		logger: logger,
	}
}

// ServeHTTP implements http.Handler.
func (h *ServerListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	servers := h.index.ListServers()

	baseURL := resolveBaseURLFromRequest(h.config, r)

	entries := make([]ServerListEntry, 0, len(servers))
	for _, s := range servers {
		key := serverKey(s.Record.Name)
		entries = append(entries, ServerListEntry{
			Name:          s.Record.Name,
			URL:           baseURL + "/mcp/servers/" + key + "/",
			Version:       s.Record.Version,
			Status:        s.Record.Status.String(),
			ToolCount:     s.ToolCount,
			ResourceCount: s.ResourceCount,
		})
	}

	body, err := json.Marshal(entries)
	if err != nil {
		h.logger.Error("server list marshal failed", slog.String("error", err.Error()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// resolveBaseURLFromRequest derives the gateway base URL from config or request headers.
func resolveBaseURLFromRequest(cfg *config.Config, r *http.Request) string {
	if cfg != nil && cfg.GatewayBaseURL != "" {
		return strings.TrimRight(cfg.GatewayBaseURL, "/")
	}

	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if host != "" {
			scheme := "https"
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
			}
			return strings.TrimRight(scheme+"://"+host, "/")
		}
	}

	if r.Host != "" {
		scheme := "https"
		if r.TLS == nil {
			scheme = "http"
		}
		return strings.TrimRight(scheme+"://"+r.Host, "/")
	}

	return ""
}
