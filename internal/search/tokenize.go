package search

import (
	"strings"
	"unicode"
)

// Tokenize splits a string into lowercase tokens on whitespace, dots,
// underscores, and hyphens. It handles tool names like "k8s.list_pods"
// and natural language queries alike.
func Tokenize(s string) []string {
	lower := strings.ToLower(s)
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.' || r == '_' || r == '-'
	})
	if len(parts) == 0 {
		return nil
	}
	return parts
}
