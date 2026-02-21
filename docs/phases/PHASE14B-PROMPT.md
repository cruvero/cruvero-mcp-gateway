# Phase 14B Implementation Prompts

## Prompt 1 of 3: ECS Structured Logging

### Required Reading (read these files before writing code)
- docs/phases/PHASE14B.md (ECS logging section)
- cmd/mcpgw/serve.go (existing newLogger function)
- internal/config/config.go (existing config pattern)
- internal/server/middleware.go (existing log fields: method, path, status, duration, request_id)

### Task

Implement ECS structured logging with a custom slog handler that remaps field names.

1. **Add dependencies**:
   ```bash
   go get go.uber.org/zap
   go get go.uber.org/zap/exp/zapslog
   go get go.elastic.co/ecszap
   ```

2. `internal/logging/ecs_handler.go`:
   ```go
   package logging
   
   import (
       "context"
       "log/slog"
       "strings"
   )
   
   // ECS field name mappings
   var defaultFieldMap = map[string]string{
       "method":     "http.request.method",
       "path":       "url.path",
       "status":     "http.response.status_code",
       "duration":   "event.duration",
       "request_id": "http.request.id",
       "error":      "error.message",
       "client_id":  "client.id",
       "tool_name":  "event.action",
       "server_id":  "server.id",
       "user_agent": "user_agent.original",
       "remote_addr":"client.address",
       "bytes_in":   "http.request.body.bytes",
       "bytes_out":  "http.response.body.bytes",
   }
   
   // ECSRemapHandler wraps a slog.Handler and remaps field names to ECS standard.
   type ECSRemapHandler struct {
       inner    slog.Handler
       fieldMap map[string]string
   }
   
   func NewECSRemapHandler(inner slog.Handler) *ECSRemapHandler {
       return &ECSRemapHandler{inner: inner, fieldMap: defaultFieldMap}
   }
   
   func NewECSRemapHandlerWithMap(inner slog.Handler, fieldMap map[string]string) *ECSRemapHandler {
       return &ECSRemapHandler{inner: inner, fieldMap: fieldMap}
   }
   
   func (h *ECSRemapHandler) Enabled(ctx context.Context, level slog.Level) bool {
       return h.inner.Enabled(ctx, level)
   }
   
   func (h *ECSRemapHandler) Handle(ctx context.Context, record slog.Record) error {
       // Create new record with remapped attributes
       newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
       
       record.Attrs(func(a slog.Attr) bool {
           remapped := h.remapAttr(a)
           newRecord.AddAttrs(remapped)
           return true
       })
       
       return h.inner.Handle(ctx, newRecord)
   }
   
   func (h *ECSRemapHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
       remapped := make([]slog.Attr, len(attrs))
       for i, a := range attrs {
           remapped[i] = h.remapAttr(a)
       }
       return &ECSRemapHandler{inner: h.inner.WithAttrs(remapped), fieldMap: h.fieldMap}
   }
   
   func (h *ECSRemapHandler) WithGroup(name string) slog.Handler {
       return &ECSRemapHandler{inner: h.inner.WithGroup(name), fieldMap: h.fieldMap}
   }
   
   // remapAttr converts a flat key like "method" to a nested ECS group like http.request.method
   func (h *ECSRemapHandler) remapAttr(a slog.Attr) slog.Attr {
       ecsKey, ok := h.fieldMap[a.Key]
       if !ok {
           return a // Pass through unmapped fields
       }
       
       // Split dotted ECS key into nested groups
       parts := strings.Split(ecsKey, ".")
       if len(parts) == 1 {
           return slog.Attr{Key: ecsKey, Value: a.Value}
       }
       
       // Build nested groups: http.request.method -> Group("http", Group("request", Attr("method", value)))
       return buildNestedAttr(parts, a.Value)
   }
   
   func buildNestedAttr(parts []string, value slog.Value) slog.Attr {
       if len(parts) == 1 {
           return slog.Attr{Key: parts[0], Value: value}
       }
       inner := buildNestedAttr(parts[1:], value)
       return slog.Group(parts[0], inner)
   }
   ```

