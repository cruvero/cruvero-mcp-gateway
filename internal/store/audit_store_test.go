package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestPostgresAuditStoreLog(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)

	entry := &types.AuditEntry{
		EventType:  "policy.deny",
		ClientID:   "client-1",
		ServerName: "alpha",
		Details: map[string]any{
			"reason": "tool denied",
		},
	}

	mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(entry.EventType, entry.ClientID, entry.ServerName, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.Log(context.Background(), entry); err != nil {
		t.Fatalf("log audit entry: %v", err)
	}
}

func TestPostgresAuditStoreLogError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)

	entry := &types.AuditEntry{EventType: "auth.failure", Details: map[string]any{"x": "y"}}

	mock.ExpectExec("INSERT INTO audit_log").
		WillReturnError(errors.New("insert failed"))

	err := s.Log(context.Background(), entry)
	if err == nil {
		t.Fatal("expected log error")
	}
	if !strings.Contains(err.Error(), "audit store") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestPostgresAuditStoreQuery(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)
	now := fixedTime()
	since := now.Add(-1 * time.Hour)
	until := now.Add(1 * time.Hour)

	expectedQuery := "SELECT " + auditColumns + " FROM audit_log WHERE event_type = $1 AND client_id = $2 AND server_name = $3 AND created_at >= $4 AND created_at <= $5 ORDER BY created_at DESC LIMIT $6 OFFSET $7"
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("policy.deny", "client-1", "alpha", since, until, 25, 10).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"policy.deny",
			"client-1",
			"alpha",
			[]byte(`{"reason":"tool denied"}`),
			now,
		))

	entries, err := s.Query(context.Background(), types.AuditFilter{
		EventType:  "policy.deny",
		ClientID:   "client-1",
		ServerName: "alpha",
		Since:      &since,
		Until:      &until,
		Limit:      25,
		Offset:     10,
	})
	if err != nil {
		t.Fatalf("query audit log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Details["reason"] != "tool denied" {
		t.Fatalf("unexpected details map: %+v", entries[0].Details)
	}
}

func TestPostgresAuditStoreQueryNoFilterAndErrors(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)
	now := fixedTime()

	expectedNoFilterQuery := "SELECT " + auditColumns + " FROM audit_log ORDER BY created_at DESC"
	mock.ExpectQuery(regexp.QuoteMeta(expectedNoFilterQuery)).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"auth.success",
			"client-1",
			"alpha",
			[]byte(`{}`),
			now,
		))

	entries, err := s.Query(context.Background(), types.AuditFilter{})
	if err != nil {
		t.Fatalf("query without filters: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	mock.ExpectQuery(regexp.QuoteMeta(expectedNoFilterQuery)).
		WillReturnError(errors.New("query failed"))

	_, err = s.Query(context.Background(), types.AuditFilter{})
	if err == nil {
		t.Fatal("expected query error")
	}
}

func TestPostgresAuditStoreLogNilEntry(t *testing.T) {
	db, _ := newMockDB(t)
	s := NewPostgresAuditStore(db)

	if err := s.Log(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil audit entry")
	}
}
