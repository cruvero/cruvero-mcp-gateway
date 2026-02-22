ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS rate_limit INT;
ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS rate_burst INT;

ALTER TABLE mcp_servers
    ADD CONSTRAINT chk_mcp_servers_rate_limit_nonnegative
    CHECK (rate_limit IS NULL OR rate_limit >= 0);

ALTER TABLE mcp_servers
    ADD CONSTRAINT chk_mcp_servers_rate_burst_nonnegative
    CHECK (rate_burst IS NULL OR rate_burst >= 0);

ALTER TABLE mcp_servers
    ADD CONSTRAINT chk_mcp_servers_rate_burst_requires_limit
    CHECK (rate_burst IS NULL OR rate_limit IS NOT NULL);
