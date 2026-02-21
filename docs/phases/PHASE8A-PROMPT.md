# Phase 8A Implementation Prompts

## Prompt 1 of 3: CLI Framework & Serve Command

### Required Reading (read these files before writing code)
- docs/phases/PHASE8A.md
- internal/config/config.go
- internal/server/server.go
- internal/store/interfaces.go

### Task

Set up the CLI framework and implement the serve command.

1. `cmd/mcpgw/main.go`:
   - Define version, commit, buildDate variables (set via ldflags)
   - Parse root command: check os.Args for subcommand
   - Route to subcommand handlers: serve, server, apikey, policy, health, migrate
   - --version flag: print version info and exit
   - Unknown command: print usage and exit with error

2. `cmd/mcpgw/serve.go`:
   - serveCommand() function
   - Load config via config.Load()
   - Initialize slog logger based on config
   - Connect to Postgres
   - Create all stores (ServerStore, APIKeyStore, AuditStore)
   - Create registration service, policy engine, rate limiter, proxy
   - Optionally create NATS client and event publisher (if MCPGW_NATS_URL set)
   - Create and configure HTTP server with all middleware and routes
   - Start metrics server on separate port
   - Start background sweeper
   - Block on signal (SIGINT, SIGTERM)
   - Graceful shutdown: stop sweeper, close NATS, close DB, stop servers

3. Test:
   - Test main routes to correct subcommand
   - Test serve initializes and shuts down cleanly (use short context timeout)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- `mcpgw serve` starts the full gateway
- Graceful shutdown on signal
- Version info printed with --version

---

## Prompt 2 of 3: Server & API Key Management Commands

### Required Reading (read these files before writing code)
- docs/phases/PHASE8A.md
- cmd/mcpgw/main.go
- internal/store/interfaces.go
- internal/auth/apikey.go

### Task

Implement server management and API key CLI commands.

1. `cmd/mcpgw/server.go`:
   - serverCommand(args []string): route to list/inspect/deregister
   - serverListCommand(): connect to DB, list servers, format as table or JSON
   - serverInspectCommand(id string): fetch and display detailed server info
   - serverDeregisterCommand(id string): confirm and delete
   - Flag parsing: --status, --format, --force
   - Table output: aligned columns using tabwriter

2. `cmd/mcpgw/apikey.go`:
   - apikeyCommand(args []string): route to create/list/revoke
   - apikeyCreateCommand(): generate key, store hash, display plaintext
   - apikeyListCommand(): list keys (never show hash)
   - apikeyRevokeCommand(id string): confirm and revoke
   - Flag parsing: --name, --scopes, --expires, --client-id, --force

3. Tests:
   - Test server list output formatting
   - Test apikey create generates valid key format
   - Test flag parsing for all commands

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Server commands display correctly formatted output
- API key creation shows plaintext once
- API key list never shows hash
- Confirmation prompts work (and --force skips them)

---

## Prompt 3 of 3: Policy, Health, Migrate Commands

### Required Reading (read these files before writing code)
- docs/phases/PHASE8A.md
- cmd/mcpgw/main.go
- internal/types/types.go (PolicyProfile)
- internal/store/interfaces.go

### Task

Implement policy, health, and migrate CLI commands.

1. `cmd/mcpgw/policy.go`:
   - policyCommand(args []string): route to list/set/reset
   - policyListCommand(): list profiles from DB, display as table
   - policySetCommand(name string): parse flags, update profile in DB
   - policyResetCommand(name string): reset to defaults
   - Flag parsing: --rate-limit, --rate-burst, --enforcement-mode, --tool-allowlist, --tool-denylist

2. `cmd/mcpgw/health.go`:
   - healthCommand(): connect to gateway health endpoints
   - Flags: --url (gateway URL, default https://localhost:8443)
   - Fetch /healthz and /readyz
   - Display: status, NATS connectivity, registered server count, version
   - Exit 0 if healthy, 1 if not

3. `cmd/mcpgw/migrate.go`:
   - migrateCommand(): run database migrations
   - Flags: --direction (up/down), --steps (int), --db-url (override)
   - Use golang-migrate with file:// source for migrations directory
   - Display: current version, applied migrations
   - Handle "no change" gracefully (not an error)

4. Tests:
   - Test policy list output
   - Test health with mock HTTP server (httptest)
   - Test migrate up and down

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All commands work with correct flag parsing
- Health check returns proper exit codes
- Migrate handles all states (fresh, partial, current)
- >=80% coverage for cmd/mcpgw package
