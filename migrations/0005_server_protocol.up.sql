ALTER TABLE mcp_servers
    ADD COLUMN protocol TEXT NOT NULL DEFAULT 'https';
