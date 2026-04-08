#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

export MCPGW_DB_URL="${MCPGW_DB_URL:-postgres://mcpgw:mcpgw@postgres:5432/mcpgw?sslmode=disable}"
GATEWAY_URL="${MCPGW_GATEWAY_URL:-https://gateway.localhost:8443}"
MCP_URL="$GATEWAY_URL/mcp"

echo "--- Health check ---"
"$PROJECT_DIR/bin/mcpgw" health --url "$GATEWAY_URL"

echo ""
echo "--- Registered servers ---"
servers_json="$("$PROJECT_DIR/bin/mcpgw" server list --status active --format json)"
printf '%s\n' "$servers_json"
if ! grep -q '"name": "mock-backend"' <<<"$servers_json"; then
  echo "mock-backend is not active" >&2
  exit 1
fi

echo ""
echo "--- Creating API key ---"
apikey_output="$("$PROJECT_DIR/bin/mcpgw" apikey create --name dev-smoke --client-id dev-smoke --expires 1h)"
printf '%s\n' "$apikey_output"
api_key="$(awk -F': ' '/^API Key:/ {print $2}' <<<"$apikey_output")"
if [ -z "$api_key" ]; then
  echo "failed to parse API key output" >&2
  exit 1
fi

echo ""
echo "--- tools/list ---"
headers_file="$(mktemp)"
body_file="$(mktemp)"
trap 'rm -f "$headers_file" "$body_file"' EXIT

curl --silent --show-error --fail \
  --dump-header "$headers_file" \
  --output "$body_file" \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --data '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"dev-smoke","version":"1.0.0"}}}' \
  "$MCP_URL"

session_id="$(awk -F': ' 'tolower($1)=="mcp-session-id" {gsub("\r", "", $2); print $2}' "$headers_file")"
initialize_response="$(cat "$body_file")"
printf '%s\n' "$initialize_response"
if [ -z "$session_id" ]; then
  echo "initialize response did not include an MCP session ID" >&2
  exit 1
fi

curl --silent --show-error --fail \
  --output /dev/null \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --header "Mcp-Session-Id: $session_id" \
  --data '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  "$MCP_URL"

tools_response="$(curl --silent --show-error --fail \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --header "Mcp-Session-Id: $session_id" \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  "$MCP_URL")"
printf '%s\n' "$tools_response"
if ! grep -q 'mock-backend.tool.echo' <<<"$tools_response"; then
  echo "mock-backend.tool.echo missing from tools/list response" >&2
  exit 1
fi

echo ""
echo "--- tools/call ---"
call_response="$(curl --silent --show-error --fail \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --header "Mcp-Session-Id: $session_id" \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mock-backend.tool.echo","arguments":{"message":"hello"}}}' \
  "$MCP_URL")"
printf '%s\n' "$call_response"
if ! grep -q 'echo-response' <<<"$call_response"; then
  echo "mock-backend.tool.echo did not return the expected payload" >&2
  exit 1
fi

echo ""
echo "=== Phase 19 Feature Validation ==="

echo ""
echo "--- [US2] Discovery document ---"
discovery_response="$(curl --silent --show-error --fail \
  --header "X-API-Key: $api_key" \
  "$GATEWAY_URL/.well-known/mcp.json")"
printf '%s\n' "$discovery_response"
if ! grep -q 'mcpServers' <<<"$discovery_response"; then
  echo "discovery document missing mcpServers field" >&2
  exit 1
fi
if ! grep -q 'mock-backend' <<<"$discovery_response"; then
  echo "discovery document missing mock-backend server" >&2
  exit 1
fi

echo ""
echo "--- [US2] Server listing ---"
server_list="$(curl --silent --show-error --fail \
  --header "X-API-Key: $api_key" \
  "$GATEWAY_URL/mcp/servers")"
printf '%s\n' "$server_list"
if ! grep -q '"tool_count"' <<<"$server_list"; then
  echo "server listing missing tool_count field" >&2
  exit 1
fi

echo ""
echo "--- [US1] Per-server endpoint: tools/list ---"
per_server_url="$GATEWAY_URL/mcp/servers/mock-backend/"
ps_init_headers="$(mktemp)"
curl --silent --show-error --fail \
  --dump-header "$ps_init_headers" \
  --output /dev/null \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --data '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"per-server-smoke","version":"1.0.0"}}}' \
  "$per_server_url"
ps_session_id="$(awk -F': ' 'tolower($1)=="mcp-session-id" {gsub("\r", "", $2); print $2}' "$ps_init_headers")"
rm -f "$ps_init_headers"

if [ -z "$ps_session_id" ]; then
  echo "per-server endpoint did not return MCP session ID" >&2
  exit 1
fi

curl --silent --show-error --fail \
  --output /dev/null \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --header "Mcp-Session-Id: $ps_session_id" \
  --data '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
  "$per_server_url"

ps_tools="$(curl --silent --show-error --fail \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --header "Mcp-Session-Id: $ps_session_id" \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  "$per_server_url")"
printf '%s\n' "$ps_tools"
if ! grep -q 'tool.echo' <<<"$ps_tools"; then
  echo "per-server tools/list missing tool.echo" >&2
  exit 1
fi

echo ""
echo "--- [US1] Per-server endpoint: tools/call ---"
ps_call="$(curl --silent --show-error --fail \
  --header "Content-Type: application/json" \
  --header "X-API-Key: $api_key" \
  --header "Mcp-Session-Id: $ps_session_id" \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tool.echo","arguments":{"message":"per-server-hello"}}}' \
  "$per_server_url")"
printf '%s\n' "$ps_call"
if ! grep -q 'echo-response' <<<"$ps_call"; then
  echo "per-server tools/call did not return expected payload" >&2
  exit 1
fi

echo ""
echo "--- [US1] Per-server endpoint: 404 for unknown server ---"
not_found_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
  --header "X-API-Key: $api_key" \
  --header "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"smoke","version":"1.0.0"}}}' \
  "$GATEWAY_URL/mcp/servers/nonexistent/")"
if [ "$not_found_status" != "404" ]; then
  echo "expected 404 for unknown server, got $not_found_status" >&2
  exit 1
fi
echo "Correctly returned 404 for unknown server"

echo ""
echo "--- [US5] Server-scoped API key ---"
scoped_key_output="$("$PROJECT_DIR/bin/mcpgw" apikey create \
  --name scoped-smoke --client-id scoped-smoke --expires 1h \
  --server-scope mock-backend)"
scoped_key="$(awk -F': ' '/^API Key:/ {print $2}' <<<"$scoped_key_output")"
if [ -z "$scoped_key" ]; then
  echo "failed to create scoped API key" >&2
  exit 1
fi
echo "Created scoped key restricted to mock-backend"

scoped_discovery="$(curl --silent --show-error --fail \
  --header "X-API-Key: $scoped_key" \
  "$GATEWAY_URL/.well-known/mcp.json")"
if ! grep -q 'mock-backend' <<<"$scoped_discovery"; then
  echo "scoped key could not access discovery document" >&2
  exit 1
fi
echo "Scoped key can access discovery document"

echo ""
echo "=== All Phase 19 smoke tests passed ==="
