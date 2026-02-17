package store

import (
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

var (
	serverColumnNames = []string{"id", "name", "spiffe_id", "version", "host", "port", "capabilities", "status", "policy_profile", "last_heartbeat", "created_at", "updated_at"}
	apiKeyColumnNames = []string{"id", "key_lookup_hash", "key_bcrypt_hash", "name", "scopes", "client_id", "expires_at", "created_at"}
	auditColumnNames  = []string{"id", "event_type", "client_id", "server_name", "details", "created_at"}
)

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}

	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})

	return db, mock
}

func fixedTime() time.Time {
	return time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
}
