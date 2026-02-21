# Phase 7A Implementation Prompts

## Prompt 1 of 3: Dockerfile & Build Configuration

### Required Reading (read these files before writing code)
- docs/phases/PHASE7A.md
- Makefile
- cmd/mcpgw/main.go

### Task

Create the production Dockerfile and update build configuration.

1. `Dockerfile`:
   ```dockerfile
   # syntax=docker/dockerfile:1
   FROM golang:1.23-alpine AS build
   WORKDIR /src
   COPY go.mod go.sum ./
   RUN go mod download
   COPY . .
   ENV CGO_ENABLED=0 GOOS=linux
   RUN go build -trimpath -ldflags="-s -w" -o /out/mcpgw ./cmd/mcpgw

   FROM gcr.io/distroless/static-debian12:nonroot AS runtime
   COPY --from=build /out/mcpgw /mcpgw
   EXPOSE 8443 9090
   USER nonroot:nonroot
   ENTRYPOINT ["/mcpgw"]
   CMD ["serve"]
   ```

2. Add `.dockerignore`: bin/, .git/, docs/, charts/, deploy/, *.md, .github/

3. Update Makefile:
   - Add `docker-build` target: `docker build -t mcpgw:latest .`
   - Add `docker-run` target: `docker run -p 8443:8443 -p 9090:9090 mcpgw:latest`

4. Add version injection via ldflags:
   - `-X main.version=$(git describe --tags --always)`
   - `-X main.commit=$(git rev-parse HEAD)`
   - `-X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)`

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- `docker build` succeeds
- Image runs and responds to health check
- Image is minimal (distroless base)

---

## Prompt 2 of 3: Prometheus Metrics

### Required Reading (read these files before writing code)
- docs/phases/PHASE7A.md
- internal/server/server.go
- internal/server/middleware.go
- internal/ratelimit/middleware.go
- internal/policy/middleware.go

### Task

Instrument the gateway with Prometheus metrics.

1. `internal/server/metrics.go`:
   - Define all metric variables using promauto (auto-registered):
     - httpRequestsTotal *prometheus.CounterVec
     - httpRequestDuration *prometheus.HistogramVec (buckets: .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10)
     - rateLimitedTotal *prometheus.CounterVec
     - policyDeniedTotal *prometheus.CounterVec
     - activeRegistrations *prometheus.GaugeVec
     - upstreamErrorsTotal *prometheus.CounterVec
     - circuitBreakerState *prometheus.GaugeVec
     - natsConnected prometheus.Gauge
     - serverSettingsAppliedTotal *prometheus.CounterVec
     - serverSettingsRejectedTotal *prometheus.CounterVec
     - serverSettingsVersion *prometheus.GaugeVec
     - toolCallsTotal *prometheus.CounterVec
     - toolCallDuration *prometheus.HistogramVec
   - Metric names and labels must match `docs/OVERVIEW.md` Section 12
   - Export metric variables for use by other packages
   - StartMetricsServer(addr string) *http.Server: serve /metrics on separate port

2. `internal/server/metrics_middleware.go`:
   - MetricsMiddleware(next http.Handler) http.Handler:
     - Wrap ResponseWriter to capture status code
     - Record start time
     - After handler: increment httpRequestsTotal, observe httpRequestDuration

3. Wire metrics into existing middleware:
   - Rate limiter: increment rateLimitedTotal on 429
   - Policy: increment policyDeniedTotal on denial
   - Registration: update activeRegistrations on state changes
   - Circuit breaker: update circuitBreakerState on transitions
   - Proxy router: increment toolCallsTotal, observe toolCallDuration
   - Events/NATS: set natsConnected on connect/disconnect
   - Server settings sync/apply flow: increment serverSettingsAppliedTotal and serverSettingsRejectedTotal, set serverSettingsVersion

4. `internal/server/metrics_test.go`:
   - Test MetricsMiddleware increments counters
   - Test /metrics endpoint returns expected metric names
   - Test histogram buckets

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All defined metrics exposed on /metrics
- Metrics contract matches `docs/OVERVIEW.md` Section 12
- HTTP middleware records requests
- Metrics server runs on separate port

---

## Prompt 3 of 3: OpenTelemetry Tracing

### Required Reading (read these files before writing code)
- docs/phases/PHASE7A.md
- internal/server/server.go
- internal/proxy/router.go
- internal/policy/engine.go

### Task

Add OpenTelemetry distributed tracing.

1. `internal/server/tracing.go`:
   - InitTracer(ctx context.Context, serviceName string) (func(), error):
     - Create OTLP exporter (configurable endpoint via OTEL_EXPORTER_OTLP_ENDPOINT)
     - Create TracerProvider with resource (service.name, service.version)
     - Set global TracerProvider
     - Return shutdown function
   - TracingMiddleware(tracer trace.Tracer) func(http.Handler) http.Handler:
     - Start span for each request: "HTTP {method} {path}"
     - Set attributes: http.method, http.url, http.status_code
     - End span after handler completes
   - Helper: SpanFromContext(ctx) to extract current span for adding events/attributes

2. Add tracing spans in key components:
   - Auth middleware: span "auth.authenticate" with auth type attribute
   - Policy engine: span "policy.evaluate" with tool name, decision attributes
   - Proxy router: span "proxy.route" with tool name, backend attributes
   - Backend client: span "upstream.call" with backend endpoint, propagate trace context in HTTP headers

3. `internal/server/tracing_test.go`:
   - Test InitTracer creates provider
   - Test TracingMiddleware creates spans
   - Test span attributes set correctly
   - Use OTel test SDK (sdktrace/tracetest) for assertions

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Traces created for each request
- Spans cover auth -> policy -> routing -> upstream
- Trace context propagated to backend calls
- Graceful shutdown flushes pending spans
- >=80% coverage for tracing code
