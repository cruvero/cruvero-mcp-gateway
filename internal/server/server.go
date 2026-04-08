package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/policy"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
)

// Server wraps the gateway HTTP server and router.
type Server struct {
	cfg             *config.Config
	logger          *slog.Logger
	router          chi.Router
	httpServer      *http.Server
	metricsSrv      *http.Server
	startTime       time.Time
	ready           atomic.Bool
	shutdownTimeout time.Duration

	policyEngine        *policy.Engine
	rateLimitBackend    ratelimit.LimiterBackend
	profileResolver     ratelimit.ProfileResolver
	proxyAuthMW         func(http.Handler) http.Handler
	proxyPolicyMW       func(http.Handler) http.Handler
	serverScopeMW       func(http.Handler) http.Handler
	cleanupInterval     time.Duration
	cleanupMaxIdleTTL   time.Duration
	eventsClient        *events.Client
	eventSubscriber     *events.Subscriber
	eventPublisher      *events.Publisher
	degradation         *events.DegradationManager
	serverCfgHandler    *events.ServerConfigHandler
	settingsHandler     *events.ServerSettingsConfigHandler
	ackHandler          *events.ServerRegisteredAckHandler
	toolMetadataHandler *events.ToolMetadataConfigHandler
	toolCountFunc       func() int
	searchStatusFunc    func() SearchStatus
	buildVersion        string
	metaLimiter         *metaRateLimiter
}

var currentServerVersion atomic.Value

func init() {
	currentServerVersion.Store("dev")
}

// SetServerVersion configures the build version reported by metadata endpoints.
func SetServerVersion(version string) {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		trimmed = "dev"
	}
	currentServerVersion.Store(trimmed)
}

func serverVersion() string {
	v, _ := currentServerVersion.Load().(string)
	if strings.TrimSpace(v) == "" {
		return "dev"
	}
	return v
}

// SearchStatus captures current search subsystem health and fallback counters.
type SearchStatus struct {
	Engine                string
	Embedder              string
	IndexedTools          int
	Ready                 bool
	VectorReady           *bool
	VectorSearchFallbacks uint64
	VectorIndexFallbacks  uint64
	EngineErrorFallbacks  uint64
}

// New builds a configured HTTP server with middleware and routes.
// When db is non-nil, a PostgresConfigStore is created for DegradationManager.
func New(cfg *config.Config, logger *slog.Logger, db *sql.DB) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	var configStore events.ConfigStore
	if db != nil {
		configStore = events.NewPostgresConfigStore(db)
	}

	router := newBaseRouter(cfg, logger)
	srv := &Server{
		cfg:             cfg,
		logger:          logger,
		router:          router,
		startTime:       time.Now().UTC(),
		shutdownTimeout: resolveShutdownTimeout(cfg),
		buildVersion:    serverVersion(),
		metaLimiter:     newMetaRateLimiter(10, 10, 10000),

		cleanupInterval:   time.Minute,
		cleanupMaxIdleTTL: 5 * time.Minute,
	}
	srv.initPolicyAndRateLimit(cfg, logger)
	srv.initNATS(cfg, configStore, logger)
	srv.initDegradationFallback(cfg, configStore, logger)

	ratelimit.SetRateLimitedObserver(func(clientID string, route string) {
		ObserveRateLimited(clientID, route)
	})
	policy.SetDeniedObserver(func(reason string, tool string) {
		ObservePolicyDenied(reason, tool)
	})

	srv.proxyPolicyMW = policy.PolicyMiddleware(srv.policyEngine, logger)

	srv.ready.Store(true)
	srv.setupRoutes()
	srv.httpServer = newHTTPServer(cfg, router)
	srv.metricsSrv = StartMetricsServer(resolveMetricsAddr(cfg))

	return srv
}

func newBaseRouter(cfg *config.Config, logger *slog.Logger) chi.Router {
	router := chi.NewRouter()
	router.Use(RequestIDMiddleware)
	router.Use(TracingMiddleware(otel.Tracer("mcpgw/server")))
	router.Use(MetricsMiddleware)
	router.Use(LoggingMiddleware(logger))
	router.Use(RecoveryMiddleware(logger))
	router.Use(RequestBodyLimitMiddleware(defaultMaxRequestBodyBytes))
	if cfg != nil && cfg.CORSEnabled {
		router.Use(CORSMiddleware(cfg.CORSAllowedOrigins))
	}
	return router
}

func resolveShutdownTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.ShutdownTimeout > 0 {
		return cfg.ShutdownTimeout
	}
	return 30 * time.Second
}

func (s *Server) initPolicyAndRateLimit(cfg *config.Config, logger *slog.Logger) {
	profiles := defaultProfiles(cfg)
	defaultProfile := profiles["default"]
	mb := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	mb.SetDefaults(float64(defaultProfile.RateLimit), defaultProfile.RateBurst)
	s.rateLimitBackend = mb
	s.profileResolver = ratelimit.NewDefaultProfileResolver(profiles, defaultProfile)
	s.policyEngine = policy.NewEngine(profiles, nil, logger)
}

func (s *Server) initNATS(cfg *config.Config, configStore events.ConfigStore, logger *slog.Logger) {
	if cfg == nil || !cfg.CruveroEnabled || strings.TrimSpace(cfg.NATSURL) == "" {
		SetNATSConnected(false)
		return
	}
	clientOpts, ok := buildNATSClientOpts(cfg, logger)
	if !ok {
		SetNATSConnected(false)
		return
	}
	natsClient, err := events.NewClient(cfg.NATSURL, cfg.GatewayID, clientOpts...)
	if err != nil {
		logger.Warn("events client init failed", slog.String("error", err.Error()))
		SetNATSConnected(false)
		return
	}
	s.eventsClient = natsClient
	s.eventPublisher = events.NewPublisher(natsClient, cfg.GatewayID, logger)
	s.policyEngine.SetViolationEventPublisher(s.eventPublisher)
	SetNATSConnected(true)

	s.initEventSubscriber(cfg, natsClient, configStore, logger)
	s.initDegradationHandlers(natsClient, configStore, logger)
}

func buildNATSClientOpts(cfg *config.Config, logger *slog.Logger) ([]events.ClientOption, bool) {
	var clientOpts []events.ClientOption
	if !cfg.NATSTLSEnabled {
		return clientOpts, true
	}
	tlsConfig, tlsErr := buildNATSTLSConfig(cfg.NATSTLSCert, cfg.NATSTLSKey, cfg.NATSTLSCa)
	if tlsErr != nil {
		logger.Error("nats tls config failed; skipping nats connection", slog.String("error", tlsErr.Error()))
		return nil, false
	}
	return append(clientOpts, events.WithTLS(tlsConfig)), true
}

func (s *Server) initEventSubscriber(cfg *config.Config, natsClient *events.Client, configStore events.ConfigStore, logger *slog.Logger) {
	subscriber := events.NewSubscriber(natsClient, logger)
	policyHandler := events.NewPolicyConfigHandler(s.policyEngine, s.rateLimitBackend, logger)
	serverHandler := events.NewServerConfigHandler(nil, logger)
	serverSettings := events.NewServerSettingsConfigHandler(nil, nil, logger)
	serverSettingsHandler := &metricsServerSettingsHandler{
		next: serverSettings,
	}
	authHandler := events.NewAuthConfigHandler(logger)
	ackHandler := events.NewServerRegisteredAckHandler(nil, logger)
	toolMetadataHandler := events.NewToolMetadataConfigHandler(configStore, nil, logger)
	subscriber.RegisterGatewaySubjects(policyHandler, serverHandler, serverSettingsHandler, authHandler)
	subscriber.RegisterHandler(events.SubjectForAck(cfg.GatewayID, events.AckScopeServerRegistered), ackHandler)
	subscriber.RegisterHandler(events.SubjectForConfig(cfg.GatewayID, events.ConfigScopeToolMetadata), toolMetadataHandler)
	if startErr := subscriber.Start(context.Background()); startErr != nil {
		logger.Warn("events subscriber start failed", slog.String("error", startErr.Error()))
		return
	}
	s.eventSubscriber = subscriber
	s.serverCfgHandler = serverHandler
	s.settingsHandler = serverSettings
	s.ackHandler = ackHandler
	s.toolMetadataHandler = toolMetadataHandler
}

