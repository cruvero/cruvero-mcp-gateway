package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/lib/pq"
)

func TestPostgresAPIKeyStoreCreate(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAPIKeyStore(db)

	expiresAt := fixedTime().Add(24 * time.Hour)
	key := &types.APIKey{
		KeyLookupHash: "lookup-hash",
		KeyBcryptHash: "bcrypt-hash",
		Name:          "integration",
		Scopes:        []string{"read", "write"},
		ClientID:      "client-1",
		ExpiresAt:     &expiresAt,
	}

	mock.ExpectExec("INSERT INTO api_keys").
		WithArgs(
			key.KeyLookupHash,
			key.KeyBcryptHash,
			key.Name,
			pq.Array(key.Scopes),
			key.ClientID,
			key.ExpiresAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.Create(context.Background(), key); err != nil {
		t.Fatalf("create api key: %v", err)
	}
}

func TestPostgresAPIKeyStoreGetByLookupHash(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAPIKeyStore(db)
	now := fixedTime()

	expectedQuery := `
SELECT ` + apiKeyColumns + `
FROM api_keys
WHERE key_lookup_hash = $1
  AND (expires_at IS NULL OR expires_at > now())
`

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("lookup-hash").
		WillReturnRows(sqlmock.NewRows(apiKeyColumnNames).AddRow(
			"key-1",
			"lookup-hash",
			"bcrypt-hash",
			"integration",
			"{read,write}",
			"client-1",
			now.Add(24*time.Hour),
			now,
		))

	key, err := s.GetByLookupHash(context.Background(), "lookup-hash")
	if err != nil {
		t.Fatalf("get by lookup hash: %v", err)
	}
	if key.KeyBcryptHash != "bcrypt-hash" {
		t.Fatalf("expected bcrypt hash, got %q", key.KeyBcryptHash)
	}
	if len(key.Scopes) != 2 || key.Scopes[0] != "read" {
		t.Fatalf("unexpected scopes: %+v", key.Scopes)
	}

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("expired-hash").
		WillReturnError(sql.ErrNoRows)

	_, err = s.GetByLookupHash(context.Background(), "expired-hash")
	if err == nil {
		t.Fatal("expected not found error for expired key")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestPostgresAPIKeyStoreList(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAPIKeyStore(db)
	now := fixedTime()

	expectedQuery := `SELECT ` + apiKeyColumns + ` FROM api_keys ORDER BY created_at DESC`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WillReturnRows(sqlmock.NewRows(apiKeyColumnNames).
			AddRow("key-1", "lookup-1", "bcrypt-1", "k1", "{read}", "client-1", nil, now).
			AddRow("key-2", "lookup-2", "bcrypt-2", "k2", "{admin}", "client-2", now.Add(1*time.Hour), now))

	keys, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestPostgresAPIKeyStoreRevokeAndDeleteExpired(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAPIKeyStore(db)

	mock.ExpectExec(regexp.QuoteMeta("UPDATE api_keys SET expires_at = now() WHERE id = $1")).
		WithArgs("key-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Revoke(context.Background(), "key-1"); err != nil {
		t.Fatalf("revoke api key: %v", err)
	}

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM api_keys WHERE expires_at IS NOT NULL AND expires_at < now()")).
		WillReturnResult(sqlmock.NewResult(0, 2))

	deleted, err := s.DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("delete expired keys: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted keys, got %d", deleted)
	}
}

func TestPostgresAPIKeyStoreCreateNilKey(t *testing.T) {
	db, _ := newMockDB(t)
	s := NewPostgresAPIKeyStore(db)

	if err := s.Create(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil key")
	}
}
