# Phase 18A: Dev Scripts — Certificate Generation & Environment Setup

## Overview

Extract the certificate generation logic from the `docker-compose.yml` `certs` service into a standalone shell script, and create a full environment bootstrap script for local development.

## Scope

### Certificate Generation Script (`scripts/gen-dev-certs.sh`)

Extract the OpenSSL commands from the `docker-compose.yml` `certs` service (lines 84-136) into a standalone script that can run on the host machine without Docker.

**Requirements:**
- Configurable output directory via `$CERT_DIR` environment variable (default: `./certs`)
- Idempotent: skip generation if `$CERT_DIR/ca.crt` already exists (with `--force` flag to regenerate)
- Generate three certificate sets:

**CA Certificate:**
- 4096-bit RSA key
- Self-signed X.509, SHA-256, 3650 days validity
- Subject: `CN=mcpgw-dev-ca`

**Server Certificate:**
- 2048-bit RSA key
- SANs: `DNS:localhost`, `DNS:gateway`, `DNS:*.mcpgw.local`, `IP:127.0.0.1`
- Signed by the CA, 3650 days validity

**Client Certificate:**
- 2048-bit RSA key
- SAN: `URI:spiffe://mcpgw.local/ns/dev/sa/test-client`
- Signed by the CA, 3650 days validity

**Output files:** `ca.key`, `ca.crt`, `server.key`, `server.crt`, `client.key`, `client.crt`
- Set permissions: `644` for all certs and keys (local dev only)
- Print summary of generated files with paths

**Script conventions:**
- `#!/usr/bin/env bash` with `set -euo pipefail`
- Require `openssl` in `$PATH`, exit with helpful message if missing
- Use temp files for CSR/extension configs, clean up on exit via `trap`

### Environment Setup Script (`scripts/dev-setup.sh`)

Create a bootstrap script that prepares a complete local development environment:

**Steps:**
1. **Generate certificates**: call `scripts/gen-dev-certs.sh` (skip if already exist)
2. **Wait for Postgres**: poll `pg_isready` until the database is available (with timeout)
3. **Run migrations**: execute `go run ./cmd/mcpgw migrate --direction up`
4. **Generate session key**: create a random 32-byte hex string for `MCPGW_ADMIN_SESSION_KEY` if not set
5. **Build gateway**: run `go build -o ./bin/mcpgw ./cmd/mcpgw`
6. **Print environment**: display the env vars needed to start the gateway

**Configuration via environment variables:**
- `MCPGW_DB_URL` (default: `postgres://mcpgw:mcpgw@localhost:5432/mcpgw?sslmode=disable`)
- `CERT_DIR` (default: `./certs`)
- `MCPGW_LISTEN_ADDR` (default: `:8443`)

**Script conventions:**
- `#!/usr/bin/env bash` with `set -euo pipefail`
- Colored output for step progress (green checkmarks, red errors)
- Each step idempotent — safe to re-run
- Exit with clear error message if any step fails

## Files Created or Modified

| File | Description |
|------|-------------|
| `scripts/gen-dev-certs.sh` | Standalone TLS certificate generation script |
| `scripts/dev-setup.sh` | Full local environment bootstrap script |

## Testing Requirements

- `scripts/gen-dev-certs.sh` generates all 6 files in the target directory
- Running the script twice (idempotent) skips generation on second run
- `--force` flag regenerates certificates even if they exist
- Generated certificates are valid: `openssl verify -CAfile ca.crt server.crt` succeeds
- Client cert has the correct SPIFFE SAN
- Server cert has the correct DNS/IP SANs
- `scripts/dev-setup.sh` completes without error in a devcontainer environment
- Both scripts are executable (`chmod +x`)
