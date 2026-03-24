DROP INDEX IF EXISTS idx_audit_log_username;

ALTER TABLE audit_log
    DROP COLUMN IF EXISTS username;
