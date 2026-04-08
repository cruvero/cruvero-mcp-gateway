ALTER TABLE api_keys ADD COLUMN server_scope TEXT[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN api_keys.server_scope IS 'Empty array = gateway-wide access. Non-empty = restricted to listed server names.';
