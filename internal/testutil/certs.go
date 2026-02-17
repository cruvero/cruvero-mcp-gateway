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

	CACert     []byte `json:"ca_cert"`
	ServerCert []byte `json:"server_cert"`
	ServerKey  []byte `json:"server_key"`
	ClientCert []byte `json:"client_cert"`
	// #nosec G117 -- test fixture key material for ephemeral certificates.
	ClientKey []byte `json:"client_key"`
	SPIFFEID  string `json:"spiffe_id"`
}

// GenerateTestCerts generates CA, server, and client certificates for mTLS test fixtures.
func GenerateTestCerts(t *testing.T) *CertBundle {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	mustNoErr(t, err, "generate ca key")

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
	mustNoErr(t, err, "create ca certificate")

	caCert, err := x509.ParseCertificate(caDER)
	mustNoErr(t, err, "parse ca certificate")

	serverCertPEM, serverKeyPEM := generateLeafCert(t, caCert, caKey, 2, false)
	clientCertPEM, clientKeyPEM := generateLeafCert(t, caCert, caKey, 3, true)

	caCertPEM, err := encodePEM("CERTIFICATE", caDER)
	mustNoErr(t, err, "encode ca certificate")

	caKeyPEM, err := encodePEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey))
	mustNoErr(t, err, "encode ca private key")

	spiffeID := "spiffe://example.org/test-client"
	return &CertBundle{
		CACertPEM:     caCertPEM,
		CAKeyPEM:      caKeyPEM,
		ServerCertPEM: serverCertPEM,
		ServerKeyPEM:  serverKeyPEM,
		ClientCertPEM: clientCertPEM,
		ClientKeyPEM:  clientKeyPEM,
		ClientSPIFFE:  spiffeID,

		CACert:     caCertPEM,
		ServerCert: serverCertPEM,
		ServerKey:  serverKeyPEM,
		ClientCert: clientCertPEM,
		ClientKey:  clientKeyPEM,
		SPIFFEID:   spiffeID,
	}
}

func generateLeafCert(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, serial int64, client bool) ([]byte, []byte) {
	t.Helper()

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	mustNoErr(t, err, "generate leaf key")

	serialBytes := big.NewInt(serial).Bytes()
	subjectKeyID := append([]byte{1, 2, 3}, serialBytes...)

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
		SubjectKeyId: subjectKeyID,
	}

	if client {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		spiffeURI, parseErr := url.Parse("spiffe://example.org/test-client")
		mustNoErr(t, parseErr, "parse spiffe uri")
		tpl.URIs = []*url.URL{spiffeURI}
		tpl.DNSNames = nil
		tpl.IPAddresses = nil
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &leafKey.PublicKey, caKey)
	mustNoErr(t, err, "create leaf certificate")

	leafCertPEM, err := encodePEM("CERTIFICATE", leafDER)
	mustNoErr(t, err, "encode leaf certificate")

	leafKeyPEM, err := encodePEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(leafKey))
	mustNoErr(t, err, "encode leaf private key")

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

func mustNoErr(t *testing.T, err error, message string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}
