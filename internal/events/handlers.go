package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/policy"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/types"
)

const (
	configCachePolicyKey         = "config.policy"
	configCacheServersKey        = "config.servers"
	configCacheServerSettingsKey = "config.server_settings"
	configCacheAuthKey           = "config.auth"
)

// PolicyConfigMessage describes incoming policy profile updates.
type PolicyConfigMessage struct {
	Profiles []types.PolicyProfile `json:"profiles"`
}

// ServerConfigMessage describes incoming server allowlist updates.
type ServerConfigMessage struct {
	SPIFFEAllowList []string `json:"spiffe_allow_list"`
}

// ServerSettingsConfig describes per-server effective non-secret settings.
type ServerSettingsConfig struct {
	ServerName        string         `json:"server_name"`
	EffectiveSettings map[string]any `json:"effective_settings"`
}

// ServerSettingsConfigMessage describes incoming versioned server settings updates.
type ServerSettingsConfigMessage struct {
	ConfigVersion int64                  `json:"config_version"`
	Servers       []ServerSettingsConfig `json:"servers"`
}

// AuthConfigMessage is a placeholder for future auth settings updates.
type AuthConfigMessage struct {
	Mode         string `json:"mode"`
	OIDCIssuer   string `json:"oidc_issuer"`
	OIDCAudience string `json:"oidc_audience"`
}

type policyEngineUpdater interface {
	ReplaceProfiles(profiles map[string]*types.PolicyProfile)
}

type serverConfigUpdater interface {
	UpdateSPIFFEAllowList(prefixes []string)
}

type serverSettingsUpdater interface {
	UpdateEffectiveSettings(configVersion int64, settingsByServer map[string]map[string]any) error
}

type registrationAckUpdater interface {
	AcknowledgeServerRegistration(
		ctx context.Context,
		registrationID string,
		leaseEpoch int64,
		capabilityHash string,
		registryVersion string,
		toolSchemaHash string,
		ackedAt time.Time,
	) error
}

// PolicyConfigHandler applies policy profile updates.
type PolicyConfigHandler struct {
	engine      policyEngineUpdater
	backend     ratelimit.LimiterBackend
	logger      *slog.Logger
	configStore ConfigStore
}

// NewPolicyConfigHandler creates a policy config handler.
func NewPolicyConfigHandler(engine *policy.Engine, backend ratelimit.LimiterBackend, logger *slog.Logger) *PolicyConfigHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &PolicyConfigHandler{
		engine:  engine,
		backend: backend,
		logger:  logger,
	}
}

// SetConfigStore sets an optional config persistence store.
func (h *PolicyConfigHandler) SetConfigStore(configStore ConfigStore) {
	if h == nil {
		return
	}
	h.configStore = configStore
}

// Handle validates and applies policy profiles from Cruvero config updates.
func (h *PolicyConfigHandler) Handle(ctx context.Context, data []byte) error {
	if h == nil {
		return fmt.Errorf("handle policy config: handler is nil")
	}

	var message PolicyConfigMessage
	if err := decodeConfigMessage(data, &message); err != nil {
		return fmt.Errorf("handle policy config: %w", err)
	}
	if len(message.Profiles) == 0 {
		return fmt.Errorf("handle policy config: profiles must not be empty")
	}

	profiles := make(map[string]*types.PolicyProfile, len(message.Profiles))
	for _, profile := range message.Profiles {
		normalized, err := normalizePolicyProfile(profile)
		if err != nil {
			return fmt.Errorf("handle policy config: %w", err)
		}
		profiles[normalized.Name] = normalized
	}

	defaultProfile, ok := profiles["default"]
	if !ok {
		return fmt.Errorf("handle policy config: default profile is required")
	}

	if h.engine != nil {
		h.engine.ReplaceProfiles(profiles)
	}
	if mb, ok := h.backend.(*ratelimit.MemoryBackend); ok && mb != nil {
		mb.SetDefaults(float64(defaultProfile.RateLimit), defaultProfile.RateBurst)
	}
	if h.configStore != nil {
		if err := h.configStore.Save(ctx, configCachePolicyKey, data); err != nil {
			return fmt.Errorf("handle policy config: persist config: %w", err)
		}
	}

	h.logger.InfoContext(ctx, "policy config updated", slog.Int("profiles", len(profiles)))
	return nil
}

// ServerConfigHandler applies registration server allowlist updates.
type ServerConfigHandler struct {
	registrationService serverConfigUpdater
	logger              *slog.Logger
	configStore         ConfigStore
}

// NewServerConfigHandler creates a server config handler.
func NewServerConfigHandler(registrationService serverConfigUpdater, logger *slog.Logger) *ServerConfigHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &ServerConfigHandler{registrationService: registrationService, logger: logger}
}

// SetConfigStore sets an optional config persistence store.
func (h *ServerConfigHandler) SetConfigStore(configStore ConfigStore) {
	if h == nil {
		return
	}
	h.configStore = configStore
}

