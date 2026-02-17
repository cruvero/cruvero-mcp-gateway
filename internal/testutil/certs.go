package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"
)

// CertBundle contains generated CA, server, and client certificate fixtures for tests.
type CertBundle struct {
	CACertPEM     []byte `json:"ca_cert_pem"`
	CAKeyPEM      []byte `json:"ca_key_pem"`
	ServerCertPEM []byte `json:"server_cert_pem"`
	ServerKeyPEM  []byte `json:"server_key_pem"`
	ClientCertPEM []byte `json:"client_cert_pem"`
	ClientKeyPEM  []byte `json:"client_key_pem"`
	ClientSPIFFE  string `json:"client_spiffe"`
}

// GenerateTestCerts generates CA, server, and client certificates for mTLS test fixtures.
func GenerateTestCerts(t *testing.T) *CertBundle {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-ca",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca certificate: %v", err)
	}

	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca certificate: %v", err)
	}

	serverCertPEM, serverKeyPEM := generateLeafCert(t, caCert, caKey, 2, false)
	clientCertPEM, clientKeyPEM := generateLeafCert(t, caCert, caKey, 3, true)

	caCertPEM, err := encodePEM("CERTIFICATE", caDER)
	if err != nil {
		t.Fatalf("encode ca certificate: %v", err)
	}

	caKeyPEM, err := encodePEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey))
	if err != nil {
		t.Fatalf("encode ca private key: %v", err)
	}

	return &CertBundle{
		CACertPEM:     caCertPEM,
		CAKeyPEM:      caKeyPEM,
		ServerCertPEM: serverCertPEM,
		ServerKeyPEM:  serverKeyPEM,
		ClientCertPEM: clientCertPEM,
		ClientKeyPEM:  clientKeyPEM,
		ClientSPIFFE:  "spiffe://example.org/test-client",
	}
}

func generateLeafCert(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, serial int64, client bool) ([]byte, []byte) {
	t.Helper()

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}

	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			CommonName: fmt.Sprintf("leaf-%d", serial),
		},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		URIs:         []*url.URL{},
		SubjectKeyId: []byte{1, 2, 3, byte(serial)},
	}

	if client {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		spiffeURI, parseErr := url.Parse("spiffe://example.org/test-client")
		if parseErr != nil {
			t.Fatalf("parse spiffe uri: %v", parseErr)
		}
		tpl.URIs = []*url.URL{spiffeURI}
		tpl.DNSNames = nil
		tpl.IPAddresses = nil
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}

	leafCertPEM, err := encodePEM("CERTIFICATE", leafDER)
	if err != nil {
		t.Fatalf("encode leaf certificate: %v", err)
	}

	leafKeyPEM, err := encodePEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey))
	if err != nil {
		t.Fatalf("encode leaf private key: %v", err)
	}

	return leafCertPEM, leafKeyPEM
}

func encodePEM(blockType string, der []byte) ([]byte, error) {
	block := &pem.Block{Type: blockType, Bytes: der}
	encoded := pem.EncodeToMemory(block)
	if len(encoded) == 0 {
		return nil, fmt.Errorf("empty PEM encoding for block type %s", blockType)
	}
	return encoded, nil
}
