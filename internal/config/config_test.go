package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://user:pass@localhost:5432/mcpgw?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != ":8443" {
		t.Fatalf("expected default listen addr :8443, got %q", cfg.ListenAddr)
	}
	if cfg.HeartbeatTTL != 30*time.Second {
		t.Fatalf("expected default heartbeat ttl 30s, got %s", cfg.HeartbeatTTL)
	}
	if cfg.RateDefault != 10 {
		t.Fatalf("expected default rate default 10, got %d", cfg.RateDefault)
	}
	if cfg.RateBurst != 20 {
		t.Fatalf("expected default rate burst 20, got %d", cfg.RateBurst)
	}
	if cfg.CircuitThreshold != 5 {
		t.Fatalf("expected default circuit threshold 5, got %d", cfg.CircuitThreshold)
	}
	if cfg.CircuitTimeout != 30*time.Second {
		t.Fatalf("expected default circuit timeout 30s, got %s", cfg.CircuitTimeout)
	}
	if cfg.RetryMax != 3 {
		t.Fatalf("expected default retry max 3, got %d", cfg.RetryMax)
	}
	if cfg.LogFormat != "json" {
		t.Fatalf("expected default log format json, got %q", cfg.LogFormat)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected default log level info, got %q", cfg.LogLevel)
	}
	if cfg.MetricsAddr != ":9090" {
		t.Fatalf("expected default metrics addr :9090, got %q", cfg.MetricsAddr)
	}
	if cfg.CruveroEnabled {
		t.Fatalf("expected default cruvero enabled false")
	}
	if cfg.CORSEnabled {
		t.Fatalf("expected default cors enabled false")
	}
	if len(cfg.GatewayID) != 36 || strings.Count(cfg.GatewayID, "-") != 4 {
		t.Fatalf("expected generated UUID gateway id, got %q", cfg.GatewayID)
	}
	if cfg.DBMaxOpenConns != 25 {
		t.Fatalf("expected default db max open conns 25, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 10 {
		t.Fatalf("expected default db max idle conns 10, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 5*time.Minute {
		t.Fatalf("expected default db conn max lifetime 5m, got %s", cfg.DBConnMaxLifetime)
	}
	if cfg.AuditRetentionDays != 90 {
		t.Fatalf("expected default audit retention days 90, got %d", cfg.AuditRetentionDays)
	}
	if cfg.AuditCleanupInterval != time.Hour {
		t.Fatalf("expected default audit cleanup interval 1h, got %s", cfg.AuditCleanupInterval)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Fatalf("expected default shutdown timeout 30s, got %s", cfg.ShutdownTimeout)
	}
	if cfg.RateLimitBackend != "memory" {
		t.Fatalf("expected default rate limit backend memory, got %q", cfg.RateLimitBackend)
	}
	if cfg.DragonflyURL != "" {
		t.Fatalf("expected empty dragonfly url by default, got %q", cfg.DragonflyURL)
	}
	if cfg.NATSTLSEnabled {
		t.Fatal("expected default nats tls enabled false")
	}
	if cfg.NATSTLSCert != "" {
		t.Fatalf("expected empty nats tls cert by default, got %q", cfg.NATSTLSCert)
	}
	if cfg.NATSTLSKey != "" {
		t.Fatalf("expected empty nats tls key by default, got %q", cfg.NATSTLSKey)
	}
	if cfg.NATSTLSCa != "" {
		t.Fatalf("expected empty nats tls ca by default, got %q", cfg.NATSTLSCa)
	}
	if cfg.AdminDevMode {
		t.Fatal("expected default admin dev mode false")
	}
	if cfg.AdminMode != "standalone" {
		t.Fatalf("expected default admin mode standalone, got %q", cfg.AdminMode)
	}
	if cfg.PlatformServiceToken != "" {
		t.Fatalf("expected empty platform service token by default, got %q", cfg.PlatformServiceToken)
	}
	if cfg.ProgressiveDiscovery {
		t.Fatal("expected default progressive discovery false")
	}
	if cfg.Orchestrate.Enabled {
		t.Fatal("expected default orchestrate enabled false")
	}
	if len(cfg.Orchestrate.Providers) != 0 {
		t.Fatalf("expected no llm providers by default, got %d", len(cfg.Orchestrate.Providers))
	}
}

func TestLoadAllEnvVars(t *testing.T) {
	clearKnownEnv(t)

	t.Setenv("MCPGW_LISTEN_ADDR", ":9443")
	t.Setenv("MCPGW_TLS_CERT", "/tls/server.crt")
	t.Setenv("MCPGW_TLS_KEY", "/tls/server.key")
	t.Setenv("MCPGW_TLS_CA", "/tls/ca.crt")
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_NATS_URL", "nats://localhost:4222")
	t.Setenv("MCPGW_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("MCPGW_OIDC_AUDIENCE", "mcpgw")
	t.Setenv("MCPGW_HEARTBEAT_TTL", "45s")
	t.Setenv("MCPGW_RATE_DEFAULT", "33")
	t.Setenv("MCPGW_RATE_BURST", "66")
	t.Setenv("MCPGW_CIRCUIT_THRESHOLD", "7")
	t.Setenv("MCPGW_CIRCUIT_TIMEOUT", "90s")
	t.Setenv("MCPGW_RETRY_MAX", "5")
	t.Setenv("MCPGW_SPIFFE_ALLOW_PREFIX", "spiffe://cluster-a, spiffe://cluster-b")
	t.Setenv("MCPGW_LOG_FORMAT", "text")
	t.Setenv("MCPGW_LOG_LEVEL", "debug")
	t.Setenv("MCPGW_METRICS_ADDR", ":9191")
	t.Setenv("MCPGW_CRUVERO_ENABLED", "true")
	t.Setenv("MCPGW_GATEWAY_ID", "gateway-fixed")
	t.Setenv("MCPGW_CORS_ENABLED", "true")
	t.Setenv("MCPGW_CORS_ALLOWED_ORIGINS", "https://example.com, https://other.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != ":9443" || cfg.TLSCertPath != "/tls/server.crt" || cfg.TLSKeyPath != "/tls/server.key" || cfg.TLSCAPath != "/tls/ca.crt" {
		t.Fatalf("expected tls/listen env values to be loaded: %+v", cfg)
	}
	if cfg.DBURL != "postgres://db" || cfg.NATSURL != "nats://localhost:4222" {
		t.Fatalf("expected db/nats env values to be loaded: %+v", cfg)
	}
	if cfg.OIDCIssuer != "https://issuer.example.com" || cfg.OIDCAudience != "mcpgw" {
		t.Fatalf("expected oidc env values to be loaded: %+v", cfg)
	}
	if cfg.HeartbeatTTL != 45*time.Second || cfg.CircuitTimeout != 90*time.Second {
		t.Fatalf("expected duration env values to be parsed: %+v", cfg)
	}
	if cfg.RateDefault != 33 || cfg.RateBurst != 66 || cfg.CircuitThreshold != 7 || cfg.RetryMax != 5 {
		t.Fatalf("expected numeric env values to be parsed: %+v", cfg)
	}
	if !cfg.CruveroEnabled || !cfg.CORSEnabled {
		t.Fatalf("expected bool env values to be parsed: %+v", cfg)
	}
	if cfg.GatewayID != "gateway-fixed" {
		t.Fatalf("expected configured gateway id, got %q", cfg.GatewayID)
	}
	if len(cfg.SPIFFEAllowList) != 2 || cfg.SPIFFEAllowList[0] != "spiffe://cluster-a" || cfg.SPIFFEAllowList[1] != "spiffe://cluster-b" {
		t.Fatalf("expected spiffe allow list to be parsed, got %+v", cfg.SPIFFEAllowList)
	}
}

func TestLoadParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "invalid heartbeat ttl", key: "MCPGW_HEARTBEAT_TTL", value: "not-duration"},
		{name: "invalid rate default", key: "MCPGW_RATE_DEFAULT", value: "not-int"},
		{name: "invalid rate burst", key: "MCPGW_RATE_BURST", value: "not-int"},
		{name: "invalid circuit threshold", key: "MCPGW_CIRCUIT_THRESHOLD", value: "not-int"},
		{name: "invalid circuit timeout", key: "MCPGW_CIRCUIT_TIMEOUT", value: "not-duration"},
		{name: "invalid retry max", key: "MCPGW_RETRY_MAX", value: "not-int"},
		{name: "invalid cruvero enabled", key: "MCPGW_CRUVERO_ENABLED", value: "not-bool"},
		{name: "invalid cors enabled", key: "MCPGW_CORS_ENABLED", value: "not-bool"},
		{name: "invalid db max open conns", key: "MCPGW_DB_MAX_OPEN_CONNS", value: "not-int"},
		{name: "invalid db max idle conns", key: "MCPGW_DB_MAX_IDLE_CONNS", value: "not-int"},
		{name: "invalid db conn max lifetime", key: "MCPGW_DB_CONN_MAX_LIFETIME", value: "not-duration"},
		{name: "invalid audit retention days", key: "MCPGW_AUDIT_RETENTION_DAYS", value: "not-int"},
		{name: "invalid audit cleanup interval", key: "MCPGW_AUDIT_CLEANUP_INTERVAL", value: "not-duration"},
		{name: "invalid shutdown timeout", key: "MCPGW_SHUTDOWN_TIMEOUT", value: "not-duration"},
		{name: "invalid admin dev mode", key: "MCPGW_ADMIN_DEV_MODE", value: "not-bool"},
		{name: "invalid admin mode", key: "MCPGW_ADMIN_MODE", value: "not-a-mode"},
		{name: "invalid progressive discovery", key: "MCPGW_PROGRESSIVE_DISCOVERY", value: "not-bool"},
		{name: "invalid orchestrate enabled", key: "MCPGW_ORCHESTRATE_ENABLED", value: "not-bool"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			clearKnownEnv(t)
			t.Setenv("MCPGW_DB_URL", "postgres://db")
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for %s", tt.key)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("expected error to mention key %s, got %v", tt.key, err)
			}
		})
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		errText string
	}{
		{
			name:    "missing db url",
			cfg:     Config{},
			errText: "MCPGW_DB_URL",
		},
		{
			name: "tls cert without key",
			cfg: Config{
				DBURL:       "postgres://db",
				TLSCertPath: "/tls/server.crt",
				RateDefault: 1,
				RateBurst:   1,
			},
			errText: "MCPGW_TLS_CERT",
		},
		{
			name: "non positive default rate",
			cfg: Config{
				DBURL:       "postgres://db",
				RateDefault: 0,
				RateBurst:   1,
				RetryMax:    1,
			},
			errText: "MCPGW_RATE_DEFAULT",
		},
		{
			name: "non positive burst rate",
			cfg: Config{
				DBURL:       "postgres://db",
				RateDefault: 1,
				RateBurst:   0,
				RetryMax:    1,
			},
			errText: "MCPGW_RATE_BURST",
		},
		{
			name: "non positive circuit threshold",
			cfg: Config{
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 0,
				RetryMax:         1,
			},
			errText: "MCPGW_CIRCUIT_THRESHOLD",
		},
		{
			name: "non positive retry max",
			cfg: Config{
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 1,
				RetryMax:         0,
			},
			errText: "MCPGW_RETRY_MAX",
		},
		{
			name: "cruvero enabled without nats",
			cfg: Config{
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 1,
				RetryMax:         1,
				CruveroEnabled:   true,
			},
			errText: "MCPGW_NATS_URL",
		},
		{
			name: "cors enabled without origins",
			cfg: Config{
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 1,
				RetryMax:         1,
				CORSEnabled:      true,
			},
			errText: "MCPGW_CORS_ALLOWED_ORIGINS",
		},
		{
			name: "invalid rate limit backend",
			cfg: Config{
				DBURL:                "postgres://db",
				RateDefault:          1,
				RateBurst:            1,
				CircuitThreshold:     1,
				RetryMax:             1,
				DBMaxOpenConns:       25,
				DBMaxIdleConns:       10,
				DBConnMaxLifetime:    5 * time.Minute,
				AuditRetentionDays:   90,
				AuditCleanupInterval: time.Hour,
				ShutdownTimeout:      30 * time.Second,
				RateLimitBackend:     "invalid",
			},
			errText: "MCPGW_RATE_LIMIT_BACKEND",
		},
		{
			name: "dragonfly backend without url",
			cfg: Config{
				DBURL:                "postgres://db",
				RateDefault:          1,
				RateBurst:            1,
				CircuitThreshold:     1,
				RetryMax:             1,
				DBMaxOpenConns:       25,
				DBMaxIdleConns:       10,
				DBConnMaxLifetime:    5 * time.Minute,
				AuditRetentionDays:   90,
				AuditCleanupInterval: time.Hour,
				ShutdownTimeout:      30 * time.Second,
				RateLimitBackend:     "dragonfly",
			},
			errText: "MCPGW_DRAGONFLY_URL",
		},
		{
			name: "nats backend without cruvero",
			cfg: Config{
				DBURL:                "postgres://db",
				RateDefault:          1,
				RateBurst:            1,
				CircuitThreshold:     1,
				RetryMax:             1,
				DBMaxOpenConns:       25,
				DBMaxIdleConns:       10,
				DBConnMaxLifetime:    5 * time.Minute,
				AuditRetentionDays:   90,
				AuditCleanupInterval: time.Hour,
				ShutdownTimeout:      30 * time.Second,
				RateLimitBackend:     "nats",
			},
			errText: "MCPGW_CRUVERO_ENABLED",
		},
		{
			name: "admin dev mode without admin enabled",
			cfg: Config{
				DBURL:                "postgres://db",
				RateDefault:          1,
				RateBurst:            1,
				CircuitThreshold:     1,
				RetryMax:             1,
				DBMaxOpenConns:       25,
				DBMaxIdleConns:       10,
				DBConnMaxLifetime:    5 * time.Minute,
				AuditRetentionDays:   90,
				AuditCleanupInterval: time.Hour,
				ShutdownTimeout:      30 * time.Second,
				AdminDevMode:         true,
				AdminEnabled:         false,
			},
			errText: "MCPGW_ADMIN_ENABLED",
		},
		{
			name: "nats tls enabled without cert",
			cfg: Config{
				DBURL:                "postgres://db",
				RateDefault:          1,
				RateBurst:            1,
				CircuitThreshold:     1,
				RetryMax:             1,
				DBMaxOpenConns:       25,
				DBMaxIdleConns:       10,
				DBConnMaxLifetime:    5 * time.Minute,
				AuditRetentionDays:   90,
				AuditCleanupInterval: time.Hour,
				ShutdownTimeout:      30 * time.Second,
				NATSTLSEnabled:       true,
				NATSTLSKey:           "/tls/nats.key",
				NATSTLSCa:            "/tls/nats-ca.crt",
			},
			errText: "MCPGW_NATS_TLS_CERT",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("expected error to contain %q, got %v", tt.errText, err)
			}
		})
	}
}

