package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// LogDecision writes policy decision audit details using the configured audit store.
func LogDecision(
	ctx context.Context,
	auditStore store.AuditStore,
	req PolicyRequest,
	decision *PolicyDecision,
) error {
	if auditStore == nil || decision == nil {
		return nil
	}

	entry := &types.AuditEntry{
		EventType:  "policy_decision",
		ClientID:   req.ClientID,
		ServerName: req.ToolName,
		Details: map[string]any{
			"tool":             req.ToolName,
			"allowed":          decision.Allowed,
			"enforcement_mode": decision.EnforcementMode,
			"violations":       decision.Violations,
			"arguments":        sanitizeForAudit(req.Arguments),
		},
	}

	if err := auditStore.Log(ctx, entry); err != nil {
		return fmt.Errorf("log policy decision: %w", err)
	}
	return nil
}

func sanitizeForAudit(args map[string]any) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}

	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = sanitizeValue(strings.TrimSpace(strings.ToLower(key)), value)
	}
	return out
}

func sanitizeValue(key string, value any) any {
	if isSensitiveKey(key) {
		return "[redacted]"
	}

	switch typed := value.(type) {
	case map[string]any:
		inner := make(map[string]any, len(typed))
		for k, v := range typed {
			inner[k] = sanitizeValue(strings.TrimSpace(strings.ToLower(k)), v)
		}
		return inner
	case []any:
		inner := make([]any, len(typed))
		for i, item := range typed {
			inner[i] = sanitizeValue(key, item)
		}
		return inner
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	if key == "" {
		return false
	}
	if strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		strings.HasSuffix(key, "key") {
		return true
	}
	return false
}
