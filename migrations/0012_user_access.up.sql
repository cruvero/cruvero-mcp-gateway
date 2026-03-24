CREATE TABLE IF NOT EXISTS gateway_users (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    oidc_sub     TEXT NOT NULL UNIQUE,
    email        TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user','viewer','blocked')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_gateway_users_email ON gateway_users USING gin (email gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_gateway_users_role ON gateway_users (role);

CREATE TABLE IF NOT EXISTS user_tool_permissions (
    user_id    TEXT NOT NULL REFERENCES gateway_users(id) ON DELETE CASCADE,
    tool_name  TEXT NOT NULL,
    granted_by TEXT NOT NULL DEFAULT '',
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, tool_name)
);
CREATE INDEX IF NOT EXISTS idx_user_tool_permissions_tool_name ON user_tool_permissions (tool_name);
