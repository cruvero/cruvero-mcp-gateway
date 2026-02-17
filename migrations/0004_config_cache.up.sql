CREATE TABLE config_cache (
    key TEXT PRIMARY KEY,
    value BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_config_cache_updated_at ON config_cache (updated_at);
