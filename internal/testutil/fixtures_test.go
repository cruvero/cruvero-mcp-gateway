package testutil

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestGenerateTestCertsProducesValidChain(t *testing.T) {
	t.Parallel()

	certs := GenerateTestCerts(t)

	ca := parseCert(t, certs.CACertPEM)
	server := parseCert(t, certs.ServerCertPEM)
	client := parseCert(t, certs.ClientCertPEM)

	roots := x509.NewCertPool()
	roots.AddCert(ca)

	if _, err := server.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost"}); err != nil {
		t.Fatalf("verify server chain: %v", err)
	}
	if _, err := client.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("verify client chain: %v", err)
	}

	if certs.ClientSPIFFE == "" {
		t.Fatal("expected client SPIFFE ID")
	}
}

func TestFixturesProduceValidValues(t *testing.T) {
	t.Parallel()

	record := TestServerRecord()
	if record.Status != types.StatusActive || !record.Status.IsRoutable() {
		t.Fatalf("expected active routable server record, got %s", record.Status)
	}

	registration := TestRegistration()
	if registration.ServiceName == "" || registration.ListenAddress == "" {
		t.Fatalf("expected populated registration fixture, got %+v", registration)
	}

	apiKey, plaintext := TestAPIKey()
	if plaintext == "" {
		t.Fatal("expected plaintext api key fixture")
	}
	if apiKey.KeyLookupHash == "" || apiKey.KeyBcryptHash == "" {
		t.Fatalf("expected API key fixture hashes, got %+v", apiKey)
	}
	if !auth.VerifyAPIKey(plaintext, apiKey.KeyBcryptHash) {
		t.Fatal("expected plaintext fixture key to verify against bcrypt hash")
	}

	profiles := TestPolicyProfiles()
	if len(profiles) != 3 {
		t.Fatalf("expected 3 policy profiles, got %d", len(profiles))
	}
	if profiles["default"].RateLimit != 10 || profiles["default"].RateBurst != 20 {
		t.Fatalf("unexpected default profile: %+v", profiles["default"])
	}
}

func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}
