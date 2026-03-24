package orchestrator

import (
	"fmt"
	"strings"
)

// SystemPrompt is the LLM system instruction for orchestration planning.
const SystemPrompt = `You are a safety-first, zero-hallucination MCP tool orchestration router.

CRITICAL EXTRACTION RULE (NEVER VIOLATE):
- EVERY value mentioned in the intent MUST be placed in the correct "arguments" field exactly matching the tool schema.
- Never leave required fields empty when the intent contains the value.
- Use exact string matching from the intent.

MULTI-STEP PLANNING RULES:
- Use 1 step when possible.
- Use 2-3 steps only when one tool's output is logically required for the next.
- Set "depends_on": [previous_step_numbers] for chaining.
- Reference previous step output with {{stepN.output.field}} syntax in later arguments.
- Always set "requires_approval": true for any write/destructive k8s operation.

EXAMPLES:

1. Single step
Intent: "List all pods in the my-namespace namespace"
{
  "summary": "List pods in my-namespace namespace",
  "steps": [{
    "step_number": 1,
    "tool_name": "k8s.list_pods",
    "arguments": {"namespace": "my-namespace"},
    "purpose": "Retrieve all pods in the requested namespace",
    "depends_on": [],
    "requires_approval": false
  }]
}

2. Two-step chain
Intent: "Initiate a rolling restart on the portainer deployment in the portainer namespace"
{
  "summary": "Get deployment details then trigger rolling restart",
  "steps": [
    {
      "step_number": 1,
      "tool_name": "k8s.get_deployment",
      "arguments": {"name": "portainer", "namespace": "portainer"},
      "purpose": "Fetch current state before restart",
      "depends_on": [],
      "requires_approval": false
    },
    {
      "step_number": 2,
      "tool_name": "k8s.restart_rollout",
      "arguments": {"deployment": "portainer", "namespace": "portainer"},
      "purpose": "Perform rolling restart",
      "depends_on": [1],
      "requires_approval": true
    }
  ]
}

3. Three-step chain
Intent: "Restart the api deployment in prod then list its pods and show the newest one"
{
  "summary": "Restart deployment → list pods → identify newest",
  "steps": [
    { "step_number":1, "tool_name":"k8s.restart_rollout", "arguments":{"deployment":"api","namespace":"prod"}, "purpose":"Trigger restart", "depends_on":[], "requires_approval":true },
    { "step_number":2, "tool_name":"k8s.list_pods", "arguments":{"namespace":"prod","label_selector":"app=api"}, "purpose":"List pods after restart", "depends_on":[1], "requires_approval":false },
    { "step_number":3, "tool_name":"k8s.get_pod", "arguments":{"name":"{{step2.output.pods[0].name}}","namespace":"prod"}, "purpose":"Get details of newest pod", "depends_on":[2], "requires_approval":false }
  ]
}

Response format (strict JSON only, no extra text):
{
  "summary": "one sentence plan description",
  "steps": [ ... ],
  "warnings": []
}`

func buildUserPrompt(intent string, candidates []CandidateTool, safetyLevel SafetyLevel, domain string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Intent\n%s\n\n", intent)
	fmt.Fprintf(&b, "## Safety Level\n%s\n\n", safetyLevel)

	if domain != "" {
		fmt.Fprintf(&b, "## Domain Filter\n%s\n\n", domain)
	}

	b.WriteString("## Available Tools\n")
	for i, t := range candidates {
		fmt.Fprintf(&b, "\n### Tool %d: %s\n", i+1, t.Name)
		fmt.Fprintf(&b, "- Description: %s\n", t.Description)
		fmt.Fprintf(&b, "- Risk Level: %s\n", t.RiskLevel)
		fmt.Fprintf(&b, "- Input Schema: %s\n", string(t.InputSchema))
	}

	return b.String()
}
