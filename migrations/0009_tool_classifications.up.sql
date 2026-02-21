CREATE TABLE IF NOT EXISTS tool_classifications (
    tool_name TEXT PRIMARY KEY,
    risk_level TEXT NOT NULL DEFAULT 'unknown' CHECK (risk_level IN ('read_only', 'write', 'destructive', 'unknown')),
    reason TEXT NOT NULL DEFAULT '',
    auto_classified BOOLEAN NOT NULL DEFAULT true,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tool_classifications_risk_level ON tool_classifications(risk_level);
