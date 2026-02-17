package registration

import (
	"context"
	"database/sql"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestServiceUpdateSPIFFEAllowListCopiesInput(t *testing.T) {
	t.Parallel()

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{}, nil)
	allow := []string{" spiffe://example.org ", "", "spiffe://another.org"}
	svc.UpdateSPIFFEAllowList(allow)

	allow[0] = "spiffe://mutated"
	got := svc.currentSPIFFEAllowList()
	if len(got) != 2 {
		t.Fatalf("expected 2 allowlist entries after trim/filter, got %d", len(got))
	}
	if got[0] != "spiffe://example.org" {
		t.Fatalf("expected normalized allowlist entry, got %q", got[0])
	}
}

func TestServiceUpdateEffectiveSettingsValidation(t *testing.T) {
	t.Parallel()

	var nilService *Service
	if err := nilService.UpdateEffectiveSettings(1, nil); err == nil {
		t.Fatal("expected error for nil service")
	}

	svc := NewService(&mockServerStore{}, &mockAuditStore{}, &config.Config{}, nil)
	if err := svc.UpdateEffectiveSettings(0, nil); err == nil {
		t.Fatal("expected error for non-positive config version")
	}

	if err := svc.UpdateEffectiveSettings(2, map[string]map[string]any{"svc-alpha": {"k": "v"}}); err != nil {
		t.Fatalf("unexpected update error: %v", err)
	}
	if err := svc.UpdateEffectiveSettings(1, map[string]map[string]any{"svc-alpha": {"k": "v"}}); err == nil {
		t.Fatal("expected stale config version error")
	}
}

func TestServiceRegisterIncludesEffectiveSettingsSnapshot(t *testing.T) {
	t.Parallel()

	serverStore := &mockServerStore{
		getBySPIFFEIDFn: func(ctx context.Context, spiffeID string) (*types.ServerRecord, error) {
			return nil, sql.ErrNoRows
		},
	}
	svc := NewService(serverStore, &mockAuditStore{}, &config.Config{}, nil)

	settings := map[string]map[string]any{
		"svc-alpha": {
			"max_concurrency": 4,
			"nested": map[string]any{
				"mode": "strict",
			},
		},
	}
	if err := svc.UpdateEffectiveSettings(7, settings); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	settings["svc-alpha"]["max_concurrency"] = 99

	resp, err := svc.Register(context.Background(), validMTLSIdentity(), validRegistrationRequest())
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.ConfigVersion != 7 {
		t.Fatalf("expected config version 7, got %d", resp.ConfigVersion)
	}
	if got, ok := resp.EffectiveSettings["max_concurrency"].(int); !ok || got != 4 {
		t.Fatalf("expected cloned settings max_concurrency=4, got %#v", resp.EffectiveSettings["max_concurrency"])
	}
}