func TestValidateSuccessAndTLSConfigured(t *testing.T) {
	cfg := Config{
		DBURL:                "postgres://db",
		TLSCertPath:          "/tls/server.crt",
		TLSKeyPath:           "/tls/server.key",
		RateDefault:          10,
		RateBurst:            20,
		CircuitThreshold:     5,
		RetryMax:             3,
		CruveroEnabled:       true,
		NATSURL:              "nats://localhost:4222",
		DBMaxOpenConns:       25,
		DBMaxIdleConns:       10,
		DBConnMaxLifetime:    5 * time.Minute,
		AuditRetentionDays:   90,
		AuditCleanupInterval: time.Hour,
		ShutdownTimeout:      30 * time.Second,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
	if !cfg.IsTLSConfigured() {
		t.Fatalf("expected tls configured")
	}
}

func clearKnownEnv(t *testing.T) {
	t.Helper()

	keys := []string{
		"MCPGW_LISTEN_ADDR",
		"MCPGW_TLS_CERT",
		"MCPGW_TLS_KEY",
		"MCPGW_TLS_CA",
		"MCPGW_DB_URL",
		"MCPGW_NATS_URL",
		"MCPGW_OIDC_ISSUER",
		"MCPGW_OIDC_AUDIENCE",
		"MCPGW_HEARTBEAT_TTL",
		"MCPGW_RATE_DEFAULT",
		"MCPGW_RATE_BURST",
		"MCPGW_CIRCUIT_THRESHOLD",
		"MCPGW_CIRCUIT_TIMEOUT",
		"MCPGW_RETRY_MAX",
		"MCPGW_SPIFFE_ALLOW_PREFIX",
		"MCPGW_LOG_FORMAT",
		"MCPGW_LOG_LEVEL",
		"MCPGW_METRICS_ADDR",
		"MCPGW_CRUVERO_ENABLED",
		"MCPGW_GATEWAY_ID",
		"MCPGW_CORS_ENABLED",
		"MCPGW_DB_MAX_OPEN_CONNS",
		"MCPGW_DB_MAX_IDLE_CONNS",
		"MCPGW_DB_CONN_MAX_LIFETIME",
		"MCPGW_CORS_ALLOWED_ORIGINS",
		"MCPGW_AUDIT_RETENTION_DAYS",
		"MCPGW_AUDIT_CLEANUP_INTERVAL",
		"MCPGW_SHUTDOWN_TIMEOUT",
		"MCPGW_RATE_LIMIT_BACKEND",
		"MCPGW_DRAGONFLY_URL",
		"MCPGW_NATS_TLS_ENABLED",
		"MCPGW_NATS_TLS_CERT",
		"MCPGW_NATS_TLS_KEY",
		"MCPGW_NATS_TLS_CA",
		"MCPGW_ADMIN_ENABLED",
		"MCPGW_ADMIN_MODE",
		"MCPGW_ADMIN_DEV_MODE",
		"MCPGW_ADMIN_OIDC_CLIENT_ID",
		"MCPGW_ADMIN_OIDC_CLIENT_SECRET",
		"MCPGW_PLATFORM_SERVICE_TOKEN",
		"MCPGW_ADMIN_SESSION_KEY",
		"MCPGW_ADMIN_SESSION_TTL",
		"MCPGW_ADMIN_REQUIRED_SCOPE",
		"MCPGW_DEVICE_FLOW_ENABLED",
		"MCPGW_DEVICE_FLOW_CLIENT_ID",
		"MCPGW_DEVICE_FLOW_CLIENT_SECRET",
		"MCPGW_DEVICE_FLOW_IDP_DEVICE_URL",
		"MCPGW_DEVICE_FLOW_IDP_TOKEN_URL",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_SERVICE_NAME",
		"MCPGW_PROGRESSIVE_DISCOVERY",
		"MCPGW_SEARCH_ENGINE",
		"MCPGW_ONNX_RUNTIME_PATH",
		"MCPGW_ONNX_MODEL_PATH",
		"MCPGW_TOKENIZER_PATH",
		"MCPGW_PLATFORM_SPIFFE_PREFIXES",
		"MCPGW_ORCHESTRATE_ENABLED",
		"MCPGW_LLM_PROVIDERS",
		"MCPGW_LLM_OPENAI_API_KEY",
		"MCPGW_LLM_AZURE_API_KEY",
		"MCPGW_LLM_XAI_API_KEY",
	}

	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestConfig_NATSTLSFields(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantErr  bool
		errText  string
		checkCfg func(t *testing.T, cfg *Config)
	}{
		{
			name: "tls enabled with all paths parses correctly",
			envVars: map[string]string{
				"MCPGW_NATS_TLS_ENABLED": "true",
				"MCPGW_NATS_TLS_CERT":    "/tls/nats.crt",
				"MCPGW_NATS_TLS_KEY":     "/tls/nats.key",
				"MCPGW_NATS_TLS_CA":      "/tls/nats-ca.crt",
			},
			checkCfg: func(t *testing.T, cfg *Config) {
				t.Helper()
				if !cfg.NATSTLSEnabled {
					t.Fatal("expected nats tls enabled true")
				}
				if cfg.NATSTLSCert != "/tls/nats.crt" {
					t.Fatalf("expected cert path /tls/nats.crt, got %q", cfg.NATSTLSCert)
				}
				if cfg.NATSTLSKey != "/tls/nats.key" {
					t.Fatalf("expected key path /tls/nats.key, got %q", cfg.NATSTLSKey)
				}
				if cfg.NATSTLSCa != "/tls/nats-ca.crt" {
					t.Fatalf("expected ca path /tls/nats-ca.crt, got %q", cfg.NATSTLSCa)
				}
			},
		},
		{
			name: "tls enabled without key path fails validation",
			envVars: map[string]string{
				"MCPGW_NATS_TLS_ENABLED": "true",
				"MCPGW_NATS_TLS_CERT":    "/tls/nats.crt",
				"MCPGW_NATS_TLS_CA":      "/tls/nats-ca.crt",
			},
			wantErr: true,
			errText: "MCPGW_NATS_TLS_KEY",
		},
		{
			name: "tls disabled with paths set causes no validation error",
			envVars: map[string]string{
				"MCPGW_NATS_TLS_ENABLED": "false",
				"MCPGW_NATS_TLS_CERT":    "/tls/nats.crt",
			},
			checkCfg: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.NATSTLSEnabled {
					t.Fatal("expected nats tls enabled false")
				}
				if cfg.NATSTLSCert != "/tls/nats.crt" {
					t.Fatalf("expected cert path preserved, got %q", cfg.NATSTLSCert)
				}
			},
		},
		{
			name: "invalid bool for tls enabled returns parse error",
			envVars: map[string]string{
				"MCPGW_NATS_TLS_ENABLED": "not-bool",
			},
			wantErr: true,
			errText: "MCPGW_NATS_TLS_ENABLED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearKnownEnv(t)
			t.Setenv("MCPGW_DB_URL", "postgres://db")
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("expected error to contain %q, got %v", tt.errText, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkCfg != nil {
				tt.checkCfg(t, cfg)
			}
		})
	}
}

