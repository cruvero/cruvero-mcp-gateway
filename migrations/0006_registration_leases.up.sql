ALTER TABLE mcp_servers
    ADD COLUMN lease_epoch BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN capability_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN sync_state TEXT NOT NULL DEFAULT 'unacked',
    ADD COLUMN last_platform_ack_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_platform_ack_at TIMESTAMPTZ;

CREATE INDEX idx_mcp_servers_sync_state ON mcp_servers (sync_state);
