CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE mcp_servers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    spiffe_id TEXT NOT NULL UNIQUE,
    version TEXT NOT NULL DEFAULT '',
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    capabilities JSONB DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending',
    policy_profile TEXT DEFAULT 'default',
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_mcp_servers_status ON mcp_servers (status);
CREATE INDEX idx_mcp_servers_spiffe_id ON mcp_servers (spiffe_id);
