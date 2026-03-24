package search

import "context"

// Document represents a searchable tool entry with multiple fields.
type Document struct {
	Key         string
	Name        string
	Title       string
	Description string
	Tags        []string
}

// ScoredResult pairs a document key with its relevance score.
type ScoredResult struct {
	Key   string
	Score float64
}

// Engine defines the interface for pluggable search backends.
type Engine interface {
	// Index replaces all documents in the search index.
	Index(ctx context.Context, docs []Document) error

	// Search returns matching documents sorted by descending relevance score.
	// An empty query returns nil. Limit controls the maximum number of results;
	// a value <= 0 means no limit.
	Search(ctx context.Context, query string, limit int) ([]ScoredResult, error)

	// Remove removes a single document by key from the index.
	Remove(key string)

	// Ready reports whether the engine is operational.
	Ready() bool
}
