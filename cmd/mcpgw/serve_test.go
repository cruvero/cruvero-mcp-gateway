package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/proxy"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/search"
	"github.com/cruvero/mcp-gateway/internal/server"
	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func setupServeMock(t *testing.T) {
	t.Helper()
	origOpen := openDBFunc
	openDBFunc = func(_ context.Context, _ string) (*sql.DB, error) {
		db, mock, err := sqlmock.New()
		if err != nil {
			return nil, err
		}
		rows := sqlmock.NewRows([]string{"id", "name", "spiffe_id", "version", "host", "port", "capabilities", "status", "policy_profile", "last_heartbeat", "created_at", "updated_at"})
		mock.ExpectQuery("SELECT (.+) FROM mcp_servers WHERE \\(\\$1::text IS NULL OR status = \\$1\\) AND \\(\\$2::text IS NULL OR name ILIKE \\$2\\) ORDER BY created_at DESC LIMIT \\$3 OFFSET \\$4").
			WithArgs("active", nil, int64(9223372036854775807), int64(0)).
			WillReturnRows(rows)
		mock.ExpectClose()
		return db, nil
	}
	t.Cleanup(func() {
		openDBFunc = origOpen
	})
}

func TestServeWithContextInitializesAndShutsDown(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://example.invalid:5432/mcpgw?sslmode=disable")
	t.Setenv("MCPGW_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "false")
	t.Setenv("MCPGW_OIDC_ISSUER", "")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "")
	t.Setenv("MCPGW_CORS_ENABLED", "false")

	setupServeMock(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWithContext(ctx)
	}()

	// Allow enough time for full initialization under CI + race detector load.
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveWithContext returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveWithContext did not shut down in time")
	}
}

func TestServeWithContext_AdminDevMode(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://example.invalid:5432/mcpgw?sslmode=disable")
	t.Setenv("MCPGW_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "false")
	t.Setenv("MCPGW_OIDC_ISSUER", "")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "")
	t.Setenv("MCPGW_CORS_ENABLED", "false")
	t.Setenv("MCPGW_ADMIN_ENABLED", "true")
	t.Setenv("MCPGW_ADMIN_DEV_MODE", "true")

	setupServeMock(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWithContext(ctx)
	}()

	// Allow enough time for full initialization under CI + race detector load.
	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveWithContext returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveWithContext did not shut down in time")
	}
}

func TestServeWithContext_AdminIntegratedModeWithoutAdminEnabled(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://example.invalid:5432/mcpgw?sslmode=disable")
	t.Setenv("MCPGW_LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_METRICS_ADDR", "127.0.0.1:0")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "false")
	t.Setenv("MCPGW_OIDC_ISSUER", "")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "")
	t.Setenv("MCPGW_CORS_ENABLED", "false")
	t.Setenv("MCPGW_ADMIN_MODE", "integrated")
	t.Setenv("MCPGW_PLATFORM_SERVICE_TOKEN", "svc-token")
	t.Setenv("MCPGW_ADMIN_ENABLED", "false")

	setupServeMock(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveWithContext(ctx)
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serveWithContext returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveWithContext did not shut down in time")
	}
}

// testBroadcaster captures Subscribe calls and allows triggering callbacks.
type testBroadcaster struct {
	mu        sync.Mutex
	callbacks map[string]func([]byte)
}

func (b *testBroadcaster) Publish(subject string, data []byte) error {
	b.mu.Lock()
	cb := b.callbacks[subject]
	b.mu.Unlock()
	if cb != nil {
		cb(data)
	}
	return nil
}

func (b *testBroadcaster) Subscribe(subject string, cb func([]byte)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.callbacks[subject] = cb
	return nil
}

func (b *testBroadcaster) Close() error { return nil }

type replayPublisherStub struct {
	mu      sync.Mutex
	calls   []string
	failFor map[string]error
}

func (s *replayPublisherStub) PublishServerRegistered(_ context.Context, server types.ServerRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, server.ID)
	if err := s.failFor[server.ID]; err != nil {
		return err
	}
	return nil
}

