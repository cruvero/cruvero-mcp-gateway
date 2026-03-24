package types

import "time"

// SynonymEntry represents a synonym group stored in the database.
type SynonymEntry struct {
	Term      string    `json:"term"`
	Synonyms  []string  `json:"synonyms"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReindexLogEntry records a single search engine reindex operation.
type ReindexLogEntry struct {
	ID         int       `json:"id"`
	Engine     string    `json:"engine"`
	Trigger    string    `json:"trigger"`
	ToolsCount int       `json:"tools_count"`
	DurationMs int       `json:"duration_ms"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
