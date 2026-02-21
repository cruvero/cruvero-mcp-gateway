# Phase 18A Implementation Prompts

## Prompt 1 of 2: Certificate Generation Script

### Required Reading (read these files before writing code)

- `docs/phases/PHASE18A.md` (full scope)
- `docker-compose.yml` (lines 84-139: `certs` service with OpenSSL commands)
- `scripts/` directory listing (existing scripts for naming conventions)

### Task

Extract the certificate generation logic from docker-compose.yml into a standalone script.

1. **Create `scripts/gen-dev-certs.sh`:**

   ```bash
   #!/usr/bin/env bash
   set -euo pipefail

   # gen-dev-certs.sh — Generate development TLS certificates for mcpgw.
   #
   # Usage:
   #   scripts/gen-dev-certs.sh [--force]
   #
   # Environment:
   #   CERT_DIR  Output directory (default: ./certs)

   CERT_DIR="${CERT_DIR:-./certs}"
   FORCE=false

   for arg in "$@"; do
       case "$arg" in
           --force) FORCE=true ;;
           *) echo "Unknown argument: $arg"; exit 1 ;;
       esac
   done

   if ! command -v openssl &>/dev/null; then
       echo "Error: openssl is required but not found in PATH"
       exit 1
   fi

   if [ -f "${CERT_DIR}/ca.crt" ] && [ "$FORCE" = false ]; then
       echo "Certificates already exist in ${CERT_DIR}. Use --force to regenerate."
       exit 0
   fi

   mkdir -p "${CERT_DIR}"
   TMPDIR=$(mktemp -d)
   trap 'rm -rf "${TMPDIR}"' EXIT

   echo "Generating CA..."
   openssl genrsa -out "${CERT_DIR}/ca.key" 4096
   openssl req -new -x509 -key "${CERT_DIR}/ca.key" -sha256 \
       -subj "/CN=mcpgw-dev-ca" -days 3650 -out "${CERT_DIR}/ca.crt"

   echo "Generating server certificate..."
   openssl genrsa -out "${CERT_DIR}/server.key" 2048
   cat > "${TMPDIR}/server-ext.cnf" <<'SANCFG'
   [req]
   distinguished_name = req_dn
   req_extensions = v3_req
   prompt = no
   [req_dn]
   CN = gateway
   [v3_req]
   subjectAltName = DNS:localhost,DNS:gateway,DNS:*.mcpgw.local,IP:127.0.0.1
   SANCFG
   openssl req -new -key "${CERT_DIR}/server.key" -out "${TMPDIR}/server.csr" \
       -config "${TMPDIR}/server-ext.cnf"
   openssl x509 -req -in "${TMPDIR}/server.csr" \
       -CA "${CERT_DIR}/ca.crt" -CAkey "${CERT_DIR}/ca.key" \
       -CAcreateserial -out "${CERT_DIR}/server.crt" -days 3650 -sha256 \
       -extensions v3_req -extfile "${TMPDIR}/server-ext.cnf"

   echo "Generating client certificate..."
   openssl genrsa -out "${CERT_DIR}/client.key" 2048
   cat > "${TMPDIR}/client-ext.cnf" <<'CLIENTCFG'
   [req]
   distinguished_name = req_dn
   req_extensions = v3_req
   prompt = no
   [req_dn]
   CN = test-client
   [v3_req]
   subjectAltName = URI:spiffe://mcpgw.local/ns/dev/sa/test-client
   CLIENTCFG
   openssl req -new -key "${CERT_DIR}/client.key" -out "${TMPDIR}/client.csr" \
       -config "${TMPDIR}/client-ext.cnf"
   openssl x509 -req -in "${TMPDIR}/client.csr" \
       -CA "${CERT_DIR}/ca.crt" -CAkey "${CERT_DIR}/ca.key" \
       -CAcreateserial -out "${CERT_DIR}/client.crt" -days 3650 -sha256 \
       -extensions v3_req -extfile "${TMPDIR}/client-ext.cnf"

   chmod 644 "${CERT_DIR}"/*.crt "${CERT_DIR}"/*.key

   echo ""
   echo "Certificate generation complete. Files in ${CERT_DIR}/:"
   ls -la "${CERT_DIR}"/*.crt "${CERT_DIR}"/*.key
   ```