func TestLoadAdminDevMode(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_ADMIN_ENABLED", "true")
	t.Setenv("MCPGW_ADMIN_DEV_MODE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed with admin dev mode, got: %v", err)
	}
	if !cfg.AdminEnabled {
		t.Fatal("expected admin enabled true")
	}
	if !cfg.AdminDevMode {
		t.Fatal("expected admin dev mode true")
	}
}

func TestValidate_AdminDevMode_SkipsOIDCRequirements(t *testing.T) {
	cfg := Config{
		DBURL:                "postgres://db",
		RateDefault:          10,
		RateBurst:            20,
		CircuitThreshold:     5,
		RetryMax:             3,
		DBMaxOpenConns:       25,
		DBMaxIdleConns:       10,
		DBConnMaxLifetime:    5 * time.Minute,
		AuditRetentionDays:   90,
		AuditCleanupInterval: time.Hour,
		ShutdownTimeout:      30 * time.Second,
		AdminEnabled:         true,
		AdminDevMode:         true,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected validation to pass with admin dev mode, got: %v", err)
	}
}

func TestValidate_AdminDevMode_GatewayIDGuard(t *testing.T) {
	tests := []struct {
		name      string
		gatewayID string
		wantErr   bool
	}{
		{name: "rejected for prod", gatewayID: "mcpgateway-prod", wantErr: true},
		{name: "rejected for staging", gatewayID: "mcpgateway-staging", wantErr: true},
		{name: "rejected for uppercase PROD", gatewayID: "mcpgateway-PROD", wantErr: true},
		{name: "rejected for mixed case Prod", gatewayID: "mcpgateway-Prod", wantErr: true},
		{name: "rejected for whitespace padded", gatewayID: " mcpgateway-prod ", wantErr: true},
		{name: "rejected for production substring", gatewayID: "production-gateway", wantErr: true},
		{name: "rejected for staging substring", gatewayID: "staging-area", wantErr: true},
		{name: "allowed for dev", gatewayID: "mcpgateway-dev", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				DBURL:                "postgres://db",
				RateDefault:          10,
				RateBurst:            20,
				CircuitThreshold:     5,
				RetryMax:             3,
				DBMaxOpenConns:       25,
				DBMaxIdleConns:       10,
				DBConnMaxLifetime:    5 * time.Minute,
				AuditRetentionDays:   90,
				AuditCleanupInterval: time.Hour,
				ShutdownTimeout:      30 * time.Second,
				AdminEnabled:         true,
				AdminDevMode:         true,
				GatewayID:            tt.gatewayID,
			}

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected validation to fail for gateway ID %q", tt.gatewayID)
				}
				if !strings.Contains(err.Error(), "prod or staging") {
					t.Fatalf("expected error about prod/staging, got: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected validation to pass for gateway ID %q, got: %v", tt.gatewayID, err)
				}
			}
		})
	}
}

