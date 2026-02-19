DROP INDEX IF EXISTS idx_mcp_servers_sync_state;

ALTER TABLE mcp_servers
    DROP COLUMN IF EXISTS last_platform_ack_at,
    DROP COLUMN IF EXISTS last_platform_ack_version,
    DROP COLUMN IF EXISTS sync_state,
    DROP COLUMN IF EXISTS capability_hash,
    DROP COLUMN IF EXISTS lease_epoch;
