package orchestrator

import "encoding/json"

// SafetyLevel controls which tools are eligible for orchestration.
type SafetyLevel string

const (
	// SafetyStrict allows only read_only and unknown risk tools.
	SafetyStrict SafetyLevel = "strict"
	// SafetyReadOnly excludes destructive tools but allows write.
	SafetyReadOnly SafetyLevel = "read_only"
)

// Mode controls whether the orchestrator only plans or also executes.
type Mode string

const (
	// ModePlan generates a plan without executing it.
	ModePlan Mode = "plan"
	// ModeExecute generates a plan and runs each step.
	ModeExecute Mode = "execute"
)

// OrchestrateRequest is the parsed input from a cruvero.orchestrate tool call.
type OrchestrateRequest struct {
	Intent      string      `json:"intent"`
	Mode        Mode        `json:"mode"`
	SafetyLevel SafetyLevel `json:"safety_level"`
	Domain      string      `json:"domain,omitempty"`
}

// CandidateTool describes a tool available for orchestration planning.
type CandidateTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	RiskLevel   string          `json:"risk_level"`
}

// PlanStep is a single step in an orchestration plan.
type PlanStep struct {
	StepNumber       int            `json:"step_number"`
	ToolName         string         `json:"tool_name"`
	Arguments        map[string]any `json:"arguments"`
	Purpose          string         `json:"purpose"`
	DependsOn        []int          `json:"depends_on,omitempty"`
	RequiresApproval bool           `json:"requires_approval"`
}

// OrchestratePlan is the LLM-generated execution plan.
type OrchestratePlan struct {
	Summary  string     `json:"summary"`
	Steps    []PlanStep `json:"steps"`
	Warnings []string   `json:"warnings,omitempty"`
}

// StepResult records the outcome of executing a single plan step.
type StepResult struct {
	Step       PlanStep `json:"step"`
	Output     string   `json:"output,omitempty"`
	IsError    bool     `json:"is_error"`
	Error      string   `json:"error,omitempty"`
	Skipped    bool     `json:"skipped,omitempty"`
	SkipReason string   `json:"skip_reason,omitempty"`
}

// OrchestrateResult is the full response from the orchestrator.
type OrchestrateResult struct {
	Mode        Mode            `json:"mode"`
	Plan        OrchestratePlan `json:"plan"`
	StepResults []StepResult    `json:"step_results,omitempty"`
}
