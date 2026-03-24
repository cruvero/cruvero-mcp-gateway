package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const userColumns = "id, oidc_sub, email, display_name, role, created_at, updated_at"
const userToolPermissionColumns = "user_id, tool_name, granted_by, granted_at"

// PostgresUserStore is a Postgres-backed implementation of UserStore.
type PostgresUserStore struct {
	db *sql.DB
}

var _ UserStore = (*PostgresUserStore)(nil)

// NewPostgresUserStore creates a PostgresUserStore.
func NewPostgresUserStore(db *sql.DB) *PostgresUserStore {
	return &PostgresUserStore{db: db}
}

// Upsert creates or updates a user. On conflict by oidc_sub, it updates email,
// display_name, and updated_at but preserves the existing role.
func (s *PostgresUserStore) Upsert(ctx context.Context, user *types.User) error {
	if user == nil {
		return fmt.Errorf("user store: user is nil")
	}

	const query = `
INSERT INTO gateway_users (id, oidc_sub, email, display_name, role, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (oidc_sub) DO UPDATE SET
    email = EXCLUDED.email,
    display_name = EXCLUDED.display_name,
    updated_at = EXCLUDED.updated_at
`

	now := time.Now().UTC()
	id := strings.TrimSpace(user.ID)
	if id == "" {
		id = "auto"
	}
	role := user.Role
	if !role.IsValid() {
		role = types.RoleUser
	}

	if _, err := s.db.ExecContext(
		ctx,
		query,
		id,
		strings.TrimSpace(user.OIDCSub),
		strings.TrimSpace(user.Email),
		strings.TrimSpace(user.DisplayName),
		string(role),
		now,
		now,
	); err != nil {
		return fmt.Errorf("user store: upsert: %w", err)
	}
	return nil
}

// Get returns a user by ID. Returns (nil, nil) for non-existent users.
func (s *PostgresUserStore) Get(ctx context.Context, id string) (*types.User, error) {
	const query = `SELECT ` + userColumns + ` FROM gateway_users WHERE id = $1`

	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(id))
	user, err := scanUser(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("user store: get: %w", err)
	}
	return user, nil
}

// GetByOIDCSub returns a user by OIDC subject claim. Returns (nil, nil) for non-existent users.
func (s *PostgresUserStore) GetByOIDCSub(ctx context.Context, oidcSub string) (*types.User, error) {
	const query = `SELECT ` + userColumns + ` FROM gateway_users WHERE oidc_sub = $1`

	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(oidcSub))
	user, err := scanUser(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("user store: get by oidc sub: %w", err)
	}
	return user, nil
}

