package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const toolClassificationColumns = "tool_name, risk_level, reason, auto_classified, updated_by, updated_at"

// PostgresToolClassificationStore is a Postgres-backed implementation of ToolClassificationStore.
type PostgresToolClassificationStore struct {
	db *sql.DB
}

var _ ToolClassificationStore = (*PostgresToolClassificationStore)(nil)

// NewPostgresToolClassificationStore creates a PostgresToolClassificationStore.
func NewPostgresToolClassificationStore(db *sql.DB) *PostgresToolClassificationStore {
	return &PostgresToolClassificationStore{db: db}
}

// Get returns a tool classification by name. Returns (nil, nil) for non-existent tools.
func (s *PostgresToolClassificationStore) Get(ctx context.Context, toolName string) (*types.ToolClassification, error) {
	const query = `SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE tool_name = $1`

	row := s.db.QueryRowContext(ctx, query, strings.TrimSpace(toolName))
	tc, err := scanToolClassification(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("tool classification store: get: %w", err)
	}
	return tc, nil
}

// GetAll returns all tool classifications ordered by tool name.
func (s *PostgresToolClassificationStore) GetAll(ctx context.Context) ([]types.ToolClassification, error) {
	const query = `SELECT ` + toolClassificationColumns + ` FROM tool_classifications ORDER BY tool_name`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("tool classification store: get all: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanToolClassifications(rows)
}

// GetByRiskLevel returns tool classifications filtered by risk level.
func (s *PostgresToolClassificationStore) GetByRiskLevel(ctx context.Context, level types.RiskLevel) ([]types.ToolClassification, error) {
	const query = `SELECT ` + toolClassificationColumns + ` FROM tool_classifications WHERE risk_level = $1 ORDER BY tool_name`

	rows, err := s.db.QueryContext(ctx, query, string(level))
	if err != nil {
		return nil, fmt.Errorf("tool classification store: get by risk level: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanToolClassifications(rows)
}

// Upsert creates or updates a tool classification.
func (s *PostgresToolClassificationStore) Upsert(ctx context.Context, classification *types.ToolClassification) error {
	if classification == nil {
		return fmt.Errorf("tool classification store: classification is nil")
	}

	const query = `
INSERT INTO tool_classifications (tool_name, risk_level, reason, auto_classified, updated_by, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tool_name) DO UPDATE SET
    risk_level = EXCLUDED.risk_level,
    reason = EXCLUDED.reason,
    auto_classified = EXCLUDED.auto_classified,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at
`

	if _, err := s.db.ExecContext(
		ctx,
		query,
		strings.TrimSpace(classification.ToolName),
		string(classification.RiskLevel),
		classification.Reason,
		classification.AutoClassified,
		classification.UpdatedBy,
		classification.UpdatedAt,
	); err != nil {
		return fmt.Errorf("tool classification store: upsert: %w", err)
	}
	return nil
}

// Search returns tool classifications matching the filter with pagination and total count.
func (s *PostgresToolClassificationStore) Search(ctx context.Context, filter types.ToolFilter) ([]types.ToolClassification, int, error) {
	var conditions []string
	var args []any
	paramIdx := 1

	if q := strings.TrimSpace(filter.Query); q != "" {
		conditions = append(conditions, fmt.Sprintf("tool_name ILIKE '%%' || $%d || '%%'", paramIdx))
		args = append(args, escapeILikePattern(q))
		paramIdx++
	}
	if filter.RiskLevel.IsValid() {
		conditions = append(conditions, fmt.Sprintf("risk_level = $%d", paramIdx))
		args = append(args, string(filter.RiskLevel))
		paramIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := "SELECT COUNT(*) FROM tool_classifications" + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("tool classification store: search count: %w", err)
	}

	dataQuery := "SELECT " + toolClassificationColumns + " FROM tool_classifications" + where +
		fmt.Sprintf(" ORDER BY tool_name LIMIT $%d OFFSET $%d", paramIdx, paramIdx+1)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	dataArgs := append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("tool classification store: search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results, err := scanToolClassifications(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// Delete removes a tool classification by name.
func (s *PostgresToolClassificationStore) Delete(ctx context.Context, toolName string) error {
	const query = `DELETE FROM tool_classifications WHERE tool_name = $1`

	if _, err := s.db.ExecContext(ctx, query, strings.TrimSpace(toolName)); err != nil {
		return fmt.Errorf("tool classification store: delete: %w", err)
	}
	return nil
}

// DeleteNotIn removes all tool classifications whose tool_name is not in activeToolNames.
// When activeToolNames is empty, all classifications are deleted.
// Returns the number of deleted rows.
func (s *PostgresToolClassificationStore) DeleteNotIn(ctx context.Context, activeToolNames []string) (int64, error) {
	var result sql.Result
	var err error

	normalized := make([]string, 0, len(activeToolNames))
	for _, name := range activeToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}

	if len(normalized) == 0 {
		result, err = s.db.ExecContext(ctx, `DELETE FROM tool_classifications`)
	} else {
		placeholders := make([]string, len(normalized))
		args := make([]any, len(normalized))
		for i, name := range normalized {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = name
		}
		query := `DELETE FROM tool_classifications WHERE tool_name NOT IN (` + strings.Join(placeholders, ", ") + `)`
		result, err = s.db.ExecContext(ctx, query, args...)
	}

	if err != nil {
		return 0, fmt.Errorf("tool classification store: delete not in: %w", err)
	}
	rows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return 0, fmt.Errorf("tool classification store: delete not in rows affected: %w", rowsErr)
	}
	return rows, nil
}

type toolClassificationScanner interface {
	Scan(dest ...any) error
}

func scanToolClassification(scanner toolClassificationScanner) (*types.ToolClassification, error) {
	var tc types.ToolClassification
	var riskLevel string
	if err := scanner.Scan(
		&tc.ToolName,
		&riskLevel,
		&tc.Reason,
		&tc.AutoClassified,
		&tc.UpdatedBy,
		&tc.UpdatedAt,
	); err != nil {
		return nil, err
	}
	tc.RiskLevel = types.RiskLevel(riskLevel)
	return &tc, nil
}

func scanToolClassifications(rows *sql.Rows) ([]types.ToolClassification, error) {
	result := make([]types.ToolClassification, 0)
	for rows.Next() {
		tc, err := scanToolClassification(rows)
		if err != nil {
			return nil, fmt.Errorf("tool classification store: scan: %w", err)
		}
		result = append(result, *tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool classification store: rows: %w", err)
	}
	return result, nil
}
