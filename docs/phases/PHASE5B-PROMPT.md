# Phase 5B Implementation Prompts

## Prompt 1 of 3: Policy Types & Dangerous Command Detection

### Required Reading (read these files before writing code)
- docs/phases/PHASE5B.md
- internal/types/types.go (PolicyProfile, EnforcementMode)
- internal/store/interfaces.go (AuditStore)

### Task

Create policy types and dangerous command detection.

1. `internal/policy/types.go`:
   - Define PolicyRequest struct with JSON tags
   - Define PolicyDecision struct: Allowed bool, Reason string, Violations []Violation, EnforcementMode
   - Define Violation struct: Type ViolationType, Detail string, Severity ViolationSeverity
   - Define ViolationType constants: ViolationDenylist, ViolationDangerousPattern, ViolationSchemaViolation, ViolationAllowlist
   - Define ViolationSeverity constants: SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical

2. `internal/policy/dangerous.go`:
   - Define DefaultDangerousPatterns []string with all patterns from the spec
   - CompilePatterns(patterns []string) ([]*regexp.Regexp, error): compile all patterns, return error on invalid regex
   - CheckDangerous(args map[string]any, patterns []*regexp.Regexp) []Violation:
     - Extract all string values from args recursively (handle nested maps and slices)
     - Test each string against all patterns
     - Return violations with pattern detail and Critical severity

3. Tests:
   - `internal/policy/dangerous_test.go`: Table-driven tests for each dangerous pattern, test benign commands, test nested args, test non-string values ignored

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All dangerous patterns detect their targets
- Recursive argument inspection works
- Benign commands don't trigger false positives

---

## Prompt 2 of 3: Policy Engine & Schema Validation

### Required Reading (read these files before writing code)
- docs/phases/PHASE5B.md
- internal/policy/types.go
- internal/policy/dangerous.go
- internal/types/types.go

### Task

Implement the policy engine and basic schema validation.

1. `internal/policy/engine.go`:
   - Define Engine struct with fields for profiles, compiled patterns, audit store, logger
   - NewEngine constructor: compile dangerous patterns, store profiles
   - Evaluate(ctx context.Context, req PolicyRequest) (*PolicyDecision, error):
     - Look up profile by req.ProfileName (fall back to "default")
     - Check allowlist: if profile has non-empty ToolAllowlist and tool not in it -> violation
     - Check denylist: if tool in profile's ToolDenylist -> violation
     - Check dangerous patterns on arguments
     - Check schema validation if schema provided
     - Build decision: Allowed = len(violations) == 0, set EnforcementMode from profile
     - If audit mode and violations exist: Allowed = true (log only)
     - Log decision via audit logger

2. `internal/policy/schema.go`:
   - ValidateArguments(args map[string]any, schema json.RawMessage) []Violation:
     - Parse schema as map[string]any
     - Check "required" fields present in args
     - Basic type validation if "properties" specifies types
     - Return violations for mismatches
     - Return empty if schema is nil or empty

3. Tests:
   - `internal/policy/engine_test.go`: Test allow (no violations), test denylist blocks, test allowlist blocks, test audit mode allows with violations, test combined checks
   - `internal/policy/schema_test.go`: Test required field missing, test type mismatch, test valid args, test nil schema

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Engine evaluates all policy dimensions
- Enforcement mode correctly controls blocking vs logging
- Schema validation catches basic violations

---

## Prompt 3 of 3: Audit Logging & Policy Middleware

### Required Reading (read these files before writing code)
- docs/phases/PHASE5B.md
- internal/policy/engine.go
- internal/store/interfaces.go
- internal/identity/context.go
- internal/server/server.go

### Task

Implement audit logging and policy middleware.

1. `internal/policy/audit.go`:
   - LogDecision(ctx context.Context, store store.AuditStore, req PolicyRequest, decision *PolicyDecision) error:
     - Build AuditEntry with event_type="policy_decision"
     - Set client_id from request
     - Set server_name from tool name
     - Details: {tool, allowed, enforcement_mode, violations, arguments (sanitized)}
     - Call store.Log

2. `internal/policy/middleware.go`:
   - PolicyMiddleware(engine *Engine, logger *slog.Logger) func(http.Handler) http.Handler:
     - Parse request to extract tool name and arguments (for MCP tool call requests)
     - Build PolicyRequest from identity context and request body
     - Call engine.Evaluate
     - If decision.Allowed: set X-Policy-Decision header, call next
     - If not allowed (enforce mode): return 403 with JSON error including violations
     - Log decision details

3. Wire into server:
   - Add PolicyMiddleware to the tool call route (after auth, after rate limiting)

4. Tests:
   - `internal/policy/audit_test.go`: Mock AuditStore, verify Log called with correct entry
   - `internal/policy/middleware_test.go`: Test allowed request passes, test denied request returns 403, test audit mode passes with header

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Audit entries contain full decision context
- Middleware enforces policy on tool calls
- 403 response includes violation details
- >=80% coverage across policy package
