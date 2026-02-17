package identity

import (
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/testutil"
)

func TestExtractSPIFFEIDFromCert(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	clientCert := parsePEMCertificate(t, certs.ClientCertPEM)

	id, err := ExtractSPIFFEID([]*x509.Certificate{clientCert})
	if err != nil {
		t.Fatalf("extract spiffe id: %v", err)
	}
	if id != certs.ClientSPIFFE {
		t.Fatalf("expected %q, got %q", certs.ClientSPIFFE, id)
	}
}

func TestExtractSPIFFEIDErrors(t *testing.T) {
	t.Parallel()

	if _, err := ExtractSPIFFEID(nil); err == nil {
		t.Fatal("expected error for empty certificate slice")
	}

	nonURI := &x509.Certificate{}
	if _, err := ExtractSPIFFEID([]*x509.Certificate{nonURI}); err == nil {
		t.Fatal("expected error when certificate has no URI SAN")
	}

	httpsURI, parseErr := url.Parse("https://example.org/workload")
	if parseErr != nil {
		t.Fatalf("parse uri: %v", parseErr)
	}
	nonSPIFFE := &x509.Certificate{URIs: []*url.URL{httpsURI}}
	if _, err := ExtractSPIFFEID([]*x509.Certificate{nonSPIFFE}); err == nil {
		t.Fatal("expected error when certificate has no SPIFFE URI SAN")
	}
}

func TestParseSPIFFEID(t *testing.T) {
	t.Parallel()

	trustDomain, workloadID, err := ParseSPIFFEID("spiffe://example.org/ns/default/sa/server")
	if err != nil {
		t.Fatalf("parse spiffe id: %v", err)
	}
	if trustDomain != "example.org" {
		t.Fatalf("expected trust domain example.org, got %q", trustDomain)
	}
	if workloadID != "ns/default/sa/server" {
		t.Fatalf("unexpected workload id: %q", workloadID)
	}
}

func TestParseSPIFFEIDErrors(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"http://example.org/ns/default",
		"spiffe:///ns/default",
		"spiffe://example.org",
		"spiffe://example.org/..",
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt, func(t *testing.T) {
			t.Parallel()
			if _, _, err := ParseSPIFFEID(tt); err == nil {
				t.Fatalf("expected parse error for %q", tt)
			}
		})
	}
}

func TestValidateSPIFFEID(t *testing.T) {
	t.Parallel()

	id := "spiffe://example.org/ns/default/sa/server"

	if err := ValidateSPIFFEID(id, nil); err != nil {
		t.Fatalf("expected nil error with empty allowlist, got %v", err)
	}

	if err := ValidateSPIFFEID(id, []string{"spiffe://example.org/ns/default"}); err != nil {
		t.Fatalf("expected allowed prefix validation to pass, got %v", err)
	}

	if err := ValidateSPIFFEID(id, []string{"spiffe://other.org"}); err == nil {
		t.Fatal("expected disallowed prefix validation to fail")
	}

	if err := ValidateSPIFFEID("bad-id", []string{"spiffe://example.org"}); err == nil {
		t.Fatal("expected invalid spiffe id to fail validation")
	}
}

func parsePEMCertificate(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode certificate pem")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	return cert
}
