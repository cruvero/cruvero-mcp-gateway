package store

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// StartAuditRetention runs a background goroutine that periodically deletes
// audit_log rows older than retentionDays. It stops when ctx is cancelled.
func StartAuditRetention(ctx context.Context, db *sql.DB, retentionDays int, interval time.Duration, logger *slog.Logger) {
	if db == nil || retentionDays <= 0 || interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := cleanupAuditLog(ctx, db, retentionDays)
			if err != nil {
				if logger != nil {
					logger.ErrorContext(ctx, "audit retention cleanup failed", slog.String("error", err.Error()))
				}
				continue
			}
			if deleted > 0 && logger != nil {
				logger.InfoContext(ctx, "audit retention cleanup completed",
					slog.Int64("deleted", deleted),
					slog.Int("retention_days", retentionDays),
				)
			}
		}
	}
}

func cleanupAuditLog(ctx context.Context, db *sql.DB, retentionDays int) (int64, error) {
	const query = `DELETE FROM audit_log WHERE created_at < now() - make_interval(days => $1)`

	result, err := db.ExecContext(ctx, query, retentionDays)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}
