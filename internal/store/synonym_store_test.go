package store

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/lib/pq"
)

func TestPostgresSynonymStore_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ SynonymStore = (*PostgresSynonymStore)(nil)
}

func TestPostgresSynonymStore_List(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)
	now := fixedTime()

	mock.ExpectQuery("SELECT term, synonyms, created_at, updated_at FROM search_synonyms ORDER BY term").
		WillReturnRows(sqlmock.NewRows([]string{"term", "synonyms", "created_at", "updated_at"}).
			AddRow("deploy", pq.Array([]string{"release", "ship"}), now, now).
			AddRow("kubernetes", pq.Array([]string{"k8s", "kube"}), now, now))

	entries, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list synonyms: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Term != "deploy" {
		t.Fatalf("expected term 'deploy', got %q", entries[0].Term)
	}
	if len(entries[0].Synonyms) != 2 || entries[0].Synonyms[0] != "release" {
		t.Fatalf("unexpected synonyms: %v", entries[0].Synonyms)
	}
}

func TestPostgresSynonymStore_ListEmpty(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)

	mock.ExpectQuery("SELECT term, synonyms, created_at, updated_at FROM search_synonyms ORDER BY term").
		WillReturnRows(sqlmock.NewRows([]string{"term", "synonyms", "created_at", "updated_at"}))

	entries, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list empty synonyms: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestPostgresSynonymStore_ListError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)

	mock.ExpectQuery("SELECT term, synonyms, created_at, updated_at FROM search_synonyms ORDER BY term").
		WillReturnError(errors.New("connection refused"))

	_, err := s.List(context.Background())
	if err == nil {
		t.Fatal("expected list error")
	}
}

func TestPostgresSynonymStore_Upsert(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)

	entry := &types.SynonymEntry{
		Term:     "deploy",
		Synonyms: []string{"release", "ship"},
	}

	mock.ExpectExec("INSERT INTO search_synonyms").
		WithArgs(entry.Term, pq.Array(entry.Synonyms)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Upsert(context.Background(), entry); err != nil {
		t.Fatalf("upsert synonym: %v", err)
	}
}

func TestPostgresSynonymStore_UpsertError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)

	entry := &types.SynonymEntry{
		Term:     "deploy",
		Synonyms: []string{"release"},
	}

	mock.ExpectExec("INSERT INTO search_synonyms").
		WithArgs(entry.Term, pq.Array(entry.Synonyms)).
		WillReturnError(errors.New("unique violation"))

	err := s.Upsert(context.Background(), entry)
	if err == nil {
		t.Fatal("expected upsert error")
	}
}

func TestPostgresSynonymStore_Delete(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)

	mock.ExpectExec("DELETE FROM search_synonyms WHERE term").
		WithArgs("deploy").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Delete(context.Background(), "deploy"); err != nil {
		t.Fatalf("delete synonym: %v", err)
	}
}

func TestPostgresSynonymStore_DeleteError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)

	mock.ExpectExec("DELETE FROM search_synonyms WHERE term").
		WithArgs("deploy").
		WillReturnError(errors.New("delete failed"))

	err := s.Delete(context.Background(), "deploy")
	if err == nil {
		t.Fatal("expected delete error")
	}
}

// Verify pq.Array is correctly integrated with the scan path.
func TestPostgresSynonymStore_ListScanTimestamps(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresSynonymStore(db)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT term, synonyms, created_at, updated_at FROM search_synonyms ORDER BY term").
		WillReturnRows(sqlmock.NewRows([]string{"term", "synonyms", "created_at", "updated_at"}).
			AddRow("ci", pq.Array([]string{"pipeline", "build"}), created, updated))

	entries, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].CreatedAt.Equal(created) {
		t.Fatalf("expected created_at %v, got %v", created, entries[0].CreatedAt)
	}
	if !entries[0].UpdatedAt.Equal(updated) {
		t.Fatalf("expected updated_at %v, got %v", updated, entries[0].UpdatedAt)
	}
}
