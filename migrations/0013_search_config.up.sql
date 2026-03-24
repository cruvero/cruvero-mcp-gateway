-- Search synonyms: DB-backed synonym groups for search query expansion.
CREATE TABLE IF NOT EXISTS search_synonyms (
    term       TEXT PRIMARY KEY,
    synonyms   TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Search reindex log: tracks reindex operations for observability.
CREATE TABLE IF NOT EXISTS search_reindex_log (
    id          SERIAL PRIMARY KEY,
    engine      TEXT NOT NULL,
    trigger     TEXT NOT NULL,
    tools_count INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'success',
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_search_reindex_log_created_at
    ON search_reindex_log (created_at DESC);

-- Seed default synonym groups matching the static synonyms in code.
INSERT INTO search_synonyms (term, synonyms) VALUES
    ('kubernetes', ARRAY['kubernetes', 'k8s', 'kube', 'kubectl']),
    ('k8s', ARRAY['kubernetes', 'k8s', 'kube', 'kubectl']),
    ('kube', ARRAY['kubernetes', 'k8s', 'kube', 'kubectl']),
    ('kubectl', ARRAY['kubernetes', 'k8s', 'kube', 'kubectl']),
    ('docker', ARRAY['docker', 'container', 'containerized']),
    ('container', ARRAY['docker', 'container', 'containerized']),
    ('containerized', ARRAY['docker', 'container', 'containerized']),
    ('postgres', ARRAY['postgres', 'postgresql', 'pg']),
    ('postgresql', ARRAY['postgres', 'postgresql', 'pg']),
    ('pg', ARRAY['postgres', 'postgresql', 'pg']),
    ('database', ARRAY['database', 'db']),
    ('db', ARRAY['database', 'db']),
    ('repository', ARRAY['repository', 'repo']),
    ('repo', ARRAY['repository', 'repo']),
    ('delete', ARRAY['delete', 'remove', 'rm']),
    ('remove', ARRAY['delete', 'remove', 'rm']),
    ('rm', ARRAY['delete', 'remove', 'rm']),
    ('create', ARRAY['create', 'new', 'add']),
    ('new', ARRAY['create', 'new', 'add']),
    ('add', ARRAY['create', 'new', 'add']),
    ('list', ARRAY['list', 'ls', 'enumerate']),
    ('ls', ARRAY['list', 'ls', 'enumerate']),
    ('enumerate', ARRAY['list', 'ls', 'enumerate']),
    ('get', ARRAY['get', 'fetch', 'retrieve']),
    ('fetch', ARRAY['get', 'fetch', 'retrieve']),
    ('retrieve', ARRAY['get', 'fetch', 'retrieve']),
    ('update', ARRAY['update', 'modify', 'edit', 'patch']),
    ('modify', ARRAY['update', 'modify', 'edit', 'patch']),
    ('edit', ARRAY['update', 'modify', 'edit', 'patch']),
    ('patch', ARRAY['update', 'modify', 'edit', 'patch']),
    ('javascript', ARRAY['javascript', 'js']),
    ('js', ARRAY['javascript', 'js']),
    ('typescript', ARRAY['typescript', 'ts']),
    ('ts', ARRAY['typescript', 'ts']),
    ('python', ARRAY['python', 'py']),
    ('py', ARRAY['python', 'py']),
    ('message', ARRAY['message', 'msg']),
    ('msg', ARRAY['message', 'msg']),
    ('configuration', ARRAY['configuration', 'config', 'cfg']),
    ('config', ARRAY['configuration', 'config', 'cfg']),
    ('cfg', ARRAY['configuration', 'config', 'cfg']),
    ('environment', ARRAY['environment', 'env']),
    ('env', ARRAY['environment', 'env']),
    ('application', ARRAY['application', 'app']),
    ('app', ARRAY['application', 'app'])
ON CONFLICT (term) DO UPDATE SET
    synonyms = EXCLUDED.synonyms,
    updated_at = NOW();
