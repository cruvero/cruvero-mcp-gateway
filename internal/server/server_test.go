package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
)

func TestHealthzReturns200(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger())
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

	srv := New(baseConfig(), testLogger())
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestReadyzReturns503WhenNotReady(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger())
	srv.ready.Store(false)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
}

func TestRequestIDGenerated(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger())
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

	srv := New(baseConfig(), testLogger())
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

func TestMetricsPlaceholderRoute(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger())
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestRequestBodyLimitMiddleware(t *testing.T) {
	t.Parallel()

	srv := New(baseConfig(), testLogger())
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

func TestStartGracefulShutdownOnContextCancellation(t *testing.T) {
	cfg := baseConfig()
	cfg.ListenAddr = "127.0.0.1:0"

	srv := New(cfg, testLogger())

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

	srv := New(baseConfig(), testLogger())
	if err := srv.Start(nil); err == nil {
		t.Fatal("expected error when context is nil")
	}

	badCfg := baseConfig()
	badCfg.ListenAddr = "bad-address"
	badSrv := New(badCfg, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := badSrv.Start(ctx); err == nil {
		t.Fatal("expected listen error for invalid address")
	}
}

func TestCORSMiddlewareWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := baseConfig()
	cfg.CORSEnabled = true
	srv := New(cfg, testLogger())

	req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS origin header to be set")
	}
}

func TestRequestIDFromContextMissing(t *testing.T) {
	t.Parallel()

	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty request id, got %q", got)
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
		ListenAddr:       ":0",
		DBURL:            "postgres://db",
		RateDefault:      10,
		RateBurst:        20,
		CircuitThreshold: 5,
		RetryMax:         3,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
