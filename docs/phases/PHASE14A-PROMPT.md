# Phase 14A Implementation Prompts

## Prompt 1 of 4: goja Sandbox VM

### Required Reading (read these files before writing code)
- docs/phases/PHASE14A.md (full scope, VM security section)
- LLM.md (conventions)
- https://github.com/dop251/goja (goja README for API reference — read if accessible, otherwise use knowledge of the library)

### Task

Create the sandboxed JavaScript VM using goja.

1. **Add dependency**: `go get github.com/dop251/goja`

2. `internal/codemode/vm.go`:
   ```go
   package codemode
   
   import (
       "context"
       "fmt"
       "sync"
       "time"
       
       "github.com/dop251/goja"
   )
   
   var (
       ErrTimeout       = fmt.Errorf("execution timeout exceeded")
       ErrMemoryLimit   = fmt.Errorf("memory limit exceeded")
       ErrSecurityBlock = fmt.Errorf("operation not permitted in sandbox")
   )
   
   type VMConfig struct {
       Timeout     time.Duration // Default 5s
       MaxMemoryMB int           // Default 64
   }
   
   func DefaultVMConfig() VMConfig {
       return VMConfig{
           Timeout:     5 * time.Second,
           MaxMemoryMB: 64,
       }
   }
   ```

   - `Execute(ctx context.Context, code string, globals map[string]interface{}) (interface{}, error)`:
     1. Create a **new** `goja.Runtime` per call (isolation guarantee).
     2. Set up timeout interrupt:
        ```go
        runtime := goja.New()
        
        // Timeout enforcement
        timer := time.AfterFunc(cfg.Timeout, func() {
            runtime.Interrupt(ErrTimeout)
        })
        defer timer.Stop()
        ```
     3. Remove potentially dangerous globals (goja provides limited globals, but be explicit):
        ```go
        // goja doesn't provide Node.js globals, but be defensive
        runtime.Set("global", goja.Undefined())
        runtime.Set("globalThis", goja.Undefined())
        ```
     4. Inject user globals:
        ```go
        for key, val := range globals {
            if err := runtime.Set(key, val); err != nil {
                return nil, fmt.Errorf("set global '%s': %w", key, err)
            }
        }
        ```
     5. Wrap user code as IIFE:
        ```go
        // The user provides an arrow function body, wrap it for execution
        wrappedCode := fmt.Sprintf("(function() { var __fn = %s; return __fn(); })()", code)
        ```
     6. Execute with panic recovery:
        ```go
        var result goja.Value
        var execErr error
        
        func() {
            defer func() {
                if r := recover(); r != nil {
                    if gojaErr, ok := r.(*goja.InterruptedError); ok {
                        execErr = fmt.Errorf("%w: %v", ErrTimeout, gojaErr.Value())
                    } else {
                        execErr = fmt.Errorf("sandbox panic: %v", r)
                    }
                }
            }()
            result, execErr = runtime.RunString(wrappedCode)
        }()
        ```
     7. Export result:
        ```go
        if execErr != nil {
            return nil, execErr
        }
        if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
            return nil, nil
        }
        return result.Export(), nil
        ```

   - `ValidateCode(code string) error`:
     - Basic syntax validation: attempt to compile without executing.
     - `goja.Compile("", code, false)` — returns parse error if invalid JS.
     - Also check for obvious escape attempts (not a security boundary, just a helper):
       - Reject code containing `require(`, `import `, `process.`, `__proto__`.

3. `internal/codemode/vm_test.go`:
   - **Test basic execution**: `() => { return 1 + 1; }` → returns `2`.
   - **Test object return**: `() => { return {a: 1, b: "hello"}; }` → returns map.
   - **Test array return**: `() => { return [1, 2, 3]; }` → returns slice.
   - **Test timeout**: `() => { while(true) {} }` → returns `ErrTimeout`.
   - **Test global injection**: inject `catalog` array, JS filters it, returns subset.
   - **Test function injection**: inject Go function as `callTool`, JS calls it, returns result.
   - **Test isolation**: execute twice, verify second execution doesn't see state from first.
   - **Test no require**: `() => { return require('fs'); }` → runtime error (require not defined).
   - **Test no fetch**: `() => { return fetch('http://evil.com'); }` → runtime error.
   - **Test no process**: `() => { return process.env; }` → runtime error.
   - **Test syntax error**: `() => { return ` → returns parse error.
   - **Test exception handling**: `() => { throw new Error('test'); }` → returns error with message.
   - **Test nested function call**: inject `callTool`, call it from JS, verify Go function called with correct args.
   - **Test large output**: JS returns a large array — verify no crash.
   - **Test concurrent execution**: 10 goroutines each execute code simultaneously — no data races.

