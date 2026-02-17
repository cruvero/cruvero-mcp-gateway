package registration

import (
	"fmt"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// Event describes a server lifecycle transition trigger.
type Event string

const (
	// EventHeartbeat indicates a heartbeat was received from the server.
	EventHeartbeat Event = "heartbeat"
	// EventHeartbeatMissed indicates expected heartbeat(s) were missed.
	EventHeartbeatMissed Event = "heartbeat_missed"
	// EventExpired indicates the server registration has reached expiration.
	EventExpired Event = "expired"
	// EventApproved indicates an admin or policy approved a pending registration.
	EventApproved Event = "approved"
	// EventDeregistered indicates explicit deregistration of a server.
	EventDeregistered Event = "deregistered"
)

type transitionKey struct {
	current types.ServerStatus
	event   Event
}

// Transition validates and applies a lifecycle transition.
func Transition(current types.ServerStatus, event Event) (types.ServerStatus, error) {
	transitions := map[transitionKey]types.ServerStatus{
		{current: types.StatusPending, event: EventApproved}:        types.StatusApproved,
		{current: types.StatusPending, event: EventExpired}:         types.StatusExpired,
		{current: types.StatusApproved, event: EventHeartbeat}:      types.StatusActive,
		{current: types.StatusApproved, event: EventExpired}:        types.StatusExpired,
		{current: types.StatusActive, event: EventHeartbeat}:        types.StatusActive,
		{current: types.StatusActive, event: EventHeartbeatMissed}:  types.StatusStale,
		{current: types.StatusActive, event: EventExpired}:          types.StatusExpired,
		{current: types.StatusStale, event: EventHeartbeat}:         types.StatusActive,
		{current: types.StatusStale, event: EventHeartbeatMissed}:   types.StatusExpired,
		{current: types.StatusStale, event: EventExpired}:           types.StatusExpired,
		{current: types.StatusPending, event: EventDeregistered}:    types.StatusExpired,
		{current: types.StatusApproved, event: EventDeregistered}:   types.StatusExpired,
		{current: types.StatusActive, event: EventDeregistered}:     types.StatusExpired,
		{current: types.StatusStale, event: EventDeregistered}:      types.StatusExpired,
		{current: types.StatusExpired, event: EventDeregistered}:    types.StatusExpired,
		{current: types.StatusExpired, event: EventHeartbeatMissed}: types.StatusExpired,
		{current: types.StatusExpired, event: EventExpired}:         types.StatusExpired,
	}

	next, ok := transitions[transitionKey{current: current, event: event}]
	if !ok {
		return "", fmt.Errorf("invalid transition: status=%s event=%s", current, event)
	}

	return next, nil
}