func (s *replayPublisherStub) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func TestReplayActiveServerRegistrations(t *testing.T) {
	t.Parallel()

	t.Run("nil publisher is safe", func(t *testing.T) {
		t.Parallel()
		replayActiveServerRegistrations(context.Background(), nil, []types.ServerRecord{
			{ID: "srv-1", Name: "alpha"},
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	})

	t.Run("replays active records and continues on publish errors", func(t *testing.T) {
		t.Parallel()
		pub := &replayPublisherStub{
			failFor: map[string]error{
				"srv-2": errors.New("nats unavailable"),
			},
		}
		replayActiveServerRegistrations(context.Background(), pub, []types.ServerRecord{
			{ID: "srv-1", Name: "alpha"},
			{ID: "srv-2", Name: "beta"},
			{ID: "srv-4", Name: "gamma"},
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))

		calls := pub.Calls()
		if len(calls) != 3 {
			t.Fatalf("expected three publish attempts for active servers, got %#v", calls)
		}
		if calls[0] != "srv-1" || calls[1] != "srv-2" || calls[2] != "srv-4" {
			t.Fatalf("unexpected publish order: %#v", calls)
		}
	})

	t.Run("replays active records with blank names when id is present", func(t *testing.T) {
		t.Parallel()
		pub := &replayPublisherStub{}
		replayActiveServerRegistrations(context.Background(), pub, []types.ServerRecord{
			{ID: "srv-1", Name: "alpha"},
			{ID: "srv-3", Name: "   "},
			{ID: "", Name: "missing-id"},
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))

		calls := pub.Calls()
		if len(calls) != 2 {
			t.Fatalf("expected two publish attempts for non-empty ids, got %#v", calls)
		}
		if calls[0] != "srv-1" || calls[1] != "srv-3" {
			t.Fatalf("unexpected publish order: %#v", calls)
		}
	})
}

func TestSubscribeToolCacheInvalidation(t *testing.T) {
	t.Parallel()

	t.Run("nil broadcaster is safe", func(t *testing.T) {
		t.Parallel()
		ps := proxy.NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)
		subscribeToolCacheInvalidation(nil, ps, slog.Default())
	})

	t.Run("nil proxy server is safe", func(t *testing.T) {
		t.Parallel()
		b := &testBroadcaster{callbacks: make(map[string]func([]byte))}
		subscribeToolCacheInvalidation(b, nil, slog.Default())
	})

	t.Run("deregistered event triggers full invalidation", func(t *testing.T) {
		t.Parallel()
		b := &testBroadcaster{callbacks: make(map[string]func([]byte))}
		ps := proxy.NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)

		subscribeToolCacheInvalidation(b, ps, slog.Default())

		evt := registration.NewRegistrationEvent("deregistered", "srv-1")
		data, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}

		b.mu.Lock()
		cb := b.callbacks[registration.SubjectRegistryUpdated]
		b.mu.Unlock()
		if cb == nil {
			t.Fatal("expected subscriber callback to be registered")
		}
		cb(data)
	})

	t.Run("registered event triggers per-server invalidation", func(t *testing.T) {
		t.Parallel()
		b := &testBroadcaster{callbacks: make(map[string]func([]byte))}
		ps := proxy.NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)

		subscribeToolCacheInvalidation(b, ps, slog.Default())

		evt := registration.NewRegistrationEvent("registered", "srv-1")
		data, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}

		b.mu.Lock()
		cb := b.callbacks[registration.SubjectRegistryUpdated]
		b.mu.Unlock()
		if cb == nil {
			t.Fatal("expected subscriber callback to be registered")
		}
		cb(data)
	})

	t.Run("status_changed event triggers per-server invalidation", func(t *testing.T) {
		t.Parallel()
		b := &testBroadcaster{callbacks: make(map[string]func([]byte))}
		ps := proxy.NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)

		subscribeToolCacheInvalidation(b, ps, slog.Default())

		evt := registration.NewRegistrationEvent("status_changed", "srv-2")
		data, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}

		b.mu.Lock()
		cb := b.callbacks[registration.SubjectRegistryUpdated]
		b.mu.Unlock()
		if cb == nil {
			t.Fatal("expected subscriber callback to be registered")
		}
		cb(data)
	})

	t.Run("malformed event data does not panic", func(t *testing.T) {
		t.Parallel()
		b := &testBroadcaster{callbacks: make(map[string]func([]byte))}
		ps := proxy.NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)

		subscribeToolCacheInvalidation(b, ps, slog.Default())

		b.mu.Lock()
		cb := b.callbacks[registration.SubjectRegistryUpdated]
		b.mu.Unlock()
		if cb == nil {
			t.Fatal("expected subscriber callback to be registered")
		}
		cb([]byte("not-json"))
	})
}

