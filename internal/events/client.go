package events

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	defaultReconnectWait = 2 * time.Second
	defaultMaxReconnects = 30
)

type clientConfig struct {
	tlsConfig    *tls.Config
	reconnectWait time.Duration
	maxReconnects int
}

// ClientOption configures NATS client setup behavior.
type ClientOption func(*clientConfig)

// WithTLS sets the TLS config for NATS connections.
func WithTLS(cfg *tls.Config) ClientOption {
	return func(c *clientConfig) {
		c.tlsConfig = cfg
	}
}

// WithReconnectWait sets how long to wait between reconnect attempts.
func WithReconnectWait(wait time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.reconnectWait = wait
	}
}

// WithMaxReconnects sets the maximum reconnect attempts.
func WithMaxReconnects(max int) ClientOption {
	return func(c *clientConfig) {
		c.maxReconnects = max
	}
}

// Client wraps a NATS connection for gateway event operations.
type Client struct {
	conn      *nats.Conn
	gatewayID string
	logger    *slog.Logger
	connected atomic.Bool

	mu                sync.RWMutex
	disconnectHandler func()
	reconnectHandler  func()
}

// NewClient connects a new NATS client with lifecycle callbacks.
func NewClient(url string, gatewayID string, opts ...ClientOption) (*Client, error) {
	natsURL := strings.TrimSpace(url)
	if natsURL == "" {
		return nil, fmt.Errorf("new events client: nats url is required")
	}
	gateway := strings.TrimSpace(gatewayID)
	if gateway == "" {
		return nil, fmt.Errorf("new events client: gateway id is required")
	}

	cfg := &clientConfig{
		reconnectWait: defaultReconnectWait,
		maxReconnects: defaultMaxReconnects,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.reconnectWait <= 0 {
		cfg.reconnectWait = defaultReconnectWait
	}

	client := &Client{
		gatewayID: gateway,
		logger:    slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}

	options := []nats.Option{
		nats.ReconnectWait(cfg.reconnectWait),
		nats.MaxReconnects(cfg.maxReconnects),
		nats.Name("cruvero-mcp-gateway-"+gateway),
		nats.DisconnectErrHandler(func(_ *nats.Conn, disconnectErr error) {
			client.connected.Store(false)
			if disconnectErr != nil {
				client.logger.Warn("nats disconnected", slog.String("error", disconnectErr.Error()))
			} else {
				client.logger.Warn("nats disconnected")
			}
			client.callDisconnectHandler()
		}),
		nats.ReconnectHandler(func(conn *nats.Conn) {
			client.connected.Store(true)
			client.logger.Info("nats reconnected", slog.String("connected_url", conn.ConnectedUrl()))
			client.callReconnectHandler()
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			client.connected.Store(false)
			client.logger.Info("nats connection closed")
		}),
	}
	if cfg.tlsConfig != nil {
		options = append(options, nats.Secure(cfg.tlsConfig))
	}

	natsConn, err := nats.Connect(natsURL, options...)
	if err != nil {
		return nil, fmt.Errorf("new events client: connect nats: %w", err)
	}

	client.conn = natsConn
	client.connected.Store(true)
	return client, nil
}

// Publish publishes a message to the given subject.
func (c *Client) Publish(subject string, data []byte) error {
	if c == nil || c.conn == nil {
		return fmt.Errorf("publish nats message: client is not initialized")
	}
	if err := c.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("publish nats message: %w", err)
	}
	return nil
}

// Subscribe subscribes to a NATS subject.
func (c *Client) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("subscribe nats subject: client is not initialized")
	}
	sub, err := c.conn.Subscribe(subject, handler)
	if err != nil {
		return nil, fmt.Errorf("subscribe nats subject: %w", err)
	}
	return sub, nil
}

// IsConnected reports whether the client is currently connected.
func (c *Client) IsConnected() bool {
	if c == nil {
		return false
	}
	return c.connected.Load()
}

// Close closes the underlying NATS connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	c.connected.Store(false)
	c.conn.Close()
	return nil
}

// Conn returns the underlying NATS connection for direct use by subsystems
// that need low-level access (e.g., pub/sub broadcasting).
func (c *Client) Conn() *nats.Conn {
	if c == nil {
		return nil
	}
	return c.conn
}

// JetStream returns a JetStream context for KV and stream operations.
func (c *Client) JetStream() (nats.JetStreamContext, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("jetstream: client is not initialized")
	}
	js, err := c.conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return js, nil
}

// GatewayID returns the configured gateway identifier.
func (c *Client) GatewayID() string {
	if c == nil {
		return ""
	}
	return c.gatewayID
}

// SetDisconnectHandler sets an optional callback for disconnect events.
func (c *Client) SetDisconnectHandler(handler func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnectHandler = handler
}

// SetReconnectHandler sets an optional callback for reconnect events.
func (c *Client) SetReconnectHandler(handler func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconnectHandler = handler
}

func (c *Client) callDisconnectHandler() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.disconnectHandler != nil {
		c.disconnectHandler()
	}
}

func (c *Client) callReconnectHandler() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.reconnectHandler != nil {
		c.reconnectHandler()
	}
}