// SetRegistrationService sets the optional registration updater dependency.
func (h *ServerConfigHandler) SetRegistrationService(registrationService serverConfigUpdater) {
	if h == nil {
		return
	}
	h.registrationService = registrationService
}

// Handle validates and applies server configuration updates.
func (h *ServerConfigHandler) Handle(ctx context.Context, data []byte) error {
	if h == nil {
		return fmt.Errorf("handle server config: handler is nil")
	}

	var message ServerConfigMessage
	if err := decodeConfigMessage(data, &message); err != nil {
		return fmt.Errorf("handle server config: %w", err)
	}

	allowList := make([]string, 0, len(message.SPIFFEAllowList))
	for _, prefix := range message.SPIFFEAllowList {
		trimmed := strings.TrimSpace(prefix)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "spiffe://") {
			return fmt.Errorf("handle server config: invalid SPIFFE prefix %q", trimmed)
		}
		allowList = append(allowList, trimmed)
	}

	if h.registrationService != nil {
		h.registrationService.UpdateSPIFFEAllowList(allowList)
	}
	if h.configStore != nil {
		if err := h.configStore.Save(ctx, configCacheServersKey, data); err != nil {
			return fmt.Errorf("handle server config: persist config: %w", err)
		}
	}

	h.logger.InfoContext(ctx, "server config updated", slog.Int("spiffe_prefix_count", len(allowList)))
	return nil
}

// ServerSettingsConfigHandler applies versioned non-secret backend settings.
type ServerSettingsConfigHandler struct {
	registrationService serverSettingsUpdater
	configStore         ConfigStore
	logger              *slog.Logger
}

// NewServerSettingsConfigHandler creates a server settings config handler.
func NewServerSettingsConfigHandler(registrationService serverSettingsUpdater, configStore ConfigStore, logger *slog.Logger) *ServerSettingsConfigHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &ServerSettingsConfigHandler{
		registrationService: registrationService,
		configStore:         configStore,
		logger:              logger,
	}
}

// SetRegistrationService sets the optional registration updater dependency.
func (h *ServerSettingsConfigHandler) SetRegistrationService(registrationService serverSettingsUpdater) {
	if h == nil {
		return
	}
	h.registrationService = registrationService
}

// Handle validates, versions, and applies effective server settings.
func (h *ServerSettingsConfigHandler) Handle(ctx context.Context, data []byte) error {
	if h == nil {
		return fmt.Errorf("handle server settings config: handler is nil")
	}

	var message ServerSettingsConfigMessage
	if err := decodeConfigMessage(data, &message); err != nil {
		return fmt.Errorf("handle server settings config: %w", err)
	}
	if message.ConfigVersion <= 0 {
		return fmt.Errorf("handle server settings config: config_version must be positive")
	}

	settingsByServer := make(map[string]map[string]any, len(message.Servers))
	for _, serverSettings := range message.Servers {
		serverName := strings.TrimSpace(serverSettings.ServerName)
		if serverName == "" {
			return fmt.Errorf("handle server settings config: server_name is required")
		}
		if len(serverSettings.EffectiveSettings) == 0 {
			return fmt.Errorf("handle server settings config: effective_settings cannot be empty for %q", serverName)
		}
		for key, value := range serverSettings.EffectiveSettings {
			if err := validateNonSecretSetting(key, value); err != nil {
				return fmt.Errorf("handle server settings config: server %q setting %q: %w", serverName, key, err)
			}
		}
		settingsByServer[serverName] = cloneSettingsMap(serverSettings.EffectiveSettings)
	}

	if h.registrationService != nil {
		if err := h.registrationService.UpdateEffectiveSettings(message.ConfigVersion, settingsByServer); err != nil {
			return fmt.Errorf("handle server settings config: apply settings: %w", err)
		}
	}
	if h.configStore != nil {
		if err := h.configStore.Save(ctx, configCacheServerSettingsKey, data); err != nil {
			return fmt.Errorf("handle server settings config: persist config: %w", err)
		}
	}

	h.logger.InfoContext(ctx, "server settings config updated", slog.Int64("config_version", message.ConfigVersion), slog.Int("server_count", len(settingsByServer)))
	return nil
}

// AuthConfigHandler validates auth config payloads.
type AuthConfigHandler struct {
	logger      *slog.Logger
	configStore ConfigStore
}

// ServerRegisteredAckHandler applies platform registration acknowledgements.
type ServerRegisteredAckHandler struct {
	registrationService registrationAckUpdater
	logger              *slog.Logger
}

// NewServerRegisteredAckHandler creates a server registration ack handler.
func NewServerRegisteredAckHandler(registrationService registrationAckUpdater, logger *slog.Logger) *ServerRegisteredAckHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &ServerRegisteredAckHandler{
		registrationService: registrationService,
		logger:              logger,
	}
}

// SetRegistrationService sets the optional registration ack updater.
func (h *ServerRegisteredAckHandler) SetRegistrationService(registrationService registrationAckUpdater) {
	if h == nil {
		return
	}
	h.registrationService = registrationService
}

