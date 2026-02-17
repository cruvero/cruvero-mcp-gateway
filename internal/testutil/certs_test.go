package testutil

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestGenerateTestCertsReturnsValidFixtures(t *testing.T) {
	t.Parallel()

	certs := GenerateTestCerts(t)

	if len(certs.CACertPEM) == 0 || len(certs.CAKeyPEM) == 0 {
		t.Fatalf("expected CA cert and key PEM data")
	}
	if len(certs.ServerCertPEM) == 0 || len(certs.ServerKeyPEM) == 0 {
		t.Fatalf("expected server cert and key PEM data")
	}
	if len(certs.ClientCertPEM) == 0 || len(certs.ClientKeyPEM) == 0 {
		t.Fatalf("expected client cert and key PEM data")
	}

	caCert := parseCertificatePEM(t, certs.CACertPEM)
	if !caCert.IsCA {
		t.Fatalf("expected generated CA certificate to have IsCA=true")
	}

	clientCert := parseCertificatePEM(t, certs.ClientCertPEM)
	if len(clientCert.URIs) == 0 {
		t.Fatalf("expected client certificate to include SPIFFE URI SAN")
	}
	if clientCert.URIs[0].String() != certs.ClientSPIFFE {
		t.Fatalf("expected SPIFFE URI %q, got %q", certs.ClientSPIFFE, clientCert.URIs[0].String())
	}
}

func parseCertificatePEM(t *testing.T, certPEM []byte) *x509.Certificate {
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
