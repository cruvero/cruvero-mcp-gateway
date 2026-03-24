CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_tool_classifications_name_trgm
    ON tool_classifications USING gin (tool_name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_audit_log_server_name_trgm
    ON audit_log USING gin (server_name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_audit_log_details_text_trgm
    ON audit_log USING gin ((details::text) gin_trgm_ops);
