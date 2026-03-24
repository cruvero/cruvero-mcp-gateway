package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/llm"
)

// --- Stub implementations ---

type stubDiscoverer struct {
	tools []CandidateTool
	err   error
}

func (s *stubDiscoverer) SearchTools(_ context.Context, _ string, _ int) ([]CandidateTool, error) {
	return s.tools, s.err
}

func (s *stubDiscoverer) GetToolSchemas(_ context.Context, _ []string) ([]CandidateTool, error) {
	return s.tools, s.err
}

type stubExecutor struct {
	outputs map[string]string
	errors  map[string]error
}

func (s *stubExecutor) ExecuteTool(_ context.Context, name string, _ map[string]any) (string, bool, error) {
	if err, ok := s.errors[name]; ok {
		return "", false, err
	}
	output := s.outputs[name]
	return output, false, nil
}

type stubAuditor struct {
	events []auditEvent
}

type auditEvent struct {
	eventType string
	details   map[string]any
}

func (s *stubAuditor) LogOrchestration(_ context.Context, eventType string, details map[string]any) {
	s.events = append(s.events, auditEvent{eventType: eventType, details: details})
}

type stubActivator struct {
	activated []string
}

func (s *stubActivator) ActivateTools(_ context.Context, names []string) {
	s.activated = append(s.activated, names...)
}

type stubLLM struct {
	responses []*llm.ChatResponse
	errs      []error
	callIdx   int
}

func (s *stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	idx := s.callIdx
	s.callIdx++
	if idx < len(s.errs) && s.errs[idx] != nil {
		return nil, s.errs[idx]
	}
	if idx < len(s.responses) {
		return s.responses[idx], nil
	}
	return nil, fmt.Errorf("no more stub responses")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makePlanJSON(summary string, steps []PlanStep) string {
	plan := OrchestratePlan{Summary: summary, Steps: steps}
	b, _ := json.Marshal(plan)
	return string(b)
}

func defaultCandidates() []CandidateTool {
	return []CandidateTool{
		{Name: "k8s.list_pods", Description: "List pods", InputSchema: json.RawMessage(`{}`), RiskLevel: "read_only"},
		{Name: "k8s.delete_pod", Description: "Delete a pod", InputSchema: json.RawMessage(`{}`), RiskLevel: "destructive"},
		{Name: "k8s.get_logs", Description: "Get pod logs", InputSchema: json.RawMessage(`{}`), RiskLevel: "read_only"},
	}
}

// --- Tests ---

func TestHandle_MissingIntent(t *testing.T) {
	t.Parallel()
	o := New(nil, nil, nil, &stubAuditor{}, &stubActivator{}, discardLogger())
	result, isErr := o.Handle(context.Background(), map[string]any{})
	if !isErr {
		t.Fatal("expected error for missing intent")
	}
	if !strings.Contains(result, "intent is required") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestHandle_InvalidMode(t *testing.T) {
	t.Parallel()
	o := New(nil, nil, nil, &stubAuditor{}, &stubActivator{}, discardLogger())
	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent": "test",
		"mode":   "invalid",
	})
	if !isErr {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(result, "mode must be") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestHandle_InvalidSafetyLevel(t *testing.T) {
	t.Parallel()
	o := New(nil, nil, nil, &stubAuditor{}, &stubActivator{}, discardLogger())
	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "test",
		"safety_level": "yolo",
	})
	if !isErr {
		t.Fatal("expected error for invalid safety_level")
	}
	if !strings.Contains(result, "safety_level must be") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestPlan_SafetyStrict_FiltersDestructive(t *testing.T) {
	t.Parallel()
	planJSON := makePlanJSON("list pods", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "list"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}
	auditor := &stubAuditor{}
	activator := &stubActivator{}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, auditor, activator, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "list kubernetes pods",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if res.Mode != ModePlan {
		t.Fatalf("expected plan mode, got %q", res.Mode)
	}
	if len(res.Plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(res.Plan.Steps))
	}
	if res.Plan.Steps[0].ToolName != "k8s.list_pods" {
		t.Fatalf("unexpected tool: %s", res.Plan.Steps[0].ToolName)
	}
	if len(auditor.events) != 1 || auditor.events[0].eventType != "orchestrate_plan" {
		t.Fatalf("expected orchestrate_plan audit event, got %+v", auditor.events)
	}
	if len(activator.activated) == 0 {
		t.Fatal("expected tools to be activated")
	}
}

