package mockbackend

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	defaultGatewayURL      = "https://gateway.localhost:8443"
	defaultListenAddr      = ":18080"
	defaultAdvertiseHost   = "mock-backend"
	defaultAdvertisePort   = 18080
	defaultAdvertiseScheme = "http"
	defaultServiceName     = "mock-backend"
	defaultServiceVersion  = "1.0.0"
	defaultReadyPoll       = 2 * time.Second
	defaultRetryInterval   = 3 * time.Second
	defaultShutdownTimeout = 5 * time.Second
)

// Config configures the local mock MCP backend service.
type Config struct {
	GatewayURL        string
	ListenAddr        string
	AdvertiseHost     string
	AdvertisePort     int
	AdvertiseProtocol string
	ServiceName       string
	ServiceVersion    string
	TLSCAPath         string
	TLSCertPath       string
	TLSKeyPath        string
	ReadyPollInterval time.Duration
	RetryInterval     time.Duration
	ShutdownTimeout   time.Duration
}

// Validate normalizes defaults and validates required configuration.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("validate config: config is nil")
	}
	if strings.TrimSpace(c.GatewayURL) == "" {
		c.GatewayURL = defaultGatewayURL
	}
	if strings.TrimSpace(c.ListenAddr) == "" {
		c.ListenAddr = defaultListenAddr
	}
	if strings.TrimSpace(c.AdvertiseHost) == "" {
		c.AdvertiseHost = defaultAdvertiseHost
	}
	if c.AdvertisePort == 0 {
		c.AdvertisePort = defaultAdvertisePort
	}
	if strings.TrimSpace(c.AdvertiseProtocol) == "" {
		c.AdvertiseProtocol = defaultAdvertiseScheme
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		c.ServiceName = defaultServiceName
	}
	if strings.TrimSpace(c.ServiceVersion) == "" {
		c.ServiceVersion = defaultServiceVersion
	}
	if c.ReadyPollInterval <= 0 {
		c.ReadyPollInterval = defaultReadyPoll
	}
	if c.RetryInterval <= 0 {
		c.RetryInterval = defaultRetryInterval
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	if strings.TrimSpace(c.TLSCAPath) == "" {
		return fmt.Errorf("validate config: TLS CA path is required")
	}
	if strings.TrimSpace(c.TLSCertPath) == "" {
		return fmt.Errorf("validate config: TLS client cert path is required")
	}
	if strings.TrimSpace(c.TLSKeyPath) == "" {
		return fmt.Errorf("validate config: TLS client key path is required")
	}

	parsedURL, err := url.Parse(c.GatewayURL)
	if err != nil {
		return fmt.Errorf("validate config: parse gateway URL: %w", err)
	}
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("validate config: gateway URL must use https")
	}
	if strings.TrimSpace(parsedURL.Host) == "" {
		return fmt.Errorf("validate config: gateway URL host is required")
	}

	protocol := strings.ToLower(strings.TrimSpace(c.AdvertiseProtocol))
	if protocol != "http" && protocol != "https" {
		return fmt.Errorf("validate config: advertise protocol must be http or https")
	}
	c.AdvertiseProtocol = protocol

	if c.AdvertisePort < 1 || c.AdvertisePort > 65535 {
		return fmt.Errorf("validate config: advertise port must be between 1 and 65535")
	}
	if _, _, err := net.SplitHostPort(normalizeListenAddr(c.ListenAddr)); err != nil {
		return fmt.Errorf("validate config: listen address: %w", err)
	}
	return nil
}

// Run starts the local mock backend, registers it with the gateway, and
// maintains heartbeats until the context is canceled.
func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	handler := newHandler(cfg)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		logger.Info("starting mock MCP backend",
			slog.String("listen_addr", cfg.ListenAddr),
			slog.String("gateway_url", cfg.GatewayURL))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	client, err := newGatewayClient(cfg)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()

	instanceID, err := keepRegistered(ctx, client, cfg, logger)
	if err != nil {
		_ = shutdownServer(server, cfg.ShutdownTimeout)
		return err
	}

	<-ctx.Done()

	deregisterCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := deregister(deregisterCtx, client, cfg.GatewayURL, instanceID); err != nil {
		logger.Warn("mock backend deregistration failed", slog.String("error", err.Error()))
	}
	return shutdownServer(server, cfg.ShutdownTimeout)
}

func normalizeListenAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}

func newHandler(cfg Config) http.Handler {
	mcpSrv := mcpserver.NewMCPServer(
		cfg.ServiceName,
		cfg.ServiceVersion,
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(true, true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("tool.echo", mcp.WithDescription("Returns a static mock response"), mcp.WithString("message")),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("echo-response"), nil
		},
	)
	mcpSrv.AddResource(
		mcp.NewResource(
			"resource://mock-backend/readme",
			"Mock backend readme",
			mcp.WithResourceDescription("Local development resource"),
			mcp.WithMIMEType("text/plain"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      req.Params.URI,
					MIMEType: "text/plain",
					Text:     "local mock backend resource",
				},
			}, nil
		},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.NewStreamableHTTPServer(mcpSrv))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	return mux
}

func newGatewayClient(cfg Config) (*http.Client, error) {
	tlsCfg, err := identity.BuildClientTLSConfig(identity.TLSConfig{
		CABundlePath: cfg.TLSCAPath,
		CertPath:     cfg.TLSCertPath,
		KeyPath:      cfg.TLSKeyPath,
	})
	if err != nil {
		return nil, fmt.Errorf("build gateway client tls config: %w", err)
	}
	tlsCfg.MinVersion = tls.VersionTLS13
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}, nil
}

