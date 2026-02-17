package policy

import (
	"encoding/json"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// ViolationType categorizes a policy violation.
type ViolationType string

const (
	// ViolationDenylist indicates the requested tool matched a denylist entry.
	ViolationDenylist ViolationType = "denylist"
	// ViolationDangerousPattern indicates nested string arguments matched a dangerous pattern.
	ViolationDangerousPattern ViolationType = "dangerous_pattern"
	// ViolationSchemaViolation indicates request arguments failed schema validation.
	ViolationSchemaViolation ViolationType = "schema_violation"
	// ViolationAllowlist indicates the requested tool was not present in the allowlist.
	ViolationAllowlist ViolationType = "allowlist"
)

// ViolationSeverity ranks policy violation impact.
type ViolationSeverity string

const (
	// SeverityLow indicates informational or low-impact violations.
	SeverityLow ViolationSeverity = "low"
	// SeverityMedium indicates violations that should be reviewed.
	SeverityMedium ViolationSeverity = "medium"
	// SeverityHigh indicates high-risk violations.
	SeverityHigh ViolationSeverity = "high"
	// SeverityCritical indicates immediately dangerous violations.
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
