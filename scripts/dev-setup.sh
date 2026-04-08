#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CERT_DIR="${CERT_DIR:-/certs}"

echo "=== MCP Gateway Dev Setup ==="

# 1. Trust the local CA inside the devcontainer.
echo ""
echo "--- Trusting local development CA ---"
if [ -f "$CERT_DIR/ca.crt" ]; then
  sudo install -m 0644 "$CERT_DIR/ca.crt" /usr/local/share/ca-certificates/mcpgw-dev-ca.crt
  sudo update-ca-certificates >/dev/null
  echo "Trusted $CERT_DIR/ca.crt"
else
  echo "CA certificate not found at $CERT_DIR/ca.crt; skipping trust update."
fi

# 2. Wait for Postgres (skip gracefully if pg_isready is unavailable or DB is unreachable).
echo ""
echo "--- Checking Postgres readiness ---"
if command -v pg_isready >/dev/null 2>&1 && [ -n "${MCPGW_DB_URL:-}" ]; then
  RETRIES=0
  MAX_RETRIES=30
  until pg_isready -d "$MCPGW_DB_URL" -q 2>/dev/null; do
    RETRIES=$((RETRIES + 1))
    if [ "$RETRIES" -ge "$MAX_RETRIES" ]; then
      echo "Postgres not ready after $MAX_RETRIES attempts, skipping migration."
      break
    fi
    echo "Waiting for Postgres ($RETRIES/$MAX_RETRIES)..."
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

# 4. Build binaries.
echo ""
echo "--- Building local binaries ---"
mkdir -p "$PROJECT_DIR/bin"
(cd "$PROJECT_DIR" && go build -o ./bin/mcpgw ./cmd/mcpgw)
(cd "$PROJECT_DIR" && go build -o ./bin/mock-mcp-backend ./cmd/mock-mcp-backend)
echo "Binary built at $PROJECT_DIR/bin/mcpgw"
echo "Binary built at $PROJECT_DIR/bin/mock-mcp-backend"

# 5. Print environment summary.
echo ""
echo "=== Dev Environment Ready ==="
echo ""
echo "Gateway URL:  https://gateway.localhost:8443"
echo "Keycloak URL: https://keycloak.localhost:8444"
echo ""
echo "Seeded Keycloak users:"
echo "  dev-admin / dev-admin"
echo "  dev-user  / dev-user"
echo ""
echo "Start the standalone gateway inside the devcontainer:"
echo "  ./scripts/dev-run.sh"
echo ""
echo "Run the local smoke check once the gateway is up:"
echo "  ./scripts/dev-smoke.sh"
echo ""
