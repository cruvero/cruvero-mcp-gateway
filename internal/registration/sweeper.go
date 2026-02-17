package registration

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/store"
)

// Sweeper periodically transitions stale/expired registrations.
type Sweeper struct {
	store  store.ServerStore
	index  *CapabilityIndex
	config *config.Config
	logger *slog.Logger

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
	s.stopCh = nil
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
	staleCount := 0
	expiredCount := 0

	staleServers, err := s.store.ListStale(ctx, staleThreshold)
	if err != nil {
		s.logger.ErrorContext(ctx, "list stale servers failed", slog.String("error", err.Error()))
	} else {
		for _, server := range staleServers {
			nextStatus, transitionErr := Transition(server.Status, EventHeartbeatMissed)
			if transitionErr != nil {
				s.logger.WarnContext(
					ctx,
					"skip stale transition",
					slog.String("server_id", server.ID),
					slog.String("status", server.Status.String()),
					slog.String("error", transitionErr.Error()),
				)
				continue
			}

			if err := s.store.UpdateStatus(ctx, server.ID, nextStatus); err != nil {
				s.logger.ErrorContext(
					ctx,
					"mark server stale failed",
					slog.String("server_id", server.ID),
					slog.String("error", err.Error()),
				)
				continue
			}

			staleCount++
			s.logger.InfoContext(ctx, "server marked stale", slog.String("server_id", server.ID))
		}
	}

	expiredServers, err := s.store.ListExpired(ctx, expiredThreshold)
	if err != nil {
		s.logger.ErrorContext(ctx, "list expired servers failed", slog.String("error", err.Error()))
	} else {
		for _, server := range expiredServers {
			nextStatus, transitionErr := Transition(server.Status, EventExpired)
			if transitionErr != nil {
				s.logger.WarnContext(
					ctx,
					"skip expired transition",
					slog.String("server_id", server.ID),
					slog.String("status", server.Status.String()),
					slog.String("error", transitionErr.Error()),
				)
				continue
			}

			if err := s.store.UpdateStatus(ctx, server.ID, nextStatus); err != nil {
				s.logger.ErrorContext(
					ctx,
					"mark server expired failed",
					slog.String("server_id", server.ID),
					slog.String("error", err.Error()),
				)
				continue
			}

			if s.index != nil {
				s.index.Remove(server.ID)
			}

			expiredCount++
			s.logger.InfoContext(ctx, "server marked expired", slog.String("server_id", server.ID))
		}
	}

	s.logger.InfoContext(
		ctx,
		"sweep complete",
		slog.Int("stale_count", staleCount),
		slog.Int("expired_count", expiredCount),
	)
}

func (s *Sweeper) interval() time.Duration {
	if s != nil && s.config != nil && s.config.HeartbeatTTL > 0 {
		return s.config.HeartbeatTTL
	}
	return 30 * time.Second
}
