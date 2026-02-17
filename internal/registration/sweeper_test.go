package registration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestSweeperMarksStale(t *testing.T) {
	t.Parallel()

	var updatedID string
	var updatedStatus types.ServerStatus

	serverStore := &mockServerStore{
		listStaleFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return []types.ServerRecord{
				{ID: "server-1", Status: types.StatusActive},
			}, nil
		},
		listExpiredFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return nil, nil
		},
		updateStatusFn: func(ctx context.Context, id string, status types.ServerStatus) error {
			updatedID = id
			updatedStatus = status
			return nil
		},
	}

	sweeper := NewSweeper(serverStore, NewCapabilityIndex(), &config.Config{HeartbeatTTL: 20 * time.Second}, testSweeperLogger())
	sweeper.sweep(context.Background())

	if updatedID != "server-1" {
		t.Fatalf("expected stale update for server-1, got %q", updatedID)
	}
	if updatedStatus != types.StatusStale {
		t.Fatalf("expected stale status, got %q", updatedStatus)
	}
}

func TestSweeperMarksExpiredAndRemovesFromIndex(t *testing.T) {
	t.Parallel()

	var updatedID string
	var updatedStatus types.ServerStatus

	idx := NewCapabilityIndex()
	idx.Add(types.ServerRecord{
		ID:     "server-2",
		Name:   "svc-2",
		Status: types.StatusActive,
		Capabilities: types.Capability{
			Tools: []string{"tool.alpha"},
		},
	})

	serverStore := &mockServerStore{
		listStaleFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return nil, nil
		},
		listExpiredFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return []types.ServerRecord{
				{ID: "server-2", Status: types.StatusStale},
			}, nil
		},
		updateStatusFn: func(ctx context.Context, id string, status types.ServerStatus) error {
			updatedID = id
			updatedStatus = status
			return nil
		},
	}

	sweeper := NewSweeper(serverStore, idx, &config.Config{HeartbeatTTL: 20 * time.Second}, testSweeperLogger())
	sweeper.sweep(context.Background())

	if updatedID != "server-2" {
		t.Fatalf("expected expired update for server-2, got %q", updatedID)
	}
	if updatedStatus != types.StatusExpired {
		t.Fatalf("expected expired status, got %q", updatedStatus)
	}
	if servers := idx.LookupTool("tool.alpha"); len(servers) != 0 {
		t.Fatalf("expected tool index removal for expired server, got %d servers", len(servers))
	}
}

func TestSweeperStartStopLifecycle(t *testing.T) {
	t.Parallel()

	var staleCalls atomic.Int32
	var expiredCalls atomic.Int32

	serverStore := &mockServerStore{
		listStaleFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			staleCalls.Add(1)
			return nil, nil
		},
		listExpiredFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			expiredCalls.Add(1)
			return nil, nil
		},
	}

	sweeper := NewSweeper(serverStore, NewCapabilityIndex(), &config.Config{HeartbeatTTL: 15 * time.Millisecond}, testSweeperLogger())
	ctx, cancel := context.WithCancel(context.Background())
	sweeper.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	sweeper.Stop()

	if staleCalls.Load() == 0 {
		t.Fatal("expected stale sweep to run at least once")
	}
	if expiredCalls.Load() == 0 {
		t.Fatal("expected expired sweep to run at least once")
	}
}

func TestSweeperNoOpWhenNoServers(t *testing.T) {
	t.Parallel()

	updated := false
	serverStore := &mockServerStore{
		listStaleFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return []types.ServerRecord{}, nil
		},
		listExpiredFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return []types.ServerRecord{}, nil
		},
		updateStatusFn: func(ctx context.Context, id string, status types.ServerStatus) error {
			updated = true
			return nil
		},
	}

	sweeper := NewSweeper(serverStore, NewCapabilityIndex(), &config.Config{}, testSweeperLogger())
	sweeper.sweep(context.Background())

	if updated {
		t.Fatal("did not expect status updates")
	}
}

func TestSweeperHandlesStoreErrors(t *testing.T) {
	t.Parallel()

	updated := false
	serverStore := &mockServerStore{
		listStaleFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return nil, errors.New("stale failure")
		},
		listExpiredFn: func(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
			return nil, errors.New("expired failure")
		},
		updateStatusFn: func(ctx context.Context, id string, status types.ServerStatus) error {
			updated = true
			return nil
		},
	}

	sweeper := NewSweeper(serverStore, NewCapabilityIndex(), &config.Config{}, testSweeperLogger())
	sweeper.sweep(context.Background())

	if updated {
		t.Fatal("did not expect status updates when list calls fail")
	}
}

func testSweeperLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