func (s *Server) initDegradationHandlers(natsClient *events.Client, configStore events.ConfigStore, logger *slog.Logger) {
	degradation := events.NewDegradationManager(natsClient, configStore, s.eventSubscriber, logger)
	natsClient.SetDisconnectHandler(func() {
		SetNATSConnected(false)
		degradation.OnDisconnect()
	})
	natsClient.SetReconnectHandler(func() {
		SetNATSConnected(true)
		if reconnectErr := degradation.OnReconnect(context.Background()); reconnectErr != nil {
			logger.Warn("degradation reconnect handler failed", slog.String("error", reconnectErr.Error()))
		}
	})
	s.degradation = degradation
}

func (s *Server) initDegradationFallback(cfg *config.Config, configStore events.ConfigStore, logger *slog.Logger) {
	if cfg == nil || !cfg.CruveroEnabled || s.degradation != nil {
		return
	}
	s.degradation = events.NewDegradationManager(nil, configStore, nil, logger)
	if s.degradation == nil || s.degradation.EverConnected() {
		return
	}
	if err := s.degradation.LoadCachedConfig(context.Background()); err != nil {
		logger.Warn("failed to load cached config from postgres", slog.String("error", err.Error()))
	} else if s.degradation.HasCachedConfig() {
		logger.Info("loaded cached config from postgres; running in degraded mode")
	}
}

func newHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	listenAddr := ":8443"
	if cfg != nil && cfg.ListenAddr != "" {
		listenAddr = cfg.ListenAddr
	}
	return &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func resolveMetricsAddr(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.MetricsAddr) != "" {
		return cfg.MetricsAddr
	}
	return ":9090"
}

// Start starts the HTTP server and shuts it down gracefully when the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("start server: server is nil")
	}
	if ctx == nil {
		return fmt.Errorf("start server: context is nil")
	}

	defer s.closeEventInfrastructure()
	s.startMetricsServer()

	shutdownErrCh := s.startShutdownWatcher(ctx)

	serveErr := s.listenAndServe()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("start server: %w", serveErr)
	}

	select {
	case shutdownErr := <-shutdownErrCh:
		if shutdownErr != nil {
			return fmt.Errorf("start server: shutdown: %w", shutdownErr)
		}
	default:
	}

	return nil
}

func (s *Server) closeEventInfrastructure() {
	if s.eventSubscriber != nil {
		_ = s.eventSubscriber.Stop()
	}
	if s.eventsClient != nil {
		_ = s.eventsClient.Close()
	}
}

func (s *Server) startMetricsServer() {
	if s.metricsSrv == nil {
		return
	}
	go func() {
		if err := s.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("metrics server failed", slog.String("error", err.Error()))
		}
	}()
}

func (s *Server) startShutdownWatcher(ctx context.Context) <-chan error {
	shutdownErrCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
		defer cancel()
		if s.metricsSrv != nil {
			if err := s.metricsSrv.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("metrics shutdown failed", slog.String("error", err.Error()))
			}
		}
		shutdownErrCh <- s.httpServer.Shutdown(shutdownCtx)
	}()
	return shutdownErrCh
}

func (s *Server) listenAndServe() error {
	if s.cfg != nil && s.cfg.IsTLSConfigured() {
		tlsCfg, err := buildInboundTLSConfig(s.cfg)
		if err != nil {
			return fmt.Errorf("start server: build inbound tls config: %w", err)
		}
		s.httpServer.TLSConfig = tlsCfg
		return s.httpServer.ListenAndServeTLS("", "")
	}
	return s.httpServer.ListenAndServe()
}

func buildInboundTLSConfig(cfg *config.Config) (*tls.Config, error) {
	if cfg == nil || !cfg.IsTLSConfigured() {
		return nil, nil
	}
	if strings.TrimSpace(cfg.TLSCAPath) == "" {
		return nil, fmt.Errorf("MCPGW_TLS_CA is required when TLS is enabled")
	}

	tlsCfg, err := identity.BuildServerTLSConfig(identity.TLSConfig{
		CertPath:     cfg.TLSCertPath,
		KeyPath:      cfg.TLSKeyPath,
		ClientCAPath: cfg.TLSCAPath,
	})
	if err != nil {
		return nil, err
	}

	// Keep health probes and non-mTLS routes reachable while still verifying
	// client certs when provided so registration routes can enforce mTLS identity.
	tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
	return tlsCfg, nil
}

func buildNATSTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load nats tls cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read nats tls ca: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse nats tls ca")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// MountRegistrationRoutes mounts registration routes under /v1/registrations with mTLS identity middleware.
func (s *Server) MountRegistrationRoutes(registrationHandler http.Handler) {
	if s == nil || registrationHandler == nil {
		return
	}

	allowedPrefixes := []string{}
	if s.cfg != nil {
		allowedPrefixes = s.cfg.SPIFFEAllowList
	}

	s.router.Route("/v1/registrations", func(r chi.Router) {
		r.Use(identity.MTLSMiddleware(allowedPrefixes, s.logger))
		r.Mount("/", registrationHandler)
	})
}

// MountProxyRoutes mounts the client-facing MCP proxy endpoint at /mcp.
func (s *Server) MountProxyRoutes(proxyHandler http.Handler) {
	if s == nil || proxyHandler == nil {
		return
	}

	s.router.Route("/mcp", func(r chi.Router) {
		if s.proxyAuthMW != nil {
			r.Use(s.proxyAuthMW)
		}
		if s.serverScopeMW != nil {
			r.Use(s.serverScopeMW)
		}
		r.Use(ratelimit.RateLimitMiddleware(s.rateLimitBackend, s.profileResolver, s.logger))
		if s.proxyPolicyMW != nil {
			r.Use(s.proxyPolicyMW)
		}
		r.Mount("/", proxyHandler)
	})
}

// MountPerServerRoutes mounts per-server MCP proxy endpoints at
// /mcp/servers/{serverName}/ with the same auth/rate-limit/policy middleware
// chain used by the unified /mcp endpoint.
func (s *Server) MountPerServerRoutes(handler http.Handler) {
	if s == nil || handler == nil {
		return
	}

	s.router.Route("/mcp/servers/{serverName}", func(r chi.Router) {
		if s.proxyAuthMW != nil {
			r.Use(s.proxyAuthMW)
		}
		if s.serverScopeMW != nil {
			r.Use(s.serverScopeMW)
		}
		r.Use(ratelimit.RateLimitMiddleware(s.rateLimitBackend, s.profileResolver, s.logger))
		if s.proxyPolicyMW != nil {
			r.Use(s.proxyPolicyMW)
		}
		r.HandleFunc("/*", handler.ServeHTTP)
		r.HandleFunc("/", handler.ServeHTTP)
	})
}

// MountAdmin mounts the admin dashboard router under /admin/.
func (s *Server) MountAdmin(adminRouter chi.Router) {
	if s == nil || adminRouter == nil {
		return
	}
	s.router.Mount("/admin", adminRouter)
}

// RateLimitBackend returns the server's rate limit backend for sharing with admin.
func (s *Server) RateLimitBackend() ratelimit.LimiterBackend {
	if s == nil {
		return nil
	}
	return s.rateLimitBackend
}

// MountCatalog mounts the platform tool catalog endpoint under
// /admin/api/tools/catalog with mTLS authentication restricted to the given
// SPIFFE ID prefixes. When platformPrefixes is empty the endpoint is not
// mounted (secure-by-default).
func (s *Server) MountCatalog(handler http.Handler, platformPrefixes []string) {
	if s == nil || handler == nil || len(platformPrefixes) == 0 {
		return
	}
	s.router.Route("/admin/api/tools/catalog", func(r chi.Router) {
		r.Use(identity.MTLSMiddleware(platformPrefixes, s.logger))
		r.Mount("/", handler)
	})
}

// MountDeviceFlowRoutes mounts device code flow endpoints under /device.
func (s *Server) MountDeviceFlowRoutes(handler http.Handler) {
	if s == nil || handler == nil {
		return
	}
	s.router.Mount("/device", handler)
}

// MountAPIKeyAPI mounts the API key management endpoints under /v1/apikeys
// with the same auth middleware used for the proxy routes.
func (s *Server) MountAPIKeyAPI(handler http.Handler) {
	if s == nil || handler == nil {
		return
	}
	s.router.Route("/v1/apikeys", func(r chi.Router) {
		if s.proxyAuthMW != nil {
			r.Use(s.proxyAuthMW)
		}
		r.Mount("/", handler)
	})
}

