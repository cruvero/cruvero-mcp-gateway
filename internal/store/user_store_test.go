package store

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
)

var userColumnNames = []string{"id", "oidc_sub", "email", "display_name", "role", "created_at", "updated_at"}
var userToolPermissionColumnNames = []string{"user_id", "tool_name", "granted_by", "granted_at"}

func TestPostgresUserStoreUpsert(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(`INSERT INTO gateway_users`).
		WithArgs("user-1", "sub-123", "user@example.com", "Test User", "user", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	user := &types.User{
		ID:          "user-1",
		OIDCSub:     "sub-123",
		Email:       "user@example.com",
		DisplayName: "Test User",
		Role:        types.RoleUser,
	}
	if err := s.Upsert(context.Background(), user); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

func TestPostgresUserStoreUpsertNil(t *testing.T) {
	t.Parallel()

	db, _ := newMockDB(t)
	s := NewPostgresUserStore(db)

	if err := s.Upsert(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil user")
	}
}

func TestPostgresUserStoreUpsertExecError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(`INSERT INTO gateway_users`).
		WithArgs("user-1", "sub-123", "user@example.com", "Test User", "user", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(fmt.Errorf("connection refused"))

	user := &types.User{
		ID:          "user-1",
		OIDCSub:     "sub-123",
		Email:       "user@example.com",
		DisplayName: "Test User",
		Role:        types.RoleUser,
	}
	if err := s.Upsert(context.Background(), user); err == nil {
		t.Fatal("expected error from Upsert, got nil")
	}
}

func TestPostgresUserStoreUpsertInvalidRole(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(`INSERT INTO gateway_users`).
		WithArgs("user-1", "sub-123", "user@example.com", "", "user", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	user := &types.User{
		ID:      "user-1",
		OIDCSub: "sub-123",
		Email:   "user@example.com",
		Role:    types.UserRole("invalid"),
	}
	if err := s.Upsert(context.Background(), user); err != nil {
		t.Fatalf("upsert with invalid role should default to user: %v", err)
	}
}

func TestPostgresUserStoreGet(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users WHERE id = $1`)).
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(userColumnNames).AddRow(
			"user-1", "sub-123", "user@example.com", "Test User", "admin", now, now,
		))

	user, err := s.Get(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if user == nil {
		t.Fatal("expected non-nil user")
	}
	if user.Role != types.RoleAdmin {
		t.Fatalf("expected admin role, got %q", user.Role)
	}
	if user.Email != "user@example.com" {
		t.Fatalf("expected user@example.com, got %q", user.Email)
	}
}

func TestPostgresUserStoreGetMissing(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users WHERE id = $1`)).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows(userColumnNames))

	user, err := s.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if user != nil {
		t.Fatalf("expected nil for missing user, got %+v", user)
	}
}

func TestPostgresUserStoreGetError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users WHERE id = $1`)).
		WithArgs("user-1").
		WillReturnError(fmt.Errorf("connection lost"))

	_, err := s.Get(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected error from Get, got nil")
	}
}

func TestPostgresUserStoreGetByOIDCSub(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users WHERE oidc_sub = $1`)).
		WithArgs("sub-123").
		WillReturnRows(sqlmock.NewRows(userColumnNames).AddRow(
			"user-1", "sub-123", "user@example.com", "Test User", "user", now, now,
		))

	user, err := s.GetByOIDCSub(context.Background(), "sub-123")
	if err != nil {
		t.Fatalf("get by oidc sub: %v", err)
	}
	if user == nil {
		t.Fatal("expected non-nil user")
	}
	if user.OIDCSub != "sub-123" {
		t.Fatalf("expected sub-123, got %q", user.OIDCSub)
	}
}

