package registration

import "time"

const (
	// SubjectRegistryUpdated is the broadcast subject for registry changes.
	SubjectRegistryUpdated = "mcpgw.registry.updated"
	// SubjectClassificationUpdated is the broadcast subject for classification changes.
	SubjectClassificationUpdated = "mcpgw.classification.updated"
)

// Broadcaster publishes and subscribes to cross-pod sync events.
type Broadcaster interface {
	// Publish sends data to all subscribers of the given subject.
	Publish(subject string, data []byte) error
	// Subscribe registers a handler for the given subject.
	Subscribe(subject string, handler func(data []byte)) error
	// Close releases all subscriptions and resources.
	Close() error
}

// RegistrationEvent describes a registration lifecycle broadcast.
type RegistrationEvent struct {
	EventType string `json:"event_type"`
	ServerID  string `json:"server_id"`
	Timestamp int64  `json:"timestamp"`
}

// ClassificationEvent describes a tool classification change broadcast.
type ClassificationEvent struct {
	ToolName  string `json:"tool_name"`
	RiskLevel string `json:"risk_level"`
	UpdatedBy string `json:"updated_by"`
	Timestamp int64  `json:"timestamp"`
}

// NewRegistrationEvent creates a timestamped registration event.
func NewRegistrationEvent(eventType, serverID string) RegistrationEvent {
	return RegistrationEvent{
		EventType: eventType,
		ServerID:  serverID,
		Timestamp: time.Now().UTC().UnixMilli(),
	}
}

// NewClassificationEvent creates a timestamped classification event.
func NewClassificationEvent(toolName, riskLevel, updatedBy string) ClassificationEvent {
	return ClassificationEvent{
		ToolName:  toolName,
		RiskLevel: riskLevel,
		UpdatedBy: updatedBy,
		Timestamp: time.Now().UTC().UnixMilli(),
	}
}
