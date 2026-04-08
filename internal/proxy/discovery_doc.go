package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
)

// discoveryServerEntry represents a single server in the well-known document.
type discoveryServerEntry struct {
	URL     string `json:"url"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

// discoveryDocument is the top-level structure of /.well-known/mcp.json.
type discoveryDocument struct {
	MCPServers map[string]discoveryServerEntry `json:"mcpServers"`
}

type cachedDoc struct {
	body      []byte
	createdAt time.Time
}

// DiscoveryDocHandler serves the /.well-known/mcp.json discovery document.
// It caches the generated JSON for a configurable TTL using atomic pointer
// swaps for lock-free reads on the hot path.
type DiscoveryDocHandler struct {
	index  *registration.CapabilityIndex
	config *config.Config
	logger *slog.Logger
	cached atomic.Pointer[cachedDoc]
}

// NewDiscoveryDocHandler creates a handler that serves the well-known
// discovery document from the capability index.
func NewDiscoveryDocHandler(
	index *registration.CapabilityIndex,
	cfg *config.Config,
	logger *slog.Logger,
) *DiscoveryDocHandler {
	return &DiscoveryDocHandler{
		index:  index,
		config: cfg,
		logger: logger,
	}
}

// Invalidate clears the cached document so the next request regenerates it.
func (h *DiscoveryDocHandler) Invalidate() {
	h.cached.Store(nil)
}

// ServeHTTP implements http.Handler.
func (h *DiscoveryDocHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	ttl := h.cacheTTL()
	if cached := h.cached.Load(); cached != nil {
		if time.Since(cached.createdAt) < ttl {
			writeCachedJSON(w, cached.body)
			return
		}
	}

	body, err := h.generate(r)
	if err != nil {
		h.logger.Error("discovery doc generation failed", slog.String("error", err.Error()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.cached.Store(&cachedDoc{
		body:      body,
		createdAt: time.Now().UTC(),
	})

	writeCachedJSON(w, body)
}

func (h *DiscoveryDocHandler) generate(r *http.Request) ([]byte, error) {
	baseURL := resolveBaseURLFromRequest(h.config, r)
	servers := h.index.ListServers()

	doc := discoveryDocument{
		MCPServers: make(map[string]discoveryServerEntry, len(servers)),
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(servers))
	for _, s := range servers {
		key := serverKey(s.Record.Name)
		keys = append(keys, key)
		doc.MCPServers[key] = discoveryServerEntry{
			URL:     baseURL + "/mcp/servers/" + key + "/",
			Name:    s.Record.Name,
			Version: s.Record.Version,
			Status:  s.Record.Status.String(),
		}
	}
	sort.Strings(keys)

	// Rebuild map in sorted order for stable JSON.
	sorted := make(map[string]discoveryServerEntry, len(keys))
	for _, k := range keys {
		sorted[k] = doc.MCPServers[k]
	}
	doc.MCPServers = sorted

	return json.Marshal(doc)
}

func (h *DiscoveryDocHandler) cacheTTL() time.Duration {
	if h.config != nil && h.config.WellKnownCacheTTL > 0 {
		return h.config.WellKnownCacheTTL
	}
	return 5 * time.Second
}

// serverKey derives a short key from the server name, stripping common prefixes.
func serverKey(name string) string {
	key := strings.TrimSpace(name)
	key = strings.TrimPrefix(key, "mcp-")
	key = strings.TrimPrefix(key, "mcp_")
	if key == "" {
		return name
	}
	return key
}

func writeCachedJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
