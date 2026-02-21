package policy

import (
	"strings"

	"github.com/cruvero/mcp-gateway/internal/types"
)

var destructiveKeywords = []string{
	"delete", "remove", "destroy", "drop", "purge", "erase", "wipe", "truncate",
	"kill", "terminate", "revoke", "force",
}

var writeKeywords = []string{
	"create", "update", "set", "put", "post", "write", "add", "insert",
	"modify", "patch", "upload", "push", "send", "publish", "assign",
	"enable", "disable", "configure", "deploy", "start", "stop", "restart",
}

var readOnlyKeywords = []string{
	"get", "list", "read", "describe", "show", "fetch", "query", "search",
	"find", "view", "inspect", "check", "status", "info", "count", "exists",
	"download", "export", "head", "ping", "health", "version",
}

// AutoClassify returns a deterministic risk level for a tool based on its name
// and optional description. It splits the name on common delimiters and checks
// keyword lists in order: destructive > read_only > write > unknown.
func AutoClassify(toolName string, toolDescription string) (types.RiskLevel, string) {
	tokens := splitToolName(toolName)

	for _, token := range tokens {
		for _, keyword := range destructiveKeywords {
			if token == keyword {
				return types.RiskDestructive, "name contains destructive keyword: " + keyword
			}
		}
	}

	for _, token := range tokens {
		for _, keyword := range readOnlyKeywords {
			if token == keyword {
				return types.RiskReadOnly, "name contains read-only keyword: " + keyword
			}
		}
	}

	for _, token := range tokens {
		for _, keyword := range writeKeywords {
			if token == keyword {
				return types.RiskWrite, "name contains write keyword: " + keyword
			}
		}
	}

	desc := strings.ToLower(strings.TrimSpace(toolDescription))
	if desc != "" {
		for _, keyword := range destructiveKeywords {
			if strings.Contains(desc, keyword) {
				return types.RiskDestructive, "description contains destructive keyword: " + keyword
			}
		}
		for _, keyword := range readOnlyKeywords {
			if strings.Contains(desc, keyword) {
				return types.RiskReadOnly, "description contains read-only keyword: " + keyword
			}
		}
		for _, keyword := range writeKeywords {
			if strings.Contains(desc, keyword) {
				return types.RiskWrite, "description contains write keyword: " + keyword
			}
		}
	}

	return types.RiskUnknown, "no classification keywords found"
}

func splitToolName(name string) []string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer(
		".", " ",
		"_", " ",
		"-", " ",
		"/", " ",
	).Replace(normalized)

	parts := strings.Fields(normalized)
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			tokens = append(tokens, trimmed)
		}
	}
	return tokens
}
