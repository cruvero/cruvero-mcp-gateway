package registration

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// channelBroadcaster delivers messages through channels for deterministic testing.
type channelBroadcaster struct {
	mu       sync.Mutex
	handlers map[string][]func(data []byte)
}

func newChannelBroadcaster() *channelBroadcaster {
	return &channelBroadcaster{
		handlers: make(map[string][]func(data []byte)),
	}
}

func (b *channelBroadcaster) Publish(subject string, data []byte) error {
	b.mu.Lock()
	handlers := make([]func([]byte), len(b.handlers[subject]))
	copy(handlers, b.handlers[subject])
	b.mu.Unlock()
	for _, handler := range handlers {
		handler(data)
	}
	return nil
}

func (b *channelBroadcaster) Subscribe(subject string, handler func(data []byte)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[subject] = append(b.handlers[subject], handler)
	return nil
}

func (b *channelBroadcaster) Close() error {
	return nil
}

func TestRegistrationSubscriberRegisteredEvent(t *testing.T) {
	t.Parallel()

	broadcaster := newChannelBroadcaster()
	index := NewCapabilityIndex()
	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{
				ID:     id,
				Status: types.StatusActive,
				Capabilities: types.Capability{
					Tools: []string{"tool.alpha"},
				},
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}, nil
		},
	}

	sub := NewRegistrationSubscriber(broadcaster, index, serverStore, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewRegistrationEvent("registered", "srv-1")
	data, _ := json.Marshal(evt)
	if err := broadcaster.Publish(SubjectRegistryUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	tools := index.ListTools()
	if len(tools) != 1 || tools[0] != "tool.alpha" {
		t.Fatalf("expected [tool.alpha], got %v", tools)
	}
}

func TestRegistrationSubscriberDeregisteredEvent(t *testing.T) {
	t.Parallel()

	broadcaster := newChannelBroadcaster()
	index := NewCapabilityIndex()

	index.Add(types.ServerRecord{
		ID:     "srv-1",
		Status: types.StatusActive,
		Capabilities: types.Capability{
			Tools: []string{"tool.alpha"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if len(index.ListTools()) == 0 {
		t.Fatal("expected index to have tools after add")
	}

	sub := NewRegistrationSubscriber(broadcaster, index, &mockServerStore{}, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewRegistrationEvent("deregistered", "srv-1")
	data, _ := json.Marshal(evt)
	if err := broadcaster.Publish(SubjectRegistryUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	tools := index.ListTools()
	if len(tools) != 0 {
		t.Fatalf("expected empty tools after deregister, got %v", tools)
	}
}

func TestRegistrationSubscriberStatusChangedEvent(t *testing.T) {
	t.Parallel()

	broadcaster := newChannelBroadcaster()
	index := NewCapabilityIndex()

	index.Add(types.ServerRecord{
		ID:     "srv-1",
		Status: types.StatusActive,
		Capabilities: types.Capability{
			Tools: []string{"tool.old"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})

	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return &types.ServerRecord{
				ID:     id,
				Status: types.StatusActive,
				Capabilities: types.Capability{
					Tools: []string{"tool.updated"},
				},
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}, nil
		},
	}

	sub := NewRegistrationSubscriber(broadcaster, index, serverStore, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewRegistrationEvent("status_changed", "srv-1")
	data, _ := json.Marshal(evt)
	if err := broadcaster.Publish(SubjectRegistryUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	tools := index.ListTools()
	if len(tools) != 1 || tools[0] != "tool.updated" {
		t.Fatalf("expected [tool.updated], got %v", tools)
	}
}

func TestRegistrationSubscriberMalformedJSON(t *testing.T) {
	t.Parallel()

	broadcaster := newChannelBroadcaster()
	index := NewCapabilityIndex()

	sub := NewRegistrationSubscriber(broadcaster, index, &mockServerStore{}, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	// Malformed JSON should not panic.
	if err := broadcaster.Publish(SubjectRegistryUpdated, []byte("not json")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(index.ListTools()) != 0 {
		t.Fatal("expected empty index after malformed event")
	}
}

func TestRegistrationSubscriberRefreshServerError(t *testing.T) {
	t.Parallel()

	broadcaster := newChannelBroadcaster()
	index := NewCapabilityIndex()
	serverStore := &mockServerStore{
		getFn: func(ctx context.Context, id string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
	}

	sub := NewRegistrationSubscriber(broadcaster, index, serverStore, testRegistrationLogger())
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}

	evt := NewRegistrationEvent("registered", "srv-missing")
	data, _ := json.Marshal(evt)

	// Should not panic even when the store returns an error.
	if err := broadcaster.Publish(SubjectRegistryUpdated, data); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func TestRegistrationSubscriberNilBroadcaster(t *testing.T) {
	t.Parallel()

	sub := NewRegistrationSubscriber(nil, NewCapabilityIndex(), &mockServerStore{}, nil)
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start with nil broadcaster should not error: %v", err)
	}
}

func TestRegistrationSubscriberNilReceiver(t *testing.T) {
	t.Parallel()

	var sub *RegistrationSubscriber
	if err := sub.Start(context.Background()); err != nil {
		t.Fatalf("start nil subscriber should not error: %v", err)
	}
}
