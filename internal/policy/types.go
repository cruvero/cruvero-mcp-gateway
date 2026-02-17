package policy

import (
	"encoding/json"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// ViolationType categorizes a policy violation.
type ViolationType string

const (
	ViolationDenylist         ViolationType = "denylist"
	ViolationDangerousPattern ViolationType = "dangerous_pattern"
	ViolationSchemaViolation  ViolationType = "schema_violation"
	ViolationAllowlist        ViolationType = "allowlist"
)

// ViolationSeverity ranks policy violation impact.
type ViolationSeverity string

const (
	SeverityLow      ViolationSeverity = "low"
	SeverityMedium   ViolationSeverity = "medium"
	SeverityHigh     ViolationSeverity = "high"
	SeverityCritical ViolationSeverity = "critical"
)

// PolicyRequest describes the inputs required for policy evaluation.
type PolicyRequest struct {
	ToolName    string          `json:"tool_name"`
	Arguments   map[string]any  `json:"arguments"`
	ClientID    string          `json:"client_id"`
	ProfileName string          `json:"profile_name"`
	Schema      json.RawMessage `json:"schema"`
}

// Violation describes a single policy failure.
type Violation struct {
	Type     ViolationType     `json:"type"`
	Detail   string            `json:"detail"`
	Severity ViolationSeverity `json:"severity"`
}

// PolicyDecision is the result of a policy evaluation.
type PolicyDecision struct {
	Allowed         bool                  `json:"allowed"`
	Reason          string                `json:"reason"`
	Violations      []Violation           `json:"violations"`
	EnforcementMode types.EnforcementMode `json:"enforcement_mode"`
}