// Handle validates and applies server registration ack payloads.
func (h *ServerRegisteredAckHandler) Handle(ctx context.Context, data []byte) error {
	if h == nil {
		return fmt.Errorf("handle server registration ack: handler is nil")
	}

	var message ServerRegisteredAckPayload
	if err := decodeConfigMessage(data, &message); err != nil {
		return fmt.Errorf("handle server registration ack: %w", err)
	}
	message.RegistrationID = strings.TrimSpace(message.RegistrationID)
	if message.RegistrationID == "" {
		return fmt.Errorf("handle server registration ack: registration_id is required")
	}
	if message.LeaseEpoch <= 0 {
		return fmt.Errorf("handle server registration ack: lease_epoch must be positive")
	}
	if message.AckedAt.IsZero() {
		message.AckedAt = time.Now().UTC()
	}

	if h.registrationService != nil {
		if err := h.registrationService.AcknowledgeServerRegistration(
			ctx,
			message.RegistrationID,
			message.LeaseEpoch,
			strings.TrimSpace(message.CapabilityHash),
			strings.TrimSpace(message.RegistryVersion),
			strings.TrimSpace(message.ToolSchemaHash),
			message.AckedAt,
		); err != nil {
			return fmt.Errorf("handle server registration ack: apply ack: %w", err)
		}
	}

	h.logger.InfoContext(
		ctx,
		"server registration ack applied",
		slog.String("registration_id", message.RegistrationID),
		slog.Int64("lease_epoch", message.LeaseEpoch),
		slog.String("registry_version", strings.TrimSpace(message.RegistryVersion)),
	)
	return nil
}

// NewAuthConfigHandler creates an auth config handler.
func NewAuthConfigHandler(logger *slog.Logger) *AuthConfigHandler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &AuthConfigHandler{logger: logger}
}

// SetConfigStore sets an optional config persistence store.
func (h *AuthConfigHandler) SetConfigStore(configStore ConfigStore) {
	if h == nil {
		return
	}
	h.configStore = configStore
}

// Handle validates payload shape and stores last-known-good auth config.
func (h *AuthConfigHandler) Handle(ctx context.Context, data []byte) error {
	if h == nil {
		return fmt.Errorf("handle auth config: handler is nil")
	}

	var message AuthConfigMessage
	if err := decodeConfigMessage(data, &message); err != nil {
		return fmt.Errorf("handle auth config: %w", err)
	}

	if h.configStore != nil {
		if err := h.configStore.Save(ctx, configCacheAuthKey, data); err != nil {
			return fmt.Errorf("handle auth config: persist config: %w", err)
		}
	}

	h.logger.InfoContext(ctx, "auth config received", slog.String("mode", strings.TrimSpace(message.Mode)))
	return nil
}

func decodeConfigMessage(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	// Allow additive fields so newer publishers remain compatible with older gateways.
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid config payload: %w", err)
	}
	return nil
}

func normalizePolicyProfile(profile types.PolicyProfile) (*types.PolicyProfile, error) {
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		return nil, fmt.Errorf("policy profile name is required")
	}
	if profile.RateLimit <= 0 {
		return nil, fmt.Errorf("policy profile %q rate_limit must be positive", name)
	}
	if profile.RateBurst <= 0 {
		return nil, fmt.Errorf("policy profile %q rate_burst must be positive", name)
	}
	if profile.EnforcementMode != types.ModeEnforce && profile.EnforcementMode != types.ModeAudit {
		return nil, fmt.Errorf("policy profile %q enforcement_mode must be enforce or audit", name)
	}

	return &types.PolicyProfile{
		Name:            name,
		RateLimit:       profile.RateLimit,
		RateBurst:       profile.RateBurst,
		ToolAllowlist:   normalizeStringList(profile.ToolAllowlist),
		ToolDenylist:    normalizeStringList(profile.ToolDenylist),
		EnforcementMode: profile.EnforcementMode,
	}, nil
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func validateNonSecretSetting(key string, value any) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return fmt.Errorf("setting key is empty")
	}

	lower := strings.ToLower(trimmed)
	for _, forbidden := range []string{"secret", "password", "token", "private_key", "apikey", "api_key"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("secret-like key is not allowed")
		}
	}

	switch typed := value.(type) {
	case nil, bool, string, float64, json.Number, int, int64:
		return nil
	case []any:
		for _, item := range typed {
			if err := validateNonSecretSetting(trimmed, item); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for nestedKey, nestedValue := range typed {
			if err := validateNonSecretSetting(nestedKey, nestedValue); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported setting value type %T", value)
	}
}

func cloneSettingsMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}

		switch typed := value.(type) {
		case map[string]any:
			out[trimmed] = cloneSettingsMap(typed)
		case []any:
			cloned := make([]any, 0, len(typed))
			for _, item := range typed {
				switch nested := item.(type) {
				case map[string]any:
					cloned = append(cloned, cloneSettingsMap(nested))
				default:
					cloned = append(cloned, nested)
				}
			}
			out[trimmed] = cloned
		default:
			out[trimmed] = typed
		}
	}
	return out
}