### Verification
```bash
go test ./internal/codemode/... -race
go vet ./...
```

### Acceptance Criteria
- Fresh runtime per execution (no state leaks)
- Timeout interrupts infinite loops
- No access to require, fetch, process, fs, or any Node.js/browser APIs
- Go functions injected as globals are callable from JS
- Concurrent executions are safe (no data races)
- Parse errors return descriptive messages

---

## Prompt 2 of 4: Catalog Snapshot & Search Handler

### Required Reading (read these files before writing code)
- docs/phases/PHASE14A.md (catalog and search sections)
- internal/codemode/vm.go (VM Execute method)
- internal/registration/index.go (CapabilityIndex, ListTools)
- internal/store/interfaces.go (ToolClassificationStore)
- internal/proxy/tools.go (tool definition cache, if exists)

### Task

Build the tool catalog snapshot and the search handler.

1. `internal/codemode/catalog.go`:
   ```go
   type CatalogEntry struct {
       Name        string                 `json:"name"`
       Description string                 `json:"description"`
       Server      string                 `json:"server"`
       InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
       RiskLevel   string                 `json:"riskLevel"`
   }
   
   // CatalogProvider builds catalog snapshots from the gateway's live state.
   type CatalogProvider struct {
       index      *registration.CapabilityIndex
       classStore store.ToolClassificationStore
       toolDefs   ToolDefinitionProvider // interface to get tool schemas
       logger     *slog.Logger
   }
   
   // ToolDefinitionProvider abstracts access to tool definitions (schemas).
   type ToolDefinitionProvider interface {
       GetToolDefinition(toolName string) (map[string]interface{}, error)
   }
   
   func (p *CatalogProvider) Build(ctx context.Context) ([]CatalogEntry, error)
   ```
   
   - `Build()`:
     1. Get all tool names from capability index (`ListTools()`).
     2. For each tool, get the owning server(s) from the index (`LookupTool(name)`).
     3. For each tool, get the classification from the classification store.
     4. For each tool, get the schema from the tool definition provider (may be cached in the proxy layer).
     5. Build `CatalogEntry` for each.
     6. Return sorted by tool name.

   - `CatalogCache` — TTL-cached catalog:
     ```go
     type CatalogCache struct {
         provider *CatalogProvider
         mu       sync.RWMutex
         catalog  []CatalogEntry
         builtAt  time.Time
         ttl      time.Duration // default 1 minute
     }
     
     func NewCatalogCache(provider *CatalogProvider, ttl time.Duration) *CatalogCache
     func (c *CatalogCache) Get(ctx context.Context) ([]CatalogEntry, error)
     func (c *CatalogCache) Invalidate()
     ```

2. `internal/codemode/search.go`:
   ```go
   type SearchHandler struct {
       vmConfig VMConfig
       catalog  *CatalogCache
       logger   *slog.Logger
   }
   
   func NewSearchHandler(cfg VMConfig, catalog *CatalogCache, logger *slog.Logger) *SearchHandler
   
   func (h *SearchHandler) Handle(ctx context.Context, args map[string]interface{}) (interface{}, error) {
       code, ok := args["code"].(string)
       if !ok || code == "" {
           return nil, fmt.Errorf("'code' argument is required and must be a string")
       }
       
       catalog, err := h.catalog.Get(ctx)
       if err != nil {
           return nil, fmt.Errorf("build catalog: %w", err)
       }
       
       result, err := Execute(ctx, h.vmConfig, code, map[string]interface{}{
           "catalog": catalog,
       })
       if err != nil {
           return nil, fmt.Errorf("search execution failed: %w", err)
       }
       
       return result, nil
   }
   ```

