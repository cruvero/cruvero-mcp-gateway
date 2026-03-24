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
		WithArgs(entry.EventType, entry.ClientID, "client-1", entry.ServerName, sqlmock.AnyArg()).
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

	expectedQuery := "SELECT " + auditColumns + " FROM audit_log" + auditWhereClause + " ORDER BY created_at desc LIMIT $8 OFFSET $9"
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("policy.deny", "client-1", "alpha", since, until, nil, nil, int64(25), int64(10)).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"policy.deny",
			"client-1",
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

	expectedNoFilterQuery := "SELECT " + auditColumns + " FROM audit_log" + auditWhereClause + " ORDER BY created_at desc LIMIT $8 OFFSET $9"
	mock.ExpectQuery(regexp.QuoteMeta(expectedNoFilterQuery)).
		WithArgs(nil, nil, nil, nil, nil, nil, nil, int64(9223372036854775807), int64(0)).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"auth.success",
			"client-1",
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
		WithArgs(nil, nil, nil, nil, nil, nil, nil, int64(9223372036854775807), int64(0)).
		WillReturnError(errors.New("query failed"))

	_, err = s.Query(context.Background(), types.AuditFilter{})
	if err == nil {
		t.Fatal("expected query error")
	}
}

func TestPostgresAuditStoreQueryWithDetailsSearch(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)
	now := fixedTime()

	expectedQuery := "SELECT " + auditColumns + " FROM audit_log" + auditWhereClause + " ORDER BY created_at desc LIMIT $8 OFFSET $9"
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(nil, nil, nil, nil, nil, "tool denied", nil, int64(50), int64(0)).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"policy.deny",
			"client-1",
			"client-1",
			"alpha",
			[]byte(`{"reason":"tool denied"}`),
			now,
		))

	entries, err := s.Query(context.Background(), types.AuditFilter{
		DetailsSearch: "tool denied",
		Limit:         50,
	})
	if err != nil {
		t.Fatalf("query with details search: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestPostgresAuditStoreCountNoFilters(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)

	expectedQuery := "SELECT COUNT(*) FROM audit_log" + auditWhereClause
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(nil, nil, nil, nil, nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	count, err := s.Count(context.Background(), types.AuditFilter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 42 {
		t.Fatalf("expected count 42, got %d", count)
	}
}

func TestPostgresAuditStoreCountWithFilters(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)

	expectedQuery := "SELECT COUNT(*) FROM audit_log" + auditWhereClause
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("tool_call", nil, "server-1", nil, nil, "search term", nil).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	count, err := s.Count(context.Background(), types.AuditFilter{
		EventType:     "tool_call",
		ServerName:    "server-1",
		DetailsSearch: "search term",
	})
	if err != nil {
		t.Fatalf("count with filters: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected count 5, got %d", count)
	}
}

func TestPostgresAuditStoreCountError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)

	expectedQuery := "SELECT COUNT(*) FROM audit_log" + auditWhereClause
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(nil, nil, nil, nil, nil, nil, nil).
		WillReturnError(errors.New("count failed"))

	_, err := s.Count(context.Background(), types.AuditFilter{})
	if err == nil {
		t.Fatal("expected count error")
	}
}

func TestPostgresAuditStoreLogNilEntry(t *testing.T) {
	db, _ := newMockDB(t)
	s := NewPostgresAuditStore(db)

	if err := s.Log(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil audit entry")
	}
}

func TestPostgresAuditStoreLogUsernameNormalization(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		wantUsername string
	}{
		{name: "empty client ID", clientID: "", wantUsername: "system"},
		{name: "spiffe client ID", clientID: "spiffe://example/ns/default/sa/svc", wantUsername: "system"},
		{name: "server-prefixed client ID", clientID: "server:backend-1", wantUsername: "system"},
		{name: "human identity", clientID: "alice@example.com", wantUsername: "alice@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newMockDB(t)
			s := NewPostgresAuditStore(db)
			entry := &types.AuditEntry{
				EventType: "tool_call",
				ClientID:  tt.clientID,
				Details:   map[string]any{},
			}

			mock.ExpectExec("INSERT INTO audit_log").
				WithArgs(entry.EventType, entry.ClientID, tt.wantUsername, entry.ServerName, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(1, 1))

			if err := s.Log(context.Background(), entry); err != nil {
				t.Fatalf("log audit entry: %v", err)
			}
		})
	}
}

func TestPostgresAuditStoreLogUsernameExplicit(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)

	entry := &types.AuditEntry{
		EventType: "tool_call",
		ClientID:  "spiffe://example/ns/default/sa/svc",
		Username:  "admin@example.com",
		Details:   map[string]any{},
	}

	mock.ExpectExec("INSERT INTO audit_log").
		WithArgs(entry.EventType, entry.ClientID, entry.Username, entry.ServerName, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.Log(context.Background(), entry); err != nil {
		t.Fatalf("log audit entry: %v", err)
	}
}

func TestPostgresAuditStoreQuerySortByUsernameAsc(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)
	now := fixedTime()

	expectedQuery := "SELECT " + auditColumns + " FROM audit_log" + auditWhereClause + " ORDER BY username asc LIMIT $8 OFFSET $9"
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(nil, nil, nil, nil, nil, nil, nil, int64(50), int64(0)).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"tool_call",
			"alice@example.com",
			"alice@example.com",
			"server-1",
			[]byte(`{"ok":true}`),
			now,
		))

	entries, err := s.Query(context.Background(), types.AuditFilter{
		SortBy:  "username",
		SortDir: "asc",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("query with sort: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestPostgresAuditStoreQueryInvalidSortFallsBack(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresAuditStore(db)
	now := fixedTime()

	expectedQuery := "SELECT " + auditColumns + " FROM audit_log" + auditWhereClause + " ORDER BY created_at desc LIMIT $8 OFFSET $9"
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(nil, nil, nil, nil, nil, nil, nil, int64(50), int64(0)).
		WillReturnRows(sqlmock.NewRows(auditColumnNames).AddRow(
			"audit-1",
			"tool_call",
			"alice@example.com",
			"alice@example.com",
			"server-1",
			[]byte(`{"ok":true}`),
			now,
		))

	entries, err := s.Query(context.Background(), types.AuditFilter{
		SortBy:  "not-a-column",
		SortDir: "sideways",
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("query with invalid sort: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
