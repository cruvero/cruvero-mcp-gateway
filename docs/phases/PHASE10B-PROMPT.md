# Phase 10B Implementation Prompts

## Prompt 1 of 4: Risk Level Types & Auto-Classification

### Required Reading (read these files before writing code)
- docs/phases/PHASE10B.md (full scope)
- internal/types/types.go (existing type patterns, JSON tags)
- internal/policy/types.go (existing ViolationType constants)
- LLM.md (naming conventions)

### Task

Create the tool risk classification types and the deterministic auto-classification function.

1. `internal/policy/classification.go`:
   - Define `RiskLevel` string type with constants:
     ```go
     type RiskLevel string
     
     const (
         RiskReadOnly    RiskLevel = "read_only"
         RiskWrite       RiskLevel = "write"
         RiskDestructive RiskLevel = "destructive"
         RiskUnknown     RiskLevel = "unknown"
     )
     ```
   - Define `ToolClassification` struct with JSON tags:
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
   - Add `String()` method on `RiskLevel`.
   - Add `IsValid()` method on `RiskLevel` — returns true for the four valid values.
   - Implement `AutoClassify(toolName string, toolDescription string) (RiskLevel, string)`:
     - Normalize `toolName` to lowercase. Split on `.`, `_`, `-`, `/` to extract individual words.
     - Define three pattern sets as package-level `var` slices (not hardcoded in the function — allows testing and future extension):
       ```go
       var destructiveKeywords = []string{
           "delete", "destroy", "remove", "drop", "purge",
           "terminate", "kill", "reset", "wipe", "revoke",
           "truncate", "erase", "uninstall", "dismount", "format",
       }
       var readOnlyKeywords = []string{
           "get", "list", "read", "describe", "search",
           "find", "show", "view", "check", "count",
           "query", "fetch", "lookup", "inspect", "status",
           "exists", "ping", "health", "info", "version",
       }
       var writeKeywords = []string{
           "create", "update", "edit", "post", "put",
           "patch", "set", "add", "insert", "modify",
           "write", "upload", "send", "push", "publish",
           "enable", "disable", "configure", "approve", "assign",
       }
       ```
     - Check destructive keywords first against name words. If match: return `(RiskDestructive, "name contains '<keyword>'")`.
     - Check read-only keywords against name words. If match: return `(RiskReadOnly, "name contains '<keyword>'")`.
     - Check write keywords against name words. If match: return `(RiskWrite, "name contains '<keyword>'")`.
     - If no match on name, repeat the same checks against `strings.ToLower(toolDescription)` using `strings.Contains`.
     - If still no match: return `(RiskUnknown, "no classification pattern matched")`.

2. `internal/policy/classification_test.go`:
   Table-driven tests with at least these cases:
   - `"github.delete_repo"` → `RiskDestructive`
   - `"k8s.get_pods"` → `RiskReadOnly`
   - `"github.create_pull_request"` → `RiskWrite`
   - `"github.list_issues"` → `RiskReadOnly`
   - `"k8s.terminate_pod"` → `RiskDestructive`
   - `"todoist.add_task"` → `RiskWrite`
   - `"custom_tool_xyz"` with description `"Removes expired sessions"` → `RiskDestructive` (from description)
   - `"custom_tool_abc"` with empty description → `RiskUnknown`
   - `"mcp.github.search_code"` → `RiskReadOnly` (tests dot-separated splitting)
   - `"DROP-database"` → `RiskDestructive` (tests hyphen splitting, case insensitivity)

3. Add `ViolationDestructive ViolationType = "destructive_tool"` to `internal/policy/types.go`.

### Verification
```bash
go test ./internal/policy/...
go vet ./...
```

### Acceptance Criteria
- All four risk levels properly defined with String() and IsValid()
- AutoClassify correctly categorizes tools based on name keywords
- AutoClassify falls back to description when name yields no match
- Destructive patterns take priority over read-only and write patterns
- Tool names with dots, underscores, hyphens are properly split into words
- Case insensitive matching

---

## Prompt 2 of 4: Tool Classification Store & Migration

### Required Reading (read these files before writing code)
- docs/phases/PHASE10B.md (store and migration sections)
- internal/store/interfaces.go (existing store interface patterns)
- internal/store/server_store.go (example Postgres store implementation pattern)
- internal/policy/classification.go (ToolClassification struct, RiskLevel type)
- migrations/ (existing migration naming and pattern)

