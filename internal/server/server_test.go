package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/testutil"
	natsserver "github.com/nats-io/nats-server/v2/server"
)

func TestHealthzReturns200(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", payload["status"])
	}
}

func TestReadyzReturns200(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestReadyzReturns503WhenNotReady(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	srv.ready.Store(false)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
}

func TestReadyzCruveroNeverConnectedNoCacheReturns503(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CruveroEnabled = true
	cfg.NATSURL = ""

	srv := New(cfg, testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["nats"] != "disconnected" {
		t.Fatalf("expected nats disconnected, got %+v", payload["nats"])
	}
}

func TestReadyzCruveroConnectedReturns200(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CruveroEnabled = true

	srv := New(cfg, testLogger(), nil)
	manager := events.NewDegradationManager(nil, nil, nil, testLogger())
	if err := manager.OnReconnect(context.Background()); err != nil {
		t.Fatalf("set connected state: %v", err)
	}
	srv.SetDegradationManager(manager)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %+v", payload["status"])
	}
	if payload["nats"] != "connected" {
		t.Fatalf("expected nats connected, got %+v", payload["nats"])
	}
}

func TestReadyzCruveroDegradedWithCacheReturns200AndSettings(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CruveroEnabled = true

	store := &serverTestConfigStore{
		keys: []string{"config.server_settings"},
		values: map[string][]byte{
			"config.server_settings": []byte(`{"config_version":9,"servers":[{"server_name":"svc-a","effective_settings":{"max_concurrency":4}}]}`),
		},
	}
	manager := events.NewDegradationManager(nil, store, nil, testLogger())
	if err := manager.LoadCachedConfig(context.Background()); err != nil {
		t.Fatalf("load cached config: %v", err)
	}

	srv := New(cfg, testLogger(), nil)
	srv.SetDegradationManager(manager)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "degraded" {
		t.Fatalf("expected status degraded, got %+v", payload["status"])
	}
	if payload["nats"] != "disconnected" {
		t.Fatalf("expected nats disconnected, got %+v", payload["nats"])
	}
	if payload["settings_sync_status"] != "cached" {
		t.Fatalf("expected settings_sync_status cached, got %+v", payload["settings_sync_status"])
	}
	if payload["settings_config_version"] != float64(9) {
		t.Fatalf("expected settings_config_version 9, got %+v", payload["settings_config_version"])
	}
}

func TestRequestIDGenerated(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatalf("expected X-Request-ID response header")
	}
}