2. **Important: heredoc formatting** — The script above is indented for markdown readability. In the actual script file, ensure:
   - Heredoc content (`[req]`, `distinguished_name`, etc.) has **no leading whitespace** (OpenSSL config files are whitespace-sensitive)
   - Heredoc closing markers (`SANCFG`, `CLIENTCFG`) are at **column 0** (bash requires this for `<<'MARKER'` heredocs)

3. **Make the script executable:**
   ```bash
   chmod +x scripts/gen-dev-certs.sh
   ```

4. **Ensure `certs/` is in `.gitignore`** — generated certificates should never be committed.

### Verification

```bash
# Run the script
CERT_DIR=/tmp/test-certs scripts/gen-dev-certs.sh

# Verify CA
openssl x509 -in /tmp/test-certs/ca.crt -noout -subject  # CN=mcpgw-dev-ca

# Verify server cert SANs
openssl x509 -in /tmp/test-certs/server.crt -noout -ext subjectAltName
# Should show: DNS:localhost, DNS:gateway, DNS:*.mcpgw.local, IP:127.0.0.1

# Verify client cert SAN (SPIFFE)
openssl x509 -in /tmp/test-certs/client.crt -noout -ext subjectAltName
# Should show: URI:spiffe://mcpgw.local/ns/dev/sa/test-client

# Verify chain
openssl verify -CAfile /tmp/test-certs/ca.crt /tmp/test-certs/server.crt
openssl verify -CAfile /tmp/test-certs/ca.crt /tmp/test-certs/client.crt

# Test idempotency
CERT_DIR=/tmp/test-certs scripts/gen-dev-certs.sh  # Should skip

# Test --force
CERT_DIR=/tmp/test-certs scripts/gen-dev-certs.sh --force  # Should regenerate

# Cleanup
rm -rf /tmp/test-certs
```

### Acceptance Criteria

- Script generates 6 files: ca.key, ca.crt, server.key, server.crt, client.key, client.crt
- CA is self-signed with CN=mcpgw-dev-ca
- Server cert has DNS:localhost, DNS:gateway, DNS:*.mcpgw.local, IP:127.0.0.1 SANs
- Client cert has SPIFFE URI SAN
- All certs verify against the CA
- Script is idempotent (skips if certs exist)
- `--force` flag regenerates even if certs exist
- Temp files cleaned up on exit (trap)
- Script requires `openssl` and exits with clear error if missing

---

## Prompt 2 of 2: Dev Environment Setup Script

### Required Reading (read these files before writing code)

- `docs/phases/PHASE18A.md` (dev-setup.sh section)
- `scripts/gen-dev-certs.sh` (just created in Prompt 1)
- `docker-compose.yml` (environment variables for the devcontainer service, lines 10-21)
- `cmd/mcpgw/` (CLI entry point — `mcpgw migrate` subcommand)

### Task

Create a full environment bootstrap script for local development.

