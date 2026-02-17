package identity

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/testutil"
)

func TestBuildServerTLSConfigSuccess(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	paths := writeCertFiles(t, certs)

	cfg, err := BuildServerTLSConfig(TLSConfig{
		CertPath:     paths.serverCert,
		KeyPath:      paths.serverKey,
		ClientCAPath: paths.caCert,
	})
	if err != nil {
		t.Fatalf("build server tls config: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected min tls version %d, got %d", tls.VersionTLS13, cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("expected client auth mode %v, got %v", tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected one certificate, got %d", len(cfg.Certificates))
	}
	if cfg.ClientCAs == nil {
		t.Fatal("expected client CA pool")
	}
}

func TestBuildClientTLSConfigSuccess(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	paths := writeCertFiles(t, certs)

	cfg, err := BuildClientTLSConfig(TLSConfig{
		CABundlePath: paths.caCert,
		CertPath:     paths.clientCert,
		KeyPath:      paths.clientKey,
	})
	if err != nil {
		t.Fatalf("build client tls config: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("expected min tls version %d, got %d", tls.VersionTLS13, cfg.MinVersion)
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected root CA pool")
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected one certificate, got %d", len(cfg.Certificates))
	}
}

func TestBuildTLSConfigErrors(t *testing.T) {
	t.Parallel()

	_, err := BuildServerTLSConfig(TLSConfig{
		CertPath:     "missing.crt",
		KeyPath:      "missing.key",
		ClientCAPath: "missing-ca.crt",
	})
	if err == nil {
		t.Fatal("expected server tls config error for missing cert files")
	}

	_, err = BuildClientTLSConfig(TLSConfig{})
	if err == nil {
		t.Fatal("expected client tls config error for missing ca path")
	}
}

func TestBuildTLSConfigInvalidCertContent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	invalidPath := filepath.Join(tempDir, "invalid.pem")
	if err := os.WriteFile(invalidPath, []byte("not-a-valid-pem"), 0o600); err != nil {
		t.Fatalf("write invalid pem file: %v", err)
	}

	_, err := LoadCertPool(invalidPath)
	if err == nil {
		t.Fatal("expected error when loading invalid cert pool")
	}
}

func TestLoadCertPoolSuccess(t *testing.T) {
	t.Parallel()

	certs := testutil.GenerateTestCerts(t)
	tempDir := t.TempDir()
	caPath := filepath.Join(tempDir, "ca.crt")
	if err := os.WriteFile(caPath, certs.CACertPEM, 0o600); err != nil {
		t.Fatalf("write ca cert: %v", err)
	}

	pool, err := LoadCertPool(caPath)
	if err != nil {
		t.Fatalf("load cert pool: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil cert pool")
	}
}

type certPaths struct {
	caCert     string
	serverCert string
	serverKey  string
	clientCert string
	clientKey  string
}

func writeCertFiles(t *testing.T, certs *testutil.CertBundle) certPaths {
	t.Helper()

	tempDir := t.TempDir()
	paths := certPaths{
		caCert:     filepath.Join(tempDir, "ca.crt"),
		serverCert: filepath.Join(tempDir, "server.crt"),
		serverKey:  filepath.Join(tempDir, "server.key"),
		clientCert: filepath.Join(tempDir, "client.crt"),
		clientKey:  filepath.Join(tempDir, "client.key"),
	}

	writes := []struct {
		path string
		data []byte
	}{
		{path: paths.caCert, data: certs.CACertPEM},
		{path: paths.serverCert, data: certs.ServerCertPEM},
		{path: paths.serverKey, data: certs.ServerKeyPEM},
		{path: paths.clientCert, data: certs.ClientCertPEM},
		{path: paths.clientKey, data: certs.ClientKeyPEM},
	}

	for _, w := range writes {
		if err := os.WriteFile(w.path, w.data, 0o600); err != nil {
			t.Fatalf("write cert fixture %s: %v", w.path, err)
		}
	}

	return paths
}
