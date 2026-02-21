package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr       = ":8443"
	defaultHeartbeatTTL     = 30 * time.Second
	defaultRateDefault      = 10
	defaultRateBurst        = 20
	defaultCircuitThreshold = 5
	defaultCircuitTimeout   = 30 * time.Second
	defaultRetryMax         = 3
	defaultLogFormat        = "json"
	defaultLogLevel         = "info"
	defaultMetricsAddr      = ":9090"
	defaultCruveroEnabled   = false
	defaultGatewayID        = "auto"
	defaultCORSEnabled             = false
	defaultOTELServiceName         = "mcpgw"
	defaultProgressiveDiscovery    = false
	defaultDBMaxOpenConns       = 25
	defaultDBMaxIdleConns       = 10
	defaultDBConnMaxLifetime    = 5 * time.Minute
	defaultAuditRetentionDays   = 90
	defaultAuditCleanupInterval = 1 * time.Hour
	defaultShutdownTimeout      = 30 * time.Second
)

// Config contains all gateway runtime settings loaded from MCPGW_* env vars.
type Config struct {
	ListenAddr           string        `json:"listen_addr"`
	TLSCertPath          string        `json:"tls_cert_path"`
	TLSKeyPath           string        `json:"tls_key_path"`
	TLSCAPath            string        `json:"tls_ca_path"`
	DBURL                string        `json:"db_url"`
	NATSURL              string        `json:"nats_url"`
	NATSTLSEnabled       bool          `json:"nats_tls_enabled"`
	NATSTLSCert          string        `json:"nats_tls_cert"`
	NATSTLSKey           string        `json:"nats_tls_key"`
	NATSTLSCa            string        `json:"nats_tls_ca"`
	OIDCIssuer           string        `json:"oidc_issuer"`
	OIDCAudience         string        `json:"oidc_audience"`
	HeartbeatTTL         time.Duration `json:"heartbeat_ttl"`
	RateDefault          int           `json:"rate_default"`
	RateBurst            int           `json:"rate_burst"`
	CircuitThreshold     int           `json:"circuit_threshold"`
	CircuitTimeout       time.Duration `json:"circuit_timeout"`
	RetryMax             int           `json:"retry_max"`
	SPIFFEAllowList      []string      `json:"spiffe_allow_list"`
	LogFormat            string        `json:"log_format"`
	LogLevel             string        `json:"log_level"`
	MetricsAddr          string        `json:"metrics_addr"`
	CruveroEnabled       bool          `json:"cruvero_enabled"`
	GatewayID            string        `json:"gateway_id"`
	CORSEnabled          bool     `json:"cors_enabled"`
	CORSAllowedOrigins   []string `json:"cors_allowed_origins"`
	DBMaxOpenConns       int           `json:"db_max_open_conns"`
	DBMaxIdleConns       int           `json:"db_max_idle_conns"`
	DBConnMaxLifetime    time.Duration `json:"db_conn_max_lifetime"`
	AuditRetentionDays   int           `json:"audit_retention_days"`
	AuditCleanupInterval time.Duration `json:"audit_cleanup_interval"`
	ShutdownTimeout      time.Duration `json:"shutdown_timeout"`
	RateLimitBackend     string        `json:"rate_limit_backend"`
	DragonflyURL         string        `json:"dragonfly_url"`
	OTLPExporterEndpoint string        `json:"otlp_exporter_endpoint"`
	OTELServiceName      string        `json:"otel_service_name"`
	DeviceFlowEnabled      bool   `json:"device_flow_enabled"`
	DeviceFlowIDPDeviceURL string `json:"device_flow_idp_device_url"`
	DeviceFlowIDPTokenURL  string `json:"device_flow_idp_token_url"`
	DeviceFlowClientID     string `json:"device_flow_client_id"`
	DeviceFlowClientSecret string `json:"device_flow_client_secret"`
	AdminEnabled           bool          `json:"admin_enabled"`
	AdminOIDCClientID      string        `json:"admin_oidc_client_id"`
	AdminOIDCClientSecret  string        `json:"admin_oidc_client_secret"`
	AdminRequiredScope     string        `json:"admin_required_scope"`
	AdminSessionKey        [32]byte      `json:"-"`
	AdminSessionTTL        time.Duration `json:"admin_session_ttl"`
	AdminDevMode           bool          `json:"admin_dev_mode"`
	ProgressiveDiscovery   bool          `json:"progressive_discovery"`
}

