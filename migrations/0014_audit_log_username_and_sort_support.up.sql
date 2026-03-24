ALTER TABLE audit_log
    ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT 'system';

UPDATE audit_log
SET username = CASE
    WHEN btrim(client_id) = '' THEN 'system'
    WHEN lower(client_id) LIKE 'spiffe://%' THEN 'system'
    WHEN lower(client_id) LIKE 'server:%' THEN 'system'
    ELSE btrim(client_id)
END
WHERE username = 'system'
   OR username IS NULL
   OR btrim(username) = '';

CREATE INDEX IF NOT EXISTS idx_audit_log_username ON audit_log (username);
