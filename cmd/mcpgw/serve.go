package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cruvero/mcp-gateway/internal/admin"
	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/server"
	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

const dbPingTimeout = 5 * time.Second

var openDBFunc = openPostgresDB

func serveCommand(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse serve command: %w", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("serve command does not accept positional arguments")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveWithContext(ctx)
}

func serveWithContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("serve command: context is nil")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("serve command: load config: %w", err)
	}

	logger := newLogger(cfg.LogFormat, cfg.LogLevel)
	server.SetTracingVersion(version)
	server.SetTracingEndpoint(cfg.OTLPExporterEndpoint)
	shutdownTracing, err := server.InitTracer(context.Background(), cfg.OTELServiceName)
	if err != nil {
		return fmt.Errorf("serve command: initialize tracer: %w", err)
	}
	defer shutdownTracing()

	db, err := openDBFunc(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("serve command: open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("close database failed", slog.String("error", closeErr.Error()))
		}
	}()

	serverStore := storepkg.NewPostgresServerStore(db)
	apiKeyStore := storepkg.NewPostgresAPIKeyStore(db)
	auditStore := storepkg.NewPostgresAuditStore(db)

	index := registration.NewCapabilityIndex()
	activeServers, err := listActiveServers(ctx, serverStore)
	if err != nil {
		return fmt.Errorf("serve command: list active servers: %w", err)
	}
	index.Rebuild(activeServers)

	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)

	classificationStore := storepkg.NewPostgresToolClassificationStore(db)

	gw := server.New(cfg, logger, db)

	// Rate limit backend override.
	var dragonflyBackend *ratelimit.DragonflyBackend
	switch cfg.RateLimitBackend {
	case "dragonfly":
		fallback := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
		rb, rbErr := ratelimit.NewDragonflyBackend(cfg.DragonflyURL,
			ratelimit.WithDragonflyFallback(fallback),
			ratelimit.WithDragonflyLogger(logger),
		)
		if rbErr != nil {
			_ = fallback.Close()
			return fmt.Errorf("serve command: create dragonfly rate limit backend: %w", rbErr)
		}
		dragonflyBackend = rb
		gw.SetRateLimitBackend(rb)
		defer func() { _ = rb.Close() }()
		logger.Info("rate limit backend: dragonfly")
	case "nats":
		evClient := gw.EventsClient()
		if evClient == nil || !evClient.IsConnected() {
			return fmt.Errorf("serve command: nats rate limit backend requires active NATS connection")
		}
		js, jsErr := evClient.JetStream()
		if jsErr != nil {
			return fmt.Errorf("serve command: get jetstream context: %w", jsErr)
		}
		fallback := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
		nb, nbErr := ratelimit.NewNATSBackend(js,
			ratelimit.WithNATSFallback(fallback),
			ratelimit.WithNATSLogger(logger),
		)
		if nbErr != nil {
			_ = fallback.Close()
			return fmt.Errorf("serve command: create nats rate limit backend: %w", nbErr)
		}
		gw.SetRateLimitBackend(nb)
		logger.Info("rate limit backend: nats")
	default:
		logger.Info("rate limit backend: memory")
	}
	gw.SetAuditStore(auditStore)
	gw.SetClassificationStore(classificationStore)
	eventPublisher := gw.EventPublisher()

	registrationService := registration.NewService(serverStore, auditStore, cfg, logger)
	registrationService.SetClassificationStore(classificationStore)
	registrationService.SetLifecycleEventPublisher(eventPublisher)
	gw.BindRegistrationService(registrationService)

	// Broadcaster selection for cross-pod sync.
	var broadcaster registration.Broadcaster
	eventsClient := gw.EventsClient()
	switch {
	case cfg.CruveroEnabled && eventsClient != nil && eventsClient.IsConnected():
		broadcaster = registration.NewNATSBroadcaster(eventsClient.Conn(), logger)
		logger.Info("broadcaster: nats")
	case dragonflyBackend != nil:
		broadcaster = registration.NewDragonflyBroadcaster(dragonflyBackend.DragonflyClient(), logger)
		logger.Info("broadcaster: dragonfly")
	default:
		broadcaster = registration.NewNoopBroadcaster()
		logger.Info("broadcaster: noop")
	}
	defer func() { _ = broadcaster.Close() }()

	registrationService.SetBroadcaster(broadcaster)

	// Start cross-pod sync subscribers.
	regSubscriber := registration.NewRegistrationSubscriber(broadcaster, index, serverStore, logger)
	if err := regSubscriber.Start(ctx); err != nil {
		logger.Warn("registration subscriber start failed", slog.String("error", err.Error()))
	}

	if policyEngine := gw.PolicyEngine(); policyEngine != nil {
		classSubscriber := registration.NewClassificationSubscriber(broadcaster, policyEngine.ClassificationCache(), logger)
		if err := classSubscriber.Start(ctx); err != nil {
			logger.Warn("classification subscriber start failed", slog.String("error", err.Error()))
		}
	}

	sweeper := registration.NewSweeper(serverStore, index, cfg, logger)
	sweeper.SetLifecycleEventPublisher(eventPublisher)
	sweeper.Start(ctx)
	defer sweeper.Stop()

	proxyTLSConfig, err := buildProxyTLSConfig(cfg)
	if err != nil {
		return fmt.Errorf("serve command: build proxy tls config: %w", err)
	}
	proxyServer := proxy.NewProxyServer(index, cfg, proxyTLSConfig, 0, logger)
	proxyServer.SetAuditStore(auditStore)

	oidcValidator, err := maybeBuildOIDCValidator(ctx, cfg)
	if err != nil {
		return fmt.Errorf("serve command: build oidc validator: %w", err)
	}

	gw.SetProxyAuthMiddleware(auth.AuthMiddleware(auth.AuthOptions{
		APIKeyStore:   apiKeyStore,
		OIDCValidator: oidcValidator,
		Logger:        logger,
	}))

	regHandler := registration.NewHandler(
		newIndexedRegistrationService(registrationService, serverStore, index, logger),
		logger,
	)
	gw.MountRegistrationRoutes(regHandler.Routes())
	gw.MountProxyRoutes(proxyServer.Handler())

	if cfg.DeviceFlowEnabled {
		deviceFlowHandler := auth.NewDeviceFlowHandler(cfg, logger)
		gw.MountDeviceFlowRoutes(deviceFlowHandler.Routes())
		logger.Info("device flow enabled")
	}

	if cfg.AdminEnabled {
		var adminAuth *admin.AdminAuth
		if cfg.AdminDevMode {
			logger.Warn("ADMIN DEV MODE ENABLED - authentication bypassed, do not use in production")
			if strings.TrimSpace(cfg.TLSCAPath) != "" {
				caLower := strings.ToLower(cfg.TLSCAPath)
				if !strings.Contains(caLower, "dev") && !strings.Contains(caLower, "local") {
					logger.Warn("admin dev mode with non-dev TLS CA path",
						slog.String("tls_ca_path", cfg.TLSCAPath))
				}
			}
		} else {
			var adminErr error
			adminAuth, adminErr = admin.NewAdminAuth(cfg, logger)
			if adminErr != nil {
				return fmt.Errorf("serve command: initialize admin auth: %w", adminErr)
			}
		}
		adminRouter := admin.NewRouter(admin.AdminDeps{
			Auth:                adminAuth,
			DevMode:             cfg.AdminDevMode,
			Logger:              logger,
			ServerStore:         serverStore,
			AuditStore:          auditStore,
			ClassificationStore: classificationStore,
			Broadcaster:         broadcaster,
			RateLimitBackend:    gw.RateLimitBackend(),
		})
		gw.MountAdmin(adminRouter)
		logger.Info("admin dashboard enabled", slog.Bool("dev_mode", cfg.AdminDevMode))
	}

	go storepkg.StartAuditRetention(ctx, db, cfg.AuditRetentionDays, cfg.AuditCleanupInterval, logger)

	logger.Info("starting mcp gateway",
		slog.String("version", version),
		slog.String("commit", commit),
		slog.String("build_date", buildDate),
		slog.String("listen_addr", cfg.ListenAddr),
	)

	if err := gw.Start(ctx); err != nil {
		return fmt.Errorf("serve command: start gateway: %w", err)
	}
	return nil
}

