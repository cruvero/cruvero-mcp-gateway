package resilience

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	defaultMaxIdlePerHost = 10
	defaultIdleTimeout    = 90 * time.Second
	defaultHandshakeTime  = 10 * time.Second
	defaultResponseTime   = 30 * time.Second
	defaultDialTimeout    = 30 * time.Second
	defaultKeepAlive      = 30 * time.Second
)

// PoolOptions configures backend HTTP transport pooling behavior.
type PoolOptions struct {
	MaxIdlePerHost  int           `json:"max_idle_per_host"`
	IdleTimeout     time.Duration `json:"idle_timeout"`
	HandshakeTimeout time.Duration `json:"handshake_timeout"`
	ResponseTimeout time.Duration `json:"response_timeout"`
}

// DefaultPoolOptions returns default transport pool settings.
func DefaultPoolOptions() PoolOptions {
	return PoolOptions{
		MaxIdlePerHost:  defaultMaxIdlePerHost,
		IdleTimeout:     defaultIdleTimeout,
		HandshakeTimeout: defaultHandshakeTime,
		ResponseTimeout: defaultResponseTime,
	}
}

// NewTransport creates a tuned HTTP transport for backend MCP calls.
func NewTransport(tlsConfig *tls.Config, opts PoolOptions) *http.Transport {
	opts = normalizePoolOptions(opts)
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: defaultDialTimeout, KeepAlive: defaultKeepAlive}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          opts.MaxIdlePerHost * 10,
		MaxIdleConnsPerHost:   opts.MaxIdlePerHost,
		IdleConnTimeout:       opts.IdleTimeout,
		TLSHandshakeTimeout:   opts.HandshakeTimeout,
		ResponseHeaderTimeout: opts.ResponseTimeout,
		TLSClientConfig:       tlsConfig,
	}
}

func normalizePoolOptions(opts PoolOptions) PoolOptions {
	defaults := DefaultPoolOptions()
	if opts.MaxIdlePerHost <= 0 {
		opts.MaxIdlePerHost = defaults.MaxIdlePerHost
	}
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaults.IdleTimeout
	}
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = defaults.HandshakeTimeout
	}
	if opts.ResponseTimeout <= 0 {
		opts.ResponseTimeout = defaults.ResponseTimeout
	}
	return opts
}