1. **Create `scripts/dev-setup.sh`:**

   ```bash
   #!/usr/bin/env bash
   set -euo pipefail

   # dev-setup.sh — Bootstrap a local development environment for mcpgw.
   #
   # Generates certificates, waits for Postgres, runs migrations, and builds
   # the gateway binary. Safe to re-run at any time.
   #
   # Environment:
   #   MCPGW_DB_URL          Postgres connection string (default: see below)
   #   CERT_DIR              Certificate output directory (default: ./certs)
   #   MCPGW_LISTEN_ADDR     Gateway listen address (default: :8443)

   SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
   PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

   MCPGW_DB_URL="${MCPGW_DB_URL:-postgres://mcpgw:mcpgw@localhost:5432/mcpgw?sslmode=disable}"
   CERT_DIR="${CERT_DIR:-${PROJECT_DIR}/certs}"
   export CERT_DIR

   GREEN='\033[0;32m'
   RED='\033[0;31m'
   NC='\033[0m'

   step() { echo -e "${GREEN}==>${NC} $1"; }
   fail() { echo -e "${RED}Error:${NC} $1" >&2; exit 1; }

   # Step 1: Generate certificates
   step "Generating development certificates..."
   "${SCRIPT_DIR}/gen-dev-certs.sh"

   # Step 2: Wait for Postgres
   step "Waiting for Postgres..."
   MAX_WAIT=30
   WAITED=0
   # Extract host and port from DB URL for pg_isready
   DB_HOST=$(echo "${MCPGW_DB_URL}" | sed -E 's|.*@([^:/]+).*|\1|')
   DB_PORT=$(echo "${MCPGW_DB_URL}" | sed -E 's|.*:([0-9]+)/.*|\1|')
   DB_PORT="${DB_PORT:-5432}"

   if command -v pg_isready &>/dev/null; then
       while ! pg_isready -h "${DB_HOST}" -p "${DB_PORT}" -q 2>/dev/null; do
           WAITED=$((WAITED + 1))
           if [ "${WAITED}" -ge "${MAX_WAIT}" ]; then
               fail "Postgres not ready after ${MAX_WAIT}s at ${DB_HOST}:${DB_PORT}"
           fi
           sleep 1
       done
       echo "  Postgres is ready."
   else
       echo "  pg_isready not found; skipping readiness check."
   fi

   # Step 3: Run migrations
   step "Running database migrations..."
   cd "${PROJECT_DIR}"
   go run ./cmd/mcpgw migrate --direction up 2>&1 || fail "Migration failed"
   echo "  Migrations complete."

   # Step 4: Generate session key (if not set)
   if [ -z "${MCPGW_ADMIN_SESSION_KEY:-}" ]; then
       step "Generating admin session key..."
       MCPGW_ADMIN_SESSION_KEY=$(openssl rand -hex 32)
       echo "  Generated. Export before starting the gateway:"
       echo "  export MCPGW_ADMIN_SESSION_KEY=${MCPGW_ADMIN_SESSION_KEY}"
   fi

   # Step 5: Build gateway
   step "Building gateway binary..."
   go build -o "${PROJECT_DIR}/bin/mcpgw" ./cmd/mcpgw
   echo "  Binary: ${PROJECT_DIR}/bin/mcpgw"

   # Step 6: Print environment summary
   echo ""
   step "Development environment ready."
   echo ""
   echo "  Start the gateway:"
   echo "    export MCPGW_DB_URL=\"${MCPGW_DB_URL}\""
   echo "    export MCPGW_TLS_CERT=\"${CERT_DIR}/server.crt\""
   echo "    export MCPGW_TLS_KEY=\"${CERT_DIR}/server.key\""
   echo "    export MCPGW_TLS_CA=\"${CERT_DIR}/ca.crt\""
   echo "    export MCPGW_ADMIN_DEV_MODE=true"
   echo "    ./bin/mcpgw serve"
   ```

2. **Make the script executable:**
   ```bash
   chmod +x scripts/dev-setup.sh
   ```

3. **Add `bin/` to `.gitignore`** if not already present — built binaries should not be committed.

### Verification

```bash
# Check script syntax
bash -n scripts/dev-setup.sh

# Verify both scripts are executable
ls -la scripts/gen-dev-certs.sh scripts/dev-setup.sh | grep -c 'x'  # Both should have execute bit

# Dry run (will fail at Postgres step if DB not running, but cert generation should work)
CERT_DIR=/tmp/dev-certs scripts/gen-dev-certs.sh
```

### Acceptance Criteria

- Script generates certs, waits for Postgres, runs migrations, generates session key, and builds binary
- Each step is idempotent — safe to re-run
- Step output uses colored markers for readability
- Postgres wait has a timeout (30s default) with clear error message
- Missing `pg_isready` is handled gracefully (skipped with warning)
- Session key is only generated if `MCPGW_ADMIN_SESSION_KEY` is not already set
- Final output shows all necessary environment variables to start the gateway
- Script uses project-relative paths (works from any working directory)
