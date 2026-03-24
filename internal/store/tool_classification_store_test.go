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

func TestPostgresToolClassificationStoreSearchNoFilters(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tool_classifications`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("github.delete_repo", "destructive", "auto", true, "auto", now).
			AddRow("k8s.get_pods", "read_only", "auto", true, "auto", now))

	results, total, err := s.Search(context.Background(), types.ToolFilter{Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestPostgresToolClassificationStoreSearchWithQueryAndRisk(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tool_classifications WHERE tool_name ILIKE '%' || $1 || '%' AND risk_level = $2`)).
		WithArgs("delete", "destructive").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE tool_name ILIKE '%' || $1 || '%' AND risk_level = $2 ORDER BY tool_name LIMIT $3 OFFSET $4`)).
		WithArgs("delete", "destructive", 50, 0).
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("github.delete_repo", "destructive", "auto", true, "auto", now))

	results, total, err := s.Search(context.Background(), types.ToolFilter{
		Query:     "delete",
		RiskLevel: types.RiskDestructive,
		Limit:     50,
		Offset:    0,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestPostgresToolClassificationStoreSearchCountError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tool_classifications`)).
		WillReturnError(fmt.Errorf("count query failed"))

	_, _, err := s.Search(context.Background(), types.ToolFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected error from count query")
	}
}

func TestPostgresToolClassificationStoreSearchScanError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tool_classifications`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnRows(sqlmock.NewRows(toolClassificationColumnNames).
			AddRow("good.tool", "read_only", "auto", true, "auto", now).
			AddRow("bad.tool", "destructive", "auto", "not_a_bool", "auto", now))

	_, _, err := s.Search(context.Background(), types.ToolFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestPostgresToolClassificationStoreSearchDataQueryError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresToolClassificationStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM tool_classifications`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnError(fmt.Errorf("data query failed"))

	_, _, err := s.Search(context.Background(), types.ToolFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected error from data query")
	}
}

func TestPostgresToolClassificationStoreDeleteNotIn(t *testing.T) {
	t.Parallel()

	t.Run("with active tool names", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		s := NewPostgresToolClassificationStore(db)

		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tool_classifications WHERE tool_name NOT IN ($1, $2)`)).
			WithArgs("tool.a", "tool.b").
			WillReturnResult(sqlmock.NewResult(0, 3))

		count, err := s.DeleteNotIn(context.Background(), []string{"tool.a", "tool.b"})
		if err != nil {
			t.Fatalf("delete not in: %v", err)
		}
		if count != 3 {
			t.Fatalf("expected 3 deleted, got %d", count)
		}
	})

	t.Run("empty list deletes all", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		s := NewPostgresToolClassificationStore(db)

		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tool_classifications`)).
			WillReturnResult(sqlmock.NewResult(0, 5))

		count, err := s.DeleteNotIn(context.Background(), nil)
		if err != nil {
			t.Fatalf("delete not in: %v", err)
		}
		if count != 5 {
			t.Fatalf("expected 5 deleted, got %d", count)
		}
	})

	t.Run("db exec error", func(t *testing.T) {
		t.Parallel()

		db, mock := newMockDB(t)
		s := NewPostgresToolClassificationStore(db)

		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM tool_classifications WHERE tool_name NOT IN ($1)`)).
			WithArgs("tool.a").
			WillReturnError(fmt.Errorf("connection lost"))

		count, err := s.DeleteNotIn(context.Background(), []string{"tool.a"})
		if err == nil {
			t.Fatal("expected error from DeleteNotIn, got nil")
		}
		if count != 0 {
			t.Fatalf("expected 0 on error, got %d", count)
		}
	})
}
