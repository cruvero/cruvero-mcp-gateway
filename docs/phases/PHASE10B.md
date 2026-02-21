# Phase 10B: Tool Risk Classification & Policy Engine Integration

## Overview

Implement a database-backed tool risk classification system that deterministically labels every tool as `read_only`, `write`, `destructive`, or `unknown`. The policy engine checks this classification before any other rule — destructive tools are blocked immediately with no LLM involvement. Tools are auto-classified on first registration based on naming conventions, and admins can override classifications.

## Scope

### Risk Level Types (`internal/policy/classification.go`)

- Define `RiskLevel` string type with constants:
  - `RiskReadOnly` = `"read_only"` — no side effects (get, list, read, search, describe)
  - `RiskWrite` = `"write"` — creates or modifies state (create, update, edit, post)
  - `RiskDestructive` = `"destructive"` — deletes, destroys, or irreversibly modifies (delete, destroy, remove, drop, purge, terminate, kill, reset, wipe, revoke)
  - `RiskUnknown` = `"unknown"` — not yet classified, treated as `write` with audit entry
- Define `ToolClassification` struct:
  ```go
  type ToolClassification struct {
      ToolName       string    `json:"tool_name"`
      RiskLevel      RiskLevel `json:"risk_level"`
      Reason         string    `json:"reason"`
      AutoClassified bool      `json:"auto_classified"`
      UpdatedBy      string    `json:"updated_by"`
      UpdatedAt      time.Time `json:"updated_at"`
  }
  ```
- `AutoClassify(toolName string, toolDescription string) (RiskLevel, string)`:
  - Normalize tool name to lowercase.
  - Check destructive patterns first (highest priority):
    - `delete`, `destroy`, `remove`, `drop`, `purge`, `terminate`, `kill`, `reset`, `wipe`, `revoke`, `truncate`, `erase`, `uninstall`
  - Check read-only patterns:
    - `get`, `list`, `read`, `describe`, `search`, `find`, `show`, `view`, `check`, `count`, `query`, `fetch`, `lookup`, `inspect`, `status`
  - Check write patterns:
    - `create`, `update`, `edit`, `post`, `put`, `patch`, `set`, `add`, `insert`, `modify`, `write`, `upload`, `send`, `push`, `publish`, `enable`, `disable`, `configure`
  - If no pattern matches: return `RiskUnknown`.
  - Also check the description for the same patterns if the name yields `RiskUnknown`.
  - Return the risk level and a human-readable reason string (e.g., `"name contains 'delete'"`, `"description contains 'remove'"`, `"no pattern matched"`).

### Tool Classification Store (`internal/store/tool_classification_store.go`)

- Define `ToolClassificationStore` interface in `internal/store/interfaces.go`:
  ```go
  type ToolClassificationStore interface {
      Get(ctx context.Context, toolName string) (*policy.ToolClassification, error)
      GetAll(ctx context.Context) ([]policy.ToolClassification, error)
      GetByRiskLevel(ctx context.Context, level policy.RiskLevel) ([]policy.ToolClassification, error)
      Upsert(ctx context.Context, classification *policy.ToolClassification) error
      Delete(ctx context.Context, toolName string) error
  }
  ```
- Implement `PostgresToolClassificationStore`:
  - `Get()`: `SELECT * FROM tool_classifications WHERE tool_name = $1`
  - `GetAll()`: `SELECT * FROM tool_classifications ORDER BY tool_name`
  - `GetByRiskLevel()`: `SELECT * FROM tool_classifications WHERE risk_level = $1 ORDER BY tool_name`
  - `Upsert()`: `INSERT INTO tool_classifications ... ON CONFLICT (tool_name) DO UPDATE SET ...`
  - `Delete()`: `DELETE FROM tool_classifications WHERE tool_name = $1`

### Database Migration (`migrations/0007_tool_classifications.up.sql`)

```sql
CREATE TABLE IF NOT EXISTS tool_classifications (
    tool_name        TEXT PRIMARY KEY,
    risk_level       TEXT NOT NULL DEFAULT 'unknown'
                     CHECK (risk_level IN ('read_only', 'write', 'destructive', 'unknown')),
    reason           TEXT NOT NULL DEFAULT '',
    auto_classified  BOOLEAN NOT NULL DEFAULT true,
    updated_by       TEXT NOT NULL DEFAULT 'system',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tool_classifications_risk ON tool_classifications(risk_level);
```

Down migration:
```sql
DROP TABLE IF EXISTS tool_classifications;
```

### Policy Engine Integration (`internal/policy/engine.go`)

