package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestCleanupAuditLog(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)

	mock.ExpectExec(`DELETE FROM audit_log WHERE created_at`).
		WithArgs(90).
		WillReturnResult(sqlmock.NewResult(0, 5))

	deleted, err := cleanupAuditLog(context.Background(), db, 90)
	if err != nil {
		t.Fatalf("cleanup audit log: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("expected 5 deleted rows, got %d", deleted)
	}
}

func TestStartAuditRetentionStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		StartAuditRetention(ctx, nil, 0, 0, nil)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartAuditRetention did not stop on nil db")
	}
}

func TestStartAuditRetentionNilDBReturnsImmediately(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	go func() {
		StartAuditRetention(context.Background(), nil, 90, time.Hour, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected immediate return for nil db")
	}
}

func TestCleanupAuditLogExecError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)

	mock.ExpectExec(`DELETE FROM audit_log WHERE created_at`).
		WithArgs(90).
		WillReturnError(fmt.Errorf("connection refused"))

	deleted, err := cleanupAuditLog(context.Background(), db, 90)
	if err == nil {
		t.Fatal("expected error from cleanupAuditLog, got nil")
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted rows on error, got %d", deleted)
	}
}