func TestPlan_SafetyReadOnly_AllowsWrite(t *testing.T) {
	t.Parallel()
	candidates := []CandidateTool{
		{Name: "db.write", Description: "Write to DB", InputSchema: json.RawMessage(`{}`), RiskLevel: "write"},
		{Name: "db.drop", Description: "Drop table", InputSchema: json.RawMessage(`{}`), RiskLevel: "destructive"},
	}
	planJSON := makePlanJSON("write data", []PlanStep{
		{StepNumber: 1, ToolName: "db.write", Arguments: map[string]any{}, Purpose: "write"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}

	o := New(llmClient, &stubDiscoverer{tools: candidates}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "write to database",
		"mode":         "plan",
		"safety_level": "read_only",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestPlan_NoCandidatesAfterFilter(t *testing.T) {
	t.Parallel()
	candidates := []CandidateTool{
		{Name: "db.drop", Description: "Drop table", InputSchema: json.RawMessage(`{}`), RiskLevel: "destructive"},
	}

	o := New(nil, &stubDiscoverer{tools: candidates}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "do something",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if !isErr {
		t.Fatal("expected error for no candidates")
	}
	if !strings.Contains(result, "no tools match intent within safety constraints") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestPlan_DiscovererError(t *testing.T) {
	t.Parallel()
	o := New(nil, &stubDiscoverer{err: errors.New("search failed")}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent": "test",
		"mode":   "plan",
	})
	if !isErr {
		t.Fatal("expected error")
	}
	if !strings.Contains(result, "search tools") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestPlan_FabricatedToolRemoved(t *testing.T) {
	t.Parallel()
	planJSON := makePlanJSON("mixed plan", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "list"},
		{StepNumber: 2, ToolName: "invented.tool", Arguments: map[string]any{}, Purpose: "fake"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "list pods",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if len(res.Plan.Steps) != 1 {
		t.Fatalf("expected 1 step after fabricated tool removal, got %d", len(res.Plan.Steps))
	}
	if len(res.Plan.Warnings) == 0 {
		t.Fatal("expected warning about fabricated tool")
	}
	if !strings.Contains(res.Plan.Warnings[0], "invented.tool") {
		t.Fatalf("expected warning about invented.tool, got: %s", res.Plan.Warnings[0])
	}
}

func TestPlan_LLMParseFailure_RetrySucceeds(t *testing.T) {
	t.Parallel()
	planJSON := makePlanJSON("retried plan", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "list"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "not json at all", Provider: "test", Model: "test"},
		{Content: planJSON, Provider: "test", Model: "test"},
	}}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "list pods",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.Plan.Summary != "retried plan" {
		t.Fatalf("expected 'retried plan' summary, got %q", res.Plan.Summary)
	}
}

func TestPlan_LLMParseFailure_RetryFails(t *testing.T) {
	t.Parallel()
	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: "bad json", Provider: "test", Model: "test"},
		{Content: "still bad", Provider: "test", Model: "test"},
	}}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "list pods",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if !isErr {
		t.Fatal("expected error when retry also returns bad JSON")
	}
	if !strings.Contains(result, "invalid JSON after retry") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestPlan_LLMError(t *testing.T) {
	t.Parallel()
	llmClient := &stubLLM{errs: []error{errors.New("llm down")}}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "list pods",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if !isErr {
		t.Fatal("expected error when LLM fails")
	}
	if !strings.Contains(result, "llm chat") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestPlan_LLMRetryError(t *testing.T) {
	t.Parallel()
	llmClient := &stubLLM{
		responses: []*llm.ChatResponse{
			{Content: "not json", Provider: "test", Model: "test"},
		},
		errs: []error{nil, errors.New("retry failed")},
	}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "list pods",
		"mode":         "plan",
		"safety_level": "strict",
	})
	if !isErr {
		t.Fatal("expected error when LLM retry fails")
	}
	if !strings.Contains(result, "llm retry chat") {
		t.Fatalf("unexpected error: %s", result)
	}
}

func TestExecute_Success(t *testing.T) {
	t.Parallel()
	planJSON := makePlanJSON("execute plan", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{"namespace": "default"}, Purpose: "list"},
		{StepNumber: 2, ToolName: "k8s.get_logs", Arguments: map[string]any{"pod": "nginx"}, Purpose: "logs", DependsOn: []int{1}},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}
	executor := &stubExecutor{
		outputs: map[string]string{
			"k8s.list_pods": `["nginx","redis"]`,
			"k8s.get_logs":  "log line 1\nlog line 2",
		},
		errors: map[string]error{},
	}
	auditor := &stubAuditor{}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, executor, auditor, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent":       "check pod logs",
		"mode":         "execute",
		"safety_level": "strict",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if res.Mode != ModeExecute {
		t.Fatalf("expected execute mode, got %q", res.Mode)
	}
	if len(res.StepResults) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(res.StepResults))
	}
	if res.StepResults[0].Output != `["nginx","redis"]` {
		t.Fatalf("unexpected output: %s", res.StepResults[0].Output)
	}

	// Expect two audit events: one for plan, one for execute
	if len(auditor.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(auditor.events))
	}
	if auditor.events[0].eventType != "orchestrate_plan" {
		t.Fatalf("expected first event to be orchestrate_plan, got %q", auditor.events[0].eventType)
	}
	if auditor.events[1].eventType != "orchestrate_execute" {
		t.Fatalf("expected second event to be orchestrate_execute, got %q", auditor.events[1].eventType)
	}
}

