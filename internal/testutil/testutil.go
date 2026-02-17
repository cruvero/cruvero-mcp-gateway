package testutil

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// SetupTestNATS starts an embedded NATS server and returns a client connection and URL.
func SetupTestNATS(t *testing.T) (*nats.Conn, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping NATS integration setup in short mode")
	}

	natsServer, err := server.NewServer(&server.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatalf("create embedded nats server: %v", err)
	}

	go natsServer.Start()
	if !natsServer.ReadyForConnections(5 * time.Second) {
		natsServer.Shutdown()
		t.Fatal("embedded nats server did not become ready")
	}

	natsURL := fmt.Sprintf("nats://%s", natsServer.Addr().String())
	conn, err := nats.Connect(natsURL)
	if err != nil {
		natsServer.Shutdown()
		t.Fatalf("connect to embedded nats server: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Drain()
		conn.Close()
		natsServer.Shutdown()
	})

	return conn, natsURL
}

// NewTestServer creates an HTTPS httptest server from test cert fixtures.
func NewTestServer(t *testing.T, handler http.Handler, certs *CertBundle) *httptest.Server {
	t.Helper()
	if handler == nil {
		handler = http.NewServeMux()
	}
	if certs == nil {
		certs = GenerateTestCerts(t)
	}

	pair, err := tls.X509KeyPair(certs.ServerCertPEM, certs.ServerKeyPEM)
	if err != nil {
		t.Fatalf("parse server keypair: %v", err)
	}

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pair},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv
}
