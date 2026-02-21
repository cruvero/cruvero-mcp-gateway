#!/usr/bin/env bash
set -euo pipefail

CERT_DIR="${CERT_DIR:-./certs}"
FORCE=false

for arg in "$@"; do
  case "$arg" in
    --force) FORCE=true ;;
    *) echo "Usage: gen-dev-certs.sh [--force]" >&2; exit 1 ;;
  esac
done

if [ "$FORCE" = false ] && [ -f "$CERT_DIR/ca.crt" ]; then
  echo "Certificates already exist in $CERT_DIR (use --force to regenerate)."
  exit 0
fi

mkdir -p "$CERT_DIR"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Generating CA (4096-bit)..."
openssl genrsa -out "$CERT_DIR/ca.key" 4096 2>/dev/null
openssl req -new -x509 -key "$CERT_DIR/ca.key" -sha256 \
  -subj "/CN=mcpgw-dev-ca" -days 3650 -out "$CERT_DIR/ca.crt"

echo "Generating server certificate (2048-bit)..."
openssl genrsa -out "$CERT_DIR/server.key" 2048 2>/dev/null
cat > "$TMPDIR/server-ext.cnf" <<'SANCFG'
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no
[req_dn]
CN = gateway
[v3_req]
subjectAltName = DNS:localhost,DNS:gateway,DNS:*.mcpgw.local,IP:127.0.0.1
SANCFG
openssl req -new -key "$CERT_DIR/server.key" -out "$TMPDIR/server.csr" \
  -config "$TMPDIR/server-ext.cnf"
openssl x509 -req -in "$TMPDIR/server.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" \
  -CAcreateserial -out "$CERT_DIR/server.crt" -days 3650 -sha256 \
  -extensions v3_req -extfile "$TMPDIR/server-ext.cnf"

echo "Generating client certificate (2048-bit, SPIFFE SAN)..."
openssl genrsa -out "$CERT_DIR/client.key" 2048 2>/dev/null
cat > "$TMPDIR/client-ext.cnf" <<'CLIENTCFG'
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no
[req_dn]
CN = test-client
[v3_req]
subjectAltName = URI:spiffe://mcpgw.local/ns/dev/sa/test-client
CLIENTCFG
openssl req -new -key "$CERT_DIR/client.key" -out "$TMPDIR/client.csr" \
  -config "$TMPDIR/client-ext.cnf"
openssl x509 -req -in "$TMPDIR/client.csr" -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" \
  -CAcreateserial -out "$CERT_DIR/client.crt" -days 3650 -sha256 \
  -extensions v3_req -extfile "$TMPDIR/client-ext.cnf"

chmod 644 "$CERT_DIR"/*.crt
chmod 600 "$CERT_DIR"/*.key

echo "Certificate generation complete in $CERT_DIR."