func TestWireSearchReadiness(t *testing.T) {
	t.Parallel()

	t.Run("nil config is safe", func(t *testing.T) {
		t.Parallel()
		gw := server.New(&config.Config{ListenAddr: "127.0.0.1:0", MetricsAddr: "127.0.0.1:0"}, slog.Default(), nil)
		ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), &config.Config{}, nil, 0, nil)
		if status := wireSearchReadiness(nil, gw, ps); status != nil {
			t.Fatal("expected nil status function")
		}
	})

	t.Run("empty search engine skips wiring", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{SearchEngine: "", ListenAddr: "127.0.0.1:0", MetricsAddr: "127.0.0.1:0"}
		gw := server.New(cfg, slog.Default(), nil)
		ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), cfg, nil, 0, nil)
		if status := wireSearchReadiness(cfg, gw, ps); status != nil {
			t.Fatal("expected nil status function")
		}
	})

	t.Run("substring engine skips wiring", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{SearchEngine: "substring", ListenAddr: "127.0.0.1:0", MetricsAddr: "127.0.0.1:0"}
		gw := server.New(cfg, slog.Default(), nil)
		ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), cfg, nil, 0, nil)
		if status := wireSearchReadiness(cfg, gw, ps); status != nil {
			t.Fatal("expected nil status function")
		}
	})

	t.Run("nil discovery index skips wiring", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{SearchEngine: "bm25", ListenAddr: "127.0.0.1:0", MetricsAddr: "127.0.0.1:0"}
		gw := server.New(cfg, slog.Default(), nil)
		// ProgressiveDiscovery=false means no discovery index
		ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), &config.Config{}, nil, 0, nil)
		if status := wireSearchReadiness(cfg, gw, ps); status != nil {
			t.Fatal("expected nil status function")
		}
	})

	t.Run("bm25 engine wires readiness", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			SearchEngine:         "bm25",
			ProgressiveDiscovery: true,
			ListenAddr:           "127.0.0.1:0",
			MetricsAddr:          "127.0.0.1:0",
		}
		gw := server.New(cfg, slog.Default(), nil)
		ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), cfg, nil, 0, nil)
		status := wireSearchReadiness(cfg, gw, ps)
		if status == nil {
			t.Fatal("expected non-nil status function")
		}
		snapshot := status()
		if snapshot.EngineType != "bm25" {
			t.Fatalf("expected engine bm25, got %q", snapshot.EngineType)
		}
		if !snapshot.EngineReady {
			t.Fatal("expected bm25 to be ready")
		}
	})

	t.Run("hybrid engine reports vector readiness", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			SearchEngine:         "hybrid",
			ProgressiveDiscovery: true,
			ListenAddr:           "127.0.0.1:0",
			MetricsAddr:          "127.0.0.1:0",
		}
		gw := server.New(cfg, slog.Default(), nil)
		// Build a ProxyServer with progressive discovery so it has a DiscoveryIndex.
		psCfg := &config.Config{ProgressiveDiscovery: true, SearchEngine: "bm25"}
		ps := proxy.NewProxyServer(registration.NewCapabilityIndex(), psCfg, nil, 0, nil)
		// Swap the engine to a hybrid engine so the VectorReady type assertion is exercised.
		bm25 := search.NewBM25Engine()
		// Use an HTTPEmbedder pointed at a non-existent URL so VectorReady returns false.
		embedder := search.NewHTTPEmbedder("http://127.0.0.1:1")
		vec := search.NewVectorEngine(embedder)
		hybrid := search.NewHybridEngine(bm25, vec)
		ps.DiscoveryIndex().SetEngine(hybrid)

		status := wireSearchReadiness(cfg, gw, ps)
		if status == nil {
			t.Fatal("expected non-nil status function")
		}
		snapshot := status()
		if snapshot.VectorReady == nil {
			t.Fatal("expected vector readiness to be reported")
		}
		if *snapshot.VectorReady {
			t.Fatal("expected vector readiness false for unreachable embedder")
		}
		if snapshot.EngineReady {
			t.Fatal("expected overall engine readiness false when vector is down")
		}
	})
}