// MountDiscoveryRoutes mounts the well-known discovery document and server
// listing endpoints. The discovery doc at /.well-known/mcp.json is only
// mounted when cfg.WellKnownEnabled is true. The server list at /mcp/servers
// is always mounted when a non-nil handler is provided.
func (s *Server) MountDiscoveryRoutes(discoveryDoc http.Handler, serverList http.Handler) {
	if s == nil {
		return
	}

	if discoveryDoc != nil && s.cfg != nil && s.cfg.WellKnownEnabled {
		s.router.Method(http.MethodGet, "/.well-known/mcp.json", discoveryDoc)
	}

	if serverList != nil {
		s.router.Method(http.MethodGet, "/mcp/servers", serverList)
	}
}

// SetRateLimitBackend replaces the default memory rate-limit backend.
// Call before MountProxyRoutes.
func (s *Server) SetRateLimitBackend(backend ratelimit.LimiterBackend) {
	if s == nil || backend == nil {
		return
	}
	s.rateLimitBackend = backend
}

// SetProxyAuthMiddleware configures auth middleware for the /mcp chain.
func (s *Server) SetProxyAuthMiddleware(middleware func(http.Handler) http.Handler) {
	if s == nil {
		return
	}
	s.proxyAuthMW = middleware
}

// SetServerScopeMiddleware configures server scope enforcement middleware for
// the /mcp chain. It runs after auth and before rate-limiting/policy.
func (s *Server) SetServerScopeMiddleware(middleware func(http.Handler) http.Handler) {
	if s == nil {
		return
	}
	s.serverScopeMW = middleware
}

// SetProxyPolicyMiddleware configures policy middleware for the /mcp chain.
func (s *Server) SetProxyPolicyMiddleware(middleware func(http.Handler) http.Handler) {
	if s == nil {
		return
	}
	s.proxyPolicyMW = middleware
}

// EventsClient returns the NATS events client, if configured.
func (s *Server) EventsClient() *events.Client {
	if s == nil {
		return nil
	}
	return s.eventsClient
}

// EventPublisher returns the configured events publisher, if NATS is enabled.
func (s *Server) EventPublisher() *events.Publisher {
	if s == nil {
		return nil
	}
	return s.eventPublisher
}

// BindRegistrationService wires registration-aware config and ack handlers.
func (s *Server) BindRegistrationService(registrationService interface {
	UpdateSPIFFEAllowList(prefixes []string)
	UpdateEffectiveSettings(configVersion int64, settingsByServer map[string]map[string]any) error
	AcknowledgeServerRegistration(
		ctx context.Context,
		registrationID string,
		leaseEpoch int64,
		capabilityHash string,
		registryVersion string,
		toolSchemaHash string,
		ackedAt time.Time,
	) error
}) {
	if s == nil || registrationService == nil {
		return
	}
	if s.serverCfgHandler != nil {
		s.serverCfgHandler.SetRegistrationService(registrationService)
	}
	if s.settingsHandler != nil {
		s.settingsHandler.SetRegistrationService(registrationService)
	}
	if s.ackHandler != nil {
		s.ackHandler.SetRegistrationService(registrationService)
	}
}

// Handler returns the root HTTP handler for testing and embedding.
func (s *Server) Handler() http.Handler {
	if s == nil {
		return http.NotFoundHandler()
	}
	return s.router
}

