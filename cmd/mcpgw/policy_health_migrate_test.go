package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/events"
	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/golang-migrate/migrate/v4"
)

type mockConfigStore struct {
	values map[string][]byte
}

func (m *mockConfigStore) Save(ctx context.Context, key string, value []byte) error {
	_ = ctx
	if m.values == nil {
		m.values = map[string][]byte{}
	}
	m.values[key] = append([]byte(nil), value...)
	return nil
}

func (m *mockConfigStore) Load(ctx context.Context, key string) ([]byte, error) {
	_ = ctx
	value, ok := m.values[key]
	if !ok {
		return nil, errors.New("sql: no rows in result set")
	}
	return append([]byte(nil), value...), nil
}

func (m *mockConfigStore) Keys(ctx context.Context) ([]string, error) {
	_ = ctx
	keys := make([]string, 0, len(m.values))
	for key := range m.values {
		keys = append(keys, key)
	}
	return keys, nil
}

type mockMigrator struct {
	upCalled    bool
	downCalled  bool
	stepsCalled int
	version     uint
	dirty       bool
	applyErr    error
}

func (m *mockMigrator) Up() error {
	m.upCalled = true
	return m.applyErr
}
func (m *mockMigrator) Down() error {
	m.downCalled = true
	return m.applyErr
}
func (m *mockMigrator) Steps(n int) error {
	m.stepsCalled = n
	return m.applyErr
}
func (m *mockMigrator) Version() (uint, bool, error) {
	return m.version, m.dirty, nil
}
func (m *mockMigrator) Close() (error, error) {
	return nil, nil
}

func TestPolicyListOutput(t *testing.T) {
	origLoad := loadConfigStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadConfigStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	store := &mockConfigStore{}
	payload, err := json.Marshal(events.PolicyConfigMessage{Profiles: []types.PolicyProfile{
		{Name: "default", RateLimit: 10, RateBurst: 20, EnforcementMode: types.ModeEnforce},
		{Name: "premium", RateLimit: 50, RateBurst: 100, EnforcementMode: types.ModeAudit, ToolAllowlist: []string{"tool.safe"}},
	}})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	store.values = map[string][]byte{configCachePolicyKey: payload}

	loadConfigStoreFunc = func(ctx context.Context) (events.ConfigStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	outBuf := &bytes.Buffer{}
	stdout = outBuf
	stderr = &bytes.Buffer{}

	if err := policyCommand([]string{"list"}); err != nil {
		t.Fatalf("policy list failed: %v", err)
	}

	got := outBuf.String()
	if !strings.Contains(got, "NAME") || !strings.Contains(got, "RATE_LIMIT") {
		t.Fatalf("missing policy header: %q", got)
	}
	if !strings.Contains(got, "premium") || !strings.Contains(got, "audit") {
		t.Fatalf("missing policy row details: %q", got)
	}
}

func TestHealthCommandWithMockServer(t *testing.T) {
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		stdout = origOut
		stderr = origErr
	})

	handler := http.NewServeMux()
	handler.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	handler.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "nats": "connected", "registered_servers": 3})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	outBuf := &bytes.Buffer{}
	stdout = outBuf
	stderr = &bytes.Buffer{}

	if err := healthCommand([]string{"--url", srv.URL}); err != nil {
		t.Fatalf("health command failed: %v", err)
	}

	output := outBuf.String()
	if !strings.Contains(output, "Health: ok") || !strings.Contains(output, "Ready: ok") {
		t.Fatalf("unexpected health output: %q", output)
	}
}

func TestMigrateUpAndDown(t *testing.T) {
	origNewRunner := newMigrateRunnerFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		newMigrateRunnerFunc = origNewRunner
		stdout = origOut
		stderr = origErr
	})

	m := &mockMigrator{version: 4}
	newMigrateRunnerFunc = func(dbURL string) (migrateRunner, error) {
		if dbURL != "postgres://override" {
			t.Fatalf("unexpected db url %q", dbURL)
		}
		return m, nil
	}

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}

	if err := migrateCommand([]string{"--direction", "up", "--steps", "2", "--db-url", "postgres://override"}); err != nil {
		t.Fatalf("migrate up failed: %v", err)
	}
	if m.stepsCalled != 2 {
		t.Fatalf("expected steps +2, got %d", m.stepsCalled)
	}

	m.stepsCalled = 0
	if err := migrateCommand([]string{"--direction", "down", "--steps", "3", "--db-url", "postgres://override"}); err != nil {
		t.Fatalf("migrate down failed: %v", err)
	}
	if m.stepsCalled != -3 {
		t.Fatalf("expected steps -3, got %d", m.stepsCalled)
	}
}

func TestMigrateNoChange(t *testing.T) {
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
		return &mockMigrator{applyErr: migrate.ErrNoChange}, nil
	}

	outBuf := &bytes.Buffer{}
	stdout = outBuf
	stderr = &bytes.Buffer{}

	if err := migrateCommand([]string{"--direction", "up", "--db-url", "postgres://override"}); err != nil {
		t.Fatalf("migrate no-change failed: %v", err)
	}
	if !strings.Contains(outBuf.String(), "no migrations to apply") {
		t.Fatalf("expected no-change output, got %q", outBuf.String())
	}
}

func TestValidateHealthBaseURL(t *testing.T) {
	t.Parallel()

	if err := validateHealthBaseURL(nil); err == nil {
		t.Fatal("expected error for nil URL")
	}

	invalidScheme, err := url.Parse("ftp://example.com")
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if err := validateHealthBaseURL(invalidScheme); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}

	withUserInfo, err := url.Parse("https://user:pass@example.com")
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if err := validateHealthBaseURL(withUserInfo); err == nil {
		t.Fatal("expected error for URL with user info")
	}
}