3. **Tests:**
   - `internal/codemode/catalog_test.go`:
     - Test Build with mock index and classification store.
     - Test cache TTL: first call builds, second uses cache, third (after TTL) rebuilds.
     - Test cache invalidation.
   - `internal/codemode/search_test.go`:
     - Test search that filters by name pattern:
       ```javascript
       () => { return catalog.filter(t => t.name.includes('github')); }
       ```
     - Test search that filters by risk level:
       ```javascript
       () => { return catalog.filter(t => t.riskLevel === 'read_only'); }
       ```
     - Test search that returns specific fields:
       ```javascript
       () => { return catalog.map(t => ({name: t.name, risk: t.riskLevel})); }
       ```
     - Test search that inspects input schemas:
       ```javascript
       () => {
           var tool = catalog.find(t => t.name === 'github.list_issues');
           return tool ? tool.inputSchema : null;
       }
       ```
     - Test search with empty catalog returns empty array.
     - Test search with invalid code returns error.

### Verification
```bash
go test ./internal/codemode/...
go vet ./...
```

### Acceptance Criteria
- Catalog snapshot includes all tools with classifications and schemas
- Cache avoids rebuilding on every request
- Search executes JavaScript against the catalog
- Search results are exported as clean JSON-serializable Go values
- Invalid code returns descriptive errors

---

## Prompt 3 of 4: Execute Handler with Policy-Enforced callTool

### Required Reading (read these files before writing code)
- docs/phases/PHASE14A.md (execute handler section, security invariant)
- internal/codemode/vm.go (VM Execute method)
- internal/proxy/router.go (existing tool routing for reference)
- internal/policy/engine.go (Evaluate method)
- internal/identity/context.go (Identity type)

### Task

Implement the execute handler where every `callTool()` invocation goes through the full policy pipeline.

1. **ToolCaller interface** `internal/codemode/execute.go`:
   ```go
   // ToolCaller calls a tool through the gateway's full policy pipeline.
   // This is NOT a direct backend call — it goes through classification,
   // allowlist/denylist, pattern detection, schema validation, rate limiting,
   // and audit logging.
   type ToolCaller interface {
       CallTool(ctx context.Context, toolName string, args map[string]interface{}, callerIdentity *identity.Identity) (interface{}, error)
   }
   ```

2. **ProxyToolCaller** — adapter that wraps the existing proxy layer:
   ```go
   // ProxyToolCaller adapts the gateway's proxy/policy layer to the ToolCaller interface.
   type ProxyToolCaller struct {
       policyEngine *policy.Engine
       proxyRouter  *proxy.Router
       auditStore   store.AuditStore
       logger       *slog.Logger
   }
   
   func NewProxyToolCaller(engine *policy.Engine, router *proxy.Router, auditStore store.AuditStore, logger *slog.Logger) *ProxyToolCaller
   
   func (c *ProxyToolCaller) CallTool(ctx context.Context, toolName string, args map[string]interface{}, callerIdentity *identity.Identity) (interface{}, error) {
       // 1. Resolve policy profile for caller
       profile := resolveProfile(callerIdentity)
       
       // 2. Evaluate policy (classification check, allowlist, denylist, patterns, schema)
       decision, err := c.policyEngine.Evaluate(ctx, policy.PolicyRequest{
           ToolName:    toolName,
           Arguments:   args,
           ClientID:    callerIdentity.ID,
           ProfileName: profile,
       })
       if err != nil {
           return nil, fmt.Errorf("policy evaluation: %w", err)
       }
       if !decision.Allowed {
           return nil, fmt.Errorf("policy denied: %s", decision.Reason)
       }
       
       // 3. Route to backend
       result, err := c.proxyRouter.Route(ctx, toolName, args)
       if err != nil {
           return nil, fmt.Errorf("tool call failed: %w", err)
       }
       
       // 4. Audit the result
       c.auditToolCallResult(ctx, toolName, callerIdentity.ID, err)
       
       return result, nil
   }
   ```

