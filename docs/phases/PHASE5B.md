# Phase 5B: Tool Safety Guardrails & Audit Logging

## Overview

Implement tool safety evaluation with allowlist/denylist enforcement, dangerous command pattern detection, argument validation, and comprehensive audit logging for all policy decisions.

## Scope

### Policy Engine (`internal/policy/engine.go`)
- `Engine` struct: profiles map[string]*types.PolicyProfile, dangerousPatterns []*regexp.Regexp, auditStore store.AuditStore, logger
- `NewEngine(profiles map[string]*types.PolicyProfile, auditStore store.AuditStore, logger *slog.Logger) *Engine`
- `Evaluate(ctx context.Context, req PolicyRequest) (*PolicyDecision, error)`:
  - Check tool against allowlist (if non-empty, tool must be in list)
  - Check tool against denylist (if tool in list, deny)
  - Check arguments against dangerous patterns
  - Check arguments against tool input schema (if available)
  - Return decision with reason

### Policy Types (`internal/policy/types.go`)
- `PolicyRequest`: ToolName string, Arguments map[string]any, ClientID string, ProfileName string, Schema json.RawMessage
- `PolicyDecision`: Allowed bool, Reason string, Violations []Violation, EnforcementMode EnforcementMode
- `Violation`: Type (denylist/dangerous_pattern/schema_violation/allowlist), Detail string, Severity (low/medium/high/critical)

### Dangerous Command Detection (`internal/policy/dangerous.go`)
- Default dangerous patterns:
  - `rm\s+-rf\s+/` -- recursive force delete from root
  - `sudo\s+` -- privilege escalation
  - `;\s*sh\b` or `;\s*bash\b` -- shell injection via command chaining
  - `curl.*\|\s*(sh|bash)` -- download and execute
  - `>(\/dev\/tcp|\/dev\/udp)` -- network exfiltration
  - `chmod\s+777` -- world-writable permissions
  - `eval\s*\(` -- eval injection
  - `\bexec\b.*\bsh\b` -- exec shell
- `CompilePatterns(patterns []string) ([]*regexp.Regexp, error)`
- `CheckDangerous(args map[string]any, patterns []*regexp.Regexp) []Violation`
- Recursive check: inspect string values at all nesting levels

### Schema Validation (`internal/policy/schema.go`)
- `ValidateArguments(args map[string]any, schema json.RawMessage) []Violation`
- Basic JSON Schema validation: required fields, type checking, enum constraints
- Keep simple -- validate what's available, don't require full JSON Schema library

### Audit Logger (`internal/policy/audit.go`)
- `LogDecision(ctx context.Context, store store.AuditStore, req PolicyRequest, decision *PolicyDecision) error`
- Write to audit_log table via AuditStore
- Include: event_type (policy_decision), client_id, tool_name, allowed/denied, violations, enforcement_mode
- Structured details as JSON in the details column

### Policy Middleware (`internal/policy/middleware.go`)
- `PolicyMiddleware(engine *Engine) func(http.Handler) http.Handler`
- Intercepts tool call requests
- Evaluates policy
- In enforce mode: block denied requests with 403
- In audit mode: log but allow
- Set response header X-Policy-Decision: allowed/denied

## Files Created

| File | Description |
|------|-------------|
| internal/policy/types.go | Policy request, decision, violation types |
| internal/policy/engine.go | Policy evaluation engine |
| internal/policy/dangerous.go | Dangerous command pattern detection |
| internal/policy/schema.go | Argument schema validation |
| internal/policy/audit.go | Audit logging |
| internal/policy/middleware.go | Policy enforcement middleware |
| internal/policy/types_test.go | Type tests |
| internal/policy/engine_test.go | Engine tests |
| internal/policy/dangerous_test.go | Pattern detection tests |
| internal/policy/schema_test.go | Schema validation tests |
| internal/policy/middleware_test.go | Middleware tests |

## Testing Requirements

- Engine: test allowlist blocks unlisted tool, test denylist blocks listed tool, test both lists interact correctly
- Dangerous: test each pattern detects its target, test benign commands pass, test nested argument inspection
- Schema: test required field validation, test type checking
- Audit: test decision logging to store
- Middleware: test enforce mode blocks, test audit mode allows with log
- Edge cases: empty allowlist means allow all, empty denylist means deny none
- Coverage: >=80%
