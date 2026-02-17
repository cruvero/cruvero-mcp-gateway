package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/registration"
	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/golang-migrate/migrate/v4"
)

type trackingAPIKeyStore struct {
	keys      []types.APIKey
	revokedID string
}

func (s *trackingAPIKeyStore) Create(ctx context.Context, key *types.APIKey) error {
	_ = ctx
	_ = key
	return nil
}
func (s *trackingAPIKeyStore) GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error) {
	_ = ctx
	_ = lookupHash
	return nil, errors.New("sql: no rows in result set")
}
func (s *trackingAPIKeyStore) List(ctx context.Context) ([]types.APIKey, error) {
	_ = ctx
	return append([]types.APIKey(nil), s.keys...), nil
}
func (s *trackingAPIKeyStore) Revoke(ctx context.Context, id string) error {
	_ = ctx
	s.revokedID = id
	return nil
}
func (s *trackingAPIKeyStore) DeleteExpired(ctx context.Context) (int64, error) {
	_ = ctx
	return 0, nil
}

type stubRegistrationService struct {
	registerResp  *registration.RegistrationResponse
	heartbeatResp *registration.HeartbeatResponse
	listResp      []types.ServerRecord
	registerErr   error
	heartbeatErr  error
	listErr       error
	deregisterErr error
}

func (s *stubRegistrationService) Register(
	ctx context.Context,
	caller *identity.Identity,
	req registration.RegistrationRequest,
) (*registration.RegistrationResponse, error) {
	_ = ctx
	_ = caller
	_ = req
	return s.registerResp, s.registerErr
}

func (s *stubRegistrationService) Heartbeat(
	ctx context.Context,
	caller *identity.Identity,
	id string,
	req registration.HeartbeatRequest,
) (*registration.HeartbeatResponse, error) {
	_ = ctx
	_ = caller
	_ = id
	_ = req
	return s.heartbeatResp, s.heartbeatErr
}

func (s *stubRegistrationService) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	_ = ctx
	_ = filter
	return s.listResp, s.listErr
}

func (s *stubRegistrationService) Deregister(ctx context.Context, caller *identity.Identity, id string) error {
	_ = ctx
	_ = caller
	_ = id
	return s.deregisterErr
}