### Task

Create the Postgres-backed tool classification store and database migration.

1. **Migration** `migrations/0007_tool_classifications.up.sql`:
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
   
   `migrations/0007_tool_classifications.down.sql`:
   ```sql
   DROP INDEX IF EXISTS idx_tool_classifications_risk;
   DROP TABLE IF EXISTS tool_classifications;
   ```

2. **Interface** in `internal/store/interfaces.go`:
   - Add `ToolClassificationStore` interface:
     ```go
     type ToolClassificationStore interface {
         Get(ctx context.Context, toolName string) (*policy.ToolClassification, error)
         GetAll(ctx context.Context) ([]policy.ToolClassification, error)
         GetByRiskLevel(ctx context.Context, level policy.RiskLevel) ([]policy.ToolClassification, error)
         Upsert(ctx context.Context, classification *policy.ToolClassification) error
         Delete(ctx context.Context, toolName string) error
     }
     ```
   - **Import cycle avoidance**: The store package importing policy types may create a cycle. If so, define a `ToolClassification` struct in `internal/types/types.go` instead and have both `store` and `policy` reference it. Alternatively, keep the struct in `policy` and have the store interface accept/return it (this works if `store` imports `policy` but `policy` does not import `store` — check the existing dependency direction). The existing pattern has `store` importing `types` but not `policy`. **Put `ToolClassification` and `RiskLevel` in `internal/types/types.go`** and have both `policy` and `store` reference `types`. Move them from `classification.go` to `types.go` but keep `AutoClassify()` in `policy/classification.go`.

3. **Implementation** `internal/store/tool_classification_store.go`:
   - `PostgresToolClassificationStore` struct with `db *sql.DB`.
   - `NewPostgresToolClassificationStore(db *sql.DB) *PostgresToolClassificationStore`.
   - `Get()`: query by tool_name, return `sql.ErrNoRows` as nil result with nil error (not found is not an error).
   - `GetAll()`: query all, order by tool_name.
   - `GetByRiskLevel()`: query with risk_level filter.
   - `Upsert()`: use `INSERT ... ON CONFLICT (tool_name) DO UPDATE SET risk_level = EXCLUDED.risk_level, reason = EXCLUDED.reason, auto_classified = EXCLUDED.auto_classified, updated_by = EXCLUDED.updated_by, updated_at = now()`.
   - `Delete()`: delete by tool_name.
   - All methods use parameterized queries (no string interpolation).
   - All methods accept `context.Context` as first parameter.

4. **Tests** `internal/store/tool_classification_store_test.go`:
   - Use the same test DB pattern as existing store tests (testutil helpers or sqlmock).
   - Test Upsert then Get — verify round-trip.
   - Test Upsert twice (idempotent) — verify second upsert updates fields.
   - Test GetAll returns all entries sorted.
   - Test GetByRiskLevel filters correctly.
   - Test Get for non-existent tool returns nil, nil.
   - Test Delete removes entry, subsequent Get returns nil.

### Verification
```bash
go test ./internal/store/...
go vet ./...
```

### Acceptance Criteria
- Migration creates table with CHECK constraint and index
- Down migration cleanly drops table and index
- Store CRUD operations work correctly with parameterized queries
- Upsert is idempotent (no duplicate key errors)
- Get for missing tool returns (nil, nil) not an error
- No import cycles between store/policy/types packages

---

## Prompt 3 of 4: Policy Engine Classification Check & Cache

### Required Reading (read these files before writing code)
- docs/phases/PHASE10B.md (policy engine integration and cache sections)
- internal/policy/engine.go (existing Evaluate method)
- internal/store/interfaces.go (ToolClassificationStore interface)
- internal/types/types.go (ToolClassification, RiskLevel — moved here in Prompt 2)

### Task

Integrate tool risk classification into the policy engine with an in-memory cache layer.

1. **Classification cache** `internal/policy/classification_cache.go`:
   ```go
   type cacheEntry struct {
       classification *types.ToolClassification
       cachedAt       time.Time
   }
   
   type ClassificationCache struct {
       mu      sync.RWMutex
       entries map[string]*cacheEntry
       ttl     time.Duration
   }
   
   func NewClassificationCache(ttl time.Duration) *ClassificationCache
   func (c *ClassificationCache) Get(toolName string) (*types.ToolClassification, bool)  // returns false if expired or missing
   func (c *ClassificationCache) Set(toolName string, classification *types.ToolClassification)
   func (c *ClassificationCache) Invalidate(toolName string)
   func (c *ClassificationCache) InvalidateAll()
   ```
   - Default TTL: 5 minutes.
   - `Get()`: check if entry exists and `time.Since(entry.cachedAt) < c.ttl`. If expired, delete and return false.
   - Thread-safe via `sync.RWMutex` (read lock for Get, write lock for Set/Invalidate).