func TestRecoveryMiddlewareCatchesPanic(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	srv.router.Get("/panic", func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestMetricsNotExposedOnMainRouter(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestRequestBodyLimitMiddleware(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger(), nil)
	srv.router.Post("/write", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	payload := bytes.Repeat([]byte("a"), int(defaultMaxRequestBodyBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/write", bytes.NewReader(payload))
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d", rec.Code)
	}
}

func TestMountProxyRoutesAppliesRateLimitMiddleware(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.RateDefault = 1
	cfg.RateBurst = 1

	srv := New(cfg, testLogger(), nil)
	srv.MountProxyRoutes(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	firstReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	firstRec := httptest.NewRecorder()
	srv.router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d", firstRec.Code)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	secondRec := httptest.NewRecorder()
	srv.router.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second status 429, got %d", secondRec.Code)
	}
	if secondRec.Header().Get("X-RateLimit-Limit") == "" {
		t.Fatal("expected X-RateLimit-Limit header")
	}
	if secondRec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestStartGracefulShutdownOnContextCancellation(t *testing.T) {
	cfg := baseConfig()
	cfg.ListenAddr = "127.0.0.1:0"

	srv := New(cfg, testLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error on graceful shutdown, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shutdown within timeout")
	}
}

func TestStartErrors(t *testing.T) {
	t.Parallel()

	var nilServer *Server
	if err := nilServer.Start(context.Background()); err == nil {
		t.Fatal("expected error when starting nil server")
	}

	srv := New(baseConfig(), testLogger(), nil)
	if err := srv.Start(nilContext()); err == nil {
		t.Fatal("expected error when context is nil")
	}

	badCfg := baseConfig()
	badCfg.ListenAddr = "bad-address"
	badSrv := New(badCfg, testLogger(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := badSrv.Start(ctx); err == nil {
		t.Fatal("expected listen error for invalid address")
	}
}

func TestBuildInboundTLSConfig(t *testing.T) {
	t.Parallel()

	t.Run("requires ca when tls enabled", func(t *testing.T) {
		t.Parallel()

		cfg := baseConfig()
		cfg.TLSCertPath = "/tmp/server.crt"
		cfg.TLSKeyPath = "/tmp/server.key"
		cfg.TLSCAPath = ""

		if _, err := buildInboundTLSConfig(cfg); err == nil {
			t.Fatal("expected error when MCPGW_TLS_CA is empty")
		}
	})

	t.Run("builds verify-if-given tls config", func(t *testing.T) {
		t.Parallel()

		certs := testutil.GenerateTestCerts(t)
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "server.crt")
		keyPath := filepath.Join(tmpDir, "server.key")
		caPath := filepath.Join(tmpDir, "ca.crt")
		if err := os.WriteFile(certPath, certs.ServerCertPEM, 0o600); err != nil {
			t.Fatalf("write server cert: %v", err)
		}
		if err := os.WriteFile(keyPath, certs.ServerKeyPEM, 0o600); err != nil {
			t.Fatalf("write server key: %v", err)
		}
		if err := os.WriteFile(caPath, certs.CACertPEM, 0o600); err != nil {
			t.Fatalf("write ca cert: %v", err)
		}

		cfg := baseConfig()
		cfg.TLSCertPath = certPath
		cfg.TLSKeyPath = keyPath
		cfg.TLSCAPath = caPath

		tlsCfg, err := buildInboundTLSConfig(cfg)
		if err != nil {
			t.Fatalf("build inbound tls config: %v", err)
		}
		if tlsCfg == nil {
			t.Fatal("expected tls config")
		}
		if tlsCfg.ClientAuth != tls.VerifyClientCertIfGiven {
			t.Fatalf("expected ClientAuth=%v, got %v", tls.VerifyClientCertIfGiven, tlsCfg.ClientAuth)
		}
		if len(tlsCfg.Certificates) != 1 {
			t.Fatalf("expected exactly one certificate, got %d", len(tlsCfg.Certificates))
		}
		if tlsCfg.ClientCAs == nil {
			t.Fatal("expected ClientCAs to be configured")
		}
	})
}

func TestCORSMiddlewareAllowlistedOrigin(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CORSEnabled = true
	cfg.CORSAllowedOrigins = []string{"https://example.com", "https://other.com"}
	srv := New(cfg, testLogger(), nil)

	t.Run("allowlisted origin echoed", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Fatalf("expected origin echoed, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatal("expected Allow-Credentials: true")
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("expected Vary: Origin, got %q", rec.Header().Get("Vary"))
		}
	})

	t.Run("non-allowlisted origin gets no ACAO", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://evil.com")
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO header for non-allowlisted origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("expected Vary: Origin always present, got %q", rec.Header().Get("Vary"))
		}
	})

	t.Run("case insensitive origin match", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "HTTPS://EXAMPLE.COM")
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "HTTPS://EXAMPLE.COM" {
			t.Fatalf("expected case-insensitive match, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("OPTIONS preflight returns 204", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
		req.Header.Set("Origin", "https://example.com")
		rec := httptest.NewRecorder()
		srv.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected status 204, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Fatalf("expected origin echoed on preflight, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestRequestIDFromContextMissing(t *testing.T) {
	t.Parallel()

	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty request id, got %q", got)
	}
}

func TestEventPublisherDisabledByDefault(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CruveroEnabled = false
	cfg.NATSURL = "nats://127.0.0.1:4222"

	srv := New(cfg, testLogger(), nil)
	if srv.EventPublisher() != nil {
		t.Fatal("expected nil event publisher when cruvero integration disabled")
	}
}

func TestEventPublisherInitializedWhenCruveroEnabled(t *testing.T) {
	t.Parallel()

	port := freeNATSPort(t)
	natsSrv := runNATSServerForServerTests(t, port)
	defer natsSrv.Shutdown()

	cfg := baseConfig()
	cfg.CruveroEnabled = true
	cfg.NATSURL = fmt.Sprintf("nats://127.0.0.1:%d", port)
	cfg.GatewayID = "gw-server-test"

	srv := New(cfg, testLogger(), nil)
	if srv.EventPublisher() == nil {
		t.Fatal("expected event publisher when cruvero enabled and nats configured")
	}
}

func TestNATSTLSFailureSkipsConnection(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CruveroEnabled = true
	cfg.NATSURL = "nats://127.0.0.1:4222"
	cfg.GatewayID = "gw-tls-skip"
	cfg.NATSTLSEnabled = true
	cfg.NATSTLSCert = "/nonexistent/nats.crt"
	cfg.NATSTLSKey = "/nonexistent/nats.key"
	cfg.NATSTLSCa = "/nonexistent/ca.crt"

	srv := New(cfg, testLogger(), nil)
	if srv.EventsClient() != nil {
		t.Fatal("expected nil events client when NATS TLS config fails")
	}
}

func TestWriteJSONEncodeErrorPath(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]any{
		"bad": make(chan int),
	})

	if rec.Code == 0 {
		t.Fatal("expected a response status to be written")
	}
}

func baseConfig() *config.Config {
	return &config.Config{
		ListenAddr:        ":0",
		MetricsAddr:       "127.0.0.1:0",
		DBURL:             "postgres://db",
		RateDefault:       10,
		RateBurst:         20,
		CircuitThreshold:  5,
		RetryMax:          3,
		DBMaxOpenConns:       25,
		DBMaxIdleConns:       10,
		DBConnMaxLifetime:    5 * time.Minute,
		AuditRetentionDays:   90,
		AuditCleanupInterval: time.Hour,
		ShutdownTimeout:      30 * time.Second,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func runNATSServerForServerTests(t *testing.T, port int) *natsserver.Server {
	t.Helper()

	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   port,
		NoLog:  true,
		NoSigs: true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(3 * time.Second) {
		srv.Shutdown()
		t.Fatal("nats server not ready")
	}
	return srv
}

func freeNATSPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free port: %v", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

type serverTestConfigStore struct {
	keys   []string
	values map[string][]byte
}

func (s *serverTestConfigStore) Save(ctx context.Context, key string, value []byte) error {
	return nil
}

func (s *serverTestConfigStore) Load(ctx context.Context, key string) ([]byte, error) {
	return s.values[key], nil
}

func (s *serverTestConfigStore) Keys(ctx context.Context) ([]string, error) {
	return append([]string(nil), s.keys...), nil
}

func nilContext() context.Context {
	return nil
}

func TestBuildNATSTLSConfig(t *testing.T) {
	t.Parallel()

	t.Run("valid certs produce tls config", func(t *testing.T) {
		t.Parallel()

		certs := testutil.GenerateTestCerts(t)
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "nats.crt")
		keyPath := filepath.Join(tmpDir, "nats.key")
		caPath := filepath.Join(tmpDir, "ca.crt")
		if err := os.WriteFile(certPath, certs.ServerCertPEM, 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}
		if err := os.WriteFile(keyPath, certs.ServerKeyPEM, 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}
		if err := os.WriteFile(caPath, certs.CACertPEM, 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}

		tlsCfg, err := buildNATSTLSConfig(certPath, keyPath, caPath)
		if err != nil {
			t.Fatalf("build nats tls config: %v", err)
		}
		if tlsCfg == nil {
			t.Fatal("expected non-nil tls config")
		}
		if tlsCfg.MinVersion != tls.VersionTLS12 {
			t.Fatalf("expected min tls version 1.2, got %d", tlsCfg.MinVersion)
		}
		if len(tlsCfg.Certificates) != 1 {
			t.Fatalf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
		}
		if tlsCfg.RootCAs == nil {
			t.Fatal("expected non-nil RootCAs")
		}
	})

	t.Run("missing cert file returns error", func(t *testing.T) {
		t.Parallel()

		certs := testutil.GenerateTestCerts(t)
		tmpDir := t.TempDir()
		keyPath := filepath.Join(tmpDir, "nats.key")
		caPath := filepath.Join(tmpDir, "ca.crt")
		if err := os.WriteFile(keyPath, certs.ServerKeyPEM, 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}
		if err := os.WriteFile(caPath, certs.CACertPEM, 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}

		_, err := buildNATSTLSConfig(filepath.Join(tmpDir, "missing.crt"), keyPath, caPath)
		if err == nil {
			t.Fatal("expected error for missing cert file")
		}
		if !strings.Contains(err.Error(), "load nats tls cert/key") {
			t.Fatalf("expected error to mention cert/key, got %v", err)
		}
	})

	t.Run("missing key file returns error", func(t *testing.T) {
		t.Parallel()

		certs := testutil.GenerateTestCerts(t)
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "nats.crt")
		caPath := filepath.Join(tmpDir, "ca.crt")
		if err := os.WriteFile(certPath, certs.ServerCertPEM, 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}
		if err := os.WriteFile(caPath, certs.CACertPEM, 0o600); err != nil {
			t.Fatalf("write ca: %v", err)
		}

		_, err := buildNATSTLSConfig(certPath, filepath.Join(tmpDir, "missing.key"), caPath)
		if err == nil {
			t.Fatal("expected error for missing key file")
		}
		if !strings.Contains(err.Error(), "load nats tls cert/key") {
			t.Fatalf("expected error to mention cert/key, got %v", err)
		}
	})

	t.Run("invalid ca pem returns error", func(t *testing.T) {
		t.Parallel()

		certs := testutil.GenerateTestCerts(t)
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "nats.crt")
		keyPath := filepath.Join(tmpDir, "nats.key")
		caPath := filepath.Join(tmpDir, "bad-ca.crt")
		if err := os.WriteFile(certPath, certs.ServerCertPEM, 0o600); err != nil {
			t.Fatalf("write cert: %v", err)
		}
		if err := os.WriteFile(keyPath, certs.ServerKeyPEM, 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}
		if err := os.WriteFile(caPath, []byte("not-valid-pem"), 0o600); err != nil {
			t.Fatalf("write bad ca: %v", err)
		}

		_, err := buildNATSTLSConfig(certPath, keyPath, caPath)
		if err == nil {
			t.Fatal("expected error for invalid ca pem")
		}
		if !strings.Contains(err.Error(), "parse nats tls ca") {
			t.Fatalf("expected error to mention parsing ca, got %v", err)
		}
	})
}