func TestExecute_StepFailure_ContinuesRemaining(t *testing.T) {
	t.Parallel()
	planJSON := makePlanJSON("failing plan", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "list"},
		{StepNumber: 2, ToolName: "k8s.get_logs", Arguments: map[string]any{}, Purpose: "logs"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}
	executor := &stubExecutor{
		outputs: map[string]string{"k8s.get_logs": "log output"},
		errors:  map[string]error{"k8s.list_pods": errors.New("connection timeout")},
	}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, executor, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent": "check pods",
		"mode":   "execute",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if len(res.StepResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.StepResults))
	}
	if !res.StepResults[0].IsError {
		t.Fatal("expected first step to be error")
	}
	if res.StepResults[1].Output != "log output" {
		t.Fatalf("expected second step to succeed, got: %s", res.StepResults[1].Output)
	}
}

func TestExecute_RequiresApproval_Skipped(t *testing.T) {
	t.Parallel()
	planJSON := makePlanJSON("approval plan", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "list", RequiresApproval: true},
		{StepNumber: 2, ToolName: "k8s.get_logs", Arguments: map[string]any{}, Purpose: "logs"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}
	executor := &stubExecutor{
		outputs: map[string]string{"k8s.get_logs": "logs"},
		errors:  map[string]error{},
	}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, executor, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent": "check things",
		"mode":   "execute",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if !res.StepResults[0].Skipped {
		t.Fatal("expected first step to be skipped")
	}
	if res.StepResults[0].SkipReason != "requires human approval" {
		t.Fatalf("unexpected skip reason: %s", res.StepResults[0].SkipReason)
	}
	if res.StepResults[1].Output != "logs" {
		t.Fatalf("expected second step to execute, got: %s", res.StepResults[1].Output)
	}
}

func TestPlan_DomainFilter(t *testing.T) {
	t.Parallel()
	candidates := []CandidateTool{
		{Name: "k8s.list_pods", Description: "List pods", InputSchema: json.RawMessage(`{}`), RiskLevel: "read_only"},
		{Name: "aws.list_buckets", Description: "List S3 buckets", InputSchema: json.RawMessage(`{}`), RiskLevel: "read_only"},
	}

	planJSON := makePlanJSON("k8s only", []PlanStep{
		{StepNumber: 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "list"},
	})

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}

	o := New(llmClient, &stubDiscoverer{tools: candidates}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent": "list pods",
		"mode":   "plan",
		"domain": "k8s",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if len(res.Plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(res.Plan.Steps))
	}
}