3. **Logger factory** — modify `newLogger()` in `cmd/mcpgw/serve.go`:
   ```go
   func newLogger(format, level string, ecsEnabled bool) (*slog.Logger, error) {
       lvl := parseLogLevel(level)
       
       if ecsEnabled {
           // zap + ecszap for ECS-compliant output
           encoderConfig := ecszap.NewDefaultEncoderConfig()
           core := ecszap.NewCore(encoderConfig, os.Stdout, zapLevelFromSlog(lvl))
           zapLogger := zap.New(core)
           
           // Bridge zap to slog
           zapHandler := zapslog.NewHandler(zapLogger.Core(), &zapslog.HandlerOptions{Level: lvl})
           
           // Wrap with ECS field remapper
           ecsHandler := logging.NewECSRemapHandler(zapHandler)
           
           return slog.New(ecsHandler), nil
       }
       
       // Standard slog handlers
       opts := &slog.HandlerOptions{Level: lvl}
       switch format {
       case "json":
           return slog.New(slog.NewJSONHandler(os.Stdout, opts)), nil
       default:
           return slog.New(slog.NewTextHandler(os.Stdout, opts)), nil
       }
   }
   ```

4. **Config** in `internal/config/config.go`:
   - Add `ECSLogging bool` (default `false`), parse `MCPGW_ECS_LOGGING`.

5. **CLI flag** in `cmd/mcpgw/main.go`:
   - Add `--ecs-logging` flag that sets `MCPGW_ECS_LOGGING=true`.

6. **Tests** `internal/logging/ecs_handler_test.go`:
   - Test field remapping: log with `method` attr → output contains `http.request.method`.
   - Test unmapped fields pass through unchanged.
   - Test nested group creation: `status` → `http.response.status_code` is properly nested.
   - Test WithAttrs remapping.
   - Test WithGroup wrapping.
   - Use a bytes.Buffer as output to capture and parse JSON.

### Verification
```bash
go test ./internal/logging/...
go vet ./...
```

### Acceptance Criteria
- ECS logging produces valid ECS-standard JSON
- Field remapping is correct for all mapped fields
- Unmapped fields pass through unchanged
- No changes to existing log call sites (40+ files)
- Toggle via env var or CLI flag

---

## Prompt 2 of 3: NATS TLS, Trivy Scanning, Dependabot

### Required Reading (read these files before writing code)
- docs/phases/PHASE14B.md (NATS TLS, CI, Dependabot sections)
- internal/events/client.go (existing NATS client, WithTLS option)
- internal/config/config.go (existing config)
- .github/workflows/image-dev.yml (existing CI workflow)

### Task

Wire NATS TLS, add container scanning, and configure Dependabot.

1. **NATS TLS config** in `internal/config/config.go`:
   - Add:
     ```go
     NATSTLSEnabled bool   `json:"nats_tls_enabled"`
     NATSTLSCert    string `json:"nats_tls_cert"`
     NATSTLSKey     string `json:"nats_tls_key"`
     NATSTLSCA      string `json:"nats_tls_ca"`
     ```
   - Parse `MCPGW_NATS_TLS_ENABLED`, `MCPGW_NATS_TLS_CERT`, `MCPGW_NATS_TLS_KEY`, `MCPGW_NATS_TLS_CA`.
   - Validate: if enabled, all three paths must be non-empty.