3. **ExecuteHandler** `internal/codemode/execute.go`:
   ```go
   type ExecuteHandler struct {
       vmConfig VMConfig
       caller   ToolCaller
       logger   *slog.Logger
   }
   
   func NewExecuteHandler(cfg VMConfig, caller ToolCaller, logger *slog.Logger) *ExecuteHandler
   
   func (h *ExecuteHandler) Handle(ctx context.Context, args map[string]interface{}, callerIdentity *identity.Identity) (interface{}, error) {
       code, ok := args["code"].(string)
       if !ok || code == "" {
           return nil, fmt.Errorf("'code' argument is required")
       }
       
       // Validate code before execution
       if err := ValidateCode(code); err != nil {
           return nil, fmt.Errorf("invalid code: %w", err)
       }
       
       // Create callTool bridge function
       // This is a synchronous function (goja is single-threaded)
       callToolFn := func(toolName string, toolArgs map[string]interface{}) (interface{}, error) {
           h.logger.Debug("code mode callTool",
               "tool", toolName,
               "caller", callerIdentity.ID,
           )
           return h.caller.CallTool(ctx, toolName, toolArgs, callerIdentity)
       }
       
       // For goja, we need to wrap this as a goja-compatible function
       gojaCallTool := func(call goja.FunctionCall, runtime *goja.Runtime) goja.Value {
           if len(call.Arguments) < 2 {
               panic(runtime.NewGoError(fmt.Errorf("callTool requires 2 arguments: name and args")))
           }
           
           name := call.Argument(0).String()
           argsExported := call.Argument(1).Export()
           
           argsMap, ok := argsExported.(map[string]interface{})
           if !ok {
               panic(runtime.NewGoError(fmt.Errorf("callTool args must be an object")))
           }
           
           result, err := callToolFn(name, argsMap)
           if err != nil {
               panic(runtime.NewGoError(err))
           }
           
           return runtime.ToValue(result)
       }
       
       result, err := Execute(ctx, h.vmConfig, code, map[string]interface{}{
           "callTool": gojaCallTool,
       })
       if err != nil {
           return nil, fmt.Errorf("execute failed: %w", err)
       }
       
       return result, nil
   }
   ```

4. **Tests** `internal/codemode/execute_test.go`:
   - **Test basic tool call**:
     - Mock ToolCaller that returns `{"result": "success"}`.
     - JS: `() => { return callTool("test.tool", {arg1: "hello"}); }`
     - Verify result matches mock return value.
   
   - **Test multi-tool composition**:
     - Mock ToolCaller with different responses per tool.
     - JS:
       ```javascript
       () => {
           var issues = callTool("github.list_issues", {repo: "test"});
           var filtered = issues.filter(function(i) { return i.state === "open"; });
           return {count: filtered.length, issues: filtered};
       }
       ```
   
   - **Test destructive tool blocked**:
     - Mock ToolCaller that returns error for destructive tools.
     - JS: `() => { return callTool("github.delete_repo", {repo: "test"}); }`
     - Verify error contains "policy denied".
   
   - **Test rate limiting**:
     - Mock ToolCaller that counts calls.
     - JS with loop calling tool 100 times.
     - Verify ToolCaller was called 100 times (rate limiting is enforced per call).
   
   - **Test error handling in JS**:
     ```javascript
     () => {
         try {
             return callTool("failing.tool", {});
         } catch(e) {
             return {error: e.message};
         }
     }
     ```
   
   - **Test caller identity passed through**:
     - Mock ToolCaller that captures identity.
     - Verify identity from the original request is passed to every callTool.

### Verification
```bash
go test ./internal/codemode/... -race
go vet ./...
```

### Acceptance Criteria
- Every callTool goes through full policy pipeline
- Destructive tools blocked even through Code Mode
- Caller identity preserved for rate limiting and audit
- Tool call errors propagate to JavaScript as catchable exceptions
- Multi-tool composition works within a single execute()
- No data races with concurrent execute() calls

---

## Prompt 4 of 4: MCP Registration, Config, Wiring & Integration Test

### Required Reading (read these files before writing code)
- docs/phases/PHASE14A.md (MCP tool registration, config, coexistence sections)
- internal/proxy/server.go (existing tool registration pattern)
- cmd/mcpgw/serve.go (existing wiring)
- internal/config/config.go (existing config pattern)

### Task

Register Code Mode tools on the MCP server, add config, wire everything, and write integration tests.

1. **Config** in `internal/config/config.go`:
   - Add:
     ```go
     CodeModeEnabled   bool          `json:"code_mode_enabled"`
     CodeModeTimeout   time.Duration `json:"code_mode_timeout"`
     CodeModeMaxMemMB  int           `json:"code_mode_max_mem_mb"`
     CodeModeOnly      bool          `json:"code_mode_only"`
     ```
   - Parse `MCPGW_CODE_MODE_ENABLED` (default `false`), `MCPGW_CODE_MODE_TIMEOUT` (default `5s`), `MCPGW_CODE_MODE_MAX_MEMORY_MB` (default `64`), `MCPGW_CODE_MODE_ONLY` (default `false`).
   - Validate: timeout must be > 0 and <= 30s. MaxMemMB must be > 0 and <= 256.

