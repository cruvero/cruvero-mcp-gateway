package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
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

	policyEngine     *policy.Engine
	rateLimitBackend ratelimit.LimiterBackend
	profileResolver  ratelimit.ProfileResolver
	proxyAuthMW       func(http.Handler) http.Handler
	proxyPolicyMW     func(http.Handler) http.Handler
	cleanupInterval   time.Duration
	cleanupMaxIdleTTL time.Duration
	eventsClient      *events.Client
	eventSubscriber   *events.Subscriber
	eventPublisher    *events.Publisher
	degradation       *events.DegradationManager
	serverCfgHandler  *events.ServerConfigHandler
	settingsHandler   *events.ServerSettingsConfigHandler
	ackHandler        *events.ServerRegisteredAckHandler
}

// New builds a configured HTTP server with middleware and routes.
func New(cfg *config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

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

	shutdownTimeout := 30 * time.Second
	if cfg != nil && cfg.ShutdownTimeout > 0 {
		shutdownTimeout = cfg.ShutdownTimeout
	}

	srv := &Server{
		cfg:             cfg,
		logger:          logger,
		router:          router,
		startTime:       time.Now().UTC(),
		shutdownTimeout: shutdownTimeout,

		cleanupInterval:   time.Minute,
		cleanupMaxIdleTTL: 5 * time.Minute,
	}
	profiles := defaultProfiles(cfg)
	defaultProfile := profiles["default"]
	mb := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	mb.SetDefaults(float64(defaultProfile.RateLimit), defaultProfile.RateBurst)
	srv.rateLimitBackend = mb
	srv.profileResolver = ratelimit.NewDefaultProfileResolver(profiles, defaultProfile)
	policyEngine := policy.NewEngine(profiles, nil, logger)
	srv.policyEngine = policyEngine

	if cfg != nil && cfg.CruveroEnabled && strings.TrimSpace(cfg.NATSURL) != "" {
		natsClient, err := events.NewClient(cfg.NATSURL, cfg.GatewayID)
		if err != nil {
			logger.Warn("events client init failed", slog.String("error", err.Error()))
		} else {
			srv.eventsClient = natsClient
			srv.eventPublisher = events.NewPublisher(natsClient, cfg.GatewayID, logger)
			policyEngine.SetViolationEventPublisher(srv.eventPublisher)
			SetNATSConnected(true)

			subscriber := events.NewSubscriber(natsClient, logger)
			policyHandler := events.NewPolicyConfigHandler(policyEngine, srv.rateLimitBackend, logger)
			serverHandler := events.NewServerConfigHandler(nil, logger)
			serverSettings := events.NewServerSettingsConfigHandler(nil, nil, logger)
			serverSettingsHandler := &metricsServerSettingsHandler{
				next: serverSettings,
			}
			authHandler := events.NewAuthConfigHandler(logger)
			ackHandler := events.NewServerRegisteredAckHandler(nil, logger)
			subscriber.RegisterGatewaySubjects(policyHandler, serverHandler, serverSettingsHandler, authHandler)
			subscriber.RegisterHandler(events.SubjectForAck(cfg.GatewayID, events.AckScopeServerRegistered), ackHandler)
			if startErr := subscriber.Start(context.Background()); startErr != nil {
				logger.Warn("events subscriber start failed", slog.String("error", startErr.Error()))
			} else {
				srv.eventSubscriber = subscriber
				srv.serverCfgHandler = serverHandler
				srv.settingsHandler = serverSettings
				srv.ackHandler = ackHandler
			}

			degradation := events.NewDegradationManager(natsClient, nil, srv.eventSubscriber, logger)
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
			srv.degradation = degradation
		}
	}
	if srv.eventsClient == nil {
		SetNATSConnected(false)
	}
	if cfg != nil && cfg.CruveroEnabled && srv.degradation == nil {
		srv.degradation = events.NewDegradationManager(nil, nil, nil, logger)
	}

	ratelimit.SetRateLimitedObserver(func(clientID string, route string) {
		ObserveRateLimited(clientID, route)
	})
	policy.SetDeniedObserver(func(reason string, tool string) {
		ObservePolicyDenied(reason, tool)
	})

	srv.proxyPolicyMW = policy.PolicyMiddleware(policyEngine, logger)

	srv.ready.Store(true)
	srv.setupRoutes()

	listenAddr := ":8443"
	if cfg != nil && cfg.ListenAddr != "" {
		listenAddr = cfg.ListenAddr
	}

	srv.httpServer = &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	metricsAddr := ":9090"
	if cfg != nil && strings.TrimSpace(cfg.MetricsAddr) != "" {
		metricsAddr = cfg.MetricsAddr
	}
	srv.metricsSrv = StartMetricsServer(metricsAddr)

	return srv
}

// Start starts the HTTP server and shuts it down gracefully when the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("start server: server is nil")
	}
	if ctx == nil {
		return fmt.Errorf("start server: context is nil")
	}

	defer func() {
		if s.eventSubscriber != nil {
			_ = s.eventSubscriber.Stop()
		}
		if s.eventsClient != nil {
			_ = s.eventsClient.Close()
		}
	}()

	if s.metricsSrv != nil {
		go func() {
			if err := s.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.logger.Error("metrics server failed", slog.String("error", err.Error()))
			}
		}()
	}

	// MemoryBackend manages its own cleanup internally; no external cleanup needed.

	shutdownErrCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		if s.metricsSrv != nil {
			if err := s.metricsSrv.Shutdown(shutdownCtx); err != nil {
				s.logger.Error("metrics shutdown failed", slog.String("error", err.Error()))
			}
		}
		shutdownErrCh <- s.httpServer.Shutdown(shutdownCtx)
	}()

	var serveErr error
	if s.cfg != nil && s.cfg.IsTLSConfigured() {
		tlsCfg, err := buildInboundTLSConfig(s.cfg)
		if err != nil {
			return fmt.Errorf("start server: build inbound tls config: %w", err)
		}
		s.httpServer.TLSConfig = tlsCfg
		serveErr = s.httpServer.ListenAndServeTLS("", "")
	} else {
		serveErr = s.httpServer.ListenAndServe()
	}

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
		r.Use(ratelimit.RateLimitMiddleware(s.rateLimitBackend, s.profileResolver, s.logger))
		if s.proxyPolicyMW != nil {
			r.Use(s.proxyPolicyMW)
		}
		r.Mount("/", proxyHandler)
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

// MountDeviceFlowRoutes mounts device code flow endpoints under /device.
func (s *Server) MountDeviceFlowRoutes(handler http.Handler) {
	if s == nil || handler == nil {
		return
	}
	s.router.Mount("/device", handler)
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

		if !everConnected && !hasCachedConfig {
			payload["status"] = "not_ready"
			writeJSON(w, http.StatusServiceUnavailable, payload)
			return
		}

		writeJSON(w, http.StatusOK, payload)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
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