func openPostgresDB(ctx context.Context, dbURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return db, nil
}

func listActiveServers(ctx context.Context, serverStore storepkg.ServerStore) ([]types.ServerRecord, error) {
	status := types.StatusActive
	records, err := serverStore.List(ctx, types.ServerFilter{Status: &status})
	if err != nil {
		return nil, fmt.Errorf("list active servers: %w", err)
	}
	return records, nil
}

func maybeBuildOIDCValidator(ctx context.Context, cfg *config.Config) (*auth.OIDCValidator, error) {
	if cfg == nil {
		return nil, nil
	}
	if strings.TrimSpace(cfg.OIDCIssuer) == "" || strings.TrimSpace(cfg.OIDCAudience) == "" {
		return nil, nil
	}

	validator, err := auth.NewOIDCValidator(ctx, cfg.OIDCIssuer, cfg.OIDCAudience)
	if err != nil {
		return nil, fmt.Errorf("initialize oidc validator: %w", err)
	}
	return validator, nil
}

func buildProxyTLSConfig(cfg *config.Config) (*tls.Config, error) {
	if cfg == nil || strings.TrimSpace(cfg.TLSCAPath) == "" {
		return nil, nil
	}

	tlsCfg, err := identity.BuildClientTLSConfig(identity.TLSConfig{
		CABundlePath: cfg.TLSCAPath,
		CertPath:     cfg.TLSCertPath,
		KeyPath:      cfg.TLSKeyPath,
	})
	if err != nil {
		return nil, fmt.Errorf("build client tls config: %w", err)
	}

	return tlsCfg, nil
}

