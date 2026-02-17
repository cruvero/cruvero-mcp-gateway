package events

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresConfigStoreSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	db, mock := newEventsMockDB(t)
	store := NewPostgresConfigStore(db)

	saveQuery := `
INSERT INTO config_cache (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now()
`
	mock.ExpectExec(regexp.QuoteMeta(saveQuery)).
		WithArgs(configCachePolicyKey, []byte(`{"profiles":[]}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	loadQuery := `SELECT value FROM config_cache WHERE key = $1`
	mock.ExpectQuery(regexp.QuoteMeta(loadQuery)).
		WithArgs(configCachePolicyKey).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow([]byte(`{"profiles":[]}`)))

	if err := store.Save(context.Background(), configCachePolicyKey, []byte(`{"profiles":[]}`)); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got, err := store.Load(context.Background(), configCachePolicyKey)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if string(got) != `{"profiles":[]}` {
		t.Fatalf("unexpected loaded payload: %q", string(got))
	}
}

func TestPostgresConfigStoreLoadMissingReturnsError(t *testing.T) {
	t.Parallel()

	db, mock := newEventsMockDB(t)
	store := NewPostgresConfigStore(db)

	loadQuery := `SELECT value FROM config_cache WHERE key = $1`
	mock.ExpectQuery(regexp.QuoteMeta(loadQuery)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err := store.Load(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected load error for missing key")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestPostgresConfigStoreUpsertLatestValue(t *testing.T) {
	t.Parallel()

	db, mock := newEventsMockDB(t)
	store := NewPostgresConfigStore(db)

	saveQuery := `
INSERT INTO config_cache (key, value, updated_at)
VALUES ($1, $2, now())
ON CONFLICT (key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now()
`
	mock.ExpectExec(regexp.QuoteMeta(saveQuery)).
		WithArgs(configCacheServerSettingsKey, []byte(`{"config_version":1}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(saveQuery)).
		WithArgs(configCacheServerSettingsKey, []byte(`{"config_version":2}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	loadQuery := `SELECT value FROM config_cache WHERE key = $1`
	mock.ExpectQuery(regexp.QuoteMeta(loadQuery)).
		WithArgs(configCacheServerSettingsKey).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow([]byte(`{"config_version":2}`)))

	if err := store.Save(context.Background(), configCacheServerSettingsKey, []byte(`{"config_version":1}`)); err != nil {
		t.Fatalf("save first value: %v", err)
	}
	if err := store.Save(context.Background(), configCacheServerSettingsKey, []byte(`{"config_version":2}`)); err != nil {
		t.Fatalf("save second value: %v", err)
	}

	got, err := store.Load(context.Background(), configCacheServerSettingsKey)
	if err != nil {
		t.Fatalf("load latest value: %v", err)
	}
	if string(got) != `{"config_version":2}` {
		t.Fatalf("expected latest payload, got %q", string(got))
	}
}

func TestPostgresConfigStoreKeys(t *testing.T) {
	t.Parallel()

	db, mock := newEventsMockDB(t)
	store := NewPostgresConfigStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT key FROM config_cache`)).
		WillReturnRows(sqlmock.NewRows([]string{"key"}).
			AddRow(configCachePolicyKey).
			AddRow(configCacheServersKey))

	keys, err := store.Keys(context.Background())
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func newEventsMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() {
		if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
			t.Fatalf("unmet sql expectations: %v", expectErr)
		}
	})

	return db, mock
}
