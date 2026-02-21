# Phase 15B Implementation Prompts

## Prompt 1 of 2: NATS TLS Config Fields & Connection Wiring

### Required Reading (read these files before writing code)

- `docs/phases/PHASE15B.md` (full scope)
- `internal/config/config.go` (Config struct, Load(), Validate() — see existing NATSURL field at line 43)
- `internal/server/server.go` (line 96: `events.NewClient(cfg.NATSURL, cfg.GatewayID)` — where TLS config needs to be passed)
- `internal/events/client.go` (NewClient function signature, existing connection options)

### Task

Add NATS TLS configuration fields and wire TLS into the NATS client connection.

1. **Add config fields** to `internal/config/config.go`:
   - Add to the Config struct (after line 43, near `NATSURL`):
     ```go
     NATSTLSEnabled bool   `json:"nats_tls_enabled"`
     NATSTLSCert    string `json:"nats_tls_cert"`
     NATSTLSKey     string `json:"nats_tls_key"`
     NATSTLSCa      string `json:"nats_tls_ca"`
     ```

2. **Parse env vars** in `Load()`:
   - Parse `MCPGW_NATS_TLS_ENABLED` as bool (default `false`), same pattern as `CruveroEnabled`
   - Parse `MCPGW_NATS_TLS_CERT`, `MCPGW_NATS_TLS_KEY`, `MCPGW_NATS_TLS_CA` as strings (no defaults)

3. **Add validation** in `Validate()`:
   ```go
   if c.NATSTLSEnabled {
       if c.NATSTLSCert == "" || c.NATSTLSKey == "" || c.NATSTLSCa == "" {
           return fmt.Errorf("MCPGW_NATS_TLS_CERT, MCPGW_NATS_TLS_KEY, and MCPGW_NATS_TLS_CA are required when MCPGW_NATS_TLS_ENABLED is true")
       }
   }
   ```

4. **Add TLS builder** to `internal/server/server.go` (private helper):
   ```go
   func buildNATSTLSConfig(certPath, keyPath, caPath string) (*tls.Config, error) {
       cert, err := tls.LoadX509KeyPair(certPath, keyPath)
       if err != nil {
           return nil, fmt.Errorf("load nats tls cert/key: %w", err)
       }

       caCert, err := os.ReadFile(caPath)
       if err != nil {
           return nil, fmt.Errorf("read nats tls ca: %w", err)
       }

       caCertPool := x509.NewCertPool()
       if !caCertPool.AppendCertsFromPEM(caCert) {
           return nil, fmt.Errorf("failed to parse nats tls ca certificate")
       }

       return &tls.Config{
           Certificates: []tls.Certificate{cert},
           RootCAs:      caCertPool,
           MinVersion:   tls.VersionTLS12,
       }, nil
   }
   ```

5. **Wire TLS into NATS client** in `server.go` `New()` function (around line 96):
   ```go
   var clientOpts []events.ClientOption
   if cfg.NATSTLSEnabled {
       tlsConfig, tlsErr := buildNATSTLSConfig(cfg.NATSTLSCert, cfg.NATSTLSKey, cfg.NATSTLSCa)
       if tlsErr != nil {
           logger.Error("nats tls config failed", slog.String("error", tlsErr.Error()))
           // Fall through — NATS client creation will fail or connect without TLS
       } else {
           clientOpts = append(clientOpts, events.WithTLS(tlsConfig))
       }
   }
   natsClient, err := events.NewClient(cfg.NATSURL, cfg.GatewayID, clientOpts...)
   ```

6. **Verify existing ClientOption support** in `internal/events/client.go`:
   - The `ClientOption` type, `WithTLS` function, and variadic `NewClient` signature **already exist** (lines 27-35, 64). Do NOT create duplicates.
   - The internal struct is `clientConfig` (line 21), not `clientOptions`.
   - `WithTLS` already applies `nats.Secure(cfg.tlsConfig)` at line 115-117.
   - Verify these are present and correct. No changes needed in the events package.

### Verification

```bash
go build ./cmd/mcpgw
go test ./internal/config/... ./internal/server/... ./internal/events/...
go vet ./...
```

### Acceptance Criteria

- Four new config fields parse correctly from env vars
- Config validation rejects `NATSTLSEnabled=true` without all three paths
- Config validation accepts `NATSTLSEnabled=false` regardless of paths
- `buildNATSTLSConfig` returns valid `*tls.Config` from real cert files
- `events.WithTLS` option is applied to NATS connection (already implemented in `client.go`)
- Existing tests pass (NewClient already accepts variadic options)

---

## Prompt 2 of 2: NATS TLS Config & Connection Tests

### Required Reading (read these files before writing code)

- `docs/phases/PHASE15B.md` (testing requirements)
- `internal/config/config_test.go` (existing test patterns — table-driven with t.Setenv)
- `internal/events/client_test.go` (existing test patterns, if present)

### Task

Add tests for NATS TLS configuration parsing, validation, and TLS config building.