func TestAPIKeyListAndRevokeCommands(t *testing.T) {
	origLoad := loadAPIKeyStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadAPIKeyStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	expires := time.Now().UTC().Add(2 * time.Hour)
	store := &trackingAPIKeyStore{
		keys: []types.APIKey{
			{
				ID:            "k-1",
				Name:          "ci",
				ClientID:      "client-a",
				Scopes:        []string{"read"},
				ExpiresAt:     &expires,
				KeyLookupHash: "lookup",
				KeyBcryptHash: "bcrypt",
				CreatedAt:     time.Now().UTC(),
			},
		},
	}
	loadAPIKeyStoreFunc = func(ctx context.Context) (storepkg.APIKeyStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	tableOut := &bytes.Buffer{}
	stdout = tableOut
	stderr = &bytes.Buffer{}
	if err := apikeyListCommand([]string{"--format", "table"}); err != nil {
		t.Fatalf("apikey list table: %v", err)
	}
	if strings.Contains(tableOut.String(), "lookup") || strings.Contains(tableOut.String(), "bcrypt") {
		t.Fatalf("list output should not contain key hashes: %q", tableOut.String())
	}

	jsonOut := &bytes.Buffer{}
	stdout = jsonOut
	if err := apikeyListCommand([]string{"--format", "json"}); err != nil {
		t.Fatalf("apikey list json: %v", err)
	}
	if strings.Contains(jsonOut.String(), "key_lookup_hash") || strings.Contains(jsonOut.String(), "key_bcrypt_hash") {
		t.Fatalf("json list output should not contain hash fields: %q", jsonOut.String())
	}

	revokeOut := &bytes.Buffer{}
	stdout = revokeOut
	if err := apikeyRevokeCommand([]string{"--force", "k-1"}); err != nil {
		t.Fatalf("apikey revoke: %v", err)
	}
	if store.revokedID != "k-1" {
		t.Fatalf("expected revoked id k-1, got %q", store.revokedID)
	}
}

func TestServerInspectAndDeregisterByIDAndName(t *testing.T) {
	origLoad := loadServerStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadServerStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	store := &mockServerStore{
		records: []types.ServerRecord{{
			ID:       "srv-1",
			Name:     "alpha",
			Status:   types.StatusActive,
			SPIFFEID: "spiffe://example.org/ns/default/sa/alpha",
		}},
	}
	loadServerStoreFunc = func(ctx context.Context) (storepkg.ServerStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	inspectOut := &bytes.Buffer{}
	stdout = inspectOut
	stderr = &bytes.Buffer{}
	if err := serverInspectCommand([]string{"alpha"}); err != nil {
		t.Fatalf("server inspect by name: %v", err)
	}
	if !strings.Contains(inspectOut.String(), "\"id\": \"srv-1\"") {
		t.Fatalf("expected server details in inspect output, got %q", inspectOut.String())
	}

	deregOut := &bytes.Buffer{}
	stdout = deregOut
	if err := serverDeregisterCommand([]string{"--force", "srv-1"}); err != nil {
		t.Fatalf("server deregister by id: %v", err)
	}
	if !strings.Contains(deregOut.String(), "deregistered server") {
		t.Fatalf("expected deregister output, got %q", deregOut.String())
	}
}

func TestPolicySetAndResetCommands(t *testing.T) {
	origLoad := loadConfigStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadConfigStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	store := &mockConfigStore{}
	basePayload, err := json.Marshal(events.PolicyConfigMessage{
		Profiles: []types.PolicyProfile{
			{Name: "default", RateLimit: 10, RateBurst: 20, EnforcementMode: types.ModeEnforce},
			{Name: "premium", RateLimit: 50, RateBurst: 100, EnforcementMode: types.ModeEnforce},
		},
	})
	if err != nil {
		t.Fatalf("marshal base payload: %v", err)
	}
	store.values = map[string][]byte{configCachePolicyKey: basePayload}
	loadConfigStoreFunc = func(ctx context.Context) (events.ConfigStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	if err := policySetCommand([]string{
		"--rate-limit", "75",
		"--rate-burst", "150",
		"--enforcement-mode", "audit",
		"--tool-allowlist", "tool.safe",
		"--tool-denylist", "tool.blocked",
		"premium",
	}); err != nil {
		t.Fatalf("policy set: %v", err)
	}

	loaded, err := loadPolicyProfiles(context.Background(), store)
	if err != nil {
		t.Fatalf("load profiles after set: %v", err)
	}
	premium := loaded["premium"]
	if premium == nil || premium.RateLimit != 75 || premium.RateBurst != 150 || premium.EnforcementMode != types.ModeAudit {
		t.Fatalf("unexpected premium profile after set: %+v", premium)
	}

	if err := policyResetCommand([]string{"premium"}); err != nil {
		t.Fatalf("policy reset: %v", err)
	}
	loaded, err = loadPolicyProfiles(context.Background(), store)
	if err != nil {
		t.Fatalf("load profiles after reset: %v", err)
	}
	if loaded["premium"].RateLimit != 50 || loaded["premium"].RateBurst != 100 {
		t.Fatalf("expected premium defaults after reset, got %+v", loaded["premium"])
	}
}

func TestPolicyHelperBranches(t *testing.T) {
	t.Parallel()

	profiles, err := loadPolicyProfiles(context.Background(), nil)
	if err != nil {
		t.Fatalf("load profiles nil store: %v", err)
	}
	if profiles["default"] == nil {
		t.Fatal("expected default profile from nil store path")
	}

	badStore := &mockConfigStore{values: map[string][]byte{configCachePolicyKey: []byte("{not-json")}}
	if _, err := loadPolicyProfiles(context.Background(), badStore); err == nil {
		t.Fatal("expected decode error for malformed policy payload")
	}

	if err := persistPolicyProfiles(context.Background(), nil, map[string]*types.PolicyProfile{}); err == nil {
		t.Fatal("expected persist error for nil config store")
	}

	custom := resolveOrDefaultProfile("custom", map[string]*types.PolicyProfile{})
	if custom.Name != "custom" || custom.RateLimit != 10 || custom.RateBurst != 20 {
		t.Fatalf("unexpected custom default profile: %+v", custom)
	}
}

func TestDBHelpersAndLoadStoreHelpers(t *testing.T) {
	origIn := stdin
	origOut := stdout
	origErr := stderr
	origOpen := openDBFunc
	t.Cleanup(func() {
		stdin = origIn
		stdout = origOut
		stderr = origErr
		openDBFunc = origOpen
	})

	if ok, err := confirmAction("anything", true); err != nil || !ok {
		t.Fatalf("expected force confirmation to succeed, ok=%t err=%v", ok, err)
	}

	stdin = strings.NewReader("yes\n")
	stdout = &bytes.Buffer{}
	if ok, err := confirmAction("confirm", false); err != nil || !ok {
		t.Fatalf("expected yes confirmation to succeed, ok=%t err=%v", ok, err)
	}

	if !sqlNoRows(errors.New("SQL: no rows in result set")) {
		t.Fatal("expected sqlNoRows to match no rows error")
	}

	errBuf := &bytes.Buffer{}
	stderr = errBuf
	closeQuietly(func() error { return errors.New("close failed") })
	if !strings.Contains(errBuf.String(), "close resource failed") {
		t.Fatalf("expected close warning output, got %q", errBuf.String())
	}

	t.Setenv("MCPGW_DB_URL", "postgres://db")
	openDBFunc = func(ctx context.Context, dbURL string) (*sql.DB, error) {
		_ = ctx
		if dbURL == "" {
			t.Fatal("expected non-empty db url")
		}
		db, _, err := sqlmock.New()
		return db, err
	}

	serverStore, closeServer, err := loadServerStore(context.Background())
	if err != nil || serverStore == nil {
		t.Fatalf("load server store: store=%v err=%v", serverStore, err)
	}
	closeQuietly(closeServer)

	apiKeyStore, closeAPIKey, err := loadAPIKeyStore(context.Background())
	if err != nil || apiKeyStore == nil {
		t.Fatalf("load apikey store: store=%v err=%v", apiKeyStore, err)
	}
	closeQuietly(closeAPIKey)

	configStore, closeConfig, err := loadConfigStore(context.Background())
	if err != nil || configStore == nil {
		t.Fatalf("load config store: store=%v err=%v", configStore, err)
	}
	closeQuietly(closeConfig)
}

func TestMigrateAndServeHelpers(t *testing.T) {
	t.Parallel()

	if err := serveCommand([]string{"extra"}); err == nil {
		t.Fatal("expected serve positional argument validation error")
	}
	if err := serveWithContext(nil); err == nil {
		t.Fatal("expected serveWithContext nil context error")
	}

	if _, err := resolveMigrateDBURL("postgres://override"); err != nil {
		t.Fatalf("resolve migrate override: %v", err)
	}
	if err := applyMigrations(&mockMigrator{}, "sideways", 0); err == nil {
		t.Fatal("expected applyMigrations direction validation error")
	}
	if err := applyMigrations(&mockMigrator{}, "up", -1); err == nil {
		t.Fatal("expected applyMigrations negative steps error")
	}

	if validator, err := maybeBuildOIDCValidator(context.Background(), nil); err != nil || validator != nil {
		t.Fatalf("expected nil validator for nil config, validator=%v err=%v", validator, err)
	}
	if tlsCfg, err := buildProxyTLSConfig(nil); err != nil || tlsCfg != nil {
		t.Fatalf("expected nil tls config for nil config, tlsCfg=%v err=%v", tlsCfg, err)
	}

	if logger := newLogger("text", "warn"); logger == nil {
		t.Fatal("expected text logger")
	}
	if logger := newLogger("json", "error"); logger == nil {
		t.Fatal("expected json logger")
	}
}

func TestNewMigrateRunnerOpenDatabaseError(t *testing.T) {
	origOpen := openDBFunc
	t.Cleanup(func() {
		openDBFunc = origOpen
	})

	openDBFunc = func(ctx context.Context, dbURL string) (*sql.DB, error) {
		_ = ctx
		_ = dbURL
		return nil, errors.New("boom")
	}

	if _, err := newMigrateRunner("postgres://db"); err == nil {
		t.Fatal("expected newMigrateRunner error when open db fails")
	}
}

func TestNewMigrateRunnerDriverErrorWithNonPostgresConnection(t *testing.T) {
	origOpen := openDBFunc
	t.Cleanup(func() {
		openDBFunc = origOpen
	})

	openDBFunc = func(ctx context.Context, dbURL string) (*sql.DB, error) {
		_ = ctx
		_ = dbURL
		db, _, err := sqlmock.New()
		return db, err
	}

	if _, err := newMigrateRunner("postgres://db"); err == nil {
		t.Fatal("expected newMigrateRunner driver initialization error")
	}
}

func TestIndexedRegistrationServiceSyncAndDelegation(t *testing.T) {
	t.Parallel()

	index := registration.NewCapabilityIndex()
	serverStore := &mockServerStore{
		records: []types.ServerRecord{{
			ID:   "srv-1",
			Name: "alpha",
			Host: "127.0.0.1",
			Port: 8443,
			Capabilities: types.Capability{
				Tools: []string{"tool.alpha"},
			},
			Status: types.StatusActive,
		}},
	}
	base := &stubRegistrationService{
		registerResp: &registration.RegistrationResponse{
			InstanceID: "srv-1",
			Status:     types.StatusActive,
		},
		heartbeatResp: &registration.HeartbeatResponse{
			ServerStatus: types.StatusStale,
		},
		listResp: []types.ServerRecord{{ID: "srv-1", Name: "alpha"}},
	}
	svc := newIndexedRegistrationService(base, serverStore, index, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	resp, err := svc.Register(context.Background(), &identity.Identity{Type: identity.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/alpha"}, registration.RegistrationRequest{})
	if err != nil {
		t.Fatalf("register delegation: %v", err)
	}
	if resp.InstanceID != "srv-1" {
		t.Fatalf("unexpected register response: %+v", resp)
	}
	if len(index.LookupTool("tool.alpha")) == 0 {
		t.Fatal("expected tool to be indexed after routable register")
	}

	if _, err := svc.Heartbeat(context.Background(), &identity.Identity{Type: identity.IdentityMTLS, ID: "spiffe://example.org/ns/default/sa/alpha"}, "srv-1", registration.HeartbeatRequest{}); err != nil {
		t.Fatalf("heartbeat delegation: %v", err)
	}
	if len(index.LookupTool("tool.alpha")) != 0 {
		t.Fatal("expected stale heartbeat path to remove tool from index")
	}

	if _, err := svc.List(context.Background(), types.ServerFilter{}); err != nil {
		t.Fatalf("list delegation: %v", err)
	}
	if err := svc.Deregister(context.Background(), &identity.Identity{Type: identity.IdentityAPIKey, ID: "admin"}, "srv-1"); err != nil {
		t.Fatalf("deregister delegation: %v", err)
	}
}

func TestCommandSubcommandValidationErrors(t *testing.T) {
	t.Parallel()

	if err := policyCommand([]string{"unknown"}); err == nil {
		t.Fatal("expected unknown policy subcommand error")
	}
	if err := apikeyListCommand([]string{"--format", "yaml"}); err == nil {
		t.Fatal("expected invalid format error")
	}
	if _, err := parseExpiry("-1h"); err == nil {
		t.Fatal("expected negative duration expiry error")
	}
	if _, err := parseExtendedDuration("abc"); err == nil {
		t.Fatal("expected invalid extended duration parse error")
	}
	if _, err := parseServerStatus("unknown"); err == nil {
		t.Fatal("expected parseServerStatus validation error")
	}
	if err := migrateCommand([]string{"--direction", "up", "extra"}); err == nil {
		t.Fatal("expected migrate positional arg validation error")
	}
}

func TestMigrateErrNilVersionPath(t *testing.T) {
	origNewRunner := newMigrateRunnerFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		newMigrateRunnerFunc = origNewRunner
		stdout = origOut
		stderr = origErr
	})

	newMigrateRunnerFunc = func(dbURL string) (migrateRunner, error) {
		_ = dbURL
		return &mockMigratorErrNilVersion{}, nil
	}

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	if err := migrateCommand([]string{"--direction", "up", "--db-url", "postgres://override"}); err != nil {
		t.Fatalf("migrate nil version path: %v", err)
	}
}

type mockMigratorErrNilVersion struct{}

func (m *mockMigratorErrNilVersion) Up() error                      { return nil }
func (m *mockMigratorErrNilVersion) Down() error                    { return nil }
func (m *mockMigratorErrNilVersion) Steps(n int) error              { _ = n; return nil }
func (m *mockMigratorErrNilVersion) Version() (uint, bool, error)   { return 0, false, migrate.ErrNilVersion }
func (m *mockMigratorErrNilVersion) Close() (error, error)          { return nil, nil }

func TestMainFunctionVersionPath(t *testing.T) {
	if os.Getenv("MCPGW_TEST_MAIN_SUBPROCESS") == "1" {
		os.Args = []string{"mcpgw", "--version"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainFunctionVersionPath")
	cmd.Env = append(os.Environ(), "MCPGW_TEST_MAIN_SUBPROCESS=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run subprocess main: %v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "version=") {
		t.Fatalf("expected version output from main, got %q", string(output))
	}
}

func TestAdditionalHelperBranches(t *testing.T) {
	t.Setenv("MCPGW_DB_URL", "postgres://from-env")
	if dbURL, err := resolveMigrateDBURL(""); err != nil || dbURL != "postgres://from-env" {
		t.Fatalf("resolve migrate db url from env: dbURL=%q err=%v", dbURL, err)
	}

	opts := globalOptions{logLevel: "debug", logFormat: "text"}
	if err := applyGlobalOptions(opts); err != nil {
		t.Fatalf("apply global options: %v", err)
	}
	if got := os.Getenv("MCPGW_LOG_LEVEL"); got != "debug" {
		t.Fatalf("expected MCPGW_LOG_LEVEL=debug, got %q", got)
	}
	if got := os.Getenv("MCPGW_LOG_FORMAT"); got != "text" {
		t.Fatalf("expected MCPGW_LOG_FORMAT=text, got %q", got)
	}

	if _, err := maybeBuildOIDCValidator(context.Background(), &config.Config{OIDCIssuer: "https://issuer"}); err != nil {
		t.Fatalf("missing oidc audience should return nil validator without error, got %v", err)
	}
	if _, err := buildProxyTLSConfig(&config.Config{TLSCAPath: "/nonexistent/ca.pem"}); err == nil {
		t.Fatal("expected buildProxyTLSConfig error for missing CA file")
	}

	if _, err := openPostgresDB(context.Background(), "postgres://127.0.0.1:1/mcpgw?sslmode=disable"); err == nil {
		t.Fatal("expected openPostgresDB error for unreachable database")
	}
}

func TestServerListJSONAndResolveRecordErrors(t *testing.T) {
	origLoad := loadServerStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadServerStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	store := &mockServerStore{records: []types.ServerRecord{{ID: "srv-1", Name: "alpha", Status: types.StatusActive}}}
	loadServerStoreFunc = func(ctx context.Context) (storepkg.ServerStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	out := &bytes.Buffer{}
	stdout = out
	stderr = &bytes.Buffer{}
	if err := serverListCommand([]string{"--format", "json"}); err != nil {
		t.Fatalf("server list json: %v", err)
	}
	if !strings.Contains(out.String(), "\"id\": \"srv-1\"") {
		t.Fatalf("expected json output for server list, got %q", out.String())
	}
	if err := serverListCommand([]string{"--format", "yaml"}); err == nil {
		t.Fatal("expected invalid server list format error")
	}

	errStore := &resolveErrorStore{}
	if _, err := resolveServerRecord(context.Background(), errStore, "missing"); err == nil {
		t.Fatal("expected resolveServerRecord not found error")
	}
}

type resolveErrorStore struct{}

func (s *resolveErrorStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	_ = ctx
	_ = id
	return nil, sql.ErrNoRows
}

func (s *resolveErrorStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	_ = ctx
	_ = name
	return nil, sql.ErrNoRows
}

func TestOIDCHelperErrorAndSyncServerNoOpPaths(t *testing.T) {
	t.Parallel()

	if _, err := maybeBuildOIDCValidator(context.Background(), &config.Config{
		OIDCIssuer:   "://bad-issuer",
		OIDCAudience: "aud",
	}); err == nil {
		t.Fatal("expected oidc validator construction error for invalid issuer")
	}

	service := &indexedRegistrationService{
		base:        &stubRegistrationService{},
		serverStore: &mockServerStore{},
		index:       registration.NewCapabilityIndex(),
		logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	service.syncServer(context.Background(), "")
	service.syncServer(context.Background(), "missing-id")
}
