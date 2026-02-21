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
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 1,
				RetryMax:         1,
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
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 1,
				RetryMax:         1,
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
				DBURL:            "postgres://db",
				RateDefault:      1,
				RateBurst:        1,
				CircuitThreshold: 1,
				RetryMax:         1,
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
		DBURL:             "postgres://db",
		TLSCertPath:       "/tls/server.crt",
		TLSKeyPath:        "/tls/server.key",
		RateDefault:       10,
		RateBurst:         20,
		CircuitThreshold:  5,
		RetryMax:          3,
		CruveroEnabled:    true,
		NATSURL:           "nats://localhost:4222",
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
	}

	for _, key := range keys {
		t.Setenv(key, "")
	}
}
