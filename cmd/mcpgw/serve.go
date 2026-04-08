package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
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
	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/llm"
	"github.com/cruvero/mcp-gateway/internal/orchestrator"
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
	shutdownTracing, err := initTracing(cfg)
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

	stores := initStores(db, cfg)
	index := registration.NewCapabilityIndex()
	activeServers, err := listActiveServers(ctx, stores.serverStore)
	if err != nil {
		return fmt.Errorf("serve command: list active servers: %w", err)
	}
	index.Rebuild(activeServers)

	gw := server.New(cfg, logger, db)
	gw.SetToolCountFunc(index.ToolCount)

	dragonflyBackend, err := initRateLimitBackend(cfg, gw, logger)
	if err != nil {
		return err
	}
	if dragonflyBackend != nil {
		defer func() { _ = dragonflyBackend.Close() }()
	}
	gw.SetAuditStore(stores.auditStore)
	gw.SetClassificationStore(stores.classificationStore)
	eventPublisher := gw.EventPublisher()

	registrationService := registration.NewService(stores.serverStore, stores.auditStore, cfg, logger)
	registrationService.SetClassificationStore(stores.classificationStore)
	registrationService.SyncClassifications(ctx, activeServers)
	registrationService.SetLifecycleEventPublisher(eventPublisher)
	replayActiveServerRegistrations(ctx, eventPublisher, activeServers, logger)
	gw.BindRegistrationService(registrationService)

	broadcaster := selectBroadcaster(cfg, gw, dragonflyBackend, logger)
	defer func() { _ = broadcaster.Close() }()

	registrationService.SetBroadcaster(broadcaster)
	startSyncSubscribers(ctx, broadcaster, index, stores.serverStore, gw, logger)

	sweeper := registration.NewSweeper(stores.serverStore, index, cfg, logger)
	sweeper.SetLifecycleEventPublisher(eventPublisher)
	sweeper.Start(ctx)
	defer sweeper.Stop()

	proxyServer, err := initProxyServer(ctx, cfg, index, stores, gw, logger)
	if err != nil {
		return err
	}
	defer proxyServer.proxy.CloseSearch()
	if orch := initOrchestrator(cfg, stores, proxyServer.proxy, logger); orch != nil {
		proxyServer.proxy.SetOrchestrator(orch)
		logger.Info("orchestrate meta-tool enabled")
	}
	searchStatus := wireSearchReadiness(cfg, gw, proxyServer.proxy)

	subscribeToolCacheInvalidation(broadcaster, proxyServer.proxy, logger)

	gw.SetProxyAuthMiddleware(auth.AuthMiddleware(auth.AuthOptions{
		APIKeyStore:   stores.apiKeyStore,
		OIDCValidator: proxyServer.oidcValidator,
		UserStore:     stores.userStore,
		AuditStore:    stores.auditStore,
		Logger:        logger,
	}))

	if cfg.ServerScopeEnforcement {
		gw.SetServerScopeMiddleware(auth.ServerScopeMiddleware(logger))
	}

	regHandler := registration.NewHandler(
		newIndexedRegistrationService(registrationService, stores.serverStore, index, logger),
		logger,
	)
	gw.MountRegistrationRoutes(regHandler.Routes())
	gw.MountProxyRoutes(proxyServer.proxy.Handler())

	if cfg.PerServerEndpoints {
		perServerTLS, tlsErr := buildProxyTLSConfig(cfg)
		if tlsErr != nil {
			return fmt.Errorf("serve command: per-server tls config: %w", tlsErr)
		}
		vsHandler := proxy.NewVirtualServerHandler(index, perServerTLS, 0, logger)
		gw.MountPerServerRoutes(vsHandler)
		logger.Info("per-server endpoints enabled")
	}

	discoveryDoc := proxy.NewDiscoveryDocHandler(index, cfg, logger)
	serverList := proxy.NewServerListHandler(index, cfg, logger)
	gw.MountDiscoveryRoutes(discoveryDoc, serverList)

	apikeyAPIHandler := server.NewAPIKeyAPIHandler(stores.apiKeyStore, logger)
	gw.MountAPIKeyAPI(apikeyAPIHandler.Routes())

	if err := mountOptionalRoutes(cfg, gw, proxyServer.proxy, stores, broadcaster, logger, searchStatus); err != nil {
		return err
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

// serveStores groups the data stores used during serve initialization.
type serveStores struct {
	serverStore         storepkg.ServerStore
	apiKeyStore         storepkg.APIKeyStore
	auditStore          storepkg.AuditStore
	classificationStore storepkg.ToolClassificationStore
	userStore           storepkg.UserStore
	synonymStore        storepkg.SynonymStore
	reindexLogStore     storepkg.ReindexLogStore
}

func initStores(db *sql.DB, cfg *config.Config) serveStores {
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	return serveStores{
		serverStore:         storepkg.NewPostgresServerStore(db),
		apiKeyStore:         storepkg.NewPostgresAPIKeyStore(db),
		auditStore:          storepkg.NewPostgresAuditStore(db),
		classificationStore: storepkg.NewPostgresToolClassificationStore(db),
		userStore:           storepkg.NewPostgresUserStore(db),
		synonymStore:        storepkg.NewPostgresSynonymStore(db),
		reindexLogStore:     storepkg.NewPostgresReindexLogStore(db),
	}
}

func initTracing(cfg *config.Config) (func(), error) {
	server.SetTracingVersion(version)
	server.SetServerVersion(version)
	server.SetTracingEndpoint(cfg.OTLPExporterEndpoint)
	shutdownTracing, err := server.InitTracer(context.Background(), cfg.OTELServiceName)
	if err != nil {
		return nil, err
	}
	return shutdownTracing, nil
}

func initRateLimitBackend(cfg *config.Config, gw *server.Server, logger *slog.Logger) (*ratelimit.DragonflyBackend, error) {
	switch cfg.RateLimitBackend {
	case "dragonfly":
		return initDragonflyBackend(cfg, gw, logger)
	case "nats":
		return nil, initNATSRateLimitBackend(gw, logger)
	default:
		logger.Info("rate limit backend: memory")
		return nil, nil
	}
}

func initDragonflyBackend(cfg *config.Config, gw *server.Server, logger *slog.Logger) (*ratelimit.DragonflyBackend, error) {
	fallback := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	rb, rbErr := ratelimit.NewDragonflyBackend(cfg.DragonflyURL,
		ratelimit.WithDragonflyFallback(fallback),
		ratelimit.WithDragonflyLogger(logger),
	)
	if rbErr != nil {
		_ = fallback.Close()
		return nil, fmt.Errorf("serve command: create dragonfly rate limit backend: %w", rbErr)
	}
	gw.SetRateLimitBackend(rb)
	logger.Info("rate limit backend: dragonfly")
	return rb, nil
}

func initNATSRateLimitBackend(gw *server.Server, logger *slog.Logger) error {
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
	return nil
}

func selectBroadcaster(cfg *config.Config, gw *server.Server, dragonflyBackend *ratelimit.DragonflyBackend, logger *slog.Logger) registration.Broadcaster {
	eventsClient := gw.EventsClient()
	switch {
	case cfg.CruveroEnabled && eventsClient != nil && eventsClient.IsConnected():
		logger.Info("broadcaster: nats")
		return registration.NewNATSBroadcaster(eventsClient.Conn(), logger)
	case dragonflyBackend != nil:
		logger.Info("broadcaster: dragonfly")
		return registration.NewDragonflyBroadcaster(dragonflyBackend.DragonflyClient(), logger)
	default:
		logger.Info("broadcaster: noop")
		return registration.NewNoopBroadcaster()
	}
}

func startSyncSubscribers(ctx context.Context, broadcaster registration.Broadcaster, index *registration.CapabilityIndex, serverStore storepkg.ServerStore, gw *server.Server, logger *slog.Logger) {
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
}

// proxySetup holds the proxy server and its associated OIDC validator.
type proxySetup struct {
	proxy         *proxy.ProxyServer
	oidcValidator *auth.OIDCValidator
}

func initProxyServer(ctx context.Context, cfg *config.Config, index *registration.CapabilityIndex, stores serveStores, gw *server.Server, logger *slog.Logger) (*proxySetup, error) {
	proxyTLSConfig, err := buildProxyTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("serve command: build proxy tls config: %w", err)
	}
	proxyServer := proxy.NewProxyServer(index, cfg, proxyTLSConfig, 0, logger)
	proxyServer.SetAuditStore(stores.auditStore)
	proxyServer.SetUserStore(stores.userStore)
	if lb := gw.RateLimitBackend(); lb != nil {
		proxyServer.Router().SetLimiter(lb)
	}
	wireToolMetadataCallback(gw, proxyServer)

	oidcValidator, err := maybeBuildOIDCValidator(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("serve command: build oidc validator: %w", err)
	}
	return &proxySetup{proxy: proxyServer, oidcValidator: oidcValidator}, nil
}