// Search returns users matching the filter with pagination and total count.
func (s *PostgresUserStore) Search(ctx context.Context, filter types.UserFilter) ([]types.User, int, error) {
	var conditions []string
	var args []any
	paramIdx := 1

	if q := strings.TrimSpace(filter.Query); q != "" {
		conditions = append(conditions, fmt.Sprintf(
			"(email ILIKE '%%' || $%d || '%%' OR display_name ILIKE '%%' || $%d || '%%')",
			paramIdx, paramIdx,
		))
		args = append(args, escapeILikePattern(q))
		paramIdx++
	}
	if filter.Role.IsValid() {
		conditions = append(conditions, fmt.Sprintf("role = $%d", paramIdx))
		args = append(args, string(filter.Role))
		paramIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := "SELECT COUNT(*) FROM gateway_users" + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("user store: search count: %w", err)
	}

	dataQuery := "SELECT " + userColumns + " FROM gateway_users" + where +
		fmt.Sprintf(" ORDER BY email LIMIT $%d OFFSET $%d", paramIdx, paramIdx+1)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(filter.Offset, 0)
	dataArgs := append(args, limit, offset) //nolint:gocritic

	rows, err := s.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("user store: search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results, err := scanUsers(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// UpdateRole changes a user's role.
func (s *PostgresUserStore) UpdateRole(ctx context.Context, id string, role types.UserRole) error {
	if !role.IsValid() {
		return fmt.Errorf("user store: update role: invalid role %q", role)
	}

	const query = `UPDATE gateway_users SET role = $1, updated_at = $2 WHERE id = $3`

	if _, err := s.db.ExecContext(ctx, query, string(role), time.Now().UTC(), strings.TrimSpace(id)); err != nil {
		return fmt.Errorf("user store: update role: %w", err)
	}
	return nil
}

// Delete removes a user by ID.
func (s *PostgresUserStore) Delete(ctx context.Context, id string) error {
	const query = `DELETE FROM gateway_users WHERE id = $1`

	if _, err := s.db.ExecContext(ctx, query, strings.TrimSpace(id)); err != nil {
		return fmt.Errorf("user store: delete: %w", err)
	}
	return nil
}

// GetToolPermissions returns all tool permissions for a user.
func (s *PostgresUserStore) GetToolPermissions(ctx context.Context, userID string) ([]types.UserToolPermission, error) {
	const query = `SELECT ` + userToolPermissionColumns + ` FROM user_tool_permissions WHERE user_id = $1 ORDER BY tool_name`

	rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(userID))
	if err != nil {
		return nil, fmt.Errorf("user store: get tool permissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanUserToolPermissions(rows)
}

// HasToolPermission checks whether a user has a specific tool permission.
func (s *PostgresUserStore) HasToolPermission(ctx context.Context, userID string, toolName string) (bool, error) {
	const query = `SELECT COUNT(*) FROM user_tool_permissions WHERE user_id = $1 AND tool_name = $2`

	var count int
	if err := s.db.QueryRowContext(ctx, query, strings.TrimSpace(userID), strings.TrimSpace(toolName)).Scan(&count); err != nil {
		return false, fmt.Errorf("user store: has tool permission: %w", err)
	}
	return count > 0, nil
}

// SetToolPermissions replaces all tool permissions for a user in a single transaction.
func (s *PostgresUserStore) SetToolPermissions(ctx context.Context, userID string, toolNames []string, grantedBy string) error {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return fmt.Errorf("user store: set tool permissions: user ID is empty")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("user store: set tool permissions: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_tool_permissions WHERE user_id = $1`, uid); err != nil {
		return fmt.Errorf("user store: set tool permissions: delete: %w", err)
	}

	if len(toolNames) > 0 {
		const insertBase = `INSERT INTO user_tool_permissions (user_id, tool_name, granted_by, granted_at) VALUES `
		now := time.Now().UTC()
		gb := strings.TrimSpace(grantedBy)

		var sb strings.Builder
		sb.WriteString(insertBase)
		args := make([]any, 0, len(toolNames)*4)
		for i, name := range toolNames {
			if i > 0 {
				sb.WriteString(", ")
			}
			base := i * 4
			fmt.Fprintf(&sb, "($%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4)
			args = append(args, uid, strings.TrimSpace(name), gb, now)
		}

		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("user store: set tool permissions: insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("user store: set tool permissions: commit: %w", err)
	}
	return nil
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(scanner userScanner) (*types.User, error) {
	var u types.User
	var role string
	if err := scanner.Scan(
		&u.ID,
		&u.OIDCSub,
		&u.Email,
		&u.DisplayName,
		&role,
		&u.CreatedAt,
		&u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	u.Role = types.UserRole(role)
	return &u, nil
}

func scanUsers(rows *sql.Rows) ([]types.User, error) {
	result := make([]types.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("user store: scan: %w", err)
		}
		result = append(result, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user store: rows: %w", err)
	}
	return result, nil
}

func scanUserToolPermissions(rows *sql.Rows) ([]types.UserToolPermission, error) {
	result := make([]types.UserToolPermission, 0)
	for rows.Next() {
		var p types.UserToolPermission
		if err := rows.Scan(&p.UserID, &p.ToolName, &p.GrantedBy, &p.GrantedAt); err != nil {
			return nil, fmt.Errorf("user store: scan permission: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user store: permission rows: %w", err)
	}
	return result, nil
}
