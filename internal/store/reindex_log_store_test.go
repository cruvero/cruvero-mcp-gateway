package store

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestPostgresReindexLogStore_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ ReindexLogStore = (*PostgresReindexLogStore)(nil)
}

func TestPostgresReindexLogStore_Log(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)

	entry := &types.ReindexLogEntry{
		Engine:     "bm25",
		Trigger:    "manual",
		ToolsCount: 42,
		DurationMs: 150,
		Status:     "success",
	}

	mock.ExpectExec("INSERT INTO search_reindex_log").
		WithArgs(entry.Engine, entry.Trigger, entry.ToolsCount, entry.DurationMs, entry.Status, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.Log(context.Background(), entry); err != nil {
		t.Fatalf("log reindex entry: %v", err)
	}
}

func TestPostgresReindexLogStore_LogWithError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)

	entry := &types.ReindexLogEntry{
		Engine:     "hybrid",
		Trigger:    "startup",
		ToolsCount: 10,
		DurationMs: 500,
		Status:     "error",
		Error:      "embedder timeout",
	}

	errStr := "embedder timeout"
	mock.ExpectExec("INSERT INTO search_reindex_log").
		WithArgs(entry.Engine, entry.Trigger, entry.ToolsCount, entry.DurationMs, entry.Status, &errStr).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.Log(context.Background(), entry); err != nil {
		t.Fatalf("log reindex entry with error: %v", err)
	}
}

func TestPostgresReindexLogStore_LogDBError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)

	entry := &types.ReindexLogEntry{
		Engine:  "bm25",
		Trigger: "manual",
		Status:  "success",
	}

	mock.ExpectExec("INSERT INTO search_reindex_log").
		WillReturnError(errors.New("insert failed"))

	err := s.Log(context.Background(), entry)
	if err == nil {
		t.Fatal("expected log error")
	}
}

func TestPostgresReindexLogStore_Recent(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)
	now := fixedTime()

	mock.ExpectQuery("SELECT id, engine, trigger, tools_count, duration_ms, status, COALESCE\\(error, ''\\), created_at FROM search_reindex_log ORDER BY created_at DESC LIMIT").
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"id", "engine", "trigger", "tools_count", "duration_ms", "status", "error", "created_at"}).
			AddRow(1, "bm25", "startup", 42, 150, "success", "", now).
			AddRow(2, "hybrid", "manual", 50, 300, "error", "timeout", now))

	entries, err := s.Recent(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Engine != "bm25" {
		t.Fatalf("expected engine 'bm25', got %q", entries[0].Engine)
	}
	if entries[1].Error != "timeout" {
		t.Fatalf("expected error 'timeout', got %q", entries[1].Error)
	}
}

func TestPostgresReindexLogStore_RecentEmpty(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)

	mock.ExpectQuery("SELECT id, engine, trigger, tools_count, duration_ms, status, COALESCE\\(error, ''\\), created_at FROM search_reindex_log ORDER BY created_at DESC LIMIT").
		WithArgs(5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "engine", "trigger", "tools_count", "duration_ms", "status", "error", "created_at"}))

	entries, err := s.Recent(context.Background(), 5)
	if err != nil {
		t.Fatalf("recent empty: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestPostgresReindexLogStore_RecentError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)

	mock.ExpectQuery("SELECT id, engine, trigger, tools_count, duration_ms, status, COALESCE\\(error, ''\\), created_at FROM search_reindex_log ORDER BY created_at DESC LIMIT").
		WithArgs(10).
		WillReturnError(errors.New("query failed"))

	_, err := s.Recent(context.Background(), 10)
	if err == nil {
		t.Fatal("expected recent error")
	}
}

func TestPostgresReindexLogStore_RecentDefaultLimit(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresReindexLogStore(db)

	// When limit <= 0, defaults to 20.
	mock.ExpectQuery("SELECT id, engine, trigger, tools_count, duration_ms, status, COALESCE\\(error, ''\\), created_at FROM search_reindex_log ORDER BY created_at DESC LIMIT").
		WithArgs(20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "engine", "trigger", "tools_count", "duration_ms", "status", "error", "created_at"}))

	entries, err := s.Recent(context.Background(), 0)
	if err != nil {
		t.Fatalf("recent default limit: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}
