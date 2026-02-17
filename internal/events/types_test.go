package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestSubjectForEvent(t *testing.T) {
	t.Parallel()

	got := SubjectForEvent("gw-1", EventServerRegistered)
	if got != "mcpgw.gw-1.events.server.registered" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestEventEnvelopeJSONRoundTrip(t *testing.T) {
	t.Parallel()

	payload := ServerDeregisteredPayload{
		ServerID: "server-1",
		Name:     "alpha",
		Reason:   "expired",
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	input := EventEnvelope{
		EventType: EventServerDeregistered,
		Timestamp: time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC),
		GatewayID: "gw-1",
		Payload:   rawPayload,
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var decoded EventEnvelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if decoded.EventType != EventServerDeregistered {
		t.Fatalf("expected %q, got %q", EventServerDeregistered, decoded.EventType)
	}
	if decoded.GatewayID != "gw-1" {
		t.Fatalf("expected gateway id gw-1, got %q", decoded.GatewayID)
	}
}

func TestPayloadJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
	}{
		{
			name: "registered payload",
			payload: ServerRegisteredPayload{
				ServerID: "server-1",
				Name:     "alpha",
				SPIFFEID: "spiffe://cluster/ns/default/sa/alpha",
				Capabilities: types.Capability{
					Tools: []string{"tool.echo"},
				},
				Endpoint: "alpha.default.svc:8443",
			},
		},
		{
			name: "deregistered payload",
			payload: ServerDeregisteredPayload{
				ServerID: "server-1",
				Name:     "alpha",
				Reason:   "manual",
			},
		},
		{
			name: "health payload",
			payload: ServerHealthChangedPayload{
				ServerID:  "server-1",
				Name:      "alpha",
				OldStatus: types.StatusActive,
				NewStatus: types.StatusStale,
			},
		},
		{
			name: "policy payload",
			payload: PolicyViolatedPayload{
				ClientID:   "client-1",
				ToolName:   "danger.tool",
				Violations: []string{"denylist"},
				Decision:   "denied",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			out := map[string]any{}
			if err := json.Unmarshal(encoded, &out); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if len(out) == 0 {
				t.Fatal("expected non-empty payload json")
			}
		})
	}
}

