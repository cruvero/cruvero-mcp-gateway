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
	errParseFmt = "parse %s: %w"

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
	AdminExternalURL       string        `json:"admin_external_url"`
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
	dur, err := loadDurations()
	if err != nil {
		return nil, err
	}

	ints, err := loadIntegers()
	if err != nil {
		return nil, err
	}

	bools, err := loadBooleans()
	if err != nil {
		return nil, err
	}

	gatewayID, err := resolveGatewayID()
	if err != nil {
		return nil, err
	}

	adminSessionKey, err := loadAdminSessionKey()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		ListenAddr:             getEnv("MCPGW_LISTEN_ADDR", defaultListenAddr),
		TLSCertPath:            os.Getenv("MCPGW_TLS_CERT"),
		TLSKeyPath:             os.Getenv("MCPGW_TLS_KEY"),
		TLSCAPath:              os.Getenv("MCPGW_TLS_CA"),
		DBURL:                  os.Getenv("MCPGW_DB_URL"),
		NATSURL:                os.Getenv("MCPGW_NATS_URL"),
		NATSTLSEnabled:         bools.natsTLSEnabled,
		NATSTLSCert:            os.Getenv("MCPGW_NATS_TLS_CERT"),
		NATSTLSKey:             os.Getenv("MCPGW_NATS_TLS_KEY"),
		NATSTLSCa:              os.Getenv("MCPGW_NATS_TLS_CA"),
		OIDCIssuer:             os.Getenv("MCPGW_OIDC_ISSUER"),
		OIDCAudience:           os.Getenv("MCPGW_OIDC_AUDIENCE"),
		HeartbeatTTL:           dur.heartbeatTTL,
		RateDefault:            ints.rateDefault,
		RateBurst:              ints.rateBurst,
		CircuitThreshold:       ints.circuitThreshold,
		CircuitTimeout:         dur.circuitTimeout,
		RetryMax:               ints.retryMax,
		SPIFFEAllowList:        parseCSV("MCPGW_SPIFFE_ALLOW_PREFIX"),
		LogFormat:              getEnv("MCPGW_LOG_FORMAT", defaultLogFormat),
		LogLevel:               getEnv("MCPGW_LOG_LEVEL", defaultLogLevel),
		MetricsAddr:            getEnv("MCPGW_METRICS_ADDR", defaultMetricsAddr),
		CruveroEnabled:         bools.cruveroEnabled,
		GatewayID:              gatewayID,
		CORSEnabled:            bools.corsEnabled,
		CORSAllowedOrigins:     parseCSV("MCPGW_CORS_ALLOWED_ORIGINS"),
		DBMaxOpenConns:         ints.dbMaxOpenConns,
		DBMaxIdleConns:         ints.dbMaxIdleConns,
		DBConnMaxLifetime:      dur.dbConnMaxLifetime,
		AuditRetentionDays:     ints.auditRetentionDays,
		AuditCleanupInterval:   dur.auditCleanupInterval,
		ShutdownTimeout:        dur.shutdownTimeout,
		RateLimitBackend:       getEnv("MCPGW_RATE_LIMIT_BACKEND", "memory"),
		DragonflyURL:           os.Getenv("MCPGW_DRAGONFLY_URL"),
		OTLPExporterEndpoint:   getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		OTELServiceName:        getEnv("OTEL_SERVICE_NAME", defaultOTELServiceName),
		DeviceFlowEnabled:      bools.deviceFlowEnabled,
		DeviceFlowIDPDeviceURL: os.Getenv("MCPGW_DEVICE_FLOW_IDP_DEVICE_URL"),
		DeviceFlowIDPTokenURL:  os.Getenv("MCPGW_DEVICE_FLOW_IDP_TOKEN_URL"),
		DeviceFlowClientID:     os.Getenv("MCPGW_DEVICE_FLOW_CLIENT_ID"),
		DeviceFlowClientSecret: os.Getenv("MCPGW_DEVICE_FLOW_CLIENT_SECRET"),
		AdminEnabled:           bools.adminEnabled,
		AdminExternalURL:       os.Getenv("MCPGW_ADMIN_EXTERNAL_URL"),
		AdminOIDCClientID:      os.Getenv("MCPGW_ADMIN_OIDC_CLIENT_ID"),
		AdminOIDCClientSecret:  os.Getenv("MCPGW_ADMIN_OIDC_CLIENT_SECRET"),
		AdminRequiredScope:     getEnv("MCPGW_ADMIN_REQUIRED_SCOPE", "admin"),
		AdminSessionKey:        adminSessionKey,
		AdminSessionTTL:        dur.adminSessionTTL,
		AdminDevMode:           bools.adminDevMode,
		ProgressiveDiscovery:   bools.progressiveDiscovery,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// parsedDurations holds all duration values parsed from environment variables.