func TestPlan_TruncatesToMaxSteps(t *testing.T) {
	t.Parallel()
	steps := make([]PlanStep, 15)
	for i := range steps {
		steps[i] = PlanStep{StepNumber: i + 1, ToolName: "k8s.list_pods", Arguments: map[string]any{}, Purpose: "step"}
	}
	planJSON := makePlanJSON("big plan", steps)

	llmClient := &stubLLM{responses: []*llm.ChatResponse{
		{Content: planJSON, Provider: "test", Model: "test"},
	}}

	o := New(llmClient, &stubDiscoverer{tools: defaultCandidates()}, nil, &stubAuditor{}, &stubActivator{}, discardLogger())

	result, isErr := o.Handle(context.Background(), map[string]any{
		"intent": "list pods",
		"mode":   "plan",
	})
	if isErr {
		t.Fatalf("unexpected error: %s", result)
	}

	var res OrchestrateResult
	_ = json.Unmarshal([]byte(result), &res)
	if len(res.Plan.Steps) != maxPlanSteps {
		t.Fatalf("expected %d steps, got %d", maxPlanSteps, len(res.Plan.Steps))
	}
	found := false
	for _, w := range res.Plan.Warnings {
		if strings.Contains(w, "truncated") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected truncation warning")
	}
}

func TestBuildUserPrompt(t *testing.T) {
	t.Parallel()
	candidates := []CandidateTool{
		{Name: "test.tool", Description: "A test tool", InputSchema: json.RawMessage(`{"type":"object"}`), RiskLevel: "read_only"},
	}

	prompt := buildUserPrompt("test intent", candidates, SafetyStrict, "myDomain")
	if !strings.Contains(prompt, "test intent") {
		t.Fatal("expected intent in prompt")
	}
	if !strings.Contains(prompt, "strict") {
		t.Fatal("expected safety level in prompt")
	}
	if !strings.Contains(prompt, "myDomain") {
		t.Fatal("expected domain in prompt")
	}
	if !strings.Contains(prompt, "test.tool") {
		t.Fatal("expected tool name in prompt")
	}
}

func TestFilterBySafety(t *testing.T) {
	t.Parallel()
	candidates := []CandidateTool{
		{Name: "a", RiskLevel: "read_only"},
		{Name: "b", RiskLevel: "write"},
		{Name: "c", RiskLevel: "destructive"},
		{Name: "d", RiskLevel: "unknown"},
	}

	strict := filterBySafety(candidates, SafetyStrict)
	if len(strict) != 2 {
		t.Fatalf("strict: expected 2 (read_only + unknown), got %d", len(strict))
	}

	readOnly := filterBySafety(candidates, SafetyReadOnly)
	if len(readOnly) != 3 {
		t.Fatalf("read_only: expected 3 (all except destructive), got %d", len(readOnly))
	}
}

func TestFilterByDomain(t *testing.T) {
	t.Parallel()
	candidates := []CandidateTool{
		{Name: "k8s.list_pods"},
		{Name: "aws.list_buckets"},
		{Name: "k8s.delete_pod"},
	}

	filtered := filterByDomain(candidates, "k8s")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 k8s tools, got %d", len(filtered))
	}

	all := filterByDomain(candidates, "")
	if len(all) != 3 {
		t.Fatalf("expected all 3 tools with empty domain, got %d", len(all))
	}
}

func TestExtractToolNames(t *testing.T) {
	t.Parallel()
	plan := &OrchestratePlan{Steps: []PlanStep{
		{ToolName: "a"},
		{ToolName: "b"},
		{ToolName: "a"},
	}}
	names := extractToolNames(plan)
	if len(names) != 2 {
		t.Fatalf("expected 2 unique names, got %d", len(names))
	}
}
