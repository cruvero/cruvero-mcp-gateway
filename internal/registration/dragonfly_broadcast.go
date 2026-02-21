package registration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/redis/go-redis/v9"
)

// DragonflyBroadcaster implements Broadcaster using DragonflyDB pub/sub.
type DragonflyBroadcaster struct {
	client *redis.Client
	logger *slog.Logger

	mu      sync.Mutex
	pubsubs []*redis.PubSub
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	ctx     context.Context
}

// NewDragonflyBroadcaster creates a broadcaster backed by a DragonflyDB client.
func NewDragonflyBroadcaster(client *redis.Client, logger *slog.Logger) *DragonflyBroadcaster {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &DragonflyBroadcaster{
		client: client,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Publish sends data to all subscribers of the subject.
func (b *DragonflyBroadcaster) Publish(subject string, data []byte) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("dragonfly broadcaster: not initialized")
	}
	if err := b.client.Publish(b.ctx, subject, data).Err(); err != nil {
		return fmt.Errorf("dragonfly broadcaster: publish %s: %w", subject, err)
	}
	return nil
}

// Subscribe registers a handler for the given subject.
func (b *DragonflyBroadcaster) Subscribe(subject string, handler func(data []byte)) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("dragonfly broadcaster: not initialized")
	}

	pubsub := b.client.Subscribe(b.ctx, subject)

	b.mu.Lock()
	b.pubsubs = append(b.pubsubs, pubsub)
	b.mu.Unlock()

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ch := pubsub.Channel()
		for {
			select {
			case <-b.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if msg != nil {
					handler([]byte(msg.Payload))
				}
			}
		}
	}()

	return nil
}

// Close cancels context, closes all pubsubs, and waits for goroutines.
func (b *DragonflyBroadcaster) Close() error {
	if b == nil {
		return nil
	}

	b.cancel()

	b.mu.Lock()
	for _, ps := range b.pubsubs {
		if ps != nil {
			_ = ps.Close()
		}
	}
	b.pubsubs = nil
	b.mu.Unlock()

	b.wg.Wait()
	return nil
}
