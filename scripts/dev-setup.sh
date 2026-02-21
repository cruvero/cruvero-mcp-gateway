#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== MCP Gateway Dev Setup ==="

# 1. Generate certificates.
echo ""
echo "--- Generating dev certificates ---"
"$SCRIPT_DIR/gen-dev-certs.sh"

# 2. Wait for Postgres (skip gracefully if pg_isready is unavailable or DB is unreachable).
echo ""
echo "--- Checking Postgres readiness ---"
if command -v pg_isready >/dev/null 2>&1 && [ -n "${MCPGW_DB_URL:-}" ]; then
  # Extract host and port from the DB URL for pg_isready.
  DB_HOST=$(echo "$MCPGW_DB_URL" | sed -n 's|.*@\([^:/]*\).*|\1|p')
  DB_PORT=$(echo "$MCPGW_DB_URL" | sed -n 's|.*:\([0-9]*\)/.*|\1|p')
  DB_HOST="${DB_HOST:-localhost}"
  DB_PORT="${DB_PORT:-5432}"

  RETRIES=0
  MAX_RETRIES=30
  until pg_isready -h "$DB_HOST" -p "$DB_PORT" -q 2>/dev/null; do
    RETRIES=$((RETRIES + 1))
    if [ "$RETRIES" -ge "$MAX_RETRIES" ]; then
      echo "Postgres not ready after $MAX_RETRIES attempts, skipping migration."
      break
    fi
    echo "Waiting for Postgres at $DB_HOST:$DB_PORT ($RETRIES/$MAX_RETRIES)..."
    sleep 1
  done

  if [ "$RETRIES" -lt "$MAX_RETRIES" ]; then
    echo "Postgres is ready."
    # 3. Run migrations.
    echo ""
    echo "--- Running migrations ---"
    (cd "$PROJECT_DIR" && go run ./cmd/mcpgw migrate)
  fi
else
  echo "pg_isready not available or MCPGW_DB_URL not set, skipping Postgres check."
fi

# 4. Generate session key if not set.
echo ""
echo "--- Checking session key ---"
if [ -z "${MCPGW_ADMIN_SESSION_KEY:-}" ]; then
  GENERATED_KEY=$(openssl rand -hex 32)
  echo "Generated session key (set MCPGW_ADMIN_SESSION_KEY to persist)."
else
  GENERATED_KEY="$MCPGW_ADMIN_SESSION_KEY"
  echo "MCPGW_ADMIN_SESSION_KEY already set."
fi

# 5. Build binary.
echo ""
echo "--- Building mcpgw binary ---"
mkdir -p "$PROJECT_DIR/bin"
(cd "$PROJECT_DIR" && go build -o ./bin/mcpgw ./cmd/mcpgw)
echo "Binary built at $PROJECT_DIR/bin/mcpgw"

# 6. Print environment summary.
CERT_DIR="${CERT_DIR:-./certs}"
echo ""
echo "=== Dev Environment Ready ==="
echo ""
echo "Export these environment variables to run the gateway:"
echo ""
echo "  export MCPGW_DB_URL=\"${MCPGW_DB_URL:-postgres://mcpgw:mcpgw@localhost:5432/mcpgw?sslmode=disable}\""
echo "  export MCPGW_TLS_CERT=\"$CERT_DIR/server.crt\""
echo "  export MCPGW_TLS_KEY=\"$CERT_DIR/server.key\""
echo "  export MCPGW_TLS_CA=\"$CERT_DIR/ca.crt\""
echo "  export MCPGW_LOG_FORMAT=\"text\""
echo "  export MCPGW_LOG_LEVEL=\"debug\""
echo "  export MCPGW_ADMIN_ENABLED=\"true\""
echo "  export MCPGW_ADMIN_DEV_MODE=\"true\""
echo "  export MCPGW_ADMIN_SESSION_KEY=\"$GENERATED_KEY\""
echo ""
echo "Then start the gateway:"
echo ""
echo "  ./bin/mcpgw serve"
echo ""