type parsedDurations struct {
	heartbeatTTL         time.Duration
	circuitTimeout       time.Duration
	dbConnMaxLifetime    time.Duration
	auditCleanupInterval time.Duration
	shutdownTimeout      time.Duration
	adminSessionTTL      time.Duration
}

// loadDurations parses all MCPGW_* duration environment variables.
func loadDurations() (parsedDurations, error) {
	heartbeatTTL, err := parseDuration("MCPGW_HEARTBEAT_TTL", defaultHeartbeatTTL)
	if err != nil {
		return parsedDurations{}, err
	}
	circuitTimeout, err := parseDuration("MCPGW_CIRCUIT_TIMEOUT", defaultCircuitTimeout)
	if err != nil {
		return parsedDurations{}, err
	}
	dbConnMaxLifetime, err := parseDuration("MCPGW_DB_CONN_MAX_LIFETIME", defaultDBConnMaxLifetime)
	if err != nil {
		return parsedDurations{}, err
	}
	auditCleanupInterval, err := parseDuration("MCPGW_AUDIT_CLEANUP_INTERVAL", defaultAuditCleanupInterval)
	if err != nil {
		return parsedDurations{}, err
	}
	shutdownTimeout, err := parseDuration("MCPGW_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return parsedDurations{}, err
	}
	adminSessionTTL, err := parseDuration("MCPGW_ADMIN_SESSION_TTL", 8*time.Hour)
	if err != nil {
		return parsedDurations{}, err
	}
	return parsedDurations{
		heartbeatTTL:         heartbeatTTL,
		circuitTimeout:       circuitTimeout,
		dbConnMaxLifetime:    dbConnMaxLifetime,
		auditCleanupInterval: auditCleanupInterval,
		shutdownTimeout:      shutdownTimeout,
		adminSessionTTL:      adminSessionTTL,
	}, nil
}

// parsedIntegers holds all integer values parsed from environment variables.
type parsedIntegers struct {
	rateDefault        int
	rateBurst          int
	circuitThreshold   int
	retryMax           int
	dbMaxOpenConns     int
	dbMaxIdleConns     int
	auditRetentionDays int
}

// loadIntegers parses all MCPGW_* integer environment variables.
func loadIntegers() (parsedIntegers, error) {
	var p parsedIntegers
	fields := []struct {
		envKey     string
		defaultVal int
		dest       *int
	}{
		{"MCPGW_RATE_DEFAULT", defaultRateDefault, &p.rateDefault},
		{"MCPGW_RATE_BURST", defaultRateBurst, &p.rateBurst},
		{"MCPGW_CIRCUIT_THRESHOLD", defaultCircuitThreshold, &p.circuitThreshold},
		{"MCPGW_RETRY_MAX", defaultRetryMax, &p.retryMax},
		{"MCPGW_DB_MAX_OPEN_CONNS", defaultDBMaxOpenConns, &p.dbMaxOpenConns},
		{"MCPGW_DB_MAX_IDLE_CONNS", defaultDBMaxIdleConns, &p.dbMaxIdleConns},
		{"MCPGW_AUDIT_RETENTION_DAYS", defaultAuditRetentionDays, &p.auditRetentionDays},
	}
	for _, f := range fields {
		v, err := parseInt(f.envKey, f.defaultVal)
		if err != nil {
			return parsedIntegers{}, err
		}
		*f.dest = v
	}
	return p, nil
}

// parsedBooleans holds all boolean values parsed from environment variables.
type parsedBooleans struct {
	cruveroEnabled       bool
	corsEnabled          bool
	natsTLSEnabled       bool
	deviceFlowEnabled    bool
	adminEnabled         bool
	adminDevMode         bool
	progressiveDiscovery bool
}

