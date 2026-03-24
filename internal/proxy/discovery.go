package proxy

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cruvero/mcp-gateway/internal/search"
)

// ToolMetadata describes enrichment metadata applied to a tool from the platform.
type ToolMetadata struct {
	ToolName string
	Category string
	Summary  string
	Tags     []string
	Priority int
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
	mu       sync.RWMutex
	entries  map[string]discoveryEntry
	metadata []ToolMetadata // stored metadata for reapplication after Index rebuilds
	engine   search.Engine  // optional pluggable search backend

	engineErrorFallbacks atomic.Uint64
}

// NewDiscoveryIndex creates an empty discovery index.
func NewDiscoveryIndex() *DiscoveryIndex {
	return &DiscoveryIndex{
		entries: make(map[string]discoveryEntry),
	}
}

// SetEngine configures a pluggable search backend. When set, Search delegates
// scoring to the engine instead of the built-in substring matcher.
func (d *DiscoveryIndex) SetEngine(e search.Engine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.engine = e
}

// Engine returns the currently configured search engine, or nil.
func (d *DiscoveryIndex) Engine() search.Engine {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.engine
}

// Index rebuilds the full index from the provided tool list.
// Previously applied metadata is reapplied to the new entries.
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
	d.applyMetadataLocked(d.metadata)
	d.reindexEngineLocked()
	d.mu.Unlock()
}

type scoredEntry struct {
	entry discoveryEntry
	score float64
}

const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50
)

// Search returns tool definitions matching the query, scored by relevance.
// Results are stripped to summaries with empty schemas and DeferLoading set.
func (d *DiscoveryIndex) Search(query, category string, offset, limit int) ([]ToolDefinition, int) {
	if strings.TrimSpace(query) == "" {
		return nil, 0
	}

	limit = clampLimit(limit)

	q := strings.ToLower(strings.TrimSpace(query))
	cat := strings.ToLower(strings.TrimSpace(category))

	scored := d.collectScoredEntries(q, cat)

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].entry.nameLower < scored[j].entry.nameLower
	})

	totalMatches := len(scored)
	scored = paginateScored(scored, offset, limit)
	if scored == nil {
		return nil, totalMatches
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

func (d *DiscoveryIndex) collectScoredEntries(q, cat string) []scoredEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.engine != nil {
		return d.collectScoredEntriesEngine(q, cat)
	}
	return d.collectScoredEntriesSubstring(q, cat)
}

func (d *DiscoveryIndex) collectScoredEntriesEngine(q, cat string) []scoredEntry {
	results, err := d.engine.Search(context.Background(), q, 0)
	if err != nil {
		d.engineErrorFallbacks.Add(1)
		slog.Warn("search engine error, falling back to substring",
			slog.String("error", err.Error()))
		return d.collectScoredEntriesSubstring(q, cat)
	}
	scored := make([]scoredEntry, 0, len(results))
	for _, r := range results {
		entry, ok := d.entries[r.Key]
		if !ok {
			continue
		}
		if cat != "" && strings.ToLower(entry.category) != cat {
			continue
		}
		scored = append(scored, scoredEntry{
			entry: entry,
			score: r.Score + float64(entry.priority),
		})
	}
	return scored
}

// EngineErrorFallbacks reports how often discovery search fell back to
// substring matching due to an engine-level error.
func (d *DiscoveryIndex) EngineErrorFallbacks() uint64 {
	if d == nil {
		return 0
	}
	return d.engineErrorFallbacks.Load()
}

func (d *DiscoveryIndex) collectScoredEntriesSubstring(q, cat string) []scoredEntry {
	scored := make([]scoredEntry, 0)
	for _, entry := range d.entries {
		if cat != "" && strings.ToLower(entry.category) != cat {
			continue
		}
		score := scoreEntry(entry, q)
		if score > 0 {
			score += float64(entry.priority)
			scored = append(scored, scoredEntry{entry: entry, score: score})
		}
	}
	return scored
}

func scoreEntry(entry discoveryEntry, q string) float64 {
	score := 0.0
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
	return score
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > maxSearchLimit {
		return maxSearchLimit
	}
	return limit
}

func paginateScored(scored []scoredEntry, offset, limit int) []scoredEntry {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(scored) {
		return nil
	}
	end := offset + limit
	if end > len(scored) {
		end = len(scored)
	}
	return scored[offset:end]
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
// The metadata is stored so it can be reapplied after subsequent Index() rebuilds.
func (d *DiscoveryIndex) ApplyMetadata(metadata []ToolMetadata) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.metadata = metadata
	d.applyMetadataLocked(metadata)
	d.reindexEngineLocked()
}

// applyMetadataLocked enriches entries with metadata. Caller must hold d.mu.
func (d *DiscoveryIndex) applyMetadataLocked(metadata []ToolMetadata) {
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
			entry.descLower = strings.ToLower(m.Summary)
		}
		if len(m.Tags) > 0 {
			lower := make([]string, len(m.Tags))
			for i, tag := range m.Tags {
				lower[i] = strings.ToLower(tag)
			}
			entry.tagsLower = lower
		}
		if m.Priority != 0 {
			entry.priority = m.Priority
		}
		d.entries[m.ToolName] = entry
	}
}

// reindexEngineLocked rebuilds the search engine index from current entries.
// Caller must hold d.mu (at least read lock; typically called under write lock).
func (d *DiscoveryIndex) reindexEngineLocked() {
	if d.engine == nil {
		return
	}
	docs := make([]search.Document, 0, len(d.entries))
	for key, entry := range d.entries {
		title := entry.titleLower
		if entry.definition.Annotations != nil {
			title = entry.definition.Annotations.Title
		}
		tags := make([]string, len(entry.tagsLower))
		copy(tags, entry.tagsLower)

		// Use the effective description: combine the original description
		// with the summary so both are searchable. Metadata may override
		// the summary with content not present in the original description.
		desc := entry.definition.Description
		if entry.summary != "" && entry.summary != desc {
			desc = desc + " " + entry.summary
		}

		docs = append(docs, search.Document{
			Key:         key,
			Name:        entry.definition.Name,
			Title:       title,
			Description: desc,
			Tags:        tags,
		})
	}
	if err := d.engine.Index(context.Background(), docs); err != nil {
		slog.Warn("search engine index error", slog.String("error", err.Error()))
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
	if offset < 0 {
		offset = 0
	}
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
	// Legacy format: mcp.<server>.<tool>
	if strings.HasPrefix(name, "mcp.") {
		rest := name[4:]
		if dotIdx := strings.Index(rest, "."); dotIdx > 0 {
			return rest[:dotIdx]
		}
		return ""
	}
	// New format: <displayName>.<tool> or just <raw>
	if dotIdx := strings.Index(name, "."); dotIdx > 0 {
		return name[:dotIdx]
	}
	return ""
}
