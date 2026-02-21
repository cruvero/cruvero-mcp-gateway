# Phase 14A: Code Mode — goja Sandbox, search() & execute() Tools

## Overview

Implement server-side Code Mode using the Cloudflare pattern adapted for Go. Instead of V8 isolates, use goja (pure-Go ECMAScript 5.1+ runtime). Two meta-tools (`mcpgw.search` and `mcpgw.execute`) replace the full tool catalog at ~500 tokens total. The LLM writes JavaScript to discover tools and compose multi-tool workflows in a single execution, reducing context window usage by 99%+.

## Scope

### goja Sandbox (`internal/codemode/vm.go`)

Create a sandboxed JavaScript execution environment with strict security boundaries:

```go
type VMConfig struct {
    Timeout     time.Duration // Max execution time (default 5s)
    MaxMemoryMB int           // Approximate memory limit (default 64MB)
}

type VM struct {
    runtime *goja.Runtime
    config  VMConfig
    logger  *slog.Logger
}
```

- `NewVM(cfg VMConfig, logger *slog.Logger) *VM`:
  - Create `goja.Runtime`.
  - **Remove dangerous globals**: delete `Proxy`, `Reflect` from global scope (goja doesn't have them by default, but verify).
  - Set up interrupt handler for timeout enforcement.
  - No `require()` — goja doesn't have one by default, but ensure no module loading is possible.

- `Execute(ctx context.Context, code string, globals map[string]interface{}) (interface{}, error)`:
  1. Create a new goja.Runtime per execution (isolation between requests).
  2. Set up timeout interrupt:
     ```go
     timer := time.AfterFunc(cfg.Timeout, func() {
         runtime.Interrupt("execution timeout exceeded")
     })
     defer timer.Stop()
     ```
  3. Inject globals: for each key-value in `globals`, call `runtime.Set(key, value)`.
  4. Wrap the user code in an async-like invocation (goja is synchronous, so wrap as IIFE):
     ```go
     wrappedCode := fmt.Sprintf("(function() { var __fn = %s; return __fn(); })()", code)
     ```
  5. Execute via `runtime.RunString(wrappedCode)`.
  6. Export the result to a Go value via `result.Export()`.
  7. On interrupt: return `ErrTimeout` error.
  8. On panic recovery: return sanitized error (never expose internal Go state).

**Security constraints enforced by goja's architecture:**
- No filesystem access (goja has no `fs` module).
- No network access (goja has no `fetch`, `XMLHttpRequest`, `net`).
- No process spawning (no `child_process`, `os.exec`).
- No `require()` or `import` (no module system).
- No `eval()` of external code beyond what's in the sandbox (goja's `eval` only works within the runtime).
- Each execution gets a fresh runtime (no state leaks between requests).

### Tool Catalog Snapshot (`internal/codemode/catalog.go`)

Build a catalog snapshot for the search tool:

```go
type CatalogEntry struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Server      string         `json:"server"`
    InputSchema map[string]any `json:"inputSchema"`
    RiskLevel   string         `json:"riskLevel"`
}

func BuildCatalog(
    index *registration.CapabilityIndex,
    classStore store.ToolClassificationStore,
    toolDefs map[string]json.RawMessage, // cached tool definitions from proxy layer
) ([]CatalogEntry, error)
```

- Iterate over all tools in the capability index.
- For each tool, look up the classification (risk level).
- Build a slice of `CatalogEntry` with all fields.
- This snapshot is injected into the search VM as the `catalog` global variable.
- Cache the snapshot with a 1-minute TTL (avoid rebuilding on every search call).

### Search Handler (`internal/codemode/search.go`)

Handles the `mcpgw.search` tool:

```go
type SearchHandler struct {
    vmConfig VMConfig
    catalog  *CatalogCache // TTL-cached catalog snapshot
    logger   *slog.Logger
}

func (h *SearchHandler) Handle(ctx context.Context, args map[string]any) (any, error) {
    code, ok := args["code"].(string)
    if !ok || code == "" {
        return nil, fmt.Errorf("'code' argument is required and must be a string")
    }
    
    // Build catalog snapshot
    catalog, err := h.catalog.Get(ctx)
    if err != nil {
        return nil, fmt.Errorf("build catalog: %w", err)
    }
    
    // Execute in sandbox
    vm := NewVM(h.vmConfig, h.logger)
    result, err := vm.Execute(ctx, code, map[string]interface{}{
        "catalog": catalog,
    })
    if err != nil {
        return nil, fmt.Errorf("search execution: %w", err)
    }
    
    return result, nil
}
```

### Execute Handler (`internal/codemode/execute.go`)

Handles the `mcpgw.execute` tool — the critical security integration point:

```go
type ExecuteHandler struct {
    vmConfig    VMConfig
    proxy       ToolCaller   // interface to call tools through the full policy pipeline
    logger      *slog.Logger
}

// ToolCaller abstracts calling a tool through the gateway's policy pipeline.
type ToolCaller interface {
    CallTool(ctx context.Context, toolName string, args map[string]any, identity *identity.Identity) (any, error)
}

func (h *ExecuteHandler) Handle(ctx context.Context, args map[string]any, callerIdentity *identity.Identity) (any, error) {
    code, ok := args["code"].(string)
    if !ok || code == "" {
        return nil, fmt.Errorf("'code' argument is required and must be a string")
    }
    
    // Create callTool function that goes through the FULL policy pipeline
    callTool := func(call goja.FunctionCall) goja.Value {
        runtime := call.This.Runtime() // get the runtime from call context
        
        toolName := call.Argument(0).String()
        toolArgs := call.Argument(1).Export()
        
        argsMap, ok := toolArgs.(map[string]interface{})
        if !ok {
            panic(runtime.NewGoError(fmt.Errorf("tool arguments must be an object")))
        }
        
        // CRITICAL: This goes through the full policy pipeline:
        // 1. Classification check (destructive → blocked)
        // 2. Allowlist/denylist
        // 3. Dangerous pattern detection
        // 4. Schema validation
        // 5. Rate limiting (counts against the caller's limits)
        result, err := h.proxy.CallTool(ctx, toolName, argsMap, callerIdentity)
        if err != nil {
            panic(runtime.NewGoError(err))
        }
        
        return runtime.ToValue(result)
    }
    
    vm := NewVM(h.vmConfig, h.logger)
    result, err := vm.Execute(ctx, code, map[string]interface{}{
        "callTool": callTool,
    })
    if err != nil {
        return nil, fmt.Errorf("execute: %w", err)
    }
    
    return result, nil
}
```

**Security invariant**: `callTool()` is NOT a direct backend call. It routes through the same policy pipeline as a regular `tools/call` request. This means:
- Destructive tools are blocked.
- Rate limits are enforced (each `callTool()` counts as one request).
- Allow/deny lists are checked.
- Arguments are validated against schemas.
- All calls are audit-logged.
- The caller's identity determines which policy profile applies.

### MCP Tool Registration (`internal/proxy/server.go`)

Register the Code Mode tools on the MCP server:

```go
if cfg.CodeModeEnabled {
    // Register mcpgw.search
    mcpServer.AddTool(mcp.Tool{
        Name:        "mcpgw.search",
        Description: "Search the tool catalog. Write JavaScript to filter/inspect tool definitions. Available globals: `catalog` (array of {name, description, server, inputSchema, riskLevel}).",
        InputSchema: mcp.ToolInputSchema{
            Type: "object",
            Properties: map[string]mcp.Property{
                "code": {Type: "string", Description: "JavaScript function body. Has access to `catalog` array. Return filtered results."},
            },
            Required: []string{"code"},
        },
    }, searchHandler.Handle)
    
    // Register mcpgw.execute
    mcpServer.AddTool(mcp.Tool{
        Name:        "mcpgw.execute",
        Description: "Execute tool calls. Write JavaScript to call tools and compose results. Available globals: `callTool(name, args)` returns tool result.",
        InputSchema: mcp.ToolInputSchema{
            Type: "object",
            Properties: map[string]mcp.Property{
                "code": {Type: "string", Description: "JavaScript function body. Use `callTool(name, args)` to invoke tools. Return composed results."},
            },
            Required: []string{"code"},
        },
    }, executeHandler.Handle)
}
```

### Config (`internal/config/config.go`)

- `MCPGW_CODE_MODE_ENABLED` (default `false`)
- `MCPGW_CODE_MODE_TIMEOUT` (default `5s`)
- `MCPGW_CODE_MODE_MAX_MEMORY_MB` (default `64`)
- `MCPGW_CODE_MODE_ONLY` (default `false`) — when true, `tools/list` returns ONLY Code Mode tools (hides full catalog)

### Code Mode + Full Catalog Coexistence

When `CODE_MODE_ONLY=false` (default): `tools/list` returns the full catalog PLUS the two Code Mode tools. Clients can use either approach.

When `CODE_MODE_ONLY=true`: `tools/list` returns ONLY `mcpgw.search` and `mcpgw.execute`. The full catalog is still accessible via `mcpgw.search` JavaScript code. This maximizes token savings.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/codemode/vm.go` | goja VM creation with sandbox restrictions |
| `internal/codemode/vm_test.go` | VM tests: timeout, security, isolation |
| `internal/codemode/catalog.go` | Tool catalog snapshot builder with cache |
| `internal/codemode/catalog_test.go` | Tests |
| `internal/codemode/search.go` | Search handler |
| `internal/codemode/search_test.go` | Tests |
| `internal/codemode/execute.go` | Execute handler with policy-enforced callTool |
| `internal/codemode/execute_test.go` | Tests |
| `internal/proxy/server.go` | Register Code Mode tools |
| `internal/config/config.go` | Code Mode config fields |
| `cmd/mcpgw/serve.go` | Wire Code Mode handlers |
| `go.mod` | Add goja dependency |

## Testing Requirements

- VM security: test that `require()` is not available, filesystem access fails, network access fails, `eval` is limited to sandbox scope
- VM timeout: test that code running > timeout is interrupted
- VM isolation: test that two executions don't share state
- Search: test JavaScript that filters catalog by name pattern, by risk level, by server
- Execute: test JavaScript that calls a tool via callTool and returns result
- Execute policy enforcement: test that callTool on a destructive tool returns error
- Execute rate limiting: test that callTool counts against the caller's rate limit
- Execute audit: test that each callTool invocation is audit-logged
- Catalog cache: test TTL expiry, test cache rebuild
- Integration: end-to-end test — search for tools, then execute a tool call, verify result
- CODE_MODE_ONLY: test that tools/list returns only 2 tools
- Coverage: >=80%
