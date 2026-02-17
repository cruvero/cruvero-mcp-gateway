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
