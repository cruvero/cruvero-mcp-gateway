package policy

import (
	"context"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestLogDecisionWritesAuditEntry(t *testing.T) {
	t.Parallel()

	store := &auditRecorder{}
	req := PolicyRequest{
		ToolName: "tool.exec",
		ClientID: "client-1",
		Arguments: map[string]any{
			"command": "ls -la",
			"api_key": "super-secret",
			"nested": []any{
				map[string]any{
					"token": "abc123",
				},
			},
		},
	}
	decision := &PolicyDecision{
		Allowed:         false,
		EnforcementMode: types.ModeEnforce,
		Violations: []Violation{
			{Type: ViolationDenylist, Detail: "tool denied", Severity: SeverityHigh},
		},
	}

	if err := LogDecision(context.Background(), store, req, decision); err != nil {
		t.Fatalf("log decision: %v", err)
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected one audit entry, got %d", len(store.entries))
	}

	entry := store.entries[0]
	if entry.EventType != "policy_decision" {
		t.Fatalf("expected event_type policy_decision, got %q", entry.EventType)
	}
	if entry.ClientID != "client-1" {
		t.Fatalf("expected client id client-1, got %q", entry.ClientID)
	}
	if entry.ServerName != "tool.exec" {
		t.Fatalf("expected server name tool.exec, got %q", entry.ServerName)
	}

	args, ok := entry.Details["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("expected argument details map, got %#v", entry.Details["arguments"])
	}
	if args["api_key"] != "[redacted]" {
		t.Fatalf("expected redacted api_key, got %#v", args["api_key"])
	}
	nested, ok := args["nested"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("expected nested array details, got %#v", args["nested"])
	}
	nestedMap, ok := nested[0].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map value, got %#v", nested[0])
	}
	if nestedMap["token"] != "[redacted]" {
		t.Fatalf("expected nested token redacted, got %#v", nestedMap["token"])
	}
}

func TestLogDecisionNoStoreNoError(t *testing.T) {
	t.Parallel()

	err := LogDecision(context.Background(), nil, PolicyRequest{}, &PolicyDecision{})
	if err != nil {
		t.Fatalf("expected nil error when store is nil, got %v", err)
	}
}

type auditRecorder struct {
	entries []*types.AuditEntry
}

func (r *auditRecorder) Log(_ context.Context, entry *types.AuditEntry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (r *auditRecorder) Query(_ context.Context, _ types.AuditFilter) ([]types.AuditEntry, error) {
	return nil, nil
}

func (r *auditRecorder) Count(_ context.Context, _ types.AuditFilter) (int, error) {
	return 0, nil
}
