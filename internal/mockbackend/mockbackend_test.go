package mockbackend

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/testutil"
)

func TestConfigValidateDefaultsAndErrors(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing TLS paths to fail validation")
	}

	cfg = Config{
		TLSCAPath:   "/tmp/ca.crt",
		TLSCertPath: "/tmp/client.crt",
		TLSKeyPath:  "/tmp/client.key",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate with defaults: %v", err)
	}
	if cfg.GatewayURL != defaultGatewayURL {
		t.Fatalf("expected default gateway URL %q, got %q", defaultGatewayURL, cfg.GatewayURL)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Fatalf("expected default listen addr %q, got %q", defaultListenAddr, cfg.ListenAddr)
	}
	if cfg.AdvertisePort != defaultAdvertisePort {
		t.Fatalf("expected default advertise port %d, got %d", defaultAdvertisePort, cfg.AdvertisePort)
	}
}

func TestGatewayLifecycleRequests(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	certDir := t.TempDir()
	writeFile(t, filepath.Join(certDir, "ca.crt"), certs.CACertPEM)
	writeFile(t, filepath.Join(certDir, "client.crt"), certs.ClientCertPEM)
	writeFile(t, filepath.Join(certDir, "client.key"), certs.ClientKeyPEM)

	var mu sync.Mutex
	registerCount := 0
	heartbeatCount := 0
	deregisterCount := 0

	handler := http.NewServeMux()
	handler.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	handler.HandleFunc("/v1/registrations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		mu.Lock()
		registerCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"instance_id":                "instance-1",
			"heartbeat_interval_seconds": 1,
		})
	})
	handler.HandleFunc("/v1/registrations/instance-1/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected heartbeat method %s", r.Method)
		}
		mu.Lock()
		heartbeatCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "active"})
	})
	handler.HandleFunc("/v1/registrations/instance-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected deregister method %s", r.Method)
		}
		mu.Lock()
		deregisterCount++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	server := testutil.NewTestServer(t, handler, certs)
	server.URL = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	cfg := Config{
		GatewayURL:        server.URL,
		ListenAddr:        "127.0.0.1:0",
		AdvertiseHost:     "mock-backend",
		AdvertisePort:     18080,
		AdvertiseProtocol: "http",
		ServiceName:       "mock-backend",
		ServiceVersion:    "1.0.0",
		TLSCAPath:         filepath.Join(certDir, "ca.crt"),
		TLSCertPath:       filepath.Join(certDir, "client.crt"),
		TLSKeyPath:        filepath.Join(certDir, "client.key"),
		ReadyPollInterval: 10 * time.Millisecond,
		RetryInterval:     10 * time.Millisecond,
		ShutdownTimeout:   time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, slog.New(slog.NewTextHandler(ioDiscard{}, nil)))
	}()

	time.Sleep(1200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit")
	}

	mu.Lock()
	defer mu.Unlock()
	if registerCount == 0 {
		t.Fatal("expected at least one registration")
	}
	if heartbeatCount == 0 {
		t.Fatal("expected at least one heartbeat")
	}
	if deregisterCount == 0 {
		t.Fatal("expected deregistration on shutdown")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("MOCK_BACKEND_GATEWAY_URL", "https://gateway.localhost:8443")
	t.Setenv("MOCK_BACKEND_LISTEN_ADDR", ":18080")
	t.Setenv("MOCK_BACKEND_ADVERTISE_HOST", "mock-backend")
	t.Setenv("MOCK_BACKEND_ADVERTISE_PORT", "18080")
	t.Setenv("MOCK_BACKEND_ADVERTISE_PROTOCOL", "http")
	t.Setenv("MOCK_BACKEND_TLS_CA", "/certs/ca.crt")
	t.Setenv("MOCK_BACKEND_TLS_CERT", "/certs/client.crt")
	t.Setenv("MOCK_BACKEND_TLS_KEY", "/certs/client.key")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.AdvertisePort != 18080 {
		t.Fatalf("expected advertise port 18080, got %d", cfg.AdvertisePort)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
