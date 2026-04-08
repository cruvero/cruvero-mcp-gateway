package proxy

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/resilience"
)

// serverNamePattern validates per-server endpoint names.
// Must start with a lowercase alphanumeric, followed by up to 62 lowercase
// alphanumeric, hyphen, or underscore characters.
var serverNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// VirtualServerHandler proxies MCP requests to individual backend servers
// at per-server endpoints (/mcp/servers/{serverName}/).
type VirtualServerHandler struct {
	index          *registration.CapabilityIndex
	logger         *slog.Logger
	tlsConfig      *tls.Config
	backendTimeout time.Duration
	instances      sync.Map // map[string]*virtualServerInstance
}

// virtualServerInstance caches a reverse proxy handler for a specific backend.
type virtualServerInstance struct {
	handler   http.Handler
	createdAt time.Time
}

// NewVirtualServerHandler creates a handler that routes per-server MCP requests.
func NewVirtualServerHandler(
	index *registration.CapabilityIndex,
	tlsConfig *tls.Config,
	backendTimeout time.Duration,
	logger *slog.Logger,
) *VirtualServerHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if backendTimeout <= 0 {
		backendTimeout = defaultBackendTimeout
	}
	return &VirtualServerHandler{
		index:          index,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
	}
}

// ServeHTTP handles per-server MCP proxy requests.
func (h *VirtualServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	serverName := strings.TrimSpace(chi.URLParam(r, "serverName"))

	if !serverNamePattern.MatchString(serverName) {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid server name: %q", serverName))
		h.logger.Debug("per-server endpoint: invalid server name",
			slog.String("server_name", serverName),
		)
		return
	}

	record := h.index.LookupServer(serverName)
	if record == nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("server %q not found", serverName))
		h.logger.Debug("per-server endpoint: server not found",
			slog.String("server_name", serverName),
		)
		return
	}

	if !record.Status.IsRoutable() {
		writeJSONError(w, http.StatusServiceUnavailable, fmt.Sprintf("server %q is not active (status: %s)", serverName, record.Status))
		h.logger.Debug("per-server endpoint: server not active",
			slog.String("server_name", serverName),
			slog.String("status", record.Status.String()),
		)
		return
	}

	handler := h.getOrCreateHandler(record.ID, record.Host, record.Port, record.Protocol)
	handler.ServeHTTP(w, r)
}

// getOrCreateHandler returns a cached reverse proxy or lazily creates one.
func (h *VirtualServerHandler) getOrCreateHandler(serverID, host string, port int, protocol string) http.Handler {
	if existing, ok := h.instances.Load(serverID); ok {
		if inst, castOK := existing.(*virtualServerInstance); castOK {
			return inst.handler
		}
	}

	scheme := backendScheme(protocol)
	targetURL := &url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", strings.TrimSpace(host), port),
		Path:   "/mcp",
	}

	transport := resilience.NewTransport(h.tlsConfig, resilience.DefaultPoolOptions())
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.URL.Path = targetURL.Path
			req.URL.RawQuery = ""
			req.Host = targetURL.Host
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			h.logger.Error("per-server proxy error",
				slog.String("server_id", serverID),
				slog.String("target", targetURL.String()),
				slog.String("error", err.Error()),
			)
			writeJSONError(w, http.StatusBadGateway, "upstream server error")
		},
	}

	inst := &virtualServerInstance{
		handler:   proxy,
		createdAt: time.Now().UTC(),
	}
	actual, _ := h.instances.LoadOrStore(serverID, inst)
	return actual.(*virtualServerInstance).handler
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
