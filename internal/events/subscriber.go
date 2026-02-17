package events

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
)

// ConfigHandler applies a gateway config update payload.
type ConfigHandler interface {
	Handle(ctx context.Context, data []byte) error
}

// Subscriber receives gateway-scoped config updates over NATS.
type Subscriber struct {
	client        *Client
	handlers      map[string]ConfigHandler
	subscriptions []*nats.Subscription
	logger        *slog.Logger

	mu sync.RWMutex
}

// NewSubscriber creates a config subscriber.
func NewSubscriber(client *Client, logger *slog.Logger) *Subscriber {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return &Subscriber{
		client:   client,
		handlers: make(map[string]ConfigHandler),
		logger:   logger,
	}
}

// RegisterHandler registers a handler for an exact subject.
func (s *Subscriber) RegisterHandler(subject string, handler ConfigHandler) {
	if s == nil || handler == nil {
		return
	}

	normalized := strings.TrimSpace(subject)
	if normalized == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[normalized] = handler
}

// RegisterGatewaySubjects registers handlers for standard gateway config subjects.
func (s *Subscriber) RegisterGatewaySubjects(
	policyHandler ConfigHandler,
	serversHandler ConfigHandler,
	serverSettingsHandler ConfigHandler,
	authHandler ConfigHandler,
) {
	if s == nil || s.client == nil {
		return
	}

	gatewayID := s.client.GatewayID()
	s.RegisterHandler(SubjectForConfig(gatewayID, ConfigScopePolicy), policyHandler)
	s.RegisterHandler(SubjectForConfig(gatewayID, ConfigScopeServers), serversHandler)
	s.RegisterHandler(SubjectForConfig(gatewayID, ConfigScopeServerSettings), serverSettingsHandler)
	s.RegisterHandler(SubjectForConfig(gatewayID, ConfigScopeAuth), authHandler)
}

// Start subscribes to all registered config subjects.
func (s *Subscriber) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("start config subscriber: subscriber is nil")
	}
	if s.client == nil {
		return fmt.Errorf("start config subscriber: client is nil")
	}
	if ctx == nil {
		return fmt.Errorf("start config subscriber: context is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.handlers) == 0 {
		return nil
	}

	s.subscriptions = make([]*nats.Subscription, 0, len(s.handlers))
	for subject := range s.handlers {
		subjectCopy := subject
		sub, err := s.client.Subscribe(subjectCopy, func(msg *nats.Msg) {
			s.routeMessage(ctx, msg)
		})
		if err != nil {
			for _, existing := range s.subscriptions {
				if existing != nil {
					_ = existing.Unsubscribe()
				}
			}
			s.subscriptions = nil
			return fmt.Errorf("start config subscriber: subscribe %q: %w", subjectCopy, err)
		}
		s.subscriptions = append(s.subscriptions, sub)
	}

	return nil
}

// Stop unsubscribes from all subjects and drains the NATS connection.
func (s *Subscriber) Stop() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	subs := s.subscriptions
	s.subscriptions = nil
	s.mu.Unlock()

	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if err := sub.Unsubscribe(); err != nil {
			return fmt.Errorf("stop config subscriber: unsubscribe: %w", err)
		}
	}

	if s.client != nil && s.client.conn != nil {
		if err := s.client.conn.Drain(); err != nil {
			return fmt.Errorf("stop config subscriber: drain connection: %w", err)
		}
	}

	return nil
}

func (s *Subscriber) routeMessage(ctx context.Context, msg *nats.Msg) {
	if s == nil || msg == nil {
		return
	}

	if ctx.Err() != nil {
		return
	}

	s.mu.RLock()
	handler, ok := s.handlers[msg.Subject]
	s.mu.RUnlock()
	if !ok || handler == nil {
		s.logger.WarnContext(ctx, "no config handler registered for subject", slog.String("subject", msg.Subject))
		return
	}

	if err := handler.Handle(ctx, msg.Data); err != nil {
		s.logger.ErrorContext(ctx, "config handler failed", slog.String("subject", msg.Subject), slog.String("error", err.Error()))
	}
}