func TestLoadProgressiveDiscoveryEnabled(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_PROGRESSIVE_DISCOVERY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed, got: %v", err)
	}
	if !cfg.ProgressiveDiscovery {
		t.Fatal("expected progressive discovery true")
	}
}

func TestValidate_SearchEngine(t *testing.T) {
	base := Config{
		DBURL:                "postgres://db",
		RateDefault:          10,
		RateBurst:            20,
		CircuitThreshold:     5,
		RetryMax:             3,
		DBMaxOpenConns:       25,
		DBMaxIdleConns:       10,
		DBConnMaxLifetime:    5 * time.Minute,
		AuditRetentionDays:   90,
		AuditCleanupInterval: time.Hour,
		ShutdownTimeout:      30 * time.Second,
	}

	tests := []struct {
		name    string
		engine  string
		wantErr bool
		errText string
	}{
		{name: "empty is valid", engine: ""},
		{name: "substring is valid", engine: "substring"},
		{name: "bm25 is valid", engine: "bm25"},
		{name: "vector is valid", engine: "vector"},
		{name: "hybrid is valid", engine: "hybrid"},
		{name: "invalid engine", engine: "invalid", wantErr: true, errText: "MCPGW_SEARCH_ENGINE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.SearchEngine = tt.engine

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected validation error")
				}
				if !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("expected error to contain %q, got %v", tt.errText, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadSearchEngineAndOnnxPaths(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_SEARCH_ENGINE", "hybrid")
	t.Setenv("MCPGW_ONNX_RUNTIME_PATH", "/opt/lib/libonnxruntime.so")
	t.Setenv("MCPGW_ONNX_MODEL_PATH", "/opt/models/model.onnx")
	t.Setenv("MCPGW_TOKENIZER_PATH", "/opt/models/tokenizer.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed, got: %v", err)
	}
	if cfg.SearchEngine != "hybrid" {
		t.Fatalf("expected search engine hybrid, got %q", cfg.SearchEngine)
	}
	if cfg.OnnxRuntimePath != "/opt/lib/libonnxruntime.so" {
		t.Fatalf("expected onnx runtime path /opt/lib/libonnxruntime.so, got %q", cfg.OnnxRuntimePath)
	}
	if cfg.OnnxModelPath != "/opt/models/model.onnx" {
		t.Fatalf("expected onnx model path /opt/models/model.onnx, got %q", cfg.OnnxModelPath)
	}
	if cfg.TokenizerPath != "/opt/models/tokenizer.json" {
		t.Fatalf("expected tokenizer path /opt/models/tokenizer.json, got %q", cfg.TokenizerPath)
	}
}

func TestLoadOnnxPathDefaults(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed, got: %v", err)
	}
	if cfg.OnnxRuntimePath != "/usr/lib/libonnxruntime.so" {
		t.Fatalf("expected default onnx runtime path /usr/lib/libonnxruntime.so, got %q", cfg.OnnxRuntimePath)
	}
	if cfg.OnnxModelPath != "/models/all-MiniLM-L6-v2.onnx" {
		t.Fatalf("expected default onnx model path /models/all-MiniLM-L6-v2.onnx, got %q", cfg.OnnxModelPath)
	}
	if cfg.TokenizerPath != "/models/tokenizer.json" {
		t.Fatalf("expected default tokenizer path /models/tokenizer.json, got %q", cfg.TokenizerPath)
	}
}

func TestLoadPlatformSPIFFEPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     []string
	}{
		{
			name:     "empty returns nil",
			envValue: "",
			want:     nil,
		},
		{
			name:     "single prefix",
			envValue: "spiffe://example.com/ns/myapp-dev/sa/gateway",
			want:     []string{"spiffe://example.com/ns/myapp-dev/sa/gateway"},
		},
		{
			name:     "multiple prefixes",
			envValue: "spiffe://cluster-a/sa/platform, spiffe://cluster-b/sa/bridge",
			want:     []string{"spiffe://cluster-a/sa/platform", "spiffe://cluster-b/sa/bridge"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearKnownEnv(t)
			t.Setenv("MCPGW_DB_URL", "postgres://db")
			if tt.envValue != "" {
				t.Setenv("MCPGW_PLATFORM_SPIFFE_PREFIXES", tt.envValue)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(cfg.PlatformSPIFFEPrefixes) != len(tt.want) {
				t.Fatalf("expected %d prefixes, got %d: %v", len(tt.want), len(cfg.PlatformSPIFFEPrefixes), cfg.PlatformSPIFFEPrefixes)
			}
			for i, w := range tt.want {
				if cfg.PlatformSPIFFEPrefixes[i] != w {
					t.Fatalf("prefix[%d]: expected %q, got %q", i, w, cfg.PlatformSPIFFEPrefixes[i])
				}
			}
		})
	}
}

