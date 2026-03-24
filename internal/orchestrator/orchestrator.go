package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/llm"
)

const maxPlanSteps = 10

// Orchestrator generates and optionally executes LLM-driven tool plans.
type Orchestrator struct {
	llm        llm.Client
	discoverer ToolDiscoverer
	executor   ToolExecutor
	auditor    AuditLogger
	activator  ToolActivator
	logger     *slog.Logger
}

// New creates an Orchestrator with the given dependencies.
func New(
	llmClient llm.Client,
	discoverer ToolDiscoverer,
	executor ToolExecutor,
	auditor AuditLogger,
	activator ToolActivator,
	logger *slog.Logger,
) *Orchestrator {
	return &Orchestrator{
		llm:        llmClient,
		discoverer: discoverer,
		executor:   executor,
		auditor:    auditor,
		activator:  activator,
		logger:     logger,
	}
}

// Handle is the entry point called by the proxy meta-tool handler.
// It parses args into an OrchestrateRequest and dispatches to plan or execute.
func (o *Orchestrator) Handle(ctx context.Context, args map[string]any) (string, bool) {
	req, err := parseRequest(args)
	if err != nil {
		return fmt.Sprintf("invalid orchestrate request: %v", err), true
	}

	var result *OrchestrateResult
	switch req.Mode {
	case ModePlan:
		result, err = o.plan(ctx, req)
	case ModeExecute:
		result, err = o.execute(ctx, req)
	default:
		return fmt.Sprintf("unsupported mode: %q", req.Mode), true
	}

	if err != nil {
		return fmt.Sprintf("orchestration failed: %v", err), true
	}

	out, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return fmt.Sprintf("marshal orchestration result: %v", marshalErr), true
	}
	return string(out), false
}

func parseRequest(args map[string]any) (*OrchestrateRequest, error) {
	intent, _ := args["intent"].(string)
	if strings.TrimSpace(intent) == "" {
		return nil, fmt.Errorf("intent is required")
	}

	mode := ModeExecute
	if m, ok := args["mode"].(string); ok && strings.TrimSpace(m) != "" {
		mode = Mode(strings.TrimSpace(m))
	}
	if mode != ModePlan && mode != ModeExecute {
		return nil, fmt.Errorf("mode must be 'plan' or 'execute'")
	}

	safety := SafetyStrict
	if s, ok := args["safety_level"].(string); ok && strings.TrimSpace(s) != "" {
		safety = SafetyLevel(strings.TrimSpace(s))
	}
	if safety != SafetyStrict && safety != SafetyReadOnly {
		return nil, fmt.Errorf("safety_level must be 'strict' or 'read_only'")
	}

	domain, _ := args["domain"].(string)

	return &OrchestrateRequest{
		Intent:      intent,
		Mode:        mode,
		SafetyLevel: safety,
		Domain:      domain,
	}, nil
}

func (o *Orchestrator) plan(ctx context.Context, req *OrchestrateRequest) (*OrchestrateResult, error) {
	candidates, err := o.discoverer.SearchTools(ctx, req.Intent, 20)
	if err != nil {
		return nil, fmt.Errorf("search tools: %w", err)
	}

	if req.Domain != "" {
		candidates = filterByDomain(candidates, req.Domain)
	}

	candidates = filterBySafety(candidates, req.SafetyLevel)

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no tools match intent within safety constraints")
	}

	plan, err := o.generatePlan(ctx, req, candidates)
	if err != nil {
		return nil, err
	}

	plan = o.validatePlan(plan, candidates)

	o.auditor.LogOrchestration(ctx, "orchestrate_plan", map[string]any{
		"intent":       req.Intent,
		"mode":         string(req.Mode),
		"safety_level": string(req.SafetyLevel),
		"domain":       req.Domain,
		"plan_summary": plan.Summary,
		"step_count":   len(plan.Steps),
	})

	toolNames := extractToolNames(plan)
	if len(toolNames) > 0 {
		o.activator.ActivateTools(ctx, toolNames)
	}

	return &OrchestrateResult{
		Mode: ModePlan,
		Plan: *plan,
	}, nil
}