func (s *Server) setupRoutes() {
	s.router.Get("/healthz", s.handleHealth)
	s.router.Get("/readyz", s.handleReady)
	s.router.Get("/meta", s.handleMeta)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}

	if s.cfg != nil && s.cfg.CruveroEnabled {
		natsStatus := "disconnected"
		status := "degraded"
		everConnected := false
		hasCachedConfig := false
		settingsSyncStatus := "unknown"
		settingsConfigVersion := int64(0)

		if s.degradation != nil {
			switch s.degradation.Status() {
			case events.DegradationStatusConnected:
				status = "ok"
				natsStatus = "connected"
			case events.DegradationStatusDegraded:
				status = "degraded"
			case events.DegradationStatusDisconnected:
				status = "degraded"
			}
			everConnected = s.degradation.EverConnected()
			hasCachedConfig = s.degradation.HasCachedConfig()
			settingsSyncStatus = s.degradation.SettingsSyncStatus()
			settingsConfigVersion = s.degradation.SettingsConfigVersion()
		}

		payload := map[string]any{
			"status": status,
			"nats":   natsStatus,
		}
		if settingsSyncStatus != "" {
			payload["settings_sync_status"] = settingsSyncStatus
		}
		if settingsConfigVersion > 0 {
			payload["settings_config_version"] = settingsConfigVersion
		}
		if s.toolCountFunc != nil {
			payload["active_tools"] = s.toolCountFunc()
		}
		s.addSearchStatus(payload)

		if !everConnected && !hasCachedConfig {
			payload["status"] = "not_ready"
			writeJSON(w, http.StatusServiceUnavailable, payload)
			return
		}

		writeJSON(w, http.StatusOK, payload)
		return
	}

	payload := map[string]any{"status": "ok"}
	if s.toolCountFunc != nil {
		payload["active_tools"] = s.toolCountFunc()
	}
	s.addSearchStatus(payload)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) addSearchStatus(payload map[string]any) {
	if s.searchStatusFunc == nil {
		return
	}
	searchStatus := s.searchStatusFunc()
	status := "ok"
	if !searchStatus.Ready {
		status = "degraded"
	}

	searchPayload := map[string]any{
		"engine": searchStatus.Engine,
		"status": status,
	}
	if searchStatus.Embedder != "" {
		searchPayload["embedder"] = searchStatus.Embedder
	}
	if searchStatus.IndexedTools > 0 {
		searchPayload["indexed_tools"] = searchStatus.IndexedTools
	}
	if searchStatus.VectorReady != nil {
		vectorStatus := "ok"
		if !*searchStatus.VectorReady {
			vectorStatus = "degraded"
		}
		searchPayload["vector_status"] = vectorStatus
	}
	if searchStatus.VectorSearchFallbacks > 0 || searchStatus.VectorIndexFallbacks > 0 || searchStatus.EngineErrorFallbacks > 0 {
		searchPayload["fallbacks"] = map[string]uint64{
			"vector_search": searchStatus.VectorSearchFallbacks,
			"vector_index":  searchStatus.VectorIndexFallbacks,
			"engine_error":  searchStatus.EngineErrorFallbacks,
		}
	}
	payload["search"] = searchPayload
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if s.metaLimiter != nil {
		if !s.metaLimiter.Allow(clientIPFromRequest(r)) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"status": "rate_limited",
			})
			return
		}
	}

	mode := "standalone"
	gatewayID := ""
	if s.cfg != nil {
		if strings.TrimSpace(s.cfg.AdminMode) == "integrated" {
			mode = "integrated"
		}
		gatewayID = strings.TrimSpace(s.cfg.GatewayID)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"admin_mode": mode,
		"version":    s.buildVersion,
		"gateway_id": gatewayID,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return "unknown"
	}

	remoteIP := parseIPToken(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = "unknown"
	}

	if remoteIP != "unknown" && isTrustedProxyIP(remoteIP) {
		if forwarded := firstForwardedIP(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
		if realIP := parseIPToken(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}

	return remoteIP
}

func parseIPToken(raw string) string {
	token := strings.TrimSpace(raw)
	if token == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(token); err == nil {
		token = host
	}
	token = strings.Trim(strings.TrimSpace(token), "[]")
	ip := net.ParseIP(token)
	if ip == nil {
		return ""
	}
	return ip.String()
}

func firstForwardedIP(forwardedFor string) string {
	for _, token := range strings.Split(forwardedFor, ",") {
		if ip := parseIPToken(token); ip != "" {
			return ip
		}
	}
	return ""
}

func isTrustedProxyIP(ipRaw string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipRaw))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

type metaRateLimiter struct {
	rate       float64
	burst      float64
	maxEntries int
	mu         sync.Mutex
	tokens     map[string]float64
	last       map[string]time.Time
}

func newMetaRateLimiter(ratePerSecond float64, burst float64, maxEntries int) *metaRateLimiter {
	if ratePerSecond <= 0 || burst <= 0 || maxEntries <= 0 {
		return nil
	}
	return &metaRateLimiter{
		rate:       ratePerSecond,
		burst:      burst,
		maxEntries: maxEntries,
		tokens:     make(map[string]float64),
		last:       make(map[string]time.Time),
	}
}

