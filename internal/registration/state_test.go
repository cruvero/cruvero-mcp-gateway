package registration

import (
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestTransitionValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current types.ServerStatus
		event   Event
		next    types.ServerStatus
	}{
		{name: "pending approved", current: types.StatusPending, event: EventApproved, next: types.StatusApproved},
		{name: "pending heartbeat", current: types.StatusPending, event: EventHeartbeat, next: types.StatusActive},
		{name: "pending expired", current: types.StatusPending, event: EventExpired, next: types.StatusExpired},
		{name: "approved heartbeat", current: types.StatusApproved, event: EventHeartbeat, next: types.StatusActive},
		{name: "approved expired", current: types.StatusApproved, event: EventExpired, next: types.StatusExpired},
		{name: "active heartbeat", current: types.StatusActive, event: EventHeartbeat, next: types.StatusActive},
		{name: "active missed", current: types.StatusActive, event: EventHeartbeatMissed, next: types.StatusStale},
		{name: "active expired", current: types.StatusActive, event: EventExpired, next: types.StatusExpired},
		{name: "stale heartbeat", current: types.StatusStale, event: EventHeartbeat, next: types.StatusActive},
		{name: "stale missed", current: types.StatusStale, event: EventHeartbeatMissed, next: types.StatusExpired},
		{name: "stale expired", current: types.StatusStale, event: EventExpired, next: types.StatusExpired},
		{name: "pending deregistered", current: types.StatusPending, event: EventDeregistered, next: types.StatusExpired},
		{name: "approved deregistered", current: types.StatusApproved, event: EventDeregistered, next: types.StatusExpired},
		{name: "active deregistered", current: types.StatusActive, event: EventDeregistered, next: types.StatusExpired},
		{name: "stale deregistered", current: types.StatusStale, event: EventDeregistered, next: types.StatusExpired},
		{name: "expired deregistered", current: types.StatusExpired, event: EventDeregistered, next: types.StatusExpired},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			next, err := Transition(tt.current, tt.event)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if next != tt.next {
				t.Fatalf("expected next status %q, got %q", tt.next, next)
			}
		})
	}
}

func TestTransitionInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current types.ServerStatus
		event   Event
	}{
		{name: "approved approve again", current: types.StatusApproved, event: EventApproved},
		{name: "expired heartbeat", current: types.StatusExpired, event: EventHeartbeat},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Transition(tt.current, tt.event); err == nil {
				t.Fatal("expected transition error")
			}
		})
	}
}
