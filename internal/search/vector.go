package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// VectorEngine implements Engine using document embeddings and cosine
// similarity for semantic search. It delegates embedding to an Embedder
// (typically an HTTP sidecar running all-MiniLM-L6-v2).
//
// Thread-safe via sync.RWMutex.
type VectorEngine struct {
	mu         sync.RWMutex
	docs       []Document
	embeddings [][]float32
	embedder   Embedder
}

// NewVectorEngine creates a vector search engine backed by the given embedder.
func NewVectorEngine(embedder Embedder) *VectorEngine {
	return &VectorEngine{embedder: embedder}
}

// Index embeds all documents via the sidecar and stores the results.
func (e *VectorEngine) Index(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		e.mu.Lock()
		e.docs = nil
		e.embeddings = nil
		e.mu.Unlock()
		return nil
	}

	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = buildEmbedText(doc)
	}

	embeddings, err := e.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed documents: %w", err)
	}

	if len(embeddings) != len(docs) {
		return fmt.Errorf("embedding count mismatch: got %d, want %d", len(embeddings), len(docs))
	}

	e.mu.Lock()
	e.docs = docs
	e.embeddings = embeddings
	e.mu.Unlock()

	return nil
}

// Search embeds the query and returns documents ranked by cosine similarity.
func (e *VectorEngine) Search(ctx context.Context, query string, limit int) ([]ScoredResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	queryEmb, err := e.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryEmb) == 0 {
		return nil, fmt.Errorf("embedder returned no vectors for query")
	}

	qVec := queryEmb[0]

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.docs) == 0 {
		return nil, nil
	}

	type scored struct {
		key   string
		score float64
	}

	results := make([]scored, 0, len(e.docs))
	for i, docEmb := range e.embeddings {
		sim := cosineSimilarity(qVec, docEmb)
		if sim > 0 {
			results = append(results, scored{key: e.docs[i].Key, score: sim})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].key < results[j].key
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	out := make([]ScoredResult, len(results))
	for i, r := range results {
		out[i] = ScoredResult{Key: r.key, Score: r.score}
	}
	return out, nil
}

// Remove removes a document by key.
func (e *VectorEngine) Remove(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := -1
	for i, doc := range e.docs {
		if doc.Key == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	e.docs = append(e.docs[:idx], e.docs[idx+1:]...)
	e.embeddings = append(e.embeddings[:idx], e.embeddings[idx+1:]...)
}

// Ready reports whether the embedding sidecar is reachable.
func (e *VectorEngine) Ready() bool {
	return e.embedder.Ready(context.Background())
}

// buildEmbedText constructs a single text from all document fields for embedding.
func buildEmbedText(doc Document) string {
	parts := make([]string, 0, 4)
	if doc.Name != "" {
		// Replace dots/underscores with spaces for better tokenization.
		name := strings.NewReplacer(".", " ", "_", " ").Replace(doc.Name)
		parts = append(parts, name)
	}
	if doc.Title != "" {
		parts = append(parts, doc.Title)
	}
	if doc.Description != "" {
		parts = append(parts, doc.Description)
	}
	if len(doc.Tags) > 0 {
		parts = append(parts, strings.Join(doc.Tags, " "))
	}
	return strings.Join(parts, ". ")
}

// cosineSimilarity computes the cosine similarity between two vectors.
// For pre-normalized vectors (as returned by sentence-transformers), this
// reduces to the dot product. We compute the full formula for robustness.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