func (l *metaRateLimiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	if strings.TrimSpace(key) == "" {
		key = "unknown"
	}

	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	prev, exists := l.last[key]
	if !exists && len(l.last) >= l.maxEntries {
		l.evictOldestLocked()
	}

	tokens := l.tokens[key]
	if prev.IsZero() {
		tokens = l.burst
	} else {
		elapsed := now.Sub(prev).Seconds()
		tokens = minFloat(l.burst, tokens+elapsed*l.rate)
	}

	if tokens < 1 {
		l.tokens[key] = tokens
		l.last[key] = now
		return false
	}

	l.tokens[key] = tokens - 1
	l.last[key] = now
	return true
}

func (l *metaRateLimiter) evictOldestLocked() {
	oldestKey := ""
	var oldestTime time.Time
	for key, ts := range l.last {
		if oldestKey == "" || ts.Before(oldestTime) {
			oldestKey = key
			oldestTime = ts
		}
	}
	if oldestKey == "" {
		return
	}
	delete(l.last, oldestKey)
	delete(l.tokens, oldestKey)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func defaultPolicyProfile(cfg *config.Config) *types.PolicyProfile {
	rateDefault := 10
	rateBurst := 20
	if cfg != nil {
		if cfg.RateDefault > 0 {
			rateDefault = cfg.RateDefault
		}
		if cfg.RateBurst > 0 {
			rateBurst = cfg.RateBurst
		}
	}

	return &types.PolicyProfile{
		Name:            "default",
		RateLimit:       rateDefault,
		RateBurst:       rateBurst,
		ToolAllowlist:   []string{},
		ToolDenylist:    []string{},
		EnforcementMode: types.ModeEnforce,
	}
}

func defaultProfiles(cfg *config.Config) map[string]*types.PolicyProfile {
	defaultProfile := defaultPolicyProfile(cfg)
	premium := &types.PolicyProfile{
		Name:            "premium",
		RateLimit:       50,
		RateBurst:       100,
		ToolAllowlist:   []string{},
		ToolDenylist:    []string{},
		EnforcementMode: types.ModeEnforce,
	}
	admin := &types.PolicyProfile{
		Name:            "admin",
		RateLimit:       100,
		RateBurst:       200,
		ToolAllowlist:   []string{},
		ToolDenylist:    []string{},
		EnforcementMode: types.ModeEnforce,
	}

	return map[string]*types.PolicyProfile{
		"default": defaultProfile,
		"premium": premium,
		"admin":   admin,
	}
}

// SetAuditStore wires the audit store into the policy engine for decision logging.
func (s *Server) SetAuditStore(auditStore store.AuditStore) {
	if s == nil || s.policyEngine == nil {
		return
	}
	s.policyEngine.SetAuditStore(auditStore)
}

// SetClassificationStore wires the tool classification store into the policy engine.
func (s *Server) SetClassificationStore(classificationStore store.ToolClassificationStore) {
	if s == nil || s.policyEngine == nil {
		return
	}
	s.policyEngine.SetClassificationStore(classificationStore)
}

// PolicyEngine returns the server's policy engine instance.
func (s *Server) PolicyEngine() *policy.Engine {
	if s == nil {
		return nil
	}
	return s.policyEngine
}

// SetDegradationManager overrides server degradation state integration.
func (s *Server) SetDegradationManager(manager *events.DegradationManager) {
	if s == nil {
		return
	}
	s.degradation = manager
}

// SetToolCountFunc sets a callback that returns the current active tool count.
func (s *Server) SetToolCountFunc(fn func() int) {
	if s == nil {
		return
	}
	s.toolCountFunc = fn
}

// SetSearchReadyFunc wires a function that reports the search engine type and
// readiness status for the readyz endpoint.
func (s *Server) SetSearchReadyFunc(fn func() (string, bool)) {
	if s == nil || fn == nil {
		return
	}
	s.searchStatusFunc = func() SearchStatus {
		engineType, ready := fn()
		return SearchStatus{
			Engine: engineType,
			Ready:  ready,
		}
	}
}

// SetSearchStatusFunc wires a function that reports detailed search status
// for the readyz endpoint.
func (s *Server) SetSearchStatusFunc(fn func() SearchStatus) {
	if s == nil {
		return
	}
	s.searchStatusFunc = fn
}

// ToolMetadataHandler returns the tool metadata config handler for late-binding.
func (s *Server) ToolMetadataHandler() *events.ToolMetadataConfigHandler {
	if s == nil {
		return nil
	}
	return s.toolMetadataHandler
}
