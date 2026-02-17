package identity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// TLSConfig contains certificate and CA paths for TLS configuration builders.
type TLSConfig struct {
	CABundlePath string `json:"ca_bundle_path"`
	CertPath     string `json:"cert_path"`
	KeyPath      string `json:"key_path"`
	ClientCAPath string `json:"client_ca_path"`
	MinVersion   uint16 `json:"min_tls_version"`
}

// BuildServerTLSConfig builds a TLS config for inbound server mTLS.
func BuildServerTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	minVersion := cfg.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS13
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("build server tls config: load certificate pair: %w", err)
	}

	clientCAPath := strings.TrimSpace(cfg.ClientCAPath)
	if clientCAPath == "" {
		clientCAPath = strings.TrimSpace(cfg.CABundlePath)
	}
	if clientCAPath == "" {
		return nil, fmt.Errorf("build server tls config: client CA bundle path is required")
	}

	clientCAPool, err := LoadCertPool(clientCAPath)
	if err != nil {
		return nil, fmt.Errorf("build server tls config: %w", err)
	}

	return &tls.Config{
		MinVersion:   minVersion,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAPool,
		Certificates: []tls.Certificate{cert},
	}, nil
}

// BuildClientTLSConfig builds a TLS config for outbound client TLS/mTLS.
func BuildClientTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	minVersion := cfg.MinVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS13
	}

	caPath := strings.TrimSpace(cfg.CABundlePath)
	if caPath == "" {
		caPath = strings.TrimSpace(cfg.ClientCAPath)
	}
	if caPath == "" {
		return nil, fmt.Errorf("build client tls config: CA bundle path is required")
	}

	rootPool, err := LoadCertPool(caPath)
	if err != nil {
		return nil, fmt.Errorf("build client tls config: %w", err)
	}

	tlsConfig := &tls.Config{
		MinVersion: minVersion,
		RootCAs:    rootPool,
	}

	if strings.TrimSpace(cfg.CertPath) != "" || strings.TrimSpace(cfg.KeyPath) != "" {
		cert, loadErr := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
		if loadErr != nil {
			return nil, fmt.Errorf("build client tls config: load certificate pair: %w", loadErr)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// LoadCertPool loads PEM encoded CA certificates into a cert pool.
func LoadCertPool(path string) (*x509.CertPool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("load cert pool: path is required")
	}

	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load cert pool: read %s: %w", path, err)
	}

	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(pemData); !ok {
		return nil, fmt.Errorf("load cert pool: parse PEM certificates from %s", path)
	}

	return pool, nil
}