- Add `classificationStore ToolClassificationStore` field to `Engine`.
- Add `SetClassificationStore(store ToolClassificationStore)` method.
- Modify `Evaluate()` to check classification **first**, before allowlist/denylist/patterns:
  ```go
  func (e *Engine) Evaluate(ctx context.Context, req PolicyRequest) (*PolicyDecision, error) {
      // Step 0: Check tool risk classification (deterministic, code-level)
      if e.classificationStore != nil {
          classification, err := e.classificationStore.Get(ctx, req.ToolName)
          if err == nil && classification != nil {
              if classification.RiskLevel == RiskDestructive {
                  decision := &PolicyDecision{
                      Allowed:         false,
                      Reason:          fmt.Sprintf("tool '%s' is classified as destructive: %s", req.ToolName, classification.Reason),
                      Violations:      []Violation{{Type: ViolationDestructive, Detail: classification.Reason, Severity: SeverityCritical}},
                      EnforcementMode: ModeEnforce, // Always enforce, never audit-only for destructive
                  }
                  e.logDecision(ctx, req, decision)
                  return decision, nil
              }
          }
          // If unknown, log for admin attention but don't block
          if err == nil && classification != nil && classification.RiskLevel == RiskUnknown {
              e.logger.Warn("tool has unknown risk classification",
                  "tool_name", req.ToolName,
                  "action", "allowing with audit")
          }
      }
      
      // Step 1-4: Existing evaluation (allowlist, denylist, patterns, schema)
      // ... existing code ...
  }
  ```
- Add `ViolationDestructive` constant to violation types.
- **Critical**: Destructive classification ALWAYS enforces, even if the policy profile is in `audit` mode. This is a hard security boundary.

### Registration Hook (`internal/registration/service.go`)

- After a successful registration, iterate over the server's declared tool capabilities.
- For each tool name, check if a classification exists in the store.
- If no classification exists, call `AutoClassify()` and insert the result via `Upsert()`.
- If a classification already exists and `auto_classified == false` (admin override), do NOT overwrite.
- If a classification exists and `auto_classified == true`, update it (tool description may have changed).
- Log new classifications at INFO level.

### In-Memory Classification Cache (`internal/policy/classification_cache.go`)

To avoid a DB query on every `tools/call`, add a simple in-memory cache:

- `ClassificationCache` struct with `sync.RWMutex`, `map[string]*ToolClassification`, `ttl time.Duration`.
- `Get(toolName string) (*ToolClassification, bool)` — returns cached entry if not expired.
- `Set(toolName string, classification *ToolClassification)` — cache with timestamp.
- `Invalidate(toolName string)` — remove from cache (called when admin updates classification).
- `InvalidateAll()` — clear entire cache.
- Default TTL: 5 minutes.
- The engine checks cache first, falls back to store, and caches the result.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/policy/classification.go` | RiskLevel types, AutoClassify function, ToolClassification struct |
| `internal/policy/classification_test.go` | Tests for AutoClassify with various tool names |
| `internal/policy/classification_cache.go` | In-memory classification cache with TTL |
| `internal/policy/classification_cache_test.go` | Cache tests |
| `internal/store/tool_classification_store.go` | Postgres implementation of ToolClassificationStore |
| `internal/store/tool_classification_store_test.go` | Store tests |
| `internal/store/interfaces.go` | Add ToolClassificationStore interface |
| `internal/policy/engine.go` | Add classification check as first evaluation step |
| `internal/policy/engine_test.go` | Tests for destructive blocking, unknown logging |
| `internal/policy/types.go` | Add ViolationDestructive constant |
| `internal/registration/service.go` | Auto-classify tools on registration |
| `internal/registration/service_test.go` | Tests for auto-classification hook |
| `cmd/mcpgw/serve.go` | Wire classification store into engine and registration service |
| `migrations/0007_tool_classifications.up.sql` | Create tool_classifications table |
| `migrations/0007_tool_classifications.down.sql` | Drop table |

## Testing Requirements

- AutoClassify: table-driven tests for every pattern category — destructive names, read-only names, write names, unknown names, description fallback
- Classification store: CRUD operations, upsert idempotency, GetByRiskLevel filtering
- Policy engine: destructive tool always blocked (even in audit mode), read_only tool allowed, unknown tool allowed with warning log, missing classification (no store) passes through to existing rules
- Cache: TTL expiry, invalidation, cache hit/miss
- Registration hook: new tool auto-classified, existing admin override not overwritten, existing auto-classification updated
- Coverage: >=80% on all new and modified packages
