package policy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Engine evaluates tool requests against configured policy profiles.
type Engine struct {
	mu                  sync.RWMutex
	profiles            map[string]*types.PolicyProfile
	dangerousPatterns   []*regexp.Regexp
	auditStore          store.AuditStore
	classificationStore store.ToolClassificationStore
	classificationCache *ClassificationCache
	logger              *slog.Logger
	publisher           ViolationEventPublisher
}

// ViolationEventPublisher publishes policy violation events.
type ViolationEventPublisher interface {
	PublishPolicyViolated(ctx context.Context, clientID string, toolName string, violations []string, decision string) error
}

// NewEngine creates a policy engine with compiled dangerous command patterns.
func NewEngine(
	profiles map[string]*types.PolicyProfile,
	auditStore store.AuditStore,
	logger *slog.Logger,
) *Engine {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	copiedProfiles := make(map[string]*types.PolicyProfile, len(profiles))
	for name, profile := range profiles {
		copiedProfiles[strings.TrimSpace(name)] = profile
	}
	if _, ok := copiedProfiles["default"]; !ok {
		copiedProfiles["default"] = &types.PolicyProfile{
			Name:            "default",
			EnforcementMode: types.ModeEnforce,
		}
	}

	patterns, err := CompilePatterns(DefaultDangerousPatterns)
	if err != nil {
		logger.Error("compile dangerous patterns failed", slog.String("error", err.Error()))
		patterns = nil
	}

	return &Engine{
		profiles:          copiedProfiles,
		dangerousPatterns: patterns,
		auditStore:        auditStore,
		logger:            logger,
	}
}

// SetAuditStore replaces the engine's audit store at runtime.
func (e *Engine) SetAuditStore(auditStore store.AuditStore) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.auditStore = auditStore
}

// SetClassificationStore wires a tool classification store and initializes the cache.
func (e *Engine) SetClassificationStore(classificationStore store.ToolClassificationStore) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.classificationStore = classificationStore
	e.classificationCache = NewClassificationCache(defaultClassificationCacheTTL)
}

// SetViolationEventPublisher sets an optional policy-violation event publisher.
func (e *Engine) SetViolationEventPublisher(publisher ViolationEventPublisher) {
	if e == nil {
		return
	}
	e.publisher = publisher
}

// ClassificationCache returns the engine's classification cache, if initialized.
func (e *Engine) ClassificationCache() *ClassificationCache {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.classificationCache
}

