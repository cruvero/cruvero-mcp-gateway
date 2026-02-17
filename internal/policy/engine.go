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
	mu                sync.RWMutex
	profiles          map[string]*types.PolicyProfile
	dangerousPatterns []*regexp.Regexp
	auditStore        store.AuditStore
	logger            *slog.Logger
	publisher         ViolationEventPublisher
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

// SetViolationEventPublisher sets an optional policy-violation event publisher.
func (e *Engine) SetViolationEventPublisher(publisher ViolationEventPublisher) {
	if e == nil {
		return
	}
	e.publisher = publisher
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

	profile := e.resolveProfile(req.ProfileName)
	mode := profile.EnforcementMode
	if mode == "" {
		mode = types.ModeEnforce
	}

	toolName := strings.TrimSpace(req.ToolName)
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

	allowed := len(violations) == 0
	reason := "allowed"
	if len(violations) > 0 {
		reason = "policy violations detected"
	}
	if mode == types.ModeAudit && len(violations) > 0 {
		allowed = true
		reason = "policy violations detected (audit mode)"
	}

	decision := &PolicyDecision{
		Allowed:         allowed,
		Reason:          reason,
		Violations:      violations,
		EnforcementMode: mode,
	}
	span.SetAttributes(
		attribute.Bool("policy.allowed", decision.Allowed),
		attribute.String("policy.reason", decision.Reason),
	)

	if err := LogDecision(ctx, e.auditStore, req, decision); err != nil {
		e.logger.ErrorContext(ctx, "policy decision audit log failed", slog.String("error", err.Error()))
	}
	if len(violations) > 0 && e.publisher != nil {
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

	return decision, nil
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

func contains(values []string, expected string) bool {
	target := strings.TrimSpace(expected)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
