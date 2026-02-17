package ratelimit

import (
	"context"
	"log/slog"
	"time"
)

const (
	defaultCleanupInterval = time.Minute
	defaultMaxIdle         = 5 * time.Minute
)

// StartCleanup runs periodic limiter cleanup until context cancellation.
func StartCleanup(ctx context.Context, store *LimiterStore, interval, maxIdle time.Duration) {
	if store == nil || ctx == nil {
		return
	}

	if interval <= 0 {
		interval = defaultCleanupInterval
	}
	if maxIdle <= 0 {
		maxIdle = defaultMaxIdle
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed := store.Cleanup(maxIdle)
				if removed > 0 {
					slog.Default().InfoContext(ctx, "ratelimit cleanup removed idle entries", slog.Int("removed", removed))
				}
			}
		}
	}()
}