1. **Config tests** in `internal/config/config_test.go`:

   Add table-driven test cases for NATS TLS fields:
   ```go
   func TestConfig_NATSTLSFields(t *testing.T) {
       tests := []struct {
           name     string
           envVars  map[string]string
           wantErr  string
           validate func(t *testing.T, cfg *Config)
       }{
           {
               name:    "defaults: TLS disabled",
               envVars: map[string]string{"MCPGW_DB_URL": "postgres://localhost/test"},
               validate: func(t *testing.T, cfg *Config) {
                   assert.False(t, cfg.NATSTLSEnabled)
                   assert.Empty(t, cfg.NATSTLSCert)
                   assert.Empty(t, cfg.NATSTLSKey)
                   assert.Empty(t, cfg.NATSTLSCa)
               },
           },
           {
               name: "TLS enabled with all paths",
               envVars: map[string]string{
                   "MCPGW_DB_URL":           "postgres://localhost/test",
                   "MCPGW_NATS_TLS_ENABLED": "true",
                   "MCPGW_NATS_TLS_CERT":    "/certs/nats-client.crt",
                   "MCPGW_NATS_TLS_KEY":     "/certs/nats-client.key",
                   "MCPGW_NATS_TLS_CA":      "/certs/nats-ca.crt",
               },
               validate: func(t *testing.T, cfg *Config) {
                   assert.True(t, cfg.NATSTLSEnabled)
                   assert.Equal(t, "/certs/nats-client.crt", cfg.NATSTLSCert)
                   assert.Equal(t, "/certs/nats-client.key", cfg.NATSTLSKey)
                   assert.Equal(t, "/certs/nats-ca.crt", cfg.NATSTLSCa)
               },
           },
           {
               name: "TLS enabled without cert path — validation error",
               envVars: map[string]string{
                   "MCPGW_DB_URL":           "postgres://localhost/test",
                   "MCPGW_NATS_TLS_ENABLED": "true",
                   "MCPGW_NATS_TLS_KEY":     "/certs/nats-client.key",
                   "MCPGW_NATS_TLS_CA":      "/certs/nats-ca.crt",
               },
               wantErr: "MCPGW_NATS_TLS_CERT",
           },
           {
               name: "TLS disabled with paths set — no validation error",
               envVars: map[string]string{
                   "MCPGW_DB_URL":        "postgres://localhost/test",
                   "MCPGW_NATS_TLS_CERT": "/certs/nats-client.crt",
               },
               validate: func(t *testing.T, cfg *Config) {
                   assert.False(t, cfg.NATSTLSEnabled)
                   assert.Equal(t, "/certs/nats-client.crt", cfg.NATSTLSCert)
               },
           },
       }
       // Run table-driven tests with t.Setenv for each env var
   }
   ```

2. **TLS builder tests** in `internal/server/server_test.go` or a new `internal/server/tls_test.go`:

   Use `t.TempDir()` to create temporary cert files for testing:
   ```go
   func TestBuildNATSTLSConfig_ValidCerts(t *testing.T) {
       // Generate self-signed test certs in t.TempDir()
       certDir := t.TempDir()
       // ... write test CA, cert, key files ...

       tlsConfig, err := buildNATSTLSConfig(
           filepath.Join(certDir, "cert.pem"),
           filepath.Join(certDir, "key.pem"),
           filepath.Join(certDir, "ca.pem"),
       )
       require.NoError(t, err)
       assert.NotNil(t, tlsConfig)
       assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion)
       assert.Len(t, tlsConfig.Certificates, 1)
   }

   func TestBuildNATSTLSConfig_MissingCertFile(t *testing.T) {
       _, err := buildNATSTLSConfig("/nonexistent/cert.pem", "/nonexistent/key.pem", "/nonexistent/ca.pem")
       require.Error(t, err)
       assert.Contains(t, err.Error(), "load nats tls cert/key")
   }

   func TestBuildNATSTLSConfig_InvalidCAPEM(t *testing.T) {
       certDir := t.TempDir()
       // Write valid cert+key but invalid CA content
       os.WriteFile(filepath.Join(certDir, "ca.pem"), []byte("not a pem"), 0644)
       // ... write valid cert and key ...

       _, err := buildNATSTLSConfig(certPath, keyPath, filepath.Join(certDir, "ca.pem"))
       require.Error(t, err)
       assert.Contains(t, err.Error(), "failed to parse nats tls ca")
   }
   ```

3. **Events client option test** in `internal/events/client_test.go`:
   ```go
   func TestWithTLS_SetsConfig(t *testing.T) {
       tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
       opt := WithTLS(tlsCfg)

       var cfg clientConfig
       opt(&cfg)

       assert.Equal(t, tlsCfg, cfg.tlsConfig)
   }
   ```

### Verification

```bash
go test -v -race ./internal/config/... ./internal/server/... ./internal/events/...
go test -coverprofile=cover.out ./internal/config/... && go tool cover -func=cover.out | grep config
```

### Acceptance Criteria

- Config defaults test passes (TLS disabled, paths empty)
- Config with all TLS paths set parses correctly
- Config validation rejects enabled TLS with missing paths
- Config validation accepts disabled TLS regardless of paths
- TLS builder creates valid config from real cert files
- TLS builder returns descriptive errors for missing/invalid files
- Events client option test verifies TLS config is stored
- Coverage >=80% on `internal/config` and `internal/events`