2. **Engine modifications** `internal/policy/engine.go`:
   - Add fields to `Engine`:
     ```go
     classificationStore store.ToolClassificationStore
     classificationCache *ClassificationCache
     ```
   - Add setter: `SetClassificationStore(s store.ToolClassificationStore)`. When called, also initialize the cache: `e.classificationCache = NewClassificationCache(5 * time.Minute)`.
   - Modify `Evaluate()` — add classification check as **Step 0** before existing logic:
     ```go
     // Step 0: Deterministic tool risk classification (code-level, no LLM)
     if e.classificationStore != nil {
         classification := e.getClassification(ctx, req.ToolName)
         if classification != nil && classification.RiskLevel == types.RiskDestructive {
             decision := &PolicyDecision{
                 Allowed:         false,
                 Reason:          fmt.Sprintf("tool '%s' classified as destructive: %s", req.ToolName, classification.Reason),
                 Violations:      []Violation{{Type: ViolationDestructive, Detail: classification.Reason, Severity: SeverityCritical}},
                 EnforcementMode: ModeEnforce, // ALWAYS enforce for destructive, ignores profile mode
             }
             e.logDecision(ctx, req, decision)
             return decision, nil
         }
     }
     ```
   - Private helper `getClassification(ctx, toolName) *types.ToolClassification`:
     - Check cache first.
     - On cache miss, query store.
     - Cache the result (even nil results to avoid repeated DB misses — cache a sentinel).
     - Return classification or nil.
   - **Critical security invariant**: Destructive classification ALWAYS blocks, regardless of:
     - The policy profile's enforcement mode (even `audit` mode does not downgrade destructive blocks).
     - The tool being in an allowlist.
     - The client having `admin` scope.
     - The request coming through Code Mode `execute()` (Phase 14).
   - For `RiskUnknown` tools: log a warning but allow the request to proceed through remaining rules. The warning should include the tool name so admins can classify it.

3. **Tests** `internal/policy/classification_cache_test.go`:
   - Test cache hit returns classification.
   - Test cache miss returns false.
   - Test expired entry returns false and is evicted.
   - Test Invalidate removes specific entry.
   - Test InvalidateAll clears cache.
   - Test concurrent access (use `testing.T` with goroutines).

4. **Tests** `internal/policy/engine_test.go` (add to existing):
   - Test destructive tool is blocked even when:
     - Policy profile is in `audit` mode.
     - Tool is in the allowlist.
     - Client has `admin` scope.
   - Test `read_only` tool passes classification check and proceeds to existing rules.
   - Test `write` tool passes classification check.
   - Test `unknown` tool passes with warning log.
   - Test when classificationStore is nil, classification check is skipped entirely (backwards compatible).
   - Test cache is populated after first call and used on second call (mock store, verify call count).

### Verification
```bash
go test ./internal/policy/...
go vet ./...
```

### Acceptance Criteria
- Destructive tools blocked with 403 in ALL cases (no override path)
- Cache reduces DB queries (store called once, subsequent calls use cache)
- Cache respects TTL and evicts expired entries
- Engine works correctly when classification store is nil (no panic, no change in behavior)
- Unknown tools produce a warning log with tool name
- No import cycles

---

## Prompt 4 of 4: Registration Auto-Classification Hook & Wiring

### Required Reading (read these files before writing code)
- docs/phases/PHASE10B.md (registration hook section)
- internal/registration/service.go (Register method, existing flow)
- internal/policy/classification.go (AutoClassify function)
- internal/store/interfaces.go (ToolClassificationStore)
- cmd/mcpgw/serve.go (component wiring)

### Task

Auto-classify tools when MCP servers register, and wire everything together in the serve command.

