#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
CERT_DIR="${CERT_DIR:-/certs}"

export MCPGW_DB_URL="${MCPGW_DB_URL:-postgres://mcpgw:mcpgw@postgres:5432/mcpgw?sslmode=disable}"
export MCPGW_LISTEN_ADDR="${MCPGW_LISTEN_ADDR:-:8443}"
export MCPGW_TLS_CERT="${MCPGW_TLS_CERT:-$CERT_DIR/server.crt}"
export MCPGW_TLS_KEY="${MCPGW_TLS_KEY:-$CERT_DIR/server.key}"
export MCPGW_TLS_CA="${MCPGW_TLS_CA:-$CERT_DIR/ca.crt}"
export MCPGW_LOG_FORMAT="${MCPGW_LOG_FORMAT:-text}"
export MCPGW_LOG_LEVEL="${MCPGW_LOG_LEVEL:-debug}"
export MCPGW_METRICS_ADDR="${MCPGW_METRICS_ADDR:-:9090}"
export MCPGW_CRUVERO_ENABLED="false"
export MCPGW_CORS_ENABLED="false"
export MCPGW_GATEWAY_ID="${MCPGW_GATEWAY_ID:-local-dev}"
export MCPGW_RATE_LIMIT_BACKEND="${MCPGW_RATE_LIMIT_BACKEND:-memory}"
export MCPGW_SPIFFE_ALLOW_PREFIX="${MCPGW_SPIFFE_ALLOW_PREFIX:-spiffe://mcpgw.local/ns/dev/sa/}"
export MCPGW_OIDC_ISSUER="${MCPGW_OIDC_ISSUER:-https://keycloak.localhost:8444/realms/mcpgw}"
export MCPGW_OIDC_AUDIENCE="${MCPGW_OIDC_AUDIENCE:-mcpgw-gateway}"
export MCPGW_ADMIN_ENABLED="true"
export MCPGW_ADMIN_MODE="standalone"
export MCPGW_ADMIN_DEV_MODE="false"
export MCPGW_ADMIN_OIDC_CLIENT_ID="${MCPGW_ADMIN_OIDC_CLIENT_ID:-mcpgw-admin}"
export MCPGW_ADMIN_REQUIRED_SCOPE="${MCPGW_ADMIN_REQUIRED_SCOPE:-admin}"
export MCPGW_ADMIN_EXTERNAL_URL="${MCPGW_ADMIN_EXTERNAL_URL:-https://gateway.localhost:8443}"
export MCPGW_DEVICE_FLOW_ENABLED="true"
export MCPGW_DEVICE_FLOW_CLIENT_ID="${MCPGW_DEVICE_FLOW_CLIENT_ID:-mcpgw-cli}"
export MCPGW_DEVICE_FLOW_IDP_DEVICE_URL="${MCPGW_DEVICE_FLOW_IDP_DEVICE_URL:-https://keycloak.localhost:8444/realms/mcpgw/protocol/openid-connect/auth/device}"
export MCPGW_DEVICE_FLOW_IDP_TOKEN_URL="${MCPGW_DEVICE_FLOW_IDP_TOKEN_URL:-https://keycloak.localhost:8444/realms/mcpgw/protocol/openid-connect/token}"

# Phase 19: Per-Server Endpoints, Capability Refresh, Scoped Keys, Routing, Namespacing
export MCPGW_PER_SERVER_ENDPOINTS="${MCPGW_PER_SERVER_ENDPOINTS:-true}"
export MCPGW_WELL_KNOWN_ENABLED="${MCPGW_WELL_KNOWN_ENABLED:-true}"
export MCPGW_WELL_KNOWN_CACHE_TTL="${MCPGW_WELL_KNOWN_CACHE_TTL:-5s}"
export MCPGW_CAPABILITY_REFRESH_ENABLED="${MCPGW_CAPABILITY_REFRESH_ENABLED:-true}"
export MCPGW_CAPABILITY_PUSH_ENABLED="${MCPGW_CAPABILITY_PUSH_ENABLED:-true}"
export MCPGW_EMBEDDING_CACHE_MAX_SIZE="${MCPGW_EMBEDDING_CACHE_MAX_SIZE:-10000}"
export MCPGW_SERVER_SCOPE_ENFORCEMENT="${MCPGW_SERVER_SCOPE_ENFORCEMENT:-true}"
export MCPGW_DEFAULT_ROUTING_STRATEGY="${MCPGW_DEFAULT_ROUTING_STRATEGY:-round_robin}"
export MCPGW_TOOL_NAMESPACE_MODE="${MCPGW_TOOL_NAMESPACE_MODE:-reject}"
export MCPGW_NAMESPACE_SEPARATOR="${MCPGW_NAMESPACE_SEPARATOR:-.}"
export MCPGW_GATEWAY_BASE_URL="${MCPGW_GATEWAY_BASE_URL:-https://gateway.localhost:8443}"

if [ -z "${MCPGW_ADMIN_SESSION_KEY:-}" ]; then
  export MCPGW_ADMIN_SESSION_KEY
  MCPGW_ADMIN_SESSION_KEY="$(openssl rand -hex 32)"
fi

if [ ! -x "$PROJECT_DIR/bin/mcpgw" ]; then
  (cd "$PROJECT_DIR" && go build -o ./bin/mcpgw ./cmd/mcpgw)
fi

exec "$PROJECT_DIR/bin/mcpgw" serve