func TestLoadOrchestrateEnabled(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_ORCHESTRATE_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed, got: %v", err)
	}
	if !cfg.Orchestrate.Enabled {
		t.Fatal("expected orchestrate enabled true")
	}
}

func TestLoadLLMProviders(t *testing.T) {
	tests := []struct {
		name          string
		providersJSON string
		apiKeys       map[string]string
		wantCount     int
		wantFirst     string
		wantErr       bool
	}{
		{
			name:          "empty env returns nil",
			providersJSON: "",
			wantCount:     0,
		},
		{
			name:          "invalid json returns error",
			providersJSON: "not-json",
			wantErr:       true,
		},
		{
			name:          "provider without api key is skipped",
			providersJSON: `[{"name":"openai","model":"gpt-4o"}]`,
			wantCount:     0,
		},
		{
			name:          "provider without model is skipped",
			providersJSON: `[{"name":"openai","model":""}]`,
			apiKeys:       map[string]string{"MCPGW_LLM_OPENAI_API_KEY": "sk-test"},
			wantCount:     0,
		},
		{
			name:          "single provider with key",
			providersJSON: `[{"name":"openai","model":"gpt-4o"}]`,
			apiKeys:       map[string]string{"MCPGW_LLM_OPENAI_API_KEY": "sk-test"},
			wantCount:     1,
			wantFirst:     "openai",
		},
		{
			name:          "multiple providers, one without key",
			providersJSON: `[{"name":"openai","model":"gpt-4o"},{"name":"xai","model":"grok-3-mini","base_url":"https://api.x.ai/v1"}]`,
			apiKeys:       map[string]string{"MCPGW_LLM_OPENAI_API_KEY": "sk-test"},
			wantCount:     1,
			wantFirst:     "openai",
		},
		{
			name:          "multiple providers both with keys preserves order",
			providersJSON: `[{"name":"xai","model":"grok-3-mini","base_url":"https://api.x.ai/v1"},{"name":"openai","model":"gpt-4o"}]`,
			apiKeys: map[string]string{
				"MCPGW_LLM_XAI_API_KEY":    "xai-key",
				"MCPGW_LLM_OPENAI_API_KEY": "sk-test",
			},
			wantCount: 2,
			wantFirst: "xai",
		},
		{
			name:          "priority sorts providers",
			providersJSON: `[{"name":"xai","model":"grok-3-mini","priority":10},{"name":"openai","model":"gpt-4o","priority":1}]`,
			apiKeys: map[string]string{
				"MCPGW_LLM_XAI_API_KEY":    "xai-key",
				"MCPGW_LLM_OPENAI_API_KEY": "sk-test",
			},
			wantCount: 2,
			wantFirst: "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearKnownEnv(t)
			t.Setenv("MCPGW_DB_URL", "postgres://db")
			if tt.providersJSON != "" {
				t.Setenv("MCPGW_LLM_PROVIDERS", tt.providersJSON)
			}
			for k, v := range tt.apiKeys {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(cfg.Orchestrate.Providers) != tt.wantCount {
				t.Fatalf("expected %d providers, got %d: %+v", tt.wantCount, len(cfg.Orchestrate.Providers), cfg.Orchestrate.Providers)
			}
			if tt.wantCount > 0 && cfg.Orchestrate.Providers[0].Name != tt.wantFirst {
				t.Fatalf("expected first provider %q, got %q", tt.wantFirst, cfg.Orchestrate.Providers[0].Name)
			}
		})
	}
}

func TestLoadAdminModeIntegratedRequiresPlatformServiceToken(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_ADMIN_MODE", "integrated")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when integrated mode is missing platform service token")
	}
	if !strings.Contains(err.Error(), "MCPGW_PLATFORM_SERVICE_TOKEN") {
		t.Fatalf("expected MCPGW_PLATFORM_SERVICE_TOKEN error, got %v", err)
	}
}

func TestLoadAdminModeIntegratedBypassesStandaloneOIDCRequirements(t *testing.T) {
	clearKnownEnv(t)
	t.Setenv("MCPGW_DB_URL", "postgres://db")
	t.Setenv("MCPGW_ADMIN_MODE", "integrated")
	t.Setenv("MCPGW_PLATFORM_SERVICE_TOKEN", "platform-svc-token")
	t.Setenv("MCPGW_ADMIN_ENABLED", "true")
	// OIDC/session vars intentionally omitted; integrated mode should not require them.

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected load to succeed in integrated mode, got %v", err)
	}
	if cfg.AdminMode != "integrated" {
		t.Fatalf("expected integrated mode, got %q", cfg.AdminMode)
	}
	if cfg.PlatformServiceToken != "platform-svc-token" {
		t.Fatalf("expected platform token to be loaded")
	}
}
