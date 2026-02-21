package registration

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/nats-io/nats.go"
)

// NATSBroadcaster implements Broadcaster using a NATS connection.
type NATSBroadcaster struct {
	conn   *nats.Conn
	logger *slog.Logger

	mu   sync.Mutex
	subs []*nats.Subscription
}

// NewNATSBroadcaster creates a broadcaster backed by a NATS connection.
func NewNATSBroadcaster(conn *nats.Conn, logger *slog.Logger) *NATSBroadcaster {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &NATSBroadcaster{
		conn:   conn,
		logger: logger,
	}
}

// Publish sends data to all subscribers of the subject.
func (b *NATSBroadcaster) Publish(subject string, data []byte) error {
	if b == nil || b.conn == nil {
		return fmt.Errorf("nats broadcaster: not initialized")
	}
	if err := b.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("nats broadcaster: publish %s: %w", subject, err)
	}
	return nil
}

// Subscribe registers a handler for the given subject.
func (b *NATSBroadcaster) Subscribe(subject string, handler func(data []byte)) error {
	if b == nil || b.conn == nil {
		return fmt.Errorf("nats broadcaster: not initialized")
	}

	sub, err := b.conn.Subscribe(subject, func(msg *nats.Msg) {
		if msg != nil {
			handler(msg.Data)
		}
	})
	if err != nil {
		return fmt.Errorf("nats broadcaster: subscribe %s: %w", subject, err)
	}

	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	return nil
}

// Close unsubscribes from all subjects.
func (b *NATSBroadcaster) Close() error {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, sub := range b.subs {
		if sub != nil {
			_ = sub.Unsubscribe()
		}
	}
	b.subs = nil
	return nil
}