// loadBooleans parses all MCPGW_* boolean environment variables.
func loadBooleans() (parsedBooleans, error) {
	var p parsedBooleans
	fields := []struct {
		envKey     string
		defaultVal bool
		dest       *bool
	}{
		{"MCPGW_CRUVERO_ENABLED", defaultCruveroEnabled, &p.cruveroEnabled},
		{"MCPGW_CORS_ENABLED", defaultCORSEnabled, &p.corsEnabled},
		{"MCPGW_NATS_TLS_ENABLED", false, &p.natsTLSEnabled},
		{"MCPGW_DEVICE_FLOW_ENABLED", false, &p.deviceFlowEnabled},
		{"MCPGW_ADMIN_ENABLED", false, &p.adminEnabled},
		{"MCPGW_ADMIN_DEV_MODE", false, &p.adminDevMode},
		{"MCPGW_PROGRESSIVE_DISCOVERY", defaultProgressiveDiscovery, &p.progressiveDiscovery},
	}
	for _, f := range fields {
		v, err := parseBool(f.envKey, f.defaultVal)
		if err != nil {
			return parsedBooleans{}, err
		}
		*f.dest = v
	}
	return p, nil
}

// resolveGatewayID reads the gateway ID from the environment and generates a
// UUID when the value is "auto" (default).
func resolveGatewayID() (string, error) {
	id := getEnv("MCPGW_GATEWAY_ID", defaultGatewayID)
	if id != "auto" {
		return id, nil
	}
	generated, err := generateUUID()
	if err != nil {
		return "", fmt.Errorf("generate gateway id: %w", err)
	}
	return generated, nil
}

// loadAdminSessionKey reads and decodes the admin session key from the
// MCPGW_ADMIN_SESSION_KEY environment variable.
func loadAdminSessionKey() ([32]byte, error) {
	rawKey := strings.TrimSpace(os.Getenv("MCPGW_ADMIN_SESSION_KEY"))
	if rawKey == "" {
		return [32]byte{}, nil
	}
	decoded, err := hex.DecodeString(rawKey)
	if err != nil {
		return [32]byte{}, fmt.Errorf("parse MCPGW_ADMIN_SESSION_KEY: %w", err)
	}
	if len(decoded) != 32 {
		return [32]byte{}, fmt.Errorf("parse MCPGW_ADMIN_SESSION_KEY: must be exactly 32 bytes (64 hex chars), got %d bytes", len(decoded))
	}
	var key [32]byte
	copy(key[:], decoded)
	return key, nil
}

// Validate checks that required fields and constraints are satisfied.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.DBURL) == "" {
		return fmt.Errorf("validate config: MCPGW_DB_URL is required")
	}
	if err := c.validateTLS(); err != nil {
		return err
	}
	if err := c.validateResilienceFields(); err != nil {
		return err
	}
	if err := c.validateNATS(); err != nil {
		return err
	}
	if err := c.validateCORS(); err != nil {
		return err
	}
	if err := c.validateDBAndAuditFields(); err != nil {
		return err
	}
	if err := c.validateShutdown(); err != nil {
		return err
	}
	if err := c.validateRateLimitBackend(); err != nil {
		return err
	}
	if err := c.validateAdmin(); err != nil {
		return err
	}
	return c.validateDeviceFlow()
}

func (c *Config) validateTLS() error {
	hasTLSCert := strings.TrimSpace(c.TLSCertPath) != ""
	hasTLSKey := strings.TrimSpace(c.TLSKeyPath) != ""
	if hasTLSCert != hasTLSKey {
		return fmt.Errorf("validate config: MCPGW_TLS_CERT and MCPGW_TLS_KEY must be set together")
	}
	return nil
}

func (c *Config) validateResilienceFields() error {
	checks := []struct {
		value int
		name  string
	}{
		{c.RateDefault, "MCPGW_RATE_DEFAULT"},
		{c.RateBurst, "MCPGW_RATE_BURST"},
		{c.CircuitThreshold, "MCPGW_CIRCUIT_THRESHOLD"},
		{c.RetryMax, "MCPGW_RETRY_MAX"},
	}
	for _, chk := range checks {
		if chk.value <= 0 {
			return fmt.Errorf("validate config: %s must be positive", chk.name)
		}
	}
	return nil
}