// ReplaceProfiles replaces all in-memory policy profiles with validated values.
func (e *Engine) ReplaceProfiles(profiles map[string]*types.PolicyProfile) {
	if e == nil {
		return
	}

	copied := make(map[string]*types.PolicyProfile, len(profiles))
	for name, profile := range profiles {
		copied[strings.TrimSpace(name)] = profile
	}
	if _, ok := copied["default"]; !ok {
		copied["default"] = &types.PolicyProfile{
			Name:            "default",
			EnforcementMode: types.ModeEnforce,
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.profiles = copied
}

// Evaluate applies allowlist, denylist, dangerous-pattern, and schema checks.
func (e *Engine) Evaluate(ctx context.Context, req PolicyRequest) (*PolicyDecision, error) {
	if e == nil {
		return nil, fmt.Errorf("evaluate policy: engine is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := otel.Tracer("mcpgw/policy").Start(ctx, "policy.evaluate")
	defer span.End()
	span.SetAttributes(attribute.String("tool.name", strings.TrimSpace(req.ToolName)))

	e.mu.RLock()
	auditStore := e.auditStore
	classificationStore := e.classificationStore
	classificationCache := e.classificationCache
	e.mu.RUnlock()

	toolName := strings.TrimSpace(req.ToolName)

	// Step 0: tool risk classification check.
	// Destructive tools are ALWAYS blocked regardless of profile mode.
	if decision := e.checkDestructiveRisk(ctx, toolName, classificationStore, classificationCache, auditStore, req, span); decision != nil {
		return decision, nil
	}

	profile := e.resolveProfile(req.ProfileName)
	mode := profile.EnforcementMode
	if mode == "" {
		mode = types.ModeEnforce
	}

	violations := e.collectViolations(profile, toolName, req)
	decision := buildDecision(violations, mode)
	span.SetAttributes(
		attribute.Bool("policy.allowed", decision.Allowed),
		attribute.String("policy.reason", decision.Reason),
	)

	e.logAndPublishDecision(ctx, auditStore, req, decision, toolName, violations)
	return decision, nil
}

func (e *Engine) checkDestructiveRisk(
	ctx context.Context,
	toolName string,
	classificationStore store.ToolClassificationStore,
	classificationCache *ClassificationCache,
	auditStore store.AuditStore,
	req PolicyRequest,
	span interface{ SetAttributes(...attribute.KeyValue) },
) *PolicyDecision {
	if classificationStore == nil {
		return nil
	}
	riskLevel := e.resolveRiskLevel(ctx, toolName, classificationStore, classificationCache)
	if riskLevel == types.RiskUnknown {
		e.logger.WarnContext(ctx, "tool has unknown risk classification",
			slog.String("tool", toolName),
		)
	}
	if riskLevel != types.RiskDestructive {
		return nil
	}

	decision := &PolicyDecision{
		Allowed: false,
		Reason:  "tool classified as destructive",
		Violations: []Violation{{
			Type:     ViolationDestructive,
			Detail:   fmt.Sprintf("tool %q is classified as destructive and is blocked", toolName),
			Severity: SeverityCritical,
		}},
		EnforcementMode: types.ModeEnforce,
	}
	span.SetAttributes(
		attribute.Bool("policy.allowed", false),
		attribute.String("policy.reason", decision.Reason),
	)
	if err := LogDecision(ctx, auditStore, req, decision); err != nil {
		e.logger.ErrorContext(ctx, "policy decision audit log failed", slog.String("error", err.Error()))
	}
	return decision
}

func (e *Engine) collectViolations(profile *types.PolicyProfile, toolName string, req PolicyRequest) []Violation {
	violations := make([]Violation, 0)

	if len(profile.ToolAllowlist) > 0 && !contains(profile.ToolAllowlist, toolName) {
		violations = append(violations, Violation{
			Type:     ViolationAllowlist,
			Detail:   fmt.Sprintf("tool %q is not in allowlist", toolName),
			Severity: SeverityHigh,
		})
	}
	if contains(profile.ToolDenylist, toolName) {
		violations = append(violations, Violation{
			Type:     ViolationDenylist,
			Detail:   fmt.Sprintf("tool %q is in denylist", toolName),
			Severity: SeverityHigh,
		})
	}

	violations = append(violations, CheckDangerous(req.Arguments, e.dangerousPatterns)...)
	violations = append(violations, ValidateArguments(req.Arguments, req.Schema)...)
	return violations
}

func buildDecision(violations []Violation, mode types.EnforcementMode) *PolicyDecision {
	allowed := len(violations) == 0
	reason := "allowed"
	if len(violations) > 0 {
		reason = "policy violations detected"
	}
	if mode == types.ModeAudit && len(violations) > 0 {
		allowed = true
		reason = "policy violations detected (audit mode)"
	}
	return &PolicyDecision{
		Allowed:         allowed,
		Reason:          reason,
		Violations:      violations,
		EnforcementMode: mode,
	}
}

func (e *Engine) logAndPublishDecision(ctx context.Context, auditStore store.AuditStore, req PolicyRequest, decision *PolicyDecision, toolName string, violations []Violation) {
	if err := LogDecision(ctx, auditStore, req, decision); err != nil {
		e.logger.ErrorContext(ctx, "policy decision audit log failed", slog.String("error", err.Error()))
	}
	if len(violations) == 0 || e.publisher == nil {
		return
	}
	violationTexts := make([]string, 0, len(violations))
	for _, violation := range violations {
		violationTexts = append(violationTexts, violation.Detail)
	}
	decisionLabel := "denied"
	if decision.Allowed {
		decisionLabel = "allowed"
	}
	if err := e.publisher.PublishPolicyViolated(ctx, req.ClientID, toolName, violationTexts, decisionLabel); err != nil {
		e.logger.ErrorContext(ctx, "publish policy violated event failed", slog.String("error", err.Error()))
	}
}

func (e *Engine) resolveProfile(name string) *types.PolicyProfile {
	e.mu.RLock()
	defer e.mu.RUnlock()

	profileName := strings.TrimSpace(name)
	if profileName != "" {
		if profile, ok := e.profiles[profileName]; ok && profile != nil {
			return profile
		}
	}
	if profile, ok := e.profiles["default"]; ok && profile != nil {
		return profile
	}
	return &types.PolicyProfile{
		Name:            "default",
		EnforcementMode: types.ModeEnforce,
	}
}

func (e *Engine) resolveRiskLevel(
	ctx context.Context,
	toolName string,
	classificationStore store.ToolClassificationStore,
	cache *ClassificationCache,
) types.RiskLevel {
	if cache != nil {
		if cached := cache.Get(toolName); cached != nil {
			return cached.RiskLevel
		}
	}

	tc, err := classificationStore.Get(ctx, toolName)
	if err != nil {
		e.logger.ErrorContext(ctx, "classification store lookup failed",
			slog.String("tool", toolName),
			slog.String("error", err.Error()),
		)
		return types.RiskUnknown
	}

	if tc == nil {
		level, _ := AutoClassify(toolName, "")
		return level
	}

	if cache != nil {
		cache.Set(toolName, tc)
	}

	return tc.RiskLevel
}

func contains(values []string, expected string) bool {
	target := strings.TrimSpace(expected)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
