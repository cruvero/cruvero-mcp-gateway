package search

// SynonymProvider resolves synonym groups for query expansion.
type SynonymProvider interface {
	// Lookup returns all synonyms for the given token, including itself.
	// Returns nil if the token has no synonym group.
	Lookup(token string) []string
}

// synonymMap maps each token to all members of its synonym group (including itself).
var synonymMap map[string][]string

func init() {
	groups := [][]string{
		{"kubernetes", "k8s", "kube", "kubectl"},
		{"docker", "container", "containerized"},
		{"postgres", "postgresql", "pg"},
		{"database", "db"},
		{"repository", "repo"},
		{"delete", "remove", "rm"},
		{"create", "new", "add"},
		{"list", "ls", "enumerate"},
		{"get", "fetch", "retrieve"},
		{"update", "modify", "edit", "patch"},
		{"javascript", "js"},
		{"typescript", "ts"},
		{"python", "py"},
		{"message", "msg"},
		{"configuration", "config", "cfg"},
		{"environment", "env"},
		{"application", "app"},
	}

	synonymMap = make(map[string][]string, len(groups)*4)
	for _, group := range groups {
		for _, member := range group {
			synonymMap[member] = group
		}
	}
}

// StaticSynonymProvider wraps the built-in static synonym map.
type StaticSynonymProvider struct{}

// Lookup returns the static synonym group for the token.
func (StaticSynonymProvider) Lookup(token string) []string {
	return synonymMap[token]
}

// MapSynonymProvider wraps an arbitrary map as a SynonymProvider.
type MapSynonymProvider struct {
	entries map[string][]string
}

// NewMapSynonymProvider creates a provider from the given map.
func NewMapSynonymProvider(entries map[string][]string) *MapSynonymProvider {
	return &MapSynonymProvider{entries: entries}
}

// Lookup returns the synonym group for the token.
func (p *MapSynonymProvider) Lookup(token string) []string {
	if p.entries == nil {
		return nil
	}
	return p.entries[token]
}

// MergedSynonymProvider layers a primary provider over a fallback.
// The primary is checked first; if it returns nil, the fallback is used.
type MergedSynonymProvider struct {
	primary  SynonymProvider
	fallback SynonymProvider
}

// NewMergedSynonymProvider creates a provider that checks primary first,
// then falls back to the fallback provider.
func NewMergedSynonymProvider(primary, fallback SynonymProvider) *MergedSynonymProvider {
	return &MergedSynonymProvider{primary: primary, fallback: fallback}
}

// Lookup checks the primary provider first, then the fallback.
func (p *MergedSynonymProvider) Lookup(token string) []string {
	if group := p.primary.Lookup(token); group != nil {
		return group
	}
	return p.fallback.Lookup(token)
}

// ExpandQuery expands each token with its synonym group members using the
// package-level static synonym map. The result is deduplicated and preserves
// the original tokens first.
func ExpandQuery(tokens []string) []string {
	return ExpandQueryWith(tokens, StaticSynonymProvider{})
}

// ExpandQueryWith expands tokens using the given SynonymProvider.
func ExpandQueryWith(tokens []string, provider SynonymProvider) []string {
	if len(tokens) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(tokens)*2)
	expanded := make([]string, 0, len(tokens)*2)

	// Add original tokens first.
	for _, tok := range tokens {
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		expanded = append(expanded, tok)
	}

	// Add synonym expansions.
	for _, tok := range tokens {
		group := provider.Lookup(tok)
		if group == nil {
			continue
		}
		for _, syn := range group {
			if _, ok := seen[syn]; ok {
				continue
			}
			seen[syn] = struct{}{}
			expanded = append(expanded, syn)
		}
	}

	return expanded
}