func TestPostgresUserStoreGetByOIDCSubMissing(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users WHERE oidc_sub = $1`)).
		WithArgs("missing-sub").
		WillReturnRows(sqlmock.NewRows(userColumnNames))

	user, err := s.GetByOIDCSub(context.Background(), "missing-sub")
	if err != nil {
		t.Fatalf("get by oidc sub: %v", err)
	}
	if user != nil {
		t.Fatalf("expected nil for missing oidc sub, got %+v", user)
	}
}

func TestPostgresUserStoreSearchNoFilters(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM gateway_users`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users ORDER BY email LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnRows(sqlmock.NewRows(userColumnNames).
			AddRow("user-1", "sub-1", "alice@example.com", "Alice", "admin", now, now).
			AddRow("user-2", "sub-2", "bob@example.com", "Bob", "user", now, now))

	results, total, err := s.Search(context.Background(), types.UserFilter{Limit: 50, Offset: 0})
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

func TestPostgresUserStoreSearchWithQueryAndRole(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM gateway_users WHERE (email ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%') AND role = $2`)).
		WithArgs("alice", "admin").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users WHERE (email ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%') AND role = $2 ORDER BY email LIMIT $3 OFFSET $4`)).
		WithArgs("alice", "admin", 50, 0).
		WillReturnRows(sqlmock.NewRows(userColumnNames).
			AddRow("user-1", "sub-1", "alice@example.com", "Alice", "admin", now, now))

	results, total, err := s.Search(context.Background(), types.UserFilter{
		Query:  "alice",
		Role:   types.RoleAdmin,
		Limit:  50,
		Offset: 0,
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

func TestPostgresUserStoreSearchCountError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM gateway_users`)).
		WillReturnError(fmt.Errorf("count query failed"))

	_, _, err := s.Search(context.Background(), types.UserFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected error from count query")
	}
}

func TestPostgresUserStoreSearchDataQueryError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM gateway_users`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users ORDER BY email LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnError(fmt.Errorf("data query failed"))

	_, _, err := s.Search(context.Background(), types.UserFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected error from data query")
	}
}

func TestPostgresUserStoreSearchScanError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM gateway_users`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users ORDER BY email LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnRows(sqlmock.NewRows(userColumnNames).
			AddRow("user-1", "sub-1", "alice@example.com", "Alice", "admin", now, now).
			AddRow("user-2", "sub-2", "bob@example.com", "Bob", "user", "not_a_time", now))

	_, _, err := s.Search(context.Background(), types.UserFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}

func TestPostgresUserStoreUpdateRole(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE gateway_users SET role = $1, updated_at = $2 WHERE id = $3`)).
		WithArgs("admin", sqlmock.AnyArg(), "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.UpdateRole(context.Background(), "user-1", types.RoleAdmin); err != nil {
		t.Fatalf("update role: %v", err)
	}
}

func TestPostgresUserStoreUpdateRoleInvalid(t *testing.T) {
	t.Parallel()

	db, _ := newMockDB(t)
	s := NewPostgresUserStore(db)

	if err := s.UpdateRole(context.Background(), "user-1", types.UserRole("mega_admin")); err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestPostgresUserStoreUpdateRoleExecError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE gateway_users SET role = $1, updated_at = $2 WHERE id = $3`)).
		WithArgs("blocked", sqlmock.AnyArg(), "user-1").
		WillReturnError(fmt.Errorf("connection lost"))

	if err := s.UpdateRole(context.Background(), "user-1", types.RoleBlocked); err == nil {
		t.Fatal("expected error from UpdateRole, got nil")
	}
}

func TestPostgresUserStoreDelete(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM gateway_users WHERE id = $1`)).
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Delete(context.Background(), "user-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestPostgresUserStoreDeleteExecError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM gateway_users WHERE id = $1`)).
		WithArgs("user-1").
		WillReturnError(fmt.Errorf("connection lost"))

	if err := s.Delete(context.Background(), "user-1"); err == nil {
		t.Fatal("expected error from Delete, got nil")
	}
}

func TestPostgresUserStoreGetToolPermissions(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userToolPermissionColumns + ` FROM user_tool_permissions WHERE user_id = $1 ORDER BY tool_name`)).
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(userToolPermissionColumnNames).
			AddRow("user-1", "github.list_repos", "admin@example.com", now).
			AddRow("user-1", "k8s.get_pods", "admin@example.com", now))

	perms, err := s.GetToolPermissions(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get tool permissions: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(perms))
	}
	if perms[0].ToolName != "github.list_repos" {
		t.Fatalf("expected github.list_repos, got %q", perms[0].ToolName)
	}
}

func TestPostgresUserStoreGetToolPermissionsQueryError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userToolPermissionColumns + ` FROM user_tool_permissions WHERE user_id = $1 ORDER BY tool_name`)).
		WithArgs("user-1").
		WillReturnError(fmt.Errorf("connection refused"))

	_, err := s.GetToolPermissions(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected error from GetToolPermissions, got nil")
	}
}

func TestPostgresUserStoreHasToolPermission(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM user_tool_permissions WHERE user_id = $1 AND tool_name = $2`)).
		WithArgs("user-1", "github.list_repos").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	has, err := s.HasToolPermission(context.Background(), "user-1", "github.list_repos")
	if err != nil {
		t.Fatalf("has tool permission: %v", err)
	}
	if !has {
		t.Fatal("expected true for existing permission")
	}
}

func TestPostgresUserStoreHasToolPermissionNotFound(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM user_tool_permissions WHERE user_id = $1 AND tool_name = $2`)).
		WithArgs("user-1", "github.delete_repo").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	has, err := s.HasToolPermission(context.Background(), "user-1", "github.delete_repo")
	if err != nil {
		t.Fatalf("has tool permission: %v", err)
	}
	if has {
		t.Fatal("expected false for missing permission")
	}
}

