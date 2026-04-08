# Local Dev Container Stack

The default local development path runs the gateway in the repo devcontainer and keeps every supporting dependency local:

- PostgreSQL for gateway persistence
- Keycloak for standalone admin OIDC and device-code auth
- Shared local CA and service certificates
- A mock MCP backend that auto-registers and heartbeats

## URLs

- Gateway: `https://gateway.localhost:8443`
- Keycloak: `https://keycloak.localhost:8444`

## Seeded Credentials

- Keycloak admin bootstrap: `admin` / `admin`
- Gateway admin user: `dev-admin` / `dev-admin`
- Non-admin user: `dev-user` / `dev-user`

## Start The Stack

1. Open the repository in the devcontainer.
2. Wait for `scripts/dev-setup.sh` to finish.
3. Start the standalone gateway inside the devcontainer:

```bash
./scripts/dev-run.sh
```

The mock backend starts with Compose and will register itself once the gateway is healthy.

## Validate

Run the smoke check inside the devcontainer:

```bash
./scripts/dev-smoke.sh
```

That check validates:

- gateway health
- mock backend registration
- API key creation
- MCP session initialization
- `tools/list`
- `tools/call` through the gateway

## Admin And Device Flow

- Open `https://gateway.localhost:8443/admin`
- Sign in as `dev-admin`
- Use `./bin/mcpgw auth login --gateway-url https://gateway.localhost:8443` for the device-code flow

## Trust Note

The devcontainer trusts the generated local CA automatically. Your host browser does not. To avoid browser certificate warnings for `gateway.localhost` and `keycloak.localhost`, trust the generated CA certificate from the shared `certs` volume before using the admin UI locally.