2. **NATS TLS wiring** in `cmd/mcpgw/serve.go`:
   ```go
   var natsOpts []events.ClientOption
   if cfg.NATSTLSEnabled {
       cert, err := tls.LoadX509KeyPair(cfg.NATSTLSCert, cfg.NATSTLSKey)
       if err != nil {
           return fmt.Errorf("load NATS TLS cert: %w", err)
       }
       caCert, err := os.ReadFile(cfg.NATSTLSCA)
       if err != nil {
           return fmt.Errorf("read NATS CA: %w", err)
       }
       caPool := x509.NewCertPool()
       if !caPool.AppendCertsFromPEM(caCert) {
           return fmt.Errorf("failed to parse NATS CA certificate")
       }
       tlsCfg := &tls.Config{
           Certificates: []tls.Certificate{cert},
           RootCAs:      caPool,
           MinVersion:   tls.VersionTLS13,
       }
       natsOpts = append(natsOpts, events.WithTLS(tlsCfg))
   }
   natsClient, err := events.NewClient(cfg.NATSURL, cfg.GatewayID, natsOpts...)
   ```

3. **Trivy scanning** — modify `.github/workflows/image-dev.yml`:
   - Add after the image build step:
     ```yaml
     - name: Run Trivy vulnerability scanner
       uses: aquasecurity/trivy-action@0.28.0
       with:
         image-ref: '${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:${{ github.sha }}'
         format: 'sarif'
         output: 'trivy-results.sarif'
         severity: 'CRITICAL,HIGH'
         exit-code: '1'
     
     - name: Upload Trivy scan results to GitHub Security
       uses: github/codeql-action/upload-sarif@v3
       if: always()
       with:
         sarif_file: 'trivy-results.sarif'
     ```

4. **Dependabot** `.github/dependabot.yml`:
   ```yaml
   version: 2
   updates:
     - package-ecosystem: "gomod"
       directory: "/"
       schedule:
         interval: "weekly"
         day: "monday"
       open-pull-requests-limit: 10
       labels:
         - "dependencies"
         - "go"
       commit-message:
         prefix: "deps"
     
     - package-ecosystem: "github-actions"
       directory: "/"
       schedule:
         interval: "weekly"
         day: "monday"
       open-pull-requests-limit: 5
       labels:
         - "dependencies"
         - "ci"
       commit-message:
         prefix: "ci"
     
     - package-ecosystem: "docker"
       directory: "/"
       schedule:
         interval: "monthly"
       open-pull-requests-limit: 3
       labels:
         - "dependencies"
         - "docker"
       commit-message:
         prefix: "docker"
   ```

5. **Helm values** additions:
   ```yaml
   # In values.yaml
   logging:
     format: json
     level: info
     ecs: false
   
   nats:
     tls:
       enabled: false
       certSecretName: ""
       # Cert/key/CA paths set via Vault secret mount
   ```
   
   Update configmap template:
   ```yaml
   MCPGW_ECS_LOGGING: {{ .Values.logging.ecs | quote }}
   MCPGW_NATS_TLS_ENABLED: {{ .Values.nats.tls.enabled | quote }}
   ```

6. **Tests:**
   - Config: test NATS TLS fields parse and validate correctly.
   - Test TLS config build from cert/key/CA (use testutil cert helpers).

### Verification
```bash
go test ./internal/config/... ./internal/events/...
go vet ./...
helm lint charts/mcpgateway
```

### Acceptance Criteria
- NATS TLS enforces TLS 1.3 minimum
- Trivy scan step is valid YAML and would run in CI
- Dependabot config covers Go modules, GitHub Actions, and Docker
- Helm values render correctly with all new fields

---

## Prompt 3 of 3: Staging/Prod Environments & Final Validation

### Required Reading (read these files before writing code)
- docs/phases/PHASE14B.md (environments section)
- deploy/argocd/applicationset.yaml (existing dev-only config)
- deploy/argocd/project.yaml (existing namespace restrictions)
- charts/mcpgateway/values-dev.yaml (existing dev overlay)
- docs/GITOPS-ROLLOUT.md (if exists, for rollout patterns)

### Task

Add staging and production environments to ArgoCD and create final validation checks.