func wireToolMetadataCallback(gw *server.Server, proxyServer *proxy.ProxyServer) {
	toolMetaHandler := gw.ToolMetadataHandler()
	if toolMetaHandler == nil {
		return
	}
	toolMetaHandler.SetOnUpdate(func(msg events.ToolMetadataConfigMessage) {
		metadata := make([]proxy.ToolMetadata, 0, len(msg.Tools))
		for _, entry := range msg.Tools {
			metadata = append(metadata, proxy.ToolMetadata{
				ToolName: entry.ToolName,
				Category: entry.Category,
				Summary:  entry.Summary,
				Tags:     entry.Tags,
				Priority: entry.Priority,
			})
		}
		proxyServer.ApplyToolMetadata(metadata)
	})
}

type searchStatusSnapshot struct {
	EngineType            string
	EmbedderType          string
	IndexedTools          int
	EngineReady           bool
	VectorReady           *bool
	VectorSearchFallbacks uint64
	VectorIndexFallbacks  uint64
	EngineErrorFallbacks  uint64
}

type searchStatusFunc func() searchStatusSnapshot

func wireSearchReadiness(cfg *config.Config, gw *server.Server, proxyServer *proxy.ProxyServer) searchStatusFunc {
	status := buildSearchStatusFunc(cfg, proxyServer)
	if status == nil {
		return nil
	}
	gw.SetSearchStatusFunc(func() server.SearchStatus {
		snapshot := status()
		return server.SearchStatus{
			Engine:                snapshot.EngineType,
			Embedder:              snapshot.EmbedderType,
			IndexedTools:          snapshot.IndexedTools,
			Ready:                 snapshot.EngineReady,
			VectorReady:           snapshot.VectorReady,
			VectorSearchFallbacks: snapshot.VectorSearchFallbacks,
			VectorIndexFallbacks:  snapshot.VectorIndexFallbacks,
			EngineErrorFallbacks:  snapshot.EngineErrorFallbacks,
		}
	})
	return status
}

