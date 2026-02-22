package registration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/policy"
	servermetrics "github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

const errDeregisterFmt = "deregister: %w"

var (
	// ErrInvalidRequest indicates request payload validation failed.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrUnauthorized indicates the caller is not authenticated.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden indicates the caller is authenticated but not authorized.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound indicates the requested registration does not exist.
	ErrNotFound = errors.New("not found")
)

// Service implements registration business logic.
type Service struct {
	serverStore         store.ServerStore
	auditStore          store.AuditStore
	classificationStore store.ToolClassificationStore
	config              *config.Config
	logger              *slog.Logger
	publisher           LifecycleEventPublisher
	broadcaster         Broadcaster

	mu                sync.RWMutex
	spiffeAllowList   []string
	settingsVersion   int64
	effectiveSettings map[string]map[string]any
}

// LifecycleEventPublisher publishes registration lifecycle events.
type LifecycleEventPublisher interface {
	PublishServerRegistered(ctx context.Context, server types.ServerRecord) error
	PublishServerDeregistered(ctx context.Context, serverID string, name string, reason string) error
	PublishServerHealthChanged(
		ctx context.Context,
		serverID string,
		name string,
		oldStatus types.ServerStatus,
		newStatus types.ServerStatus,
	) error
}

// NewService creates a registration service.
func NewService(serverStore store.ServerStore, auditStore store.AuditStore, cfg *config.Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	allowList := []string(nil)
	if cfg != nil {
		allowList = cfg.SPIFFEAllowList
	}

	return &Service{
		serverStore:       serverStore,
		auditStore:        auditStore,
		config:            cfg,
		logger:            logger,
		spiffeAllowList:   copyStringSlice(allowList),
		effectiveSettings: make(map[string]map[string]any),
	}
}

// SetLifecycleEventPublisher sets an optional lifecycle events publisher.
func (s *Service) SetLifecycleEventPublisher(publisher LifecycleEventPublisher) {
	if s == nil {
		return
	}
	s.publisher = publisher
}

// SetBroadcaster sets an optional cross-pod broadcaster for registration events.
func (s *Service) SetBroadcaster(b Broadcaster) {
	if s == nil {
		return
	}
	s.broadcaster = b
}

// SetClassificationStore wires a tool classification store for auto-classification.
func (s *Service) SetClassificationStore(classificationStore store.ToolClassificationStore) {
	if s == nil {
		return
	}
	s.classificationStore = classificationStore
}

// UpdateSPIFFEAllowList replaces the registration SPIFFE prefix allowlist.
func (s *Service) UpdateSPIFFEAllowList(prefixes []string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.spiffeAllowList = copyStringSlice(prefixes)
}

