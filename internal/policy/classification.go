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

// classifyByName checks whether any token from the tool name matches a keyword
// list, returning the matched keyword or empty string.
func classifyByName(tokens []string, keywords []string) string {
	for _, token := range tokens {
		for _, keyword := range keywords {
			if token == keyword {
				return keyword
			}
		}
	}
	return ""
}

// classifyByDescription checks whether the description contains any keyword,
// returning the matched keyword or empty string.
func classifyByDescription(desc string, keywords []string) string {
	for _, keyword := range keywords {
		if strings.Contains(desc, keyword) {
			return keyword
		}
	}
	return ""
}

// nameClassificationRules defines the ordered set of keyword lists and their
// corresponding risk levels for tool-name classification.
var nameClassificationRules = []struct {
	keywords []string
	level    types.RiskLevel
	label    string
}{
	{destructiveKeywords, types.RiskDestructive, "destructive"},
	{readOnlyKeywords, types.RiskReadOnly, "read-only"},
	{writeKeywords, types.RiskWrite, "write"},
}

// AutoClassify returns a deterministic risk level for a tool based on its name
// and optional description. It splits the name on common delimiters and checks
// keyword lists in order: destructive > read_only > write > unknown.
func AutoClassify(toolName string, toolDescription string) (types.RiskLevel, string) {
	tokens := splitToolName(toolName)

	for _, rule := range nameClassificationRules {
		if keyword := classifyByName(tokens, rule.keywords); keyword != "" {
			return rule.level, "name contains " + rule.label + " keyword: " + keyword
		}
	}

	desc := strings.ToLower(strings.TrimSpace(toolDescription))
	if desc != "" {
		for _, rule := range nameClassificationRules {
			if keyword := classifyByDescription(desc, rule.keywords); keyword != "" {
				return rule.level, "description contains " + rule.label + " keyword: " + keyword
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