1. **Registration service modification** `internal/registration/service.go`:
   - Add `classificationStore store.ToolClassificationStore` field to `Service`.
   - Add setter: `SetClassificationStore(s store.ToolClassificationStore)`.
   - In the `Register()` method, **after** the server record is successfully stored, classify tools:
     ```go
     // Auto-classify tools from newly registered server
     if s.classificationStore != nil {
         s.classifyNewTools(ctx, req.Capabilities, identity.ID)
     }
     ```
   - Implement `classifyNewTools(ctx context.Context, capabilities types.Capability, registeredBy string)`:
     ```go
     func (s *Service) classifyNewTools(ctx context.Context, cap types.Capability, registeredBy string) {
         for _, toolName := range cap.Tools {
             existing, err := s.classificationStore.Get(ctx, toolName)
             if err != nil {
                 s.logger.Error("failed to check tool classification", "tool", toolName, "error", err)
                 continue
             }
             
             // Never overwrite admin (manual) classifications
             if existing != nil && !existing.AutoClassified {
                 continue
             }
             
             level, reason := policy.AutoClassify(toolName, "") // description not available at registration time
             classification := &types.ToolClassification{
                 ToolName:       toolName,
                 RiskLevel:      level,
                 Reason:         reason,
                 AutoClassified: true,
                 UpdatedBy:      "system:auto_classify",
                 UpdatedAt:      time.Now(),
             }
             
             if err := s.classificationStore.Upsert(ctx, classification); err != nil {
                 s.logger.Error("failed to auto-classify tool", "tool", toolName, "error", err)
                 continue
             }
             
             s.logger.Info("auto-classified tool",
                 "tool", toolName,
                 "risk_level", string(level),
                 "reason", reason,
             )
         }
     }
     ```
   - Classification errors are logged but do NOT fail the registration. Tool classification is best-effort during registration.

2. **Wiring** in `cmd/mcpgw/serve.go`:
   - After creating the DB connection and running migrations:
     ```go
     classificationStore := store.NewPostgresToolClassificationStore(db)
     ```
   - Wire into policy engine:
     ```go
     srv.PolicyEngine().SetClassificationStore(classificationStore)
     ```
     (or however the engine is accessed — match the existing pattern from P0-1 audit store wiring).
   - Wire into registration service:
     ```go
     registrationService.SetClassificationStore(classificationStore)
     ```
   - Ensure the `classificationStore` is created before both the policy engine and registration service are fully initialized.

3. **Add `mcpgw tool` CLI subcommands** in `cmd/mcpgw/tool.go` (new file):
   - `mcpgw tool list` — list all tool classifications:
     - Flags: `--risk-level` (filter by risk level), `--format` (table/json, default table)
     - Connects directly to the database.
     - Output columns: tool_name, risk_level, reason, auto_classified, updated_by, updated_at
   - `mcpgw tool classify {tool-name}` — manually classify a tool:
     - Flags: `--risk-level` (required, one of: read_only, write, destructive, unknown), `--reason` (required)
     - Sets `auto_classified = false` and `updated_by` to the current OS user or a provided `--user` flag.
     - Confirms before applying (use `--force` to skip).
   - `mcpgw tool auto-classify` — re-run auto-classification on all tools that are currently `auto_classified = true`:
     - Useful after updating the keyword lists.
     - Does NOT touch manually classified tools.

4. **Tests:**
   - `internal/registration/service_test.go`:
     - Test Register with classification store: tools are auto-classified after successful registration.
     - Test Register without classification store (nil): no panic, no classification.
     - Test that existing admin classification (`auto_classified = false`) is not overwritten.
     - Test that existing auto classification IS updated on re-registration.
     - Test classification error does not fail registration.
   - `cmd/mcpgw/tool_test.go`:
     - Test `--risk-level` flag parsing and validation.
     - Test output format (at least JSON format is parseable).

### Verification
```bash
go test ./internal/registration/... ./cmd/mcpgw/...
go vet ./...
golangci-lint run ./...
```

### Acceptance Criteria
- Registering an MCP server with tools named `delete_repo`, `get_pods`, `create_pr` produces three classifications: destructive, read_only, write
- Re-registering the same server updates auto-classifications but preserves admin overrides
- Classification errors during registration are logged but don't fail the registration
- `mcpgw tool list` shows all classifications
- `mcpgw tool classify github.delete_repo --risk-level write --reason "admin override: safe in sandbox"` changes classification and sets `auto_classified = false`
- Subsequent auto-classification does not overwrite the admin override
- All components wired correctly in serve.go — end-to-end: register server → tools classified → tools/call on destructive tool → blocked
