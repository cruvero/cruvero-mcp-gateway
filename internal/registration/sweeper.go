package registration

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	servermetrics "github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// Sweeper periodically transitions stale/expired registrations.
type Sweeper struct {
	store     store.ServerStore
	index     *CapabilityIndex
	config    *config.Config
	logger    *slog.Logger
	publisher LifecycleEventPublisher

	mu      sync.Mutex
	ticker  *time.Ticker
	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool
}

// NewSweeper creates a registration sweeper.
func NewSweeper(serverStore store.ServerStore, index *CapabilityIndex, cfg *config.Config, logger *slog.Logger) *Sweeper {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return &Sweeper{
		store:  serverStore,
		index:  index,
		config: cfg,
		logger: logger,
	}
}

// SetLifecycleEventPublisher sets an optional event publisher for status transitions.
func (s *Sweeper) SetLifecycleEventPublisher(publisher LifecycleEventPublisher) {
	if s == nil {
		return
	}
	s.publisher = publisher
}

// Start launches the background sweeper loop.
func (s *Sweeper) Start(ctx context.Context) {
	if s == nil || s.store == nil || ctx == nil {
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}

	interval := s.interval()
	s.ticker = time.NewTicker(interval)
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.running = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			if s.ticker != nil {
				s.ticker.Stop()
				s.ticker = nil
			}
			s.running = false
			close(s.doneCh)
			s.mu.Unlock()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-s.ticker.C:
				s.sweep(ctx)
			}
		}
	}()
}

// Stop requests shutdown and waits for the background loop to exit.
func (s *Sweeper) Stop() {
	if s == nil {
		return
	}

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.running = false
	s.mu.Unlock()

	close(stopCh)
	<-doneCh
}

func (s *Sweeper) sweep(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}

	staleThreshold := s.interval()
	expiredThreshold := 3 * s.interval()

	staleCount := s.sweepStale(ctx, staleThreshold)
	expiredCount := s.sweepExpired(ctx, expiredThreshold)

	s.logger.InfoContext(
		ctx,
		"sweep complete",
		slog.Int("stale_count", staleCount),
		slog.Int("expired_count", expiredCount),
	)
}

// sweepStale transitions servers that missed heartbeats to stale status.
func (s *Sweeper) sweepStale(ctx context.Context, threshold time.Duration) int {
	servers, err := s.store.ListStale(ctx, threshold)
	if err != nil {
		s.logger.ErrorContext(ctx, "list stale servers failed", slog.String("error", err.Error()))
		return 0
	}

	count := 0
	for _, server := range servers {
		if s.transitionServer(ctx, server, EventHeartbeatMissed, "stale") {
			count++
		}
	}
	return count
}

// sweepExpired transitions servers past the expiration threshold and removes
// them from the capability index.
func (s *Sweeper) sweepExpired(ctx context.Context, threshold time.Duration) int {
	servers, err := s.store.ListExpired(ctx, threshold)
	if err != nil {
		s.logger.ErrorContext(ctx, "list expired servers failed", slog.String("error", err.Error()))
		return 0
	}

	count := 0
	for _, server := range servers {
		if s.transitionServer(ctx, server, EventExpired, "expired") {
			if s.index != nil {
				s.index.Remove(server.ID)
			}
			count++
		}
	}
	return count
}

// transitionServer applies a state-machine transition for a single server,
// updates the store, adjusts metrics, and publishes a health-changed event.
// It returns true if the transition succeeded.
func (s *Sweeper) transitionServer(
	ctx context.Context,
	server types.ServerRecord,
	event Event,
	label string,
) bool {
	nextStatus, transitionErr := Transition(server.Status, event)
	if transitionErr != nil {
		s.logger.WarnContext(
			ctx,
			"skip "+label+" transition",
			slog.String("server_id", server.ID),
			slog.String("status", server.Status.String()),
			slog.String("error", transitionErr.Error()),
		)
		return false
	}

	if err := s.store.UpdateStatus(ctx, server.ID, nextStatus); err != nil {
		s.logger.ErrorContext(
			ctx,
			"mark server "+label+" failed",
			slog.String("server_id", server.ID),
			slog.String("error", err.Error()),
		)
		return false
	}

	servermetrics.AddActiveRegistrations(server.Status.String(), -1)
	servermetrics.AddActiveRegistrations(nextStatus.String(), 1)
	s.publishHealthChanged(ctx, server, nextStatus)

	s.logger.InfoContext(ctx, "server marked "+label, slog.String("server_id", server.ID))
	return true
}

// publishHealthChanged publishes a health-changed event if a publisher is configured.
func (s *Sweeper) publishHealthChanged(ctx context.Context, server types.ServerRecord, nextStatus types.ServerStatus) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.PublishServerHealthChanged(ctx, server.ID, server.Name, server.Status, nextStatus); err != nil {
		s.logger.ErrorContext(ctx, "publish server health changed event failed", slog.String("error", err.Error()))
	}
}

func (s *Sweeper) interval() time.Duration {
	if s != nil && s.config != nil && s.config.HeartbeatTTL > 0 {
		return s.config.HeartbeatTTL
	}
	return 30 * time.Second
}
