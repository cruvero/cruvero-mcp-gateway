# Phase 7A: Dockerfile, Prometheus Metrics, OTel Tracing

## Overview

Create the production Dockerfile, instrument the gateway with Prometheus metrics, and add OpenTelemetry distributed tracing.

## Scope

### Dockerfile
- Multi-stage build:
  - Build stage: `golang:1.23-alpine`
  - Runtime stage: `gcr.io/distroless/static-debian12:nonroot`
- Build flags: `CGO_ENABLED=0 GOOS=linux`, `-trimpath`, `-ldflags="-s -w"`
- Cache go mod download layer
- Copy binary only to runtime stage
- Expose 8443 (HTTPS) and 9090 (metrics)
- USER nonroot:nonroot
- ENTRYPOINT ["/mcpgw", "serve"]

### Prometheus Metrics (`internal/server/metrics.go`)
- Initialize Prometheus registry
- Define metrics:
  - `mcpgw_http_requests_total` (counter): labels: method, path, status_code
  - `mcpgw_http_request_duration_seconds` (histogram): labels: method, path
  - `mcpgw_rate_limited_total` (counter): labels: client_id, route
  - `mcpgw_policy_denied_total` (counter): labels: reason, tool
  - `mcpgw_active_registrations` (gauge): labels: status
  - `mcpgw_upstream_errors_total` (counter): labels: backend, error_type
  - `mcpgw_circuit_breaker_state` (gauge): labels: backend, state
  - `mcpgw_nats_connected` (gauge): labels: none
  - `mcpgw_server_settings_applied_total` (counter): labels: server_name
  - `mcpgw_server_settings_rejected_total` (counter): labels: server_name, reason
  - `mcpgw_server_settings_version` (gauge): labels: server_name
  - `mcpgw_tool_calls_total` (counter): labels: tool, backend, status
  - `mcpgw_tool_call_duration_seconds` (histogram): labels: tool, backend
- Mount `/metrics` endpoint using promhttp.Handler
- Serve metrics on separate port (MCPGW_METRICS_ADDR, default :9090)

### Metrics Middleware (`internal/server/metrics_middleware.go`)
- HTTP middleware that records request count and duration
- Wrap response writer to capture status code
- Record in Prometheus metrics

### Metrics Integration
- Rate limiter: increment mcpgw_rate_limited_total on 429
- Policy engine: increment mcpgw_policy_denied_total on denial
- Registration: update mcpgw_active_registrations gauge on changes
- Proxy: increment mcpgw_tool_calls_total and record duration
- Circuit breaker: update mcpgw_circuit_breaker_state on transitions
- NATS events client: set mcpgw_nats_connected on connect/disconnect transitions
- Server settings apply flow: increment mcpgw_server_settings_applied_total and mcpgw_server_settings_rejected_total, and set mcpgw_server_settings_version

### OpenTelemetry Tracing (`internal/server/tracing.go`)
- Initialize OTel tracer provider with OTLP exporter
- Configure from env: OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME (default "mcpgw")
- TracingMiddleware: create span per HTTP request with attributes (method, path, status)
- Add spans in key operations:
  - Auth: span around authentication decision
  - Policy: span around policy evaluation
  - Routing: span around backend selection
  - Upstream: span around backend HTTP call (propagate trace context)
- Graceful shutdown: flush pending spans

## Files Created

| File | Description |
|------|-------------|
| Dockerfile | Multi-stage production build |
| internal/server/metrics.go | Prometheus metric definitions |
| internal/server/metrics_middleware.go | HTTP metrics middleware |
| internal/server/tracing.go | OTel tracer setup and middleware |
| internal/server/metrics_test.go | Metrics tests |
| internal/server/tracing_test.go | Tracing tests |

## Testing Requirements

- Metrics: test that requests increment counters, test histogram recording, test /metrics endpoint returns Prometheus format
- Tracing: test span creation, test context propagation, test graceful shutdown
- Dockerfile: test build succeeds (CI job)
- Coverage: >=80% for new instrumentation code
