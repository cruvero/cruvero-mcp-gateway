package server

import (
	"context"
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
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/go-chi/chi/v5"
)

const shutdownTimeout = 10 * time.Second

// Server wraps the gateway HTTP server and router.
type Server struct {
	cfg        *config.Config
	logger     *slog.Logger
	router     chi.Router
	httpServer *http.Server
	startTime  time.Time
	ready      atomic.Bool

	rateLimiterStore  *ratelimit.LimiterStore
	profileResolver   ratelimit.ProfileResolver
	proxyAuthMW       func(http.Handler) http.Handler
	proxyPolicyMW     func(http.Handler) http.Handler
	cleanupInterval   time.Duration
	cleanupMaxIdleTTL time.Duration
	eventsClient      *events.Client
	eventSubscriber   *events.Subscriber
	eventPublisher    *events.Publisher
	degradation       *events.DegradationManager
}

// New builds a configured HTTP server with middleware and routes.
func New(cfg *config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	router := chi.NewRouter()
	router.Use(RequestIDMiddleware)
	router.Use(LoggingMiddleware(logger))
	router.Use(RecoveryMiddleware(logger))
	router.Use(RequestBodyLimitMiddleware(defaultMaxRequestBodyBytes))
	if cfg != nil && cfg.CORSEnabled {
		router.Use(CORSMiddleware)
	}

	srv := &Server{
		cfg:       cfg,
		logger:    logger,
		router:    router,
		startTime: time.Now().UTC(),

		cleanupInterval:   time.Minute,
		cleanupMaxIdleTTL: 5 * time.Minute,
	}
	profiles := defaultProfiles(cfg)
	defaultProfile := profiles["default"]
	srv.rateLimiterStore = ratelimit.NewLimiterStore(float64(defaultProfile.RateLimit), defaultProfile.RateBurst)
	srv.profileResolver = ratelimit.NewDefaultProfileResolver(profiles, defaultProfile)
	policyEngine := policy.NewEngine(profiles, nil, logger)

	if cfg != nil && cfg.CruveroEnabled && strings.TrimSpace(cfg.NATSURL) != "" {
		natsClient, err := events.NewClient(cfg.NATSURL, cfg.GatewayID)
		if err != nil {
			logger.Warn("events client init failed", slog.String("error", err.Error()))
		} else {
			srv.eventsClient = natsClient
			srv.eventPublisher = events.NewPublisher(natsClient, cfg.GatewayID, logger)
			policyEngine.SetViolationEventPublisher(srv.eventPublisher)

			subscriber := events.NewSubscriber(natsClient, logger)
			policyHandler := events.NewPolicyConfigHandler(policyEngine, srv.rateLimiterStore, logger)
			serverHandler := events.NewServerConfigHandler(nil, logger)
			serverSettingsHandler := events.NewServerSettingsConfigHandler(nil, nil, logger)
			authHandler := events.NewAuthConfigHandler(logger)
			subscriber.RegisterGatewaySubjects(policyHandler, serverHandler, serverSettingsHandler, authHandler)
			if startErr := subscriber.Start(context.Background()); startErr != nil {
				logger.Warn("events subscriber start failed", slog.String("error", startErr.Error()))
			} else {
				srv.eventSubscriber = subscriber
			}

			degradation := events.NewDegradationManager(natsClient, nil, srv.eventSubscriber, logger)
			natsClient.SetDisconnectHandler(func() {
				degradation.OnDisconnect()
			})
			natsClient.SetReconnectHandler(func() {
				if reconnectErr := degradation.OnReconnect(context.Background()); reconnectErr != nil {
					logger.Warn("degradation reconnect handler failed", slog.String("error", reconnectErr.Error()))
				}
			})
			srv.degradation = degradation
		}
	}
	if cfg != nil && cfg.CruveroEnabled && srv.degradation == nil {
		srv.degradation = events.NewDegradationManager(nil, nil, nil, logger)
	}

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

	ratelimit.StartCleanup(ctx, s.rateLimiterStore, s.cleanupInterval, s.cleanupMaxIdleTTL)

	shutdownErrCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErrCh <- s.httpServer.Shutdown(shutdownCtx)
	}()

	var serveErr error
	if s.cfg != nil && s.cfg.IsTLSConfigured() {
		serveErr = s.httpServer.ListenAndServeTLS(s.cfg.TLSCertPath, s.cfg.TLSKeyPath)
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
		r.Use(ratelimit.RateLimitMiddleware(s.rateLimiterStore, s.profileResolver, s.logger))
		if s.proxyPolicyMW != nil {
			r.Use(s.proxyPolicyMW)
		}
		r.Mount("/", proxyHandler)
	})
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

// EventPublisher returns the configured events publisher, if NATS is enabled.
func (s *Server) EventPublisher() *events.Publisher {
	if s == nil {
		return nil
	}
	return s.eventPublisher
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
	s.router.Get("/metrics", s.handleMetrics)
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

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "metrics_placeholder"})
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

// SetDegradationManager overrides server degradation state integration.
func (s *Server) SetDegradationManager(manager *events.DegradationManager) {
	if s == nil {
		return
	}
	s.degradation = manager
}