// UpdateEffectiveSettings replaces versioned effective settings for registered backends.
func (s *Service) UpdateEffectiveSettings(configVersion int64, settingsByServer map[string]map[string]any) error {
	if s == nil {
		return fmt.Errorf("update effective settings: service is nil")
	}
	if configVersion <= 0 {
		return fmt.Errorf("update effective settings: config version must be positive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if configVersion < s.settingsVersion {
		return fmt.Errorf("update effective settings: stale config version %d < %d", configVersion, s.settingsVersion)
	}

	next := make(map[string]map[string]any, len(settingsByServer))
	for serverName, settings := range settingsByServer {
		name := strings.TrimSpace(serverName)
		if name == "" {
			continue
		}
		next[name] = cloneSettings(settings)
	}

	s.settingsVersion = configVersion
	s.effectiveSettings = next
	return nil
}

// Register creates or updates a server registration from an mTLS identity.
func (s *Service) Register(ctx context.Context, caller *identitypkg.Identity, req RegistrationRequest) (*RegistrationResponse, error) {
	if caller == nil || caller.Type != identitypkg.IdentityMTLS {
		return nil, fmt.Errorf("register: %w", ErrUnauthorized)
	}
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("register: %w: %v", ErrInvalidRequest, err)
	}
	if err := identitypkg.ValidateSPIFFEID(caller.ID, s.currentSPIFFEAllowList()); err != nil {
		return nil, fmt.Errorf("register: %w: %v", ErrForbidden, err)
	}

	now := time.Now().UTC()
	policySnapshot := defaultPolicyProfile(s.config)
	capabilityHash := hashCapabilities(req.Capabilities)
	record := &types.ServerRecord{
		ID:           newUUID(),
		Name:         strings.TrimSpace(req.ServiceName),
		SPIFFEID:     caller.ID,
		Version:      strings.TrimSpace(req.Version),
		Host:         strings.TrimSpace(req.Listen.Host),
		Port:         req.Listen.Port,
		Protocol:     normalizedListenProtocol(req.Listen.Protocol),
		Capabilities: req.Capabilities,
		// Register marks servers active immediately so newly registered tools
		// are routable before the first heartbeat arrives.
		Status:                 types.StatusActive,
		PolicyProfile:          policySnapshot.Name,
		LeaseEpoch:             1,
		CapabilityHash:         capabilityHash,
		SyncState:              types.SyncStateUnacked,
		LastPlatformAckVersion: "",
		LastPlatformAckAt:      nil,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	existing, err := s.serverStore.GetBySPIFFEID(ctx, caller.ID)
	created := false
	switch {
	case err == nil && existing != nil:
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		record.LeaseEpoch = nextLeaseEpoch(existing.LeaseEpoch)
		if updateErr := s.serverStore.Update(ctx, record); updateErr != nil {
			return nil, fmt.Errorf("register: update existing registration: %w", updateErr)
		}
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("register: lookup by spiffe id: %w", err)
	default:
		if createErr := s.serverStore.Create(ctx, record); createErr != nil {
			return nil, fmt.Errorf("register: create registration: %w", createErr)
		}
		created = true
	}
	if created {
		servermetrics.AddActiveRegistrations(record.Status.String(), 1)
	}

	s.postRegister(ctx, caller.ID, record, req.Capabilities)

	heartbeatIntervalSeconds := heartbeatIntervalSeconds(s.config)
	configVersion, effectiveSettings := s.effectiveSettingsForServer(record.Name)
	return &RegistrationResponse{
		InstanceID:               record.ID,
		RegistrationID:           record.ID,
		LeaseEpoch:               record.LeaseEpoch,
		CapabilityHash:           record.CapabilityHash,
		SyncState:                record.SyncState,
		LastPlatformAckVersion:   record.LastPlatformAckVersion,
		PolicySnapshot:           policySnapshot,
		HeartbeatInterval:        heartbeatIntervalSeconds,
		HeartbeatIntervalSeconds: heartbeatIntervalSeconds,
		ConfigVersion:            configVersion,
		EffectiveSettings:        effectiveSettings,
		Status:                   record.Status,
	}, nil
}

func (s *Service) postRegister(ctx context.Context, callerID string, record *types.ServerRecord, capabilities types.Capability) {
	s.logAudit(ctx, &types.AuditEntry{
		EventType:  "server.registered",
		ClientID:   callerID,
		ServerName: record.Name,
		Details: map[string]any{
			"server_id": record.ID,
			"spiffe_id": record.SPIFFEID,
		},
	})
	if s.publisher != nil {
		if err := s.publisher.PublishServerRegistered(ctx, *record); err != nil {
			s.logger.ErrorContext(ctx, "publish server registered event failed", slog.String("error", err.Error()))
		}
	}
	s.publishBroadcast("registered", record.ID)
	s.classifyNewTools(ctx, capabilities, callerID)
}

// AcknowledgeServerRegistration marks a registration lease as platform-synced.
func (s *Service) AcknowledgeServerRegistration(
	ctx context.Context,
	registrationID string,
	leaseEpoch int64,
	capabilityHash string,
	ackVersion string,
	_ string,
	ackedAt time.Time,
) error {
	if strings.TrimSpace(registrationID) == "" {
		return fmt.Errorf("ack registration: %w: missing registration id", ErrInvalidRequest)
	}
	if leaseEpoch <= 0 {
		return fmt.Errorf("ack registration: %w: invalid lease epoch", ErrInvalidRequest)
	}
	if ackedAt.IsZero() {
		ackedAt = time.Now().UTC()
	}

	if err := s.serverStore.AcknowledgeRegistration(
		ctx,
		registrationID,
		leaseEpoch,
		capabilityHash,
		ackVersion,
		ackedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ack registration: %w", ErrNotFound)
		}
		return fmt.Errorf("ack registration: %w", err)
	}
	return nil
}

// List returns registrations matching the provided filter.
func (s *Service) List(ctx context.Context, filter types.ServerFilter) ([]types.ServerRecord, error) {
	records, err := s.serverStore.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list registrations: %w", err)
	}
	return records, nil
}

// Deregister removes a registration when the caller is admin or self.
func (s *Service) Deregister(ctx context.Context, caller *identitypkg.Identity, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("deregister: %w: missing id", ErrInvalidRequest)
	}
	if caller == nil {
		return fmt.Errorf(errDeregisterFmt, ErrUnauthorized)
	}

	record, err := s.serverStore.Get(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf(errDeregisterFmt, ErrNotFound)
		}
		return fmt.Errorf("deregister: get registration: %w", err)
	}

	isAdmin := caller.HasScope(identitypkg.ScopeAdmin)
	isSelf := caller.Type == identitypkg.IdentityMTLS && caller.ID == record.SPIFFEID
	if !isAdmin && !isSelf {
		return fmt.Errorf(errDeregisterFmt, ErrForbidden)
	}

	if err := s.serverStore.Delete(ctx, id); err != nil {
		return fmt.Errorf("deregister: delete registration: %w", err)
	}
	servermetrics.AddActiveRegistrations(record.Status.String(), -1)

	s.logAudit(ctx, &types.AuditEntry{
		EventType:  "server.deregistered",
		ClientID:   caller.ID,
		ServerName: record.Name,
		Details: map[string]any{
			"server_id": record.ID,
			"spiffe_id": record.SPIFFEID,
		},
	})
	if s.publisher != nil {
		if err := s.publisher.PublishServerDeregistered(ctx, record.ID, record.Name, "deregistered"); err != nil {
			s.logger.ErrorContext(ctx, "publish server deregistered event failed", slog.String("error", err.Error()))
		}
	}
	s.publishBroadcast("deregistered", record.ID)

	return nil
}

