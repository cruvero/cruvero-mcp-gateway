package identity

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/testutil"
)

func TestMTLSMiddlewareValidConnection(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	clientCert := parseCert(t, certs.ClientCertPEM)

	handler := MTLSMiddleware([]string{"spiffe://example.org"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok {
			t.Fatal("expected identity in context")
		}
		if id.Type != IdentityMTLS {
			t.Fatalf("expected mtls identity type, got %q", id.Type)
		}
		if id.ID != certs.ClientSPIFFE {
			t.Fatalf("expected spiffe id %q, got %q", certs.ClientSPIFFE, id.ID)
		}
		if !id.HasScope(ScopeAdmin) {
			t.Fatalf("expected admin scope in mtls identity")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{clientCert},
		VerifiedChains:   [][]*x509.Certificate{{clientCert}},
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestMTLSMiddlewareNoTLSConnection(t *testing.T) {
	t.Parallel()

	handler := MTLSMiddleware(nil, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
	assertErrorBody(t, rec, "missing or invalid mTLS client certificate")
}

func TestMTLSMiddlewareNoVerifiedChain(t *testing.T) {
	t.Parallel()

	handler := MTLSMiddleware(nil, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestMTLSMiddlewareDisallowedSPIFFEID(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	clientCert := parseCert(t, certs.ClientCertPEM)

	handler := MTLSMiddleware([]string{"spiffe://other.org"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.TLS = &tls.ConnectionState{
		VerifiedChains: [][]*x509.Certificate{{clientCert}},
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
	assertErrorBody(t, rec, "spiffe identity is not allowed")
}

func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json body: %v", err)
	}
	if payload["error"] != expected {
		t.Fatalf("expected error %q, got %q", expected, payload["error"])
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
