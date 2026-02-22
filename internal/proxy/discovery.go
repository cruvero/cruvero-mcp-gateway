package proxy

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// ToolMetadata describes enrichment metadata applied to a tool from the platform.
type ToolMetadata struct {
	ToolName    string
	Category    string
	DisplayName string
	Summary     string
	Tags        []string
	Priority    int
}

// DiscoveryStats holds aggregate statistics about the discovery index.
type DiscoveryStats struct {
	TotalTools      int            `json:"total_tools"`
	Categories      map[string]int `json:"categories"`
	MetadataVersion int64          `json:"metadata_version,omitempty"`
}

type discoveryEntry struct {
	definition ToolDefinition
	nameLower  string
	descLower  string
	titleLower string
	category   string
	summary    string
	tagsLower  []string
	priority   int
}

// DiscoveryIndex provides in-memory search over federated tool definitions.
type DiscoveryIndex struct {
	mu      sync.RWMutex
	entries map[string]discoveryEntry
}

// NewDiscoveryIndex creates an empty discovery index.
func NewDiscoveryIndex() *DiscoveryIndex {
	return &DiscoveryIndex{
		entries: make(map[string]discoveryEntry),
	}
}

// Index rebuilds the full index from the provided tool list.
func (d *DiscoveryIndex) Index(tools []ToolDefinition) {
	entries := make(map[string]discoveryEntry, len(tools))
	for _, tool := range tools {
		title := ""
		if tool.Annotations != nil {
			title = tool.Annotations.Title
		}
		entries[tool.Name] = discoveryEntry{
			definition: tool,
			nameLower:  strings.ToLower(tool.Name),
			descLower:  strings.ToLower(tool.Description),
			titleLower: strings.ToLower(title),
			category:   categoryFromFederatedName(tool.Name),
			summary:    extractSummary(tool.Description),
		}
	}

	d.mu.Lock()
	d.entries = entries
	d.mu.Unlock()
}

type scoredEntry struct {
	entry discoveryEntry
	score int
}

const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50
)

// Search returns tool definitions matching the query, scored by relevance.
// Results are stripped to summaries with empty schemas and DeferLoading set.
func (d *DiscoveryIndex) Search(query, category string, limit int) ([]ToolDefinition, int) {
	if strings.TrimSpace(query) == "" {
		return nil, 0
	}

	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	q := strings.ToLower(strings.TrimSpace(query))
	cat := strings.ToLower(strings.TrimSpace(category))

	d.mu.RLock()
	scored := make([]scoredEntry, 0)
	for _, entry := range d.entries {
		if cat != "" && strings.ToLower(entry.category) != cat {
			continue
		}

		score := 0
		if entry.nameLower == q {
			score += 10
		} else if strings.Contains(entry.nameLower, q) {
			score += 5
		}
		if strings.Contains(entry.titleLower, q) {
			score += 3
		}
		if strings.Contains(entry.descLower, q) {
			score += 1
		}
		for _, tag := range entry.tagsLower {
			if strings.Contains(tag, q) {
				score += 2
			}
		}
		if score > 0 {
			score += entry.priority
			scored = append(scored, scoredEntry{entry: entry, score: score})
		}
	}
	d.mu.RUnlock()

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].entry.nameLower < scored[j].entry.nameLower
	})

	totalMatches := len(scored)
	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]ToolDefinition, 0, len(scored))
	for _, s := range scored {
		results = append(results, ToolDefinition{
			Name:         s.entry.definition.Name,
			Description:  s.entry.summary,
			InputSchema:  json.RawMessage(`{}`),
			DeferLoading: true,
		})
	}
	return results, totalMatches
}

const maxGetToolsNames = 20

// GetTools returns full tool definitions for the given names, max 20.
func (d *DiscoveryIndex) GetTools(names []string) []ToolDefinition {
	if len(names) == 0 {
		return nil
	}
	clamped := names
	if len(clamped) > maxGetToolsNames {
		clamped = clamped[:maxGetToolsNames]
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	results := make([]ToolDefinition, 0, len(clamped))
	for _, name := range clamped {
		key := strings.TrimSpace(name)
		if entry, ok := d.entries[key]; ok {
			results = append(results, entry.definition)
		}
	}
	return results
}

const maxSummaryLen = 100

func extractSummary(description string) string {
	if description == "" {
		return ""
	}

	// Find sentence boundary: ". " followed by next sentence, or "." at end
	for i := 0; i < len(description)-1; i++ {
		if description[i] == '.' && description[i+1] == ' ' {
			sentence := description[:i+1]
			if len(sentence) <= maxSummaryLen {
				return sentence
			}
			break
		}
	}

	// Check for trailing period at end of single sentence
	if len(description) <= maxSummaryLen {
		return description
	}

	// Too long, truncate with ellipsis
	if maxSummaryLen >= 3 {
		return description[:maxSummaryLen-3] + "..."
	}
	return description[:maxSummaryLen]
}

// ApplyMetadata enriches indexed entries with platform-provided metadata.
func (d *DiscoveryIndex) ApplyMetadata(metadata []ToolMetadata) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, m := range metadata {
		entry, ok := d.entries[m.ToolName]
		if !ok {
			continue
		}
		if m.Category != "" {
			entry.category = m.Category
		}
		if m.Summary != "" {
			entry.summary = m.Summary
		}
		if len(m.Tags) > 0 {
			lower := make([]string, len(m.Tags))
			for i, tag := range m.Tags {
				lower[i] = strings.ToLower(tag)
			}
			entry.tagsLower = lower
		}
		entry.priority = m.Priority
		d.entries[m.ToolName] = entry
	}
}

// Stats returns aggregate statistics about the index.
func (d *DiscoveryIndex) Stats() DiscoveryStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	categories := make(map[string]int)
	for _, entry := range d.entries {
		cat := entry.category
		if cat == "" {
			cat = "uncategorized"
		}
		categories[cat]++
	}

	return DiscoveryStats{
		TotalTools: len(d.entries),
		Categories: categories,
	}
}

// Browse returns a paginated list of tool summaries, optionally filtered by category.
func (d *DiscoveryIndex) Browse(category string, offset, limit int) ([]ToolDefinition, int) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	cat := strings.ToLower(strings.TrimSpace(category))

	d.mu.RLock()
	defer d.mu.RUnlock()

	// Collect matching entries sorted by name.
	matching := make([]discoveryEntry, 0, len(d.entries))
	for _, entry := range d.entries {
		if cat != "" && strings.ToLower(entry.category) != cat {
			continue
		}
		matching = append(matching, entry)
	}
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].nameLower < matching[j].nameLower
	})

	total := len(matching)
	if offset >= total {
		return nil, total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	results := make([]ToolDefinition, 0, end-offset)
	for _, entry := range matching[offset:end] {
		results = append(results, ToolDefinition{
			Name:         entry.definition.Name,
			Description:  entry.summary,
			InputSchema:  json.RawMessage(`{}`),
			DeferLoading: true,
		})
	}
	return results, total
}

func categoryFromFederatedName(name string) string {
	// Format: mcp.<server>.<tool>
	if !strings.HasPrefix(name, "mcp.") {
		return ""
	}
	rest := name[4:]
	dotIdx := strings.Index(rest, ".")
	if dotIdx <= 0 {
		return ""
	}
	return rest[:dotIdx]
}