func (o *Orchestrator) execute(ctx context.Context, req *OrchestrateRequest) (*OrchestrateResult, error) {
	planResult, err := o.plan(ctx, req)
	if err != nil {
		return nil, err
	}

	results := make([]StepResult, 0, len(planResult.Plan.Steps))
	for _, step := range planResult.Plan.Steps {
		if step.RequiresApproval {
			results = append(results, StepResult{
				Step:       step,
				Skipped:    true,
				SkipReason: "requires human approval",
			})
			continue
		}

		output, isError, execErr := o.executor.ExecuteTool(ctx, step.ToolName, step.Arguments)
		sr := StepResult{Step: step}
		if execErr != nil {
			sr.IsError = true
			sr.Error = execErr.Error()
		} else {
			sr.Output = output
			sr.IsError = isError
			if isError {
				sr.Error = output
			}
		}
		results = append(results, sr)
	}

	o.auditor.LogOrchestration(ctx, "orchestrate_execute", map[string]any{
		"intent":       req.Intent,
		"mode":         string(req.Mode),
		"safety_level": string(req.SafetyLevel),
		"plan_summary": planResult.Plan.Summary,
		"step_count":   len(planResult.Plan.Steps),
		"results":      results,
	})

	return &OrchestrateResult{
		Mode:        ModeExecute,
		Plan:        planResult.Plan,
		StepResults: results,
	}, nil
}

func (o *Orchestrator) generatePlan(ctx context.Context, req *OrchestrateRequest, candidates []CandidateTool) (*OrchestratePlan, error) {
	userPrompt := buildUserPrompt(req.Intent, candidates, req.SafetyLevel, req.Domain)

	messages := []llm.ChatMessage{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: userPrompt},
	}

	resp, err := o.llm.Chat(ctx, llm.ChatRequest{
		Messages:    messages,
		MaxTokens:   4096,
		Temperature: 0.0,
		JSONOutput:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	var plan OrchestratePlan
	if parseErr := json.Unmarshal([]byte(resp.Content), &plan); parseErr != nil {
		// Retry once with corrective instruction.
		messages = append(messages,
			llm.ChatMessage{Role: "assistant", Content: resp.Content},
			llm.ChatMessage{Role: "user", Content: "Your previous response was not valid JSON. Respond with valid JSON only, matching the schema specified in the system prompt."},
		)
		retryResp, retryErr := o.llm.Chat(ctx, llm.ChatRequest{
			Messages:    messages,
			MaxTokens:   4096,
			Temperature: 0.0,
			JSONOutput:  true,
		})
		if retryErr != nil {
			return nil, fmt.Errorf("llm retry chat: %w", retryErr)
		}
		if retryParseErr := json.Unmarshal([]byte(retryResp.Content), &plan); retryParseErr != nil {
			return nil, fmt.Errorf("llm returned invalid JSON after retry: %s", retryResp.Content)
		}
	}

	return &plan, nil
}

func (o *Orchestrator) validatePlan(plan *OrchestratePlan, candidates []CandidateTool) *OrchestratePlan {
	candidateSet := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		candidateSet[c.Name] = struct{}{}
	}

	validSteps := make([]PlanStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if _, ok := candidateSet[step.ToolName]; !ok {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("removed fabricated tool %q from plan", step.ToolName))
			o.logger.Warn("llm fabricated tool removed from plan",
				slog.String("tool", step.ToolName))
			continue
		}
		validSteps = append(validSteps, step)
	}
	plan.Steps = validSteps

	if len(plan.Steps) > maxPlanSteps {
		plan.Steps = plan.Steps[:maxPlanSteps]
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("plan truncated to %d steps", maxPlanSteps))
	}

	return plan
}

func filterBySafety(candidates []CandidateTool, level SafetyLevel) []CandidateTool {
	filtered := make([]CandidateTool, 0, len(candidates))
	for _, c := range candidates {
		switch level {
		case SafetyStrict:
			if c.RiskLevel == "read_only" || c.RiskLevel == "unknown" {
				filtered = append(filtered, c)
			}
		case SafetyReadOnly:
			if c.RiskLevel != "destructive" {
				filtered = append(filtered, c)
			}
		}
	}
	return filtered
}

func filterByDomain(candidates []CandidateTool, domain string) []CandidateTool {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" {
		return candidates
	}
	filtered := make([]CandidateTool, 0, len(candidates))
	for _, c := range candidates {
		if strings.Contains(strings.ToLower(c.Name), d) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func extractToolNames(plan *OrchestratePlan) []string {
	seen := make(map[string]struct{}, len(plan.Steps))
	names := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if _, ok := seen[step.ToolName]; ok {
			continue
		}
		seen[step.ToolName] = struct{}{}
		names = append(names, step.ToolName)
	}
	return names
}
