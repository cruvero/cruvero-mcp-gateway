package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	storepkg "github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

type mockServerStore struct {
	records []types.ServerRecord
	filter  types.ServerFilter
}

func (m *mockServerStore) Create(ctx context.Context, record *types.ServerRecord) error {
	_ = ctx
	_ = record
	return nil
}
func (m *mockServerStore) Get(ctx context.Context, id string) (*types.ServerRecord, error) {
	_ = ctx
	for i := range m.records {
		if m.records[i].ID == id {
			rec := m.records[i]
			return &rec, nil
		}
	}
	return nil, errors.New("sql: no rows in result set")
}
func (m *mockServerStore) GetByName(ctx context.Context, name string) (*types.ServerRecord, error) {
	_ = ctx
	for i := range m.records {
		if m.records[i].Name == name {
			rec := m.records[i]
			return &rec, nil
		}
	}
	return nil, errors.New("sql: no rows in result set")
}
func (m *mockServerStore) GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
	_ = ctx
	_ = spiffeID
	return nil, errors.New("sql: no rows in result set")
}
func (m *mockServerStore) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	_ = ctx
	m.filter = filter
	return append([]types.ServerRecord(nil), m.records...), nil
}
func (m *mockServerStore) Update(ctx context.Context, record *types.ServerRecord) error {
	_ = ctx
	_ = record
	return nil
}
func (m *mockServerStore) UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error {
	_ = ctx
	_ = id
	_ = status
	return nil
}
func (m *mockServerStore) UpdateHeartbeat(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}
func (m *mockServerStore) Delete(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}
func (m *mockServerStore) ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	_ = ctx
	_ = threshold
	return nil, nil
}
func (m *mockServerStore) ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error) {
	_ = ctx
	_ = threshold
	return nil, nil
}

type mockAPIKeyStore struct {
	created *types.APIKey
	listed  []types.APIKey
}

func (m *mockAPIKeyStore) Create(ctx context.Context, key *types.APIKey) error {
	_ = ctx
	copyKey := *key
	copyKey.ID = "key-1"
	copyKey.CreatedAt = time.Unix(1700000000, 0).UTC()
	m.created = &copyKey
	return nil
}

func (m *mockAPIKeyStore) GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error) {
	_ = ctx
	_ = lookupHash
	if m.created == nil {
		return nil, errors.New("sql: no rows in result set")
	}
	copyKey := *m.created
	return &copyKey, nil
}

func (m *mockAPIKeyStore) List(ctx context.Context) ([]types.APIKey, error) {
	_ = ctx
	return append([]types.APIKey(nil), m.listed...), nil
}

func (m *mockAPIKeyStore) Revoke(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}

func (m *mockAPIKeyStore) DeleteExpired(ctx context.Context) (int64, error) {
	_ = ctx
	return 0, nil
}

func TestServerListFormatting(t *testing.T) {
	origLoad := loadServerStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadServerStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	now := time.Now().UTC()
	store := &mockServerStore{records: []types.ServerRecord{
		{
			ID:       "srv-1",
			Name:     "alpha",
			Status:   types.StatusActive,
			SPIFFEID: "spiffe://example.org/ns/default/sa/alpha",
			Capabilities: types.Capability{
				Tools:     []string{"tool.alpha"},
				Resources: []string{"resource.alpha"},
			},
			LastHeartbeat: &now,
		},
	}}

	loadServerStoreFunc = func(ctx context.Context) (storepkg.ServerStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	outBuf := &bytes.Buffer{}
	stdout = outBuf
	stderr = &bytes.Buffer{}

	if err := serverCommand([]string{"list", "--status", "active", "--format", "table"}); err != nil {
		t.Fatalf("server list failed: %v", err)
	}

	got := outBuf.String()
	if !strings.Contains(got, "ID") || !strings.Contains(got, "NAME") || !strings.Contains(got, "CAPABILITIES") {
		t.Fatalf("missing table header: %q", got)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "tools=1") {
		t.Fatalf("missing server row values: %q", got)
	}
	if store.filter.Status == nil || *store.filter.Status != types.StatusActive {
		t.Fatalf("expected status filter active, got %+v", store.filter)
	}
}

func TestAPIKeyCreateGeneratesPlaintext(t *testing.T) {
	origLoad := loadAPIKeyStoreFunc
	origOut := stdout
	origErr := stderr
	t.Cleanup(func() {
		loadAPIKeyStoreFunc = origLoad
		stdout = origOut
		stderr = origErr
	})

	store := &mockAPIKeyStore{}
	loadAPIKeyStoreFunc = func(ctx context.Context) (storepkg.APIKeyStore, func() error, error) {
		_ = ctx
		return store, func() error { return nil }, nil
	}

	outBuf := &bytes.Buffer{}
	stdout = outBuf
	stderr = &bytes.Buffer{}

	err := apikeyCommand([]string{"create", "--name", "ci-key", "--client-id", "cli-client", "--scopes", "read,write", "--expires", "1d"})
	if err != nil {
		t.Fatalf("apikey create failed: %v", err)
	}

	if store.created == nil {
		t.Fatal("expected API key record to be created")
	}
	if store.created.KeyLookupHash == "" || store.created.KeyBcryptHash == "" {
		t.Fatalf("expected hashes to be stored, got %+v", store.created)
	}

	output := outBuf.String()
	if !strings.Contains(output, "API Key: mcpgw_") {
		t.Fatalf("expected plaintext API key output, got %q", output)
	}
	if strings.Contains(output, store.created.KeyBcryptHash) || strings.Contains(output, store.created.KeyLookupHash) {
		t.Fatalf("hashes should not be printed, got %q", output)
	}
}

func TestServerAndAPIKeyFlagParsing(t *testing.T) {
	if err := serverCommand([]string{}); err == nil {
		t.Fatal("expected server command error for missing subcommand")
	}
	if err := serverCommand([]string{"inspect"}); err == nil {
		t.Fatal("expected server inspect arg validation error")
	}
	if err := serverCommand([]string{"deregister"}); err == nil {
		t.Fatal("expected server deregister arg validation error")
	}

	if err := apikeyCommand([]string{}); err == nil {
		t.Fatal("expected apikey command error for missing subcommand")
	}
	if err := apikeyCommand([]string{"create"}); err == nil {
		t.Fatal("expected apikey create validation error")
	}
	if err := apikeyCommand([]string{"revoke"}); err == nil {
		t.Fatal("expected apikey revoke validation error")
	}
}