func TestInitOrchestrator(t *testing.T) {
	t.Parallel()
	logger := slog.Default()
	ps := proxy.NewProxyServer(
		registration.NewCapabilityIndex(),
		&config.Config{ProgressiveDiscovery: true},
		nil, 0, logger,
	)

	t.Run("disabled returns nil", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{}
		orch := initOrchestrator(cfg, serveStores{}, ps, logger)
		if orch != nil {
			t.Fatal("expected nil when disabled")
		}
	})

	t.Run("enabled without providers returns nil", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{Orchestrate: config.OrchestrateConfig{Enabled: true}}
		orch := initOrchestrator(cfg, serveStores{}, ps, logger)
		if orch != nil {
			t.Fatal("expected nil when no providers")
		}
	})

	t.Run("enabled with bad provider returns nil", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Orchestrate: config.OrchestrateConfig{
				Enabled: true,
				Providers: []config.LLMProviderConfig{
					{Name: "openai", APIKey: "", Model: "gpt-4o"},
				},
			},
		}
		orch := initOrchestrator(cfg, serveStores{}, ps, logger)
		if orch != nil {
			t.Fatal("expected nil when provider has no API key")
		}
	})

	t.Run("enabled with valid provider returns orchestrator", func(t *testing.T) {
		t.Parallel()
		db, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock: %v", err)
		}
		defer func() { _ = db.Close() }()

		stores := serveStores{
			auditStore:          storepkg.NewPostgresAuditStore(db),
			classificationStore: storepkg.NewPostgresToolClassificationStore(db),
		}
		cfg := &config.Config{
			Orchestrate: config.OrchestrateConfig{
				Enabled: true,
				Providers: []config.LLMProviderConfig{
					{Name: "openai", APIKey: "sk-test", Model: "gpt-4o"},
				},
			},
		}
		orch := initOrchestrator(cfg, stores, ps, logger)
		if orch == nil {
			t.Fatal("expected non-nil orchestrator")
		}
	})
}

// stubClassificationStore is a minimal in-memory stub for ToolClassificationStore.
type stubClassificationStore struct {
	storepkg.ToolClassificationStore
	data map[string]*types.ToolClassification
}

func (s *stubClassificationStore) Get(_ context.Context, name string) (*types.ToolClassification, error) {
	c := s.data[name]
	return c, nil
}

func TestOrchestratorDiscoverer(t *testing.T) {
	t.Parallel()

	di := proxy.NewDiscoveryIndex()
	di.Index([]proxy.ToolDefinition{
		{Name: "k8s.list_pods", Description: "List pods", InputSchema: json.RawMessage(`{}`)},
		{Name: "k8s.get_logs", Description: "Get logs", InputSchema: json.RawMessage(`{}`)},
	})

	classStore := &stubClassificationStore{
		data: map[string]*types.ToolClassification{
			"k8s.list_pods": {ToolName: "k8s.list_pods", RiskLevel: types.RiskReadOnly},
		},
	}

	d := &orchestratorDiscoverer{discovery: di, classifications: classStore}

	t.Run("SearchTools returns enriched candidates", func(t *testing.T) {
		t.Parallel()
		results, err := d.SearchTools(context.Background(), "list", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected results")
		}
		found := false
		for _, r := range results {
			if r.Name == "k8s.list_pods" {
				found = true
				if r.RiskLevel != "read_only" {
					t.Fatalf("expected risk_level read_only, got %q", r.RiskLevel)
				}
			}
		}
		if !found {
			t.Fatal("expected k8s.list_pods in results")
		}
	})

	t.Run("GetToolSchemas returns full schemas", func(t *testing.T) {
		t.Parallel()
		results, err := d.GetToolSchemas(context.Background(), []string{"k8s.list_pods", "k8s.get_logs"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("nil discovery returns error", func(t *testing.T) {
		t.Parallel()
		nd := &orchestratorDiscoverer{discovery: nil}
		_, err := nd.SearchTools(context.Background(), "test", 10)
		if err == nil {
			t.Fatal("expected error")
		}
		_, err = nd.GetToolSchemas(context.Background(), []string{"test"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestOrchestratorAuditor(t *testing.T) {
	t.Parallel()

	t.Run("nil store is safe", func(t *testing.T) {
		t.Parallel()
		a := &orchestratorAuditor{store: nil, logger: slog.Default()}
		a.LogOrchestration(context.Background(), "test", map[string]any{"key": "val"})
	})
}

func TestOrchestratorActivator(t *testing.T) {
	t.Parallel()

	t.Run("nil proxy is safe", func(t *testing.T) {
		t.Parallel()
		a := &orchestratorActivator{proxy: nil}
		a.ActivateTools(context.Background(), []string{"tool"})
	})

	t.Run("calls ActivateToolsByName", func(t *testing.T) {
		t.Parallel()
		ps := proxy.NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{ProgressiveDiscovery: true},
			nil, 0, nil,
		)
		a := &orchestratorActivator{proxy: ps}
		// Should not panic even with no matching tools.
		a.ActivateTools(context.Background(), []string{"nonexistent"})
	})
}