1. **AppProject** `deploy/argocd/project.yaml`:
   - Add to destinations:
     ```yaml
     destinations:
       - namespace: cruvero-dev
         server: https://kubernetes.default.svc
       - namespace: cruvero-staging
         server: https://kubernetes.default.svc
       - namespace: cruvero-prod
         server: https://kubernetes.default.svc
     ```

2. **ApplicationSet** `deploy/argocd/applicationset.yaml`:
   - Add staging and prod entries to the list generator (see PHASE14B.md for full entries).
   - Staging and prod: `autoSync: false`, `prune: false`, `selfHeal: false` (gated deployments).
   - Add sync windows for prod (maintenance windows only):
     ```yaml
     # In project.yaml syncWindows (if supported by AppProject)
     ```

3. **Staging values** `charts/mcpgateway/values-staging.yaml`:
   ```yaml
   replicaCount: 2
   
   resources:
     limits:
       cpu: 500m
       memory: 512Mi
     requests:
       cpu: 250m
       memory: 256Mi
   
   rateLimit:
     backend: redis
   
   redis:
     enabled: true
     url: "redis://dragonfly-staging:6379"
   
   ingress:
     enabled: true
     host: gateway-staging.corp.example.com
   
   admin:
     enabled: true
   
   codeMode:
     enabled: true
   ```

4. **Production values** `charts/mcpgateway/values-prod.yaml`:
   ```yaml
   replicaCount: 3
   
   autoscaling:
     enabled: true
     minReplicas: 3
     maxReplicas: 20
     targetCPUUtilizationPercentage: 70
   
   resources:
     limits:
       cpu: "1"
       memory: 1Gi
     requests:
       cpu: 500m
       memory: 512Mi
   
   rateLimit:
     backend: redis
   
   redis:
     enabled: true
     url: "redis://dragonfly-prod:6379"
   
   ingress:
     enabled: true
     host: gateway.corp.example.com
     annotations:
       nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
       nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
       nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
   
   logging:
     ecs: true
   
   admin:
     enabled: true
   
   codeMode:
     enabled: true
   
   nats:
     tls:
       enabled: true
   
   deviceFlow:
     enabled: true
   ```

5. **Image workflows** — create `.github/workflows/image-staging.yml`:
   ```yaml
   name: Build Staging Image
   on:
     push:
       tags:
         - 'v*-rc*'  # Release candidates
   # ... similar to image-dev.yml but pushes to staging registry tag
   ```
   
   And `.github/workflows/image-prod.yml`:
   ```yaml
   name: Build Production Image
   on:
     push:
       tags:
         - 'v[0-9]+.[0-9]+.[0-9]+'  # Semver releases only
   ```

6. **Final validation script** `scripts/validate-all.sh`:
   ```bash
   #!/bin/bash
   set -euo pipefail
   
   echo "=== Build ==="
   make build
   
   echo "=== Unit Tests ==="
   make test
   
   echo "=== Vet ==="
   make vet
   
   echo "=== Lint ==="
   make lint
   
   echo "=== Helm Lint ==="
   helm lint charts/mcpgateway
   
   echo "=== Helm Template (dev) ==="
   helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml
   
   echo "=== Helm Template (staging) ==="
   helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-staging.yaml
   
   echo "=== Helm Template (prod) ==="
   helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml
   
   echo "=== All Checks Passed ==="
   ```

7. **Update MISSING-FUNCTIONALITY.md**: mark B3, B4, B6, B7 as resolved.

### Verification
```bash
bash scripts/validate-all.sh
```

### Acceptance Criteria
- ApplicationSet renders apps for dev, staging, and prod
- Staging and prod are gated (no auto-sync)
- Helm templates render cleanly for all three environments
- Image workflows trigger on correct git events (tags)
- Trivy scans run on all image builds
- Dependabot is configured
- All MISSING-FUNCTIONALITY items addressed (B3, B4, B6, B7 resolved)
- Full validation script passes
