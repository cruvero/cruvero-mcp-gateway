ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS chk_mcp_servers_rate_burst_requires_limit;
ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS chk_mcp_servers_rate_burst_nonnegative;
ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS chk_mcp_servers_rate_limit_nonnegative;
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS rate_burst;
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS rate_limit;