func buildSearchStatusFunc(cfg *config.Config, proxyServer *proxy.ProxyServer) searchStatusFunc {
	if cfg == nil || proxyServer == nil || cfg.SearchEngine == "" || cfg.SearchEngine == "substring" {
		return nil
	}
	di := proxyServer.DiscoveryIndex()
	if di == nil {
		return nil
	}

	return func() searchStatusSnapshot {
		snapshot := searchStatusSnapshot{
			EngineType:   cfg.SearchEngine,
			EmbedderType: proxyServer.EmbedderType(),
			IndexedTools: di.Stats().TotalTools,
			EngineReady:  false,
		}
		engine := di.Engine()
		if engine == nil {
			snapshot.EngineErrorFallbacks = di.EngineErrorFallbacks()
			return snapshot
		}

		snapshot.EngineReady = engine.Ready()
		if ve, ok := engine.(interface{ VectorReady() bool }); ok {
			vectorReady := ve.VectorReady()
			snapshot.VectorReady = &vectorReady
			snapshot.EngineReady = snapshot.EngineReady && vectorReady
		}
		if vf, ok := engine.(interface{ VectorSearchFallbacks() uint64 }); ok {
			snapshot.VectorSearchFallbacks = vf.VectorSearchFallbacks()
		}
		if vf, ok := engine.(interface{ VectorIndexFallbacks() uint64 }); ok {
			snapshot.VectorIndexFallbacks = vf.VectorIndexFallbacks()
		}
		snapshot.EngineErrorFallbacks = di.EngineErrorFallbacks()
		return snapshot
	}
}

