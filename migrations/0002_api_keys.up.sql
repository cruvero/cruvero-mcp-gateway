CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_lookup_hash TEXT UNIQUE NOT NULL,
    key_bcrypt_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    client_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_api_keys_lookup_hash ON api_keys (key_lookup_hash);
CREATE INDEX idx_api_keys_client_id ON api_keys (client_id);
