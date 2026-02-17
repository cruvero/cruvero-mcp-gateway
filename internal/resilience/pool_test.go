package resilience

import (
	"crypto/tls"
	"testing"
	"time"
)

func TestNewTransportWithOptions(t *testing.T) {
	t.Parallel()

	opts := PoolOptions{
		MaxIdlePerHost:  7,
		IdleTimeout:     42 * time.Second,
		HandshakeTimeout: 12 * time.Second,
		ResponseTimeout: 17 * time.Second,
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}

	transport := NewTransport(tlsConfig, opts)
	if transport == nil {
		t.Fatal("expected transport")
	}
	if transport.MaxIdleConnsPerHost != opts.MaxIdlePerHost {
		t.Fatalf("expected MaxIdleConnsPerHost=%d, got %d", opts.MaxIdlePerHost, transport.MaxIdleConnsPerHost)
	}
	if transport.MaxIdleConns != opts.MaxIdlePerHost*10 {
		t.Fatalf("expected MaxIdleConns=%d, got %d", opts.MaxIdlePerHost*10, transport.MaxIdleConns)
	}
	if transport.IdleConnTimeout != opts.IdleTimeout {
		t.Fatalf("expected IdleConnTimeout=%v, got %v", opts.IdleTimeout, transport.IdleConnTimeout)
	}
	if transport.TLSHandshakeTimeout != opts.HandshakeTimeout {
		t.Fatalf("expected TLSHandshakeTimeout=%v, got %v", opts.HandshakeTimeout, transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != opts.ResponseTimeout {
		t.Fatalf("expected ResponseHeaderTimeout=%v, got %v", opts.ResponseTimeout, transport.ResponseHeaderTimeout)
	}
	if transport.TLSClientConfig != tlsConfig {
		t.Fatal("expected transport to use provided tls config")
	}
}

func TestNewTransportUsesDefaults(t *testing.T) {
	t.Parallel()

	transport := NewTransport(nil, PoolOptions{})
	defaults := DefaultPoolOptions()

	if transport.MaxIdleConnsPerHost != defaults.MaxIdlePerHost {
		t.Fatalf("expected default MaxIdleConnsPerHost=%d, got %d", defaults.MaxIdlePerHost, transport.MaxIdleConnsPerHost)
	}
	if transport.IdleConnTimeout != defaults.IdleTimeout {
		t.Fatalf("expected default IdleConnTimeout=%v, got %v", defaults.IdleTimeout, transport.IdleConnTimeout)
	}
	if transport.TLSHandshakeTimeout != defaults.HandshakeTimeout {
		t.Fatalf("expected default TLSHandshakeTimeout=%v, got %v", defaults.HandshakeTimeout, transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout != defaults.ResponseTimeout {
		t.Fatalf("expected default ResponseHeaderTimeout=%v, got %v", defaults.ResponseTimeout, transport.ResponseHeaderTimeout)
	}
}