func mountOptionalRoutes(cfg *config.Config, gw *server.Server, proxyServer *proxy.ProxyServer, stores serveStores, broadcaster registration.Broadcaster, logger *slog.Logger, searchStatus searchStatusFunc) error {
	if cfg.ProgressiveDiscovery {
		logger.Info("progressive discovery enabled")
	}
	if cfg.DeviceFlowEnabled {
		deviceFlowHandler := auth.NewDeviceFlowHandler(cfg, logger)
		gw.MountDeviceFlowRoutes(deviceFlowHandler.Routes())
		logger.Info("device flow enabled")
	}
	if cfg.AdminEnabled || strings.TrimSpace(cfg.AdminMode) == "integrated" {
		if err := mountAdmin(cfg, gw, proxyServer, stores, broadcaster, logger, searchStatus); err != nil {
			return err
		}
	}
	if len(cfg.PlatformSPIFFEPrefixes) > 0 {
		catalogHandler := server.NewCatalogHandler(
			&catalogListerAdapter{proxy: proxyServer},
			stores.classificationStore,
			stores.serverStore,
			cfg.GatewayID,
			logger,
		)
		gw.MountCatalog(catalogHandler.Routes(), cfg.PlatformSPIFFEPrefixes)
		logger.Info("platform catalog endpoint enabled")
	}
	return nil
}

// catalogListerAdapter converts proxy.CatalogEntry to server.CatalogEntry,
// bridging the proxy and server packages without creating an import cycle.
type catalogListerAdapter struct {
	proxy *proxy.ProxyServer
}

func (a *catalogListerAdapter) ListCatalogTools(ctx context.Context) ([]server.CatalogEntry, error) {
	entries, err := a.proxy.ListCatalogTools(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]server.CatalogEntry, len(entries))
	for i, e := range entries {
		result[i] = server.CatalogEntry{
			Name:        e.Name,
			Description: e.Description,
			InputSchema: e.InputSchema,
			Annotations: e.Annotations,
			ServerID:    e.ServerID,
			ServerName:  e.ServerName,
		}
	}
	return result, nil
}

func mountAdmin(cfg *config.Config, gw *server.Server, proxyServer *proxy.ProxyServer, stores serveStores, broadcaster registration.Broadcaster, logger *slog.Logger, searchStatus searchStatusFunc) error {
	adminAuth, err := buildAdminAuth(cfg, logger)
	if err != nil {
		return fmt.Errorf("serve command: initialize admin auth: %w", err)
	}
	var adminSearchStatus func() admin.SearchStatusSnapshot
	if searchStatus != nil {
		adminSearchStatus = func() admin.SearchStatusSnapshot {
			snapshot := searchStatus()
			return admin.SearchStatusSnapshot{
				EngineType:            snapshot.EngineType,
				EmbedderType:          snapshot.EmbedderType,
				IndexedTools:          snapshot.IndexedTools,
				EngineReady:           snapshot.EngineReady,
				VectorReady:           snapshot.VectorReady,
				VectorSearchFallbacks: snapshot.VectorSearchFallbacks,
				VectorIndexFallbacks:  snapshot.VectorIndexFallbacks,
				EngineErrorFallbacks:  snapshot.EngineErrorFallbacks,
			}
		}
	}

	adminRouter := admin.NewRouter(admin.AdminDeps{
		Auth:                 adminAuth,
		DevMode:              cfg.AdminDevMode,
		Mode:                 cfg.AdminMode,
		PlatformServiceToken: cfg.PlatformServiceToken,
		Logger:               logger,
		ServerStore:          stores.serverStore,
		AuditStore:           stores.auditStore,
		ClassificationStore:  stores.classificationStore,
		UserStore:            stores.userStore,
		Broadcaster:          broadcaster,
		RateLimitBackend:     gw.RateLimitBackend(),
		DiscoveryIndex:       proxyServer.DiscoveryIndex(),
		ProgressiveDiscovery: cfg.ProgressiveDiscovery,
		SynonymStore:         stores.synonymStore,
		ReindexLogStore:      stores.reindexLogStore,
		SearchEngineType:     cfg.SearchEngine,
		EmbedderType:         proxyServer.EmbedderType(),
		SearchStatusFunc:     adminSearchStatus,
	})
	gw.MountAdmin(adminRouter)
	logger.Info("admin dashboard enabled", slog.Bool("dev_mode", cfg.AdminDevMode))
	return nil
}