func newLogger(format string, level string) *slog.Logger {
	logLevel := slog.LevelInfo
	switch strings.TrimSpace(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: logLevel}
	if strings.TrimSpace(format) == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

type indexedRegistrationService struct {
	base        registration.RegistrationService
	serverStore storepkg.ServerStore
	index       *registration.CapabilityIndex
	logger      *slog.Logger
}

func newIndexedRegistrationService(
	base registration.RegistrationService,
	serverStore storepkg.ServerStore,
	index *registration.CapabilityIndex,
	logger *slog.Logger,
) registration.RegistrationService {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &indexedRegistrationService{
		base:        base,
		serverStore: serverStore,
		index:       index,
		logger:      logger,
	}
}

func (s *indexedRegistrationService) Register(
	ctx context.Context,
	caller *identity.Identity,
	req registration.RegistrationRequest,
) (*registration.RegistrationResponse, error) {
	resp, err := s.base.Register(ctx, caller, req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Status.IsRoutable() {
		s.syncServer(ctx, resp.InstanceID)
	}
	return resp, nil
}

func (s *indexedRegistrationService) Heartbeat(
	ctx context.Context,
	caller *identity.Identity,
	id string,
	req registration.HeartbeatRequest,
) (*registration.HeartbeatResponse, error) {
	resp, err := s.base.Heartbeat(ctx, caller, id, req)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.ServerStatus.IsRoutable() {
		s.syncServer(ctx, id)
	} else if s.index != nil {
		s.index.Remove(id)
	}
	return resp, nil
}

func (s *indexedRegistrationService) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	return s.base.List(ctx, filter)
}

func (s *indexedRegistrationService) Deregister(ctx context.Context, caller *identity.Identity, id string) error {
	if err := s.base.Deregister(ctx, caller, id); err != nil {
		return err
	}
	if s.index != nil {
		s.index.Remove(id)
	}
	return nil
}

func (s *indexedRegistrationService) syncServer(ctx context.Context, id string) {
	if s == nil || s.index == nil || s.serverStore == nil || strings.TrimSpace(id) == "" {
		return
	}
	record, err := s.serverStore.Get(ctx, id)
	if err != nil {
		s.logger.Warn("sync capability index failed", slog.String("server_id", id), slog.String("error", err.Error()))
		return
	}
	s.index.Add(*record)
}
