package proxy

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// ProxyServer hosts the gateway MCP server and backend client registry.
type ProxyServer struct {
	index   *registration.CapabilityIndex
	clients map[string]*BackendClient
	config  *config.Config
	logger  *slog.Logger

	tlsConfig      *tls.Config
	backendTimeout time.Duration

	mu                sync.Mutex
	mcpServer         *server.MCPServer
	streamableHandler *server.StreamableHTTPServer
}

// NewProxyServer creates a new proxy server.
func NewProxyServer(
	index *registration.CapabilityIndex,
	cfg *config.Config,
	tlsConfig *tls.Config,
	backendTimeout time.Duration,
	logger *slog.Logger,
) *ProxyServer {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if backendTimeout <= 0 {
		backendTimeout = defaultBackendTimeout
	}

	return &ProxyServer{
		index:          index,
		clients:        make(map[string]*BackendClient),
		config:         cfg,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
	}
}

// SetupMCP initializes the mcp-go server and streamable HTTP transport.
func (p *ProxyServer) SetupMCP() error {
	if p == nil {
		return fmt.Errorf("setup mcp: proxy server is nil")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.streamableHandler != nil {
		return nil
	}

	mcpServer := server.NewMCPServer(
		"Cruvero MCP Gateway",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
		server.WithResourceRecovery(),
	)

	p.mcpServer = mcpServer
	p.streamableHandler = server.NewStreamableHTTPServer(
		mcpServer,
		server.WithHeartbeatInterval(15*time.Second),
	)

	return nil
}

// Handler returns the MCP HTTP handler for mounting into the main router.
func (p *ProxyServer) Handler() http.Handler {
	if p == nil {
		return http.NotFoundHandler()
	}

	if err := p.SetupMCP(); err != nil {
		p.logger.Error("proxy setup failed", slog.String("error", err.Error()))
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "proxy setup failed", http.StatusInternalServerError)
		})
	}

	return p.streamableHandler
}

func (p *ProxyServer) getOrCreateClient(record types.ServerRecord) *BackendClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	client, ok := p.clients[record.ID]
	if ok {
		return client
	}

	client = NewBackendClient(record, p.tlsConfig, p.backendTimeout)
	client.logger = p.logger
	p.clients[record.ID] = client
	return client
}
