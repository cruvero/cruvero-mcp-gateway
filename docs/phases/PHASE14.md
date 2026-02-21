# Phase 14: Code Mode Integration & Production Hardening

## Goal

Implement the Cloudflare-style Code Mode pattern using goja (pure-Go JavaScript runtime) to reduce tool catalog token consumption by 99%+, complete all remaining production hardening items (ECS logging, NATS TLS, container scanning, staging/prod environments), and perform final validation.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [14A](PHASE14A.md) | Code Mode — goja Sandbox, search() & execute() Tools | [4 prompts](PHASE14A-PROMPT.md) | codemode (new), proxy, config |
| [14B](PHASE14B.md) | Production Hardening — ECS Logging, NATS TLS, CI, Environments | [3 prompts](PHASE14B-PROMPT.md) | logging (new), config, CI, deploy |

## Dependencies

- Phase 10 (tool classification — Code Mode `execute()` must respect destructive blocks)
- Phase 11 (distributed rate limiting — Code Mode requests are rate-limited)
- Phase 12 (admin dashboard, auth — Code Mode is accessible to authenticated users)
- Phase 4A (MCP proxy layer, tool catalog aggregation)

## Deliverables

- `mcpgw.search` meta-tool: LLM writes JavaScript to search/filter the tool catalog
- `mcpgw.execute` meta-tool: LLM writes JavaScript to call tools, compose results, and filter data
- goja sandbox: no filesystem, no network, no `require()`, 5-second timeout, memory bounded
- Code Mode tools registered alongside (or instead of) the full tool catalog
- Full policy pipeline enforced for every `callTool()` invocation inside Code Mode
- ECS structured logging via zap + ecszap + slog bridge
- NATS TLS wired from config into events client
- Trivy container scanning in CI
- Staging and production ArgoCD environments + image workflows
- Dependabot configuration

## Packages Created

- `internal/codemode` — goja VM management, search/execute handlers, sandbox security

## Packages Modified

- `internal/proxy` — register Code Mode tools, expose tool catalog for search
- `internal/config` — Code Mode, ECS logging, NATS TLS config fields
- `internal/logging` (new) — ECS handler wrapper for slog
- `cmd/mcpgw` — wire Code Mode, logging, NATS TLS

## New Dependencies (go.mod)

- `github.com/dop251/goja` — pure-Go JavaScript runtime
- `go.uber.org/zap` — high-performance structured logging
- `go.uber.org/zap/exp/zapslog` — slog-to-zap bridge
- `go.elastic.co/ecszap` — ECS encoder for zap

## Success Criteria

- `tools/list` with Code Mode enabled returns exactly 2 tools (~500 tokens)
- `mcpgw.search` executes JavaScript that filters the tool catalog and returns results
- `mcpgw.execute` executes JavaScript that calls tools via `callTool()` and returns composed results
- Every `callTool()` goes through the full policy pipeline (classification, allowlist, denylist, patterns)
- Destructive tool blocked even when called through Code Mode `execute()`
- goja sandbox prevents: filesystem access, network access, `require()`, infinite loops (timeout), excessive memory
- ECS logging produces valid ECS JSON when enabled
- NATS TLS works with cert/key/CA from config
- Trivy scans run in CI on image builds
- Staging and production environments deploy via ArgoCD
- `go test ./...` passes with >=80% coverage
