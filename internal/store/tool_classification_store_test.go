package store

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
)

var toolClassificationColumnNames = []string{"tool_name", "risk_level", "reason", "auto_classified", "updated_by", "updated_at"}

func TestPostgresToolClassificationStoreGet(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE tool_name = $1`)).
		WithArgs("github.delete_repo").
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).AddRow(
			"github.delete_repo", "destructive", "name contains destructive keyword: delete", true, "auto", now,
		))

	tc, err := s.Get(context.Background(), "github.delete_repo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if tc == nil {
		t.Fatal("expected non-nil classification")
	}
	if tc.RiskLevel != types.RiskDestructive {
		t.Fatalf("expected destructive, got %q", tc.RiskLevel)
	}
}

func TestPostgresToolClassificationStoreGetMissing(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE tool_name = $1`)).
		WithArgs("missing.tool").
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames))

	tc, err := s.Get(context.Background(), "missing.tool")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if tc != nil {
		t.Fatalf("expected nil for missing tool, got %+v", tc)
	}
}

func TestPostgresToolClassificationStoreGetAll(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name`)).
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("github.delete_repo", "destructive", "auto", true, "auto", now).
			AddRow("k8s.get_pods", "read_only", "auto", true, "auto", now))

	all, err := s.GetAll(context.Background())
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 classifications, got %d", len(all))
	}
}

func TestPostgresToolClassificationStoreGetByRiskLevel(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE risk_level = $1 ORDER BY tool_name`)).
		WithArgs("destructive").
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("github.delete_repo", "destructive", "auto", true, "auto", now))

	results, err := s.GetByRiskLevel(context.Background(), types.RiskDestructive)
	if err != nil {
		t.Fatalf("get by risk level: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestPostgresToolClassificationStoreUpsert(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectExec(`INSERT INTO tool_classifications`).
		WithArgs("github.delete_repo", "destructive", "auto-classified", true, "auto", now).
		WillReturnResult(sqlmock.NewResult(0, 1))

	tc := &types.ToolClassification{
		ToolName:       "github.delete_repo",
		RiskLevel:      types.RiskDestructive,
		Reason:         "auto-classified",
		AutoClassified: true,
		UpdatedBy:      "auto",
		UpdatedAt:      now,
	}
	if err := s.Upsert(context.Background(), tc); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

func TestPostgresToolClassificationStoreUpsertNil(t *testing.T) {
	t.Parallel()

	db, _ := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	if err := s.Upsert(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil classification")
	}
}

func TestPostgresToolClassificationStoreDelete(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tool_classifications WHERE tool_name = $1`)).
		WithArgs("github.delete_repo").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Delete(context.Background(), "github.delete_repo"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestPostgresToolClassificationStoreGetByRiskLevelQueryError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE risk_level = $1 ORDER BY tool_name`)).
		WithArgs("destructive").
		WillReturnError(fmt.Errorf("connection refused"))

	results, err := s.GetByRiskLevel(context.Background(), types.RiskDestructive)
	if err == nil {
		t.Fatal("expected error from GetByRiskLevel, got nil")
	}
	if results != nil {
		t.Fatalf("expected nil results on error, got %+v", results)
	}
}

func TestPostgresToolClassificationStoreUpsertExecError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectExec(`INSERT INTO tool_classifications`).
		WithArgs("github.delete_repo", "destructive", "auto-classified", true, "auto", now).
		WillReturnError(fmt.Errorf("unique constraint violation"))

	tc := &types.ToolClassification{
		ToolName:       "github.delete_repo",
		RiskLevel:      types.RiskDestructive,
		Reason:         "auto-classified",
		AutoClassified: true,
		UpdatedBy:      "auto",
		UpdatedAt:      now,
	}
	if err := s.Upsert(context.Background(), tc); err == nil {
		t.Fatal("expected error from Upsert, got nil")
	}
}

func TestPostgresToolClassificationStoreDeleteExecError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tool_classifications WHERE tool_name = $1`)).
		WithArgs("github.delete_repo").
		WillReturnError(fmt.Errorf("connection lost"))

	if err := s.Delete(context.Background(), "github.delete_repo"); err == nil {
		t.Fatal("expected error from Delete, got nil")
	}
}

func TestScanToolClassificationsRowScanError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	// Return a row with a wrong column count to trigger a scan error.
	// The first row scans correctly; the second has a wrong type to force an error.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name`)).
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("github.delete_repo", "destructive", "auto", true, "auto", now).
			AddRow("bad.tool", "destructive", "auto", "not_a_bool", "auto", now))

	results, err := s.GetAll(context.Background())
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
	if results != nil {
		t.Fatalf("expected nil results on scan error, got %+v", results)
	}
}

func TestScanToolClassificationsRowsErr(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name`)).
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("github.delete_repo", "destructive", "auto", true, "auto", now).
			RowError(0, fmt.Errorf("row iteration error")))

	results, err := s.GetAll(context.Background())
	if err == nil {
		t.Fatal("expected rows error, got nil")
	}
	if results != nil {
		t.Fatalf("expected nil results on rows error, got %+v", results)
	}
}
