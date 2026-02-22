package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	natsBucketName    = "ratelimits"
	natsBucketTTL     = 10 * time.Minute
	natsCASMaxRetries = 3
)

type natsWindowEntry struct {
	Count       int   `json:"count"`
	WindowStart int64 `json:"window_start"`
}

// NATSBackendOption configures the NATS rate limit backend.
type NATSBackendOption func(*NATSBackend)

// WithNATSFallback sets a fallback backend when NATS KV operations fail.
func WithNATSFallback(fallback LimiterBackend) NATSBackendOption {
	return func(nb *NATSBackend) {
		nb.fallback = fallback
	}
}

// WithNATSLogger sets a structured logger for the NATS backend.
func WithNATSLogger(logger *slog.Logger) NATSBackendOption {
	return func(nb *NATSBackend) {
		nb.logger = logger
	}
}

// NATSBackend implements distributed rate limiting using NATS JetStream KV.
type NATSBackend struct {
	kv       nats.KeyValue
	fallback LimiterBackend
	logger   *slog.Logger
}

// NewNATSBackend creates a NATS KV-based rate limiter from a JetStream context.
func NewNATSBackend(js nats.JetStreamContext, opts ...NATSBackendOption) (*NATSBackend, error) {
	if js == nil {
		return nil, fmt.Errorf("new nats backend: jetstream context is nil")
	}

	nb := &NATSBackend{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(nb)
		}
	}

	kv, err := js.KeyValue(natsBucketName)
	if err != nil {
		// Bucket doesn't exist yet; create it.
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:  natsBucketName,
			TTL:     natsBucketTTL,
			Storage: nats.MemoryStorage,
		})
		if err != nil {
			return nil, fmt.Errorf("new nats backend: create kv bucket: %w", err)
		}
	}
	nb.kv = kv
	return nb, nil
}

// Allow checks whether a request is allowed under a CAS-loop sliding window.
func (nb *NATSBackend) Allow(_ context.Context, key LimiterKey, limit float64, burst int) (bool, int, time.Duration, error) {
	if nb == nil {
		return true, 0, 0, nil
	}

	if limit <= 0 {
		limit = defaultRateLimit
	}
	if burst <= 0 {
		burst = defaultRateBurst
	}

	normalized := normalizeKey(key)
	kvKey := fmt.Sprintf("rl.%s.%s", normalized.ClientID, normalized.Route)
	now := time.Now().UTC().UnixMicro()
	windowMicros := int64(float64(time.Second.Microseconds()) * float64(burst) / limit)

	for attempt := 0; attempt < natsCASMaxRetries; attempt++ {
		window, revision, err := nb.loadWindow(kvKey, now, windowMicros)
		if err != nil {
			return nb.handleUnavailable(key, limit, burst)
		}

		if window.Count >= burst {
			return nb.overLimitResult(window, windowMicros, now)
		}

		window.Count++
		if nb.casWrite(kvKey, window, revision) {
			remaining := burst - window.Count
			if remaining < 0 {
				remaining = 0
			}
			return true, remaining, 0, nil
		}
		// CAS conflict; retry.
	}

	// CAS retries exhausted; fall back or allow.
	nb.logger.Warn("nats kv CAS retries exhausted", slog.String("key", kvKey))
	return nb.handleUnavailable(key, limit, burst)
}

// loadWindow fetches the current window from NATS KV, resetting if expired.
// Returns an error only for non-transient failures that should trigger fallback.
func (nb *NATSBackend) loadWindow(kvKey string, now, windowMicros int64) (natsWindowEntry, uint64, error) {
	entry, err := nb.kv.Get(kvKey)
	if err != nil && err != nats.ErrKeyNotFound {
		nb.logger.Error("nats kv get failed", slog.String("key", kvKey), slog.String("error", err.Error()))
		return natsWindowEntry{}, 0, err
	}

	if err == nats.ErrKeyNotFound || entry == nil {
		return natsWindowEntry{Count: 0, WindowStart: now}, 0, nil
	}

	var window natsWindowEntry
	if jsonErr := json.Unmarshal(entry.Value(), &window); jsonErr != nil {
		nb.logger.Error("nats kv unmarshal failed", slog.String("key", kvKey), slog.String("error", jsonErr.Error()))
		return natsWindowEntry{}, 0, jsonErr
	}

	if now-window.WindowStart > windowMicros {
		return natsWindowEntry{Count: 0, WindowStart: now}, 0, nil
	}

	return window, entry.Revision(), nil
}

// overLimitResult computes the retry-after duration when the burst is exhausted.
func (nb *NATSBackend) overLimitResult(window natsWindowEntry, windowMicros, now int64) (bool, int, time.Duration, error) {
	retryMicros := window.WindowStart + windowMicros - now
	retryAfter := time.Duration(retryMicros) * time.Microsecond
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	return false, 0, retryAfter, nil
}

// casWrite attempts a create (revision 0) or update for the given window entry.
// Returns true on success, false on CAS conflict.
func (nb *NATSBackend) casWrite(kvKey string, window natsWindowEntry, revision uint64) bool {
	data, _ := json.Marshal(window)
	if revision == 0 {
		_, err := nb.kv.Create(kvKey, data)
		return err == nil
	}
	_, err := nb.kv.Update(kvKey, data, revision)
	return err == nil
}

// Close is a no-op for the NATS backend (the connection is managed by events.Client).
func (nb *NATSBackend) Close() error {
	return nil
}

func (nb *NATSBackend) handleUnavailable(key LimiterKey, limit float64, burst int) (bool, int, time.Duration, error) {
	if nb.fallback != nil {
		return nb.fallback.Allow(context.Background(), key, limit, burst)
	}
	return true, burst, 0, nil
}