// Load reads all MCPGW_* environment variables into Config and validates them.
func Load() (*Config, error) {
	heartbeatTTL, err := parseDuration("MCPGW_HEARTBEAT_TTL", defaultHeartbeatTTL)
	if err != nil {
		return nil, err
	}

	rateDefault, err := parseInt("MCPGW_RATE_DEFAULT", defaultRateDefault)
	if err != nil {
		return nil, err
	}

	rateBurst, err := parseInt("MCPGW_RATE_BURST", defaultRateBurst)
	if err != nil {
		return nil, err
	}

	circuitThreshold, err := parseInt("MCPGW_CIRCUIT_THRESHOLD", defaultCircuitThreshold)
	if err != nil {
		return nil, err
	}

	circuitTimeout, err := parseDuration("MCPGW_CIRCUIT_TIMEOUT", defaultCircuitTimeout)
	if err != nil {
		return nil, err
	}

	retryMax, err := parseInt("MCPGW_RETRY_MAX", defaultRetryMax)
	if err != nil {
		return nil, err
	}

	cruveroEnabled, err := parseBool("MCPGW_CRUVERO_ENABLED", defaultCruveroEnabled)
	if err != nil {
		return nil, err
	}

	corsEnabled, err := parseBool("MCPGW_CORS_ENABLED", defaultCORSEnabled)
	if err != nil {
		return nil, err
	}

	natsTLSEnabled, err := parseBool("MCPGW_NATS_TLS_ENABLED", false)
	if err != nil {
		return nil, err
	}

	dbMaxOpenConns, err := parseInt("MCPGW_DB_MAX_OPEN_CONNS", defaultDBMaxOpenConns)
	if err != nil {
		return nil, err
	}

	dbMaxIdleConns, err := parseInt("MCPGW_DB_MAX_IDLE_CONNS", defaultDBMaxIdleConns)
	if err != nil {
		return nil, err
	}

	dbConnMaxLifetime, err := parseDuration("MCPGW_DB_CONN_MAX_LIFETIME", defaultDBConnMaxLifetime)
	if err != nil {
		return nil, err
	}

	auditRetentionDays, err := parseInt("MCPGW_AUDIT_RETENTION_DAYS", defaultAuditRetentionDays)
	if err != nil {
		return nil, err
	}

	auditCleanupInterval, err := parseDuration("MCPGW_AUDIT_CLEANUP_INTERVAL", defaultAuditCleanupInterval)
	if err != nil {
		return nil, err
	}

	shutdownTimeout, err := parseDuration("MCPGW_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return nil, err
	}

	gatewayID := getEnv("MCPGW_GATEWAY_ID", defaultGatewayID)
	if gatewayID == "auto" {
		gatewayID, err = generateUUID()
		if err != nil {
			return nil, fmt.Errorf("generate gateway id: %w", err)
		}
	}

	rateLimitBackend := getEnv("MCPGW_RATE_LIMIT_BACKEND", "memory")

	deviceFlowEnabled, err := parseBool("MCPGW_DEVICE_FLOW_ENABLED", false)
	if err != nil {
		return nil, err
	}

	adminEnabled, err := parseBool("MCPGW_ADMIN_ENABLED", false)
	if err != nil {
		return nil, err
	}

	adminSessionTTL, err := parseDuration("MCPGW_ADMIN_SESSION_TTL", 8*time.Hour)
	if err != nil {
		return nil, err
	}

	adminDevMode, err := parseBool("MCPGW_ADMIN_DEV_MODE", false)
	if err != nil {
		return nil, err
	}

	progressiveDiscovery, err := parseBool("MCPGW_PROGRESSIVE_DISCOVERY", defaultProgressiveDiscovery)
	if err != nil {
		return nil, err
	}

	var adminSessionKey [32]byte
	if rawKey := strings.TrimSpace(os.Getenv("MCPGW_ADMIN_SESSION_KEY")); rawKey != "" {
		decoded, decodeErr := hex.DecodeString(rawKey)
		if decodeErr != nil {
			return nil, fmt.Errorf("parse MCPGW_ADMIN_SESSION_KEY: %w", decodeErr)
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("parse MCPGW_ADMIN_SESSION_KEY: must be exactly 32 bytes (64 hex chars), got %d bytes", len(decoded))
		}
		copy(adminSessionKey[:], decoded)
	}

	cfg := &Config{
		ListenAddr:           getEnv("MCPGW_LISTEN_ADDR", defaultListenAddr),
		TLSCertPath:          os.Getenv("MCPGW_TLS_CERT"),
		TLSKeyPath:           os.Getenv("MCPGW_TLS_KEY"),
		TLSCAPath:            os.Getenv("MCPGW_TLS_CA"),
		DBURL:                os.Getenv("MCPGW_DB_URL"),
		NATSURL:              os.Getenv("MCPGW_NATS_URL"),
		NATSTLSEnabled:       natsTLSEnabled,
		NATSTLSCert:          os.Getenv("MCPGW_NATS_TLS_CERT"),
		NATSTLSKey:           os.Getenv("MCPGW_NATS_TLS_KEY"),
		NATSTLSCa:            os.Getenv("MCPGW_NATS_TLS_CA"),
		OIDCIssuer:           os.Getenv("MCPGW_OIDC_ISSUER"),
		OIDCAudience:         os.Getenv("MCPGW_OIDC_AUDIENCE"),
		HeartbeatTTL:         heartbeatTTL,
		RateDefault:          rateDefault,
		RateBurst:            rateBurst,
		CircuitThreshold:     circuitThreshold,
		CircuitTimeout:       circuitTimeout,
		RetryMax:             retryMax,
		SPIFFEAllowList:      parseCSV("MCPGW_SPIFFE_ALLOW_PREFIX"),
		LogFormat:            getEnv("MCPGW_LOG_FORMAT", defaultLogFormat),
		LogLevel:             getEnv("MCPGW_LOG_LEVEL", defaultLogLevel),
		MetricsAddr:          getEnv("MCPGW_METRICS_ADDR", defaultMetricsAddr),
		CruveroEnabled:       cruveroEnabled,
		GatewayID:            gatewayID,
		CORSEnabled:          corsEnabled,
		CORSAllowedOrigins:   parseCSV("MCPGW_CORS_ALLOWED_ORIGINS"),
		DBMaxOpenConns:       dbMaxOpenConns,
		DBMaxIdleConns:       dbMaxIdleConns,
		DBConnMaxLifetime:    dbConnMaxLifetime,
		AuditRetentionDays:   auditRetentionDays,
		AuditCleanupInterval: auditCleanupInterval,
		ShutdownTimeout:      shutdownTimeout,
		RateLimitBackend:     rateLimitBackend,
		DragonflyURL:         os.Getenv("MCPGW_DRAGONFLY_URL"),
		OTLPExporterEndpoint:   getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTELServiceName:        getEnv("OTEL_SERVICE_NAME", defaultOTELServiceName),
		DeviceFlowEnabled:      deviceFlowEnabled,
		DeviceFlowIDPDeviceURL: os.Getenv("MCPGW_DEVICE_FLOW_IDP_DEVICE_URL"),
		DeviceFlowIDPTokenURL:  os.Getenv("MCPGW_DEVICE_FLOW_IDP_TOKEN_URL"),
		DeviceFlowClientID:     os.Getenv("MCPGW_DEVICE_FLOW_CLIENT_ID"),
		DeviceFlowClientSecret: os.Getenv("MCPGW_DEVICE_FLOW_CLIENT_SECRET"),
		AdminEnabled:           adminEnabled,
		AdminOIDCClientID:      os.Getenv("MCPGW_ADMIN_OIDC_CLIENT_ID"),
		AdminOIDCClientSecret:  os.Getenv("MCPGW_ADMIN_OIDC_CLIENT_SECRET"),
		AdminRequiredScope:     getEnv("MCPGW_ADMIN_REQUIRED_SCOPE", "admin"),
		AdminSessionKey:        adminSessionKey,
		AdminSessionTTL:        adminSessionTTL,
		AdminDevMode:           adminDevMode,
		ProgressiveDiscovery:   progressiveDiscovery,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required fields and constraints are satisfied.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.DBURL) == "" {
		return fmt.Errorf("validate config: MCPGW_DB_URL is required")
	}

	hasTLSCert := strings.TrimSpace(c.TLSCertPath) != ""
	hasTLSKey := strings.TrimSpace(c.TLSKeyPath) != ""
	if hasTLSCert != hasTLSKey {
		return fmt.Errorf("validate config: MCPGW_TLS_CERT and MCPGW_TLS_KEY must be set together")
	}

	if c.RateDefault <= 0 {
		return fmt.Errorf("validate config: MCPGW_RATE_DEFAULT must be positive")
	}

	if c.RateBurst <= 0 {
		return fmt.Errorf("validate config: MCPGW_RATE_BURST must be positive")
	}

	if c.CircuitThreshold <= 0 {
		return fmt.Errorf("validate config: MCPGW_CIRCUIT_THRESHOLD must be positive")
	}

	if c.RetryMax <= 0 {
		return fmt.Errorf("validate config: MCPGW_RETRY_MAX must be positive")
	}

	if c.CruveroEnabled && strings.TrimSpace(c.NATSURL) == "" {
		return fmt.Errorf("validate config: MCPGW_NATS_URL is required when MCPGW_CRUVERO_ENABLED is true")
	}

	if c.NATSTLSEnabled {
		if strings.TrimSpace(c.NATSTLSCert) == "" || strings.TrimSpace(c.NATSTLSKey) == "" || strings.TrimSpace(c.NATSTLSCa) == "" {
			return fmt.Errorf("validate config: MCPGW_NATS_TLS_CERT, MCPGW_NATS_TLS_KEY, and MCPGW_NATS_TLS_CA are all required when MCPGW_NATS_TLS_ENABLED is true")
		}
	}

	if c.CORSEnabled && len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("validate config: MCPGW_CORS_ALLOWED_ORIGINS is required when MCPGW_CORS_ENABLED is true")
	}

	if c.DBMaxOpenConns <= 0 {
		return fmt.Errorf("validate config: MCPGW_DB_MAX_OPEN_CONNS must be positive")
	}

	if c.DBMaxIdleConns <= 0 {
		return fmt.Errorf("validate config: MCPGW_DB_MAX_IDLE_CONNS must be positive")
	}

	if c.DBConnMaxLifetime <= 0 {
		return fmt.Errorf("validate config: MCPGW_DB_CONN_MAX_LIFETIME must be positive")
	}

	if c.AuditRetentionDays <= 0 {
		return fmt.Errorf("validate config: MCPGW_AUDIT_RETENTION_DAYS must be positive")
	}

	if c.AuditCleanupInterval <= 0 {
		return fmt.Errorf("validate config: MCPGW_AUDIT_CLEANUP_INTERVAL must be positive")
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("validate config: MCPGW_SHUTDOWN_TIMEOUT must be positive")
	}

	if c.ShutdownTimeout > 120*time.Second {
		return fmt.Errorf("validate config: MCPGW_SHUTDOWN_TIMEOUT must not exceed 120s")
	}

	switch c.RateLimitBackend {
	case "memory", "dragonfly", "nats", "":
		// valid
	default:
		return fmt.Errorf("validate config: MCPGW_RATE_LIMIT_BACKEND must be memory, dragonfly, or nats")
	}

	if c.RateLimitBackend == "dragonfly" && strings.TrimSpace(c.DragonflyURL) == "" {
		return fmt.Errorf("validate config: MCPGW_DRAGONFLY_URL is required when MCPGW_RATE_LIMIT_BACKEND is dragonfly")
	}

	if c.RateLimitBackend == "nats" && !c.CruveroEnabled {
		return fmt.Errorf("validate config: MCPGW_CRUVERO_ENABLED must be true when MCPGW_RATE_LIMIT_BACKEND is nats")
	}

	if c.AdminDevMode && !c.AdminEnabled {
		return fmt.Errorf("validate config: MCPGW_ADMIN_ENABLED must be true when MCPGW_ADMIN_DEV_MODE is true")
	}

	if c.AdminEnabled && !c.AdminDevMode {
		if strings.TrimSpace(c.AdminOIDCClientID) == "" {
			return fmt.Errorf("validate config: MCPGW_ADMIN_OIDC_CLIENT_ID is required when MCPGW_ADMIN_ENABLED is true")
		}
		if strings.TrimSpace(c.OIDCIssuer) == "" {
			return fmt.Errorf("validate config: MCPGW_OIDC_ISSUER is required when MCPGW_ADMIN_ENABLED is true")
		}
		emptyKey := [32]byte{}
		if c.AdminSessionKey == emptyKey {
			return fmt.Errorf("validate config: MCPGW_ADMIN_SESSION_KEY is required when MCPGW_ADMIN_ENABLED is true")
		}
	}

	if c.DeviceFlowEnabled {
		if strings.TrimSpace(c.DeviceFlowIDPDeviceURL) == "" {
			return fmt.Errorf("validate config: MCPGW_DEVICE_FLOW_IDP_DEVICE_URL is required when MCPGW_DEVICE_FLOW_ENABLED is true")
		}
		if strings.TrimSpace(c.DeviceFlowIDPTokenURL) == "" {
			return fmt.Errorf("validate config: MCPGW_DEVICE_FLOW_IDP_TOKEN_URL is required when MCPGW_DEVICE_FLOW_ENABLED is true")
		}
		if strings.TrimSpace(c.DeviceFlowClientID) == "" {
			return fmt.Errorf("validate config: MCPGW_DEVICE_FLOW_CLIENT_ID is required when MCPGW_DEVICE_FLOW_ENABLED is true")
		}
	}

	return nil
}

// IsTLSConfigured reports whether server TLS cert and key are configured.
func (c *Config) IsTLSConfigured() bool {
	return strings.TrimSpace(c.TLSCertPath) != "" && strings.TrimSpace(c.TLSKeyPath) != ""
}

func getEnv(key string, defaultValue string) string {
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func parseDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func parseInt(key string, defaultValue int) (int, error) {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func parseBool(key string, defaultValue bool) (bool, error) {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func parseCSV(key string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func generateUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	), nil
}