func (c *Config) validateNATS() error {
	if c.CruveroEnabled && strings.TrimSpace(c.NATSURL) == "" {
		return fmt.Errorf("validate config: MCPGW_NATS_URL is required when MCPGW_CRUVERO_ENABLED is true")
	}
	if c.NATSTLSEnabled {
		if strings.TrimSpace(c.NATSTLSCert) == "" || strings.TrimSpace(c.NATSTLSKey) == "" || strings.TrimSpace(c.NATSTLSCa) == "" {
			return fmt.Errorf("validate config: MCPGW_NATS_TLS_CERT, MCPGW_NATS_TLS_KEY, and MCPGW_NATS_TLS_CA are all required when MCPGW_NATS_TLS_ENABLED is true")
		}
	}
	return nil
}

func (c *Config) validateCORS() error {
	if c.CORSEnabled && len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("validate config: MCPGW_CORS_ALLOWED_ORIGINS is required when MCPGW_CORS_ENABLED is true")
	}
	return nil
}

func (c *Config) validateDBAndAuditFields() error {
	checks := []struct {
		value int
		name  string
	}{
		{c.DBMaxOpenConns, "MCPGW_DB_MAX_OPEN_CONNS"},
		{c.DBMaxIdleConns, "MCPGW_DB_MAX_IDLE_CONNS"},
		{c.AuditRetentionDays, "MCPGW_AUDIT_RETENTION_DAYS"},
	}
	for _, chk := range checks {
		if chk.value <= 0 {
			return fmt.Errorf("validate config: %s must be positive", chk.name)
		}
	}

	durationChecks := []struct {
		value time.Duration
		name  string
	}{
		{c.DBConnMaxLifetime, "MCPGW_DB_CONN_MAX_LIFETIME"},
		{c.AuditCleanupInterval, "MCPGW_AUDIT_CLEANUP_INTERVAL"},
	}
	for _, chk := range durationChecks {
		if chk.value <= 0 {
			return fmt.Errorf("validate config: %s must be positive", chk.name)
		}
	}
	return nil
}

func (c *Config) validateShutdown() error {
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("validate config: MCPGW_SHUTDOWN_TIMEOUT must be positive")
	}
	if c.ShutdownTimeout > 120*time.Second {
		return fmt.Errorf("validate config: MCPGW_SHUTDOWN_TIMEOUT must not exceed 120s")
	}
	return nil
}

func (c *Config) validateRateLimitBackend() error {
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
	return nil
}

func (c *Config) validateAdmin() error {
	if c.AdminDevMode && !c.AdminEnabled {
		return fmt.Errorf("validate config: MCPGW_ADMIN_ENABLED must be true when MCPGW_ADMIN_DEV_MODE is true")
	}
	if !c.AdminEnabled || c.AdminDevMode {
		return nil
	}
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
	return nil
}

func (c *Config) validateDeviceFlow() error {
	if !c.DeviceFlowEnabled {
		return nil
	}
	if strings.TrimSpace(c.DeviceFlowIDPDeviceURL) == "" {
		return fmt.Errorf("validate config: MCPGW_DEVICE_FLOW_IDP_DEVICE_URL is required when MCPGW_DEVICE_FLOW_ENABLED is true")
	}
	if strings.TrimSpace(c.DeviceFlowIDPTokenURL) == "" {
		return fmt.Errorf("validate config: MCPGW_DEVICE_FLOW_IDP_TOKEN_URL is required when MCPGW_DEVICE_FLOW_ENABLED is true")
	}
	if strings.TrimSpace(c.DeviceFlowClientID) == "" {
		return fmt.Errorf("validate config: MCPGW_DEVICE_FLOW_CLIENT_ID is required when MCPGW_DEVICE_FLOW_ENABLED is true")
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
		return 0, fmt.Errorf(errParseFmt, key, err)
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
		return 0, fmt.Errorf(errParseFmt, key, err)
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
		return false, fmt.Errorf(errParseFmt, key, err)
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
