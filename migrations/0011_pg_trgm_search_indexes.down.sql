DROP INDEX IF EXISTS idx_audit_log_details_text_trgm;
DROP INDEX IF EXISTS idx_audit_log_server_name_trgm;
DROP INDEX IF EXISTS idx_tool_classifications_name_trgm;
-- Do NOT drop pg_trgm extension; other schemas may depend on it.