func keepRegistered(ctx context.Context, client *http.Client, cfg Config, logger *slog.Logger) (string, error) {
	for {
		if err := waitForGatewayReady(ctx, client, cfg); err != nil {
			return "", err
		}

		instanceID, heartbeatInterval, err := register(ctx, client, cfg)
		if err != nil {
			logger.Warn("mock backend registration failed", slog.String("error", err.Error()))
			if err := sleepWithContext(ctx, cfg.RetryInterval); err != nil {
				return "", err
			}
			continue
		}

		logger.Info("mock backend registered",
			slog.String("instance_id", instanceID),
			slog.Duration("heartbeat_interval", heartbeatInterval))

		for {
			if err := sleepWithContext(ctx, heartbeatInterval); err != nil {
				return instanceID, nil
			}
			capHash := computeCapabilitiesHash([]string{"tool.echo"}, []string{"resource://mock-backend/"})
			if err := heartbeat(ctx, client, cfg.GatewayURL, instanceID, capHash); err != nil {
				logger.Warn("mock backend heartbeat failed", slog.String("instance_id", instanceID), slog.String("error", err.Error()))
				break
			}
		}

		if err := sleepWithContext(ctx, cfg.RetryInterval); err != nil {
			return "", err
		}
	}
}

func waitForGatewayReady(ctx context.Context, client *http.Client, cfg Config) error {
	readyURL := strings.TrimRight(cfg.GatewayURL, "/") + "/readyz"
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
		if err != nil {
			return fmt.Errorf("build readiness request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if err := sleepWithContext(ctx, cfg.ReadyPollInterval); err != nil {
			return err
		}
	}
}

func register(ctx context.Context, client *http.Client, cfg Config) (string, time.Duration, error) {
	requestBody := registration.RegistrationRequest{
		ServiceName: cfg.ServiceName,
		Version:     cfg.ServiceVersion,
		Listen: registration.ListenConfig{
			Host:     cfg.AdvertiseHost,
			Port:     cfg.AdvertisePort,
			Protocol: cfg.AdvertiseProtocol,
		},
		Capabilities: types.Capability{
			Tools:     []string{"tool.echo"},
			Resources: []string{"resource://mock-backend/"},
		},
		Labels: map[string]string{
			"env": "local-dev",
		},
	}

	response := registration.RegistrationResponse{}
	if err := doJSON(ctx, client, http.MethodPost, strings.TrimRight(cfg.GatewayURL, "/")+"/v1/registrations", requestBody, &response); err != nil {
		return "", 0, err
	}
	if strings.TrimSpace(response.InstanceID) == "" {
		return "", 0, fmt.Errorf("register backend: gateway returned an empty instance id")
	}
	interval := time.Duration(response.HeartbeatIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(response.HeartbeatInterval) * time.Second
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return response.InstanceID, interval, nil
}

func heartbeat(ctx context.Context, client *http.Client, gatewayURL string, instanceID string, capabilitiesHash string) error {
	payload := map[string]any{}
	if capabilitiesHash != "" {
		payload["capabilities_hash"] = capabilitiesHash
	}
	return doJSON(ctx, client, http.MethodPost, strings.TrimRight(gatewayURL, "/")+"/v1/registrations/"+instanceID+"/heartbeat", payload, nil)
}

func computeCapabilitiesHash(tools []string, resources []string) string {
	h := sha256.New()
	data, _ := json.Marshal(map[string]any{"tools": tools, "resources": resources})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func deregister(ctx context.Context, client *http.Client, gatewayURL string, instanceID string) error {
	return doJSON(ctx, client, http.MethodDelete, strings.TrimRight(gatewayURL, "/")+"/v1/registrations/"+instanceID, nil, nil)
}

func doJSON(ctx context.Context, client *http.Client, method string, endpoint string, requestBody any, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("%s %s: marshal request: %w", method, endpoint, err)
		}
		body = strings.NewReader(string(payload))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("%s %s: build request: %w", method, endpoint, err)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: unexpected status %d: %s", method, endpoint, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	if responseBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(responseBody); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, endpoint, err)
	}
	return nil
}

func shutdownServer(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return server.Shutdown(ctx)
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// LoadConfigFromEnv reads mock backend configuration from environment variables.
func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		GatewayURL:        strings.TrimSpace(os.Getenv("MOCK_BACKEND_GATEWAY_URL")),
		ListenAddr:        strings.TrimSpace(os.Getenv("MOCK_BACKEND_LISTEN_ADDR")),
		AdvertiseHost:     strings.TrimSpace(os.Getenv("MOCK_BACKEND_ADVERTISE_HOST")),
		AdvertiseProtocol: strings.TrimSpace(os.Getenv("MOCK_BACKEND_ADVERTISE_PROTOCOL")),
		ServiceName:       strings.TrimSpace(os.Getenv("MOCK_BACKEND_NAME")),
		ServiceVersion:    strings.TrimSpace(os.Getenv("MOCK_BACKEND_VERSION")),
		TLSCAPath:         strings.TrimSpace(os.Getenv("MOCK_BACKEND_TLS_CA")),
		TLSCertPath:       strings.TrimSpace(os.Getenv("MOCK_BACKEND_TLS_CERT")),
		TLSKeyPath:        strings.TrimSpace(os.Getenv("MOCK_BACKEND_TLS_KEY")),
	}
	if rawPort := strings.TrimSpace(os.Getenv("MOCK_BACKEND_ADVERTISE_PORT")); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil {
			return Config{}, fmt.Errorf("parse MOCK_BACKEND_ADVERTISE_PORT: %w", err)
		}
		cfg.AdvertisePort = port
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