func buildAdminAuth(cfg *config.Config, logger *slog.Logger) (*admin.AdminAuth, error) {
	if strings.TrimSpace(cfg.AdminMode) == "integrated" {
		return nil, nil
	}
	if cfg.AdminDevMode {
		logger.Warn("ADMIN DEV MODE ENABLED - authentication bypassed, do not use in production")
		warnNonDevTLSCA(cfg, logger)
		return nil, nil
	}
	return admin.NewAdminAuth(cfg, logger)
}

func warnNonDevTLSCA(cfg *config.Config, logger *slog.Logger) {
	if strings.TrimSpace(cfg.TLSCAPath) == "" {
		return
	}
	caLower := strings.ToLower(cfg.TLSCAPath)
	if !strings.Contains(caLower, "dev") && !strings.Contains(caLower, "local") {
		logger.Warn("admin dev mode with non-dev TLS CA path",
			slog.String("tls_ca_path", cfg.TLSCAPath))
	}
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

type serverRegisteredPublisher interface {
	PublishServerRegistered(ctx context.Context, server types.ServerRecord) error
}

func replayActiveServerRegistrations(ctx context.Context, publisher serverRegisteredPublisher, servers []types.ServerRecord, logger *slog.Logger) {
	if publisher == nil || len(servers) == 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	replayed := 0
	failed := 0
	for _, record := range servers {
		if strings.TrimSpace(record.ID) == "" {
			logger.Warn("skipping active server registration replay due to empty id",
				slog.String("server_name", record.Name))
			continue
		}
		if strings.TrimSpace(record.Name) == "" {
			logger.Warn("active server has blank name; replaying registration event anyway",
				slog.String("server_id", record.ID))
		}
		if err := publisher.PublishServerRegistered(ctx, record); err != nil {
			failed++
			logger.Warn("replay server.registered event failed",
				slog.String("server_id", record.ID),
				slog.String("server_name", record.Name),
				slog.String("error", err.Error()))
			continue
		}
		replayed++
	}
	if replayed > 0 || failed > 0 {
		logger.Info("replayed active server registration events",
			slog.Int("replayed", replayed),
			slog.Int("failed", failed))
	}
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

func subscribeToolCacheInvalidation(broadcaster registration.Broadcaster, proxyServer *proxy.ProxyServer, logger *slog.Logger) {
	if broadcaster == nil || proxyServer == nil {
		return
	}
	if err := broadcaster.Subscribe(registration.SubjectRegistryUpdated, func(data []byte) {
		var evt registration.RegistrationEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			logger.Error("unmarshal registry event for tool cache failed", slog.String("error", err.Error()))
			return
		}
		switch evt.EventType {
		case "deregistered":
			proxyServer.InvalidateToolCache()
		case "registered", "status_changed":
			if strings.TrimSpace(evt.ServerID) != "" {
				proxyServer.InvalidateToolCacheForServer(evt.ServerID)
			}
		default:
			// Other event types do not affect the tool cache.
		}
	}); err != nil {
		logger.Warn("tool cache invalidation subscriber failed", slog.String("error", err.Error()))
	}
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

// --- Orchestrator wiring ---

func initOrchestrator(cfg *config.Config, stores serveStores, ps *proxy.ProxyServer, logger *slog.Logger) *orchestrator.Orchestrator {
	if !cfg.Orchestrate.Enabled || len(cfg.Orchestrate.Providers) == 0 {
		return nil
	}

	providers := make([]llm.ProviderEntry, 0, len(cfg.Orchestrate.Providers))
	for _, p := range cfg.Orchestrate.Providers {
		client, err := llm.NewProviderClient(p.Name, p.APIKey, p.BaseURL, p.Model)
		if err != nil {
			logger.Warn("llm provider init failed, skipping",
				slog.String("provider", p.Name),
				slog.String("error", err.Error()))
			continue
		}
		providers = append(providers, llm.ProviderEntry{Name: p.Name, Client: client})
	}
	if len(providers) == 0 {
		logger.Warn("no LLM providers initialized, orchestrate disabled")
		return nil
	}

	failover := llm.NewFailoverClient(providers, logger)

	discoverer := &orchestratorDiscoverer{
		discovery:       ps.DiscoveryIndex(),
		classifications: stores.classificationStore,
	}
	executor := &orchestratorExecutor{proxy: ps}
	auditor := &orchestratorAuditor{store: stores.auditStore, logger: logger}
	activator := &orchestratorActivator{proxy: ps}

	return orchestrator.New(failover, discoverer, executor, auditor, activator, logger)
}

// orchestratorDiscoverer bridges the proxy DiscoveryIndex to the orchestrator interface.
type orchestratorDiscoverer struct {
	discovery       *proxy.DiscoveryIndex
	classifications storepkg.ToolClassificationStore
}

func (d *orchestratorDiscoverer) SearchTools(ctx context.Context, query string, limit int) ([]orchestrator.CandidateTool, error) {
	if d.discovery == nil {
		return nil, fmt.Errorf("discovery index not available")
	}
	results, _ := d.discovery.Search(query, "", 0, limit)
	return d.enrichCandidates(ctx, results), nil
}

func (d *orchestratorDiscoverer) GetToolSchemas(ctx context.Context, names []string) ([]orchestrator.CandidateTool, error) {
	if d.discovery == nil {
		return nil, fmt.Errorf("discovery index not available")
	}
	results := d.discovery.GetTools(names)
	return d.enrichCandidates(ctx, results), nil
}

func (d *orchestratorDiscoverer) enrichCandidates(ctx context.Context, tools []proxy.ToolDefinition) []orchestrator.CandidateTool {
	candidates := make([]orchestrator.CandidateTool, 0, len(tools))
	for _, t := range tools {
		risk := "unknown"
		if d.classifications != nil {
			if c, err := d.classifications.Get(ctx, t.Name); err == nil && c != nil {
				risk = string(c.RiskLevel)
			}
		}
		candidates = append(candidates, orchestrator.CandidateTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			RiskLevel:   risk,
		})
	}
	return candidates
}

// orchestratorExecutor calls through the full tool routing pipeline (RBAC,
// routing, and audit) via ProxyServer.ExecuteToolCall.
type orchestratorExecutor struct {
	proxy *proxy.ProxyServer
}

func (e *orchestratorExecutor) ExecuteTool(ctx context.Context, toolName string, args map[string]any) (string, bool, error) {
	return e.proxy.ExecuteToolCall(ctx, toolName, args)
}

// orchestratorAuditor bridges the AuditStore to the orchestrator interface.
type orchestratorAuditor struct {
	store  storepkg.AuditStore
	logger *slog.Logger
}

func (a *orchestratorAuditor) LogOrchestration(ctx context.Context, eventType string, details map[string]any) {
	if a.store == nil {
		return
	}
	entry := &types.AuditEntry{
		EventType: eventType,
		Details:   details,
	}
	if err := a.store.Log(ctx, entry); err != nil {
		a.logger.Error("orchestration audit log failed",
			slog.String("event_type", eventType),
			slog.String("error", err.Error()))
	}
}

// orchestratorActivator bridges the ProxyServer to the orchestrator interface.
type orchestratorActivator struct {
	proxy *proxy.ProxyServer
}

func (a *orchestratorActivator) ActivateTools(ctx context.Context, toolNames []string) {
	if a.proxy != nil {
		a.proxy.ActivateToolsByName(ctx, toolNames)
	}
}