2. **MCP tool registration** in `internal/proxy/server.go`:
   - Add a method or modify initialization to register Code Mode tools when enabled.
   - When `CodeModeOnly=true`, filter the regular tools from `tools/list` response (only return `mcpgw.search` and `mcpgw.execute`).
   - When `CodeModeOnly=false`, include both Code Mode tools AND the full catalog.

3. **Wiring** in `cmd/mcpgw/serve.go`:
   ```go
   if cfg.CodeModeEnabled {
       vmConfig := codemode.VMConfig{
           Timeout:     cfg.CodeModeTimeout,
           MaxMemoryMB: cfg.CodeModeMaxMemMB,
       }
       
       catalogProvider := codemode.NewCatalogProvider(capabilityIndex, classificationStore, toolDefProvider, logger)
       catalogCache := codemode.NewCatalogCache(catalogProvider, 1*time.Minute)
       
       searchHandler := codemode.NewSearchHandler(vmConfig, catalogCache, logger)
       
       proxyToolCaller := codemode.NewProxyToolCaller(policyEngine, proxyRouter, auditStore, logger)
       executeHandler := codemode.NewExecuteHandler(vmConfig, proxyToolCaller, logger)
       
       proxyServer.RegisterCodeMode(searchHandler, executeHandler, cfg.CodeModeOnly)
       logger.Info("code mode enabled",
           "timeout", cfg.CodeModeTimeout,
           "code_mode_only", cfg.CodeModeOnly,
       )
   }
   ```

4. **Helm values**:
   ```yaml
   codeMode:
     enabled: false
     timeout: 5s
     maxMemoryMB: 64
     only: false  # Set to true to hide full catalog
   ```

5. **Integration test** `internal/codemode/integration_test.go`:
   Full end-to-end test:
   ```go
   func TestCodeMode_EndToEnd(t *testing.T) {
       // Set up: mock backend MCP server with tools
       // Register tools: github.list_issues (read_only), github.create_pr (write), github.delete_repo (destructive)
       
       // Test 1: Search for tools
       searchResult, err := searchHandler.Handle(ctx, map[string]interface{}{
           "code": `() => { return catalog.filter(function(t) { return t.name.includes('github'); }); }`,
       })
       assert.NoError(t, err)
       tools := searchResult.([]interface{})
       assert.Len(t, tools, 3) // All 3 github tools
       
       // Test 2: Search filtered by risk
       searchResult, err = searchHandler.Handle(ctx, map[string]interface{}{
           "code": `() => { return catalog.filter(function(t) { return t.riskLevel === 'read_only'; }); }`,
       })
       assert.NoError(t, err)
       tools = searchResult.([]interface{})
       assert.Len(t, tools, 1) // Only list_issues
       
       // Test 3: Execute a read-only tool
       execResult, err := executeHandler.Handle(ctx, map[string]interface{}{
           "code": `() => { return callTool("github.list_issues", {repo: "test"}); }`,
       }, testIdentity)
       assert.NoError(t, err)
       assert.NotNil(t, execResult)
       
       // Test 4: Execute a destructive tool → blocked
       _, err = executeHandler.Handle(ctx, map[string]interface{}{
           "code": `() => { return callTool("github.delete_repo", {repo: "test"}); }`,
       }, testIdentity)
       assert.Error(t, err)
       assert.Contains(t, err.Error(), "policy denied")
       
       // Test 5: Token count verification
       // tools/list with CodeModeOnly=true should return exactly 2 tools
       toolsList := proxyServer.ListTools()
       assert.Len(t, toolsList, 2)
       assert.Equal(t, "mcpgw.search", toolsList[0].Name)
       assert.Equal(t, "mcpgw.execute", toolsList[1].Name)
   }
   ```

### Verification
```bash
go test ./internal/codemode/... ./internal/proxy/... ./cmd/mcpgw/...
go vet ./...
golangci-lint run ./...
helm lint charts/mcpgateway
```

### Acceptance Criteria
- tools/list with Code Mode returns 2 tools (~500 tokens)
- Search discovers tools without loading the full catalog into context
- Execute calls tools through the full policy pipeline
- Destructive tools blocked even through Code Mode
- Config validation catches invalid values
- Helm values render correctly
- Integration test covers the complete search→execute flow