func (s *Service) classifyNewTools(ctx context.Context, cap types.Capability, callerID string) {
	if s.classificationStore == nil || len(cap.Tools) == 0 {
		return
	}

	for _, toolName := range cap.Tools {
		name := strings.TrimSpace(toolName)
		if name == "" {
			continue
		}

		existing, err := s.classificationStore.Get(ctx, name)
		if err != nil {
			s.logger.ErrorContext(ctx, "classification lookup failed",
				slog.String("tool", name),
				slog.String("error", err.Error()),
			)
			continue
		}

		if existing != nil && !existing.AutoClassified {
			continue
		}

		riskLevel, reason := policy.AutoClassify(name, "")
		tc := &types.ToolClassification{
			ToolName:       name,
			RiskLevel:      riskLevel,
			Reason:         reason,
			AutoClassified: true,
			UpdatedBy:      callerID,
			UpdatedAt:      time.Now().UTC(),
		}

		if err := s.classificationStore.Upsert(ctx, tc); err != nil {
			s.logger.ErrorContext(ctx, "auto-classify tool failed",
				slog.String("tool", name),
				slog.String("error", err.Error()),
			)
		}
	}
}

func (s *Service) publishBroadcast(eventType, serverID string) {
	if s.broadcaster == nil {
		return
	}

	evt := NewRegistrationEvent(eventType, serverID)
	data, err := json.Marshal(evt)
	if err != nil {
		s.logger.Error("marshal broadcast event failed", slog.String("error", err.Error()))
		return
	}
	if err := s.broadcaster.Publish(SubjectRegistryUpdated, data); err != nil {
		s.logger.Error("publish broadcast event failed",
			slog.String("event_type", eventType),
			slog.String("server_id", serverID),
			slog.String("error", err.Error()),
		)
	}
}

func (s *Service) logAudit(ctx context.Context, entry *types.AuditEntry) {
	if s.auditStore == nil || entry == nil {
		return
	}
	if err := s.auditStore.Log(ctx, entry); err != nil {
		s.logger.ErrorContext(ctx, "audit log failed", slog.String("error", err.Error()))
	}
}

func defaultPolicyProfile(cfg *config.Config) *types.PolicyProfile {
	rateLimit := 10
	rateBurst := 20
	if cfg != nil {
		if cfg.RateDefault > 0 {
			rateLimit = cfg.RateDefault
		}
		if cfg.RateBurst > 0 {
			rateBurst = cfg.RateBurst
		}
	}

	return &types.PolicyProfile{
		Name:            "default",
		RateLimit:       rateLimit,
		RateBurst:       rateBurst,
		ToolAllowlist:   []string{},
		ToolDenylist:    []string{},
		EnforcementMode: types.ModeEnforce,
	}
}

func heartbeatIntervalSeconds(cfg *config.Config) int {
	if cfg == nil || cfg.HeartbeatTTL <= 0 {
		return 30
	}
	seconds := int(cfg.HeartbeatTTL / time.Second)
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func newUUID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("reg-%d", time.Now().UnixNano())
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
	)
}

func normalizedListenProtocol(raw string) string {
	protocol := strings.ToLower(strings.TrimSpace(raw))
	if protocol == "http" || protocol == "https" {
		return protocol
	}
	return "https"
}

func (s *Service) currentSPIFFEAllowList() []string {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyStringSlice(s.spiffeAllowList)
}

func (s *Service) effectiveSettingsForServer(serverName string) (int64, map[string]any) {
	if s == nil {
		return 0, map[string]any{}
	}

	key := strings.TrimSpace(serverName)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if settings, ok := s.effectiveSettings[key]; ok {
		return s.settingsVersion, cloneSettings(settings)
	}
	return s.settingsVersion, map[string]any{}
}

func copyStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneSettings(input map[string]any) map[string]any {
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
			out[trimmed] = cloneSettings(typed)
		case []any:
			cloned := make([]any, 0, len(typed))
			for _, item := range typed {
				switch nested := item.(type) {
				case map[string]any:
					cloned = append(cloned, cloneSettings(nested))
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

func nextLeaseEpoch(current int64) int64 {
	if current <= 0 {
		return 1
	}
	return current + 1
}

func hashCapabilities(cap types.Capability) string {
	normalized := types.Capability{
		Tools:     append([]string(nil), cap.Tools...),
		Resources: append([]string(nil), cap.Resources...),
		Prompts:   append([]string(nil), cap.Prompts...),
	}
	sort.Strings(normalized.Tools)
	sort.Strings(normalized.Resources)
	sort.Strings(normalized.Prompts)

	payload, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