func TestPostgresUserStoreHasToolPermissionError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM user_tool_permissions WHERE user_id = $1 AND tool_name = $2`)).
		WithArgs("user-1", "github.list_repos").
		WillReturnError(fmt.Errorf("connection lost"))

	_, err := s.HasToolPermission(context.Background(), "user-1", "github.list_repos")
	if err == nil {
		t.Fatal("expected error from HasToolPermission, got nil")
	}
}

func TestPostgresUserStoreSetToolPermissions(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_tool_permissions WHERE user_id = $1`)).
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO user_tool_permissions`).
		WithArgs("user-1", "github.list_repos", "admin@example.com", sqlmock.AnyArg(),
			"user-1", "k8s.get_pods", "admin@example.com", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	err := s.SetToolPermissions(context.Background(), "user-1", []string{"github.list_repos", "k8s.get_pods"}, "admin@example.com")
	if err != nil {
		t.Fatalf("set tool permissions: %v", err)
	}
}

func TestPostgresUserStoreSetToolPermissionsEmpty(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_tool_permissions WHERE user_id = $1`)).
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := s.SetToolPermissions(context.Background(), "user-1", []string{}, "admin@example.com")
	if err != nil {
		t.Fatalf("set empty tool permissions: %v", err)
	}
}

func TestPostgresUserStoreSetToolPermissionsEmptyUserID(t *testing.T) {
	t.Parallel()

	db, _ := newMockDB(t)
	s := NewPostgresUserStore(db)

	if err := s.SetToolPermissions(context.Background(), "", []string{"tool"}, "admin"); err == nil {
		t.Fatal("expected error for empty user ID")
	}
}

func TestPostgresUserStoreSetToolPermissionsBeginError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectBegin().WillReturnError(fmt.Errorf("begin failed"))

	if err := s.SetToolPermissions(context.Background(), "user-1", []string{"tool"}, "admin"); err == nil {
		t.Fatal("expected error from begin tx")
	}
}

func TestPostgresUserStoreSetToolPermissionsDeleteError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_tool_permissions WHERE user_id = $1`)).
		WithArgs("user-1").
		WillReturnError(fmt.Errorf("delete failed"))
	mock.ExpectRollback()

	if err := s.SetToolPermissions(context.Background(), "user-1", []string{"tool"}, "admin"); err == nil {
		t.Fatal("expected error from delete")
	}
}

func TestPostgresUserStoreSetToolPermissionsInsertError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_tool_permissions WHERE user_id = $1`)).
		WithArgs("user-1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO user_tool_permissions`).
		WithArgs("user-1", "tool-a", "admin", sqlmock.AnyArg()).
		WillReturnError(fmt.Errorf("insert failed"))
	mock.ExpectRollback()

	if err := s.SetToolPermissions(context.Background(), "user-1", []string{"tool-a"}, "admin"); err == nil {
		t.Fatal("expected error from insert")
	}
}

func TestScanUsersRowScanError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM gateway_users`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userColumns + ` FROM gateway_users ORDER BY email LIMIT $1 OFFSET $2`)).
		WithArgs(50, 0).
		WillReturnRows(sqlmock.NewRows(userColumnNames).
			AddRow("user-1", "sub-1", "alice@example.com", "Alice", "admin", now, now).
			RowError(0, fmt.Errorf("row iteration error")))

	results, _, err := s.Search(context.Background(), types.UserFilter{Limit: 50})
	if err == nil {
		t.Fatal("expected rows error, got nil")
	}
	if results != nil {
		t.Fatalf("expected nil results on rows error, got %+v", results)
	}
}

func TestScanUserToolPermissionsScanError(t *testing.T) {
	t.Parallel()

	db, mock := newMockDB(t)
	s := NewPostgresUserStore(db)
	now := fixedTime()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + userToolPermissionColumns + ` FROM user_tool_permissions WHERE user_id = $1 ORDER BY tool_name`)).
		WithArgs("user-1").
		WillReturnRows(sqlmock.NewRows(userToolPermissionColumnNames).
			AddRow("user-1", "tool-a", "admin", now).
			AddRow("user-1", "tool-b", "admin", "not_a_time"))

	_, err := s.GetToolPermissions(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
}
