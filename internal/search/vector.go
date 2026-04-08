package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// vectorPartition groups documents and their embeddings for a single server.
type vectorPartition struct {
	serverID   string
	docs       []Document
	embeddings [][]float32
}

// VectorEngine implements Engine using document embeddings and cosine
// similarity for semantic search. It delegates embedding to an Embedder
// (typically an HTTP sidecar running all-MiniLM-L6-v2).
//
// Thread-safe via sync.RWMutex. Supports server-partitioned storage
// with incremental updates using an optional EmbeddingCache.
type VectorEngine struct {
	mu         sync.RWMutex
	partitions map[string]*vectorPartition
	embedder   Embedder
	cache      *EmbeddingCache

	// Flat storage kept for backward-compatible access in tests.
	docs       []Document
	embeddings [][]float32
}

// NewVectorEngine creates a vector search engine backed by the given embedder.
func NewVectorEngine(embedder Embedder) *VectorEngine {
	return &VectorEngine{
		embedder:   embedder,
		partitions: make(map[string]*vectorPartition),
	}
}

// SetEmbeddingCache attaches an LRU embedding cache for incremental updates.
func (e *VectorEngine) SetEmbeddingCache(cache *EmbeddingCache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = cache
}

// Index embeds all documents via the sidecar and stores the results.
// This clears all partitions and stores everything in a flat structure,
// preserving backward compatibility.
func (e *VectorEngine) Index(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		e.mu.Lock()
		e.docs = nil
		e.embeddings = nil
		e.partitions = make(map[string]*vectorPartition)
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
	e.partitions = map[string]*vectorPartition{
		"__default__": {serverID: "__default__", docs: docs, embeddings: embeddings},
	}
	e.mu.Unlock()

	return nil
}

// Search embeds the query and returns documents ranked by cosine similarity.
func (e *VectorEngine) Search(ctx context.Context, query string, limit int) ([]ScoredResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	e.mu.RLock()
	empty := len(e.partitions) == 0
	e.mu.RUnlock()

	if empty {
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

	type scored struct {
		key   string
		score float64
	}

	var results []scored

	for _, p := range e.partitions {
		for i, docEmb := range p.embeddings {
			sim := cosineSimilarity(qVec, docEmb)
			if sim > 0 {
				results = append(results, scored{key: p.docs[i].Key, score: sim})
			}
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

// AddPartition adds or replaces documents for a server partition, using
// the embedding cache for incremental efficiency when available.
func (e *VectorEngine) AddPartition(ctx context.Context, serverID string, docs []Document) error {
	if len(docs) == 0 {
		e.RemovePartition(serverID)
		return nil
	}

	embeddings, err := e.embedWithCache(ctx, docs)
	if err != nil {
		return fmt.Errorf("add vector partition: %w", err)
	}

	e.mu.Lock()
	e.partitions[serverID] = &vectorPartition{
		serverID:   serverID,
		docs:       docs,
		embeddings: embeddings,
	}
	e.rebuildFlatLocked()
	e.mu.Unlock()

	return nil
}

// RemovePartition removes all documents for a server.
func (e *VectorEngine) RemovePartition(serverID string) {
	e.mu.Lock()
	delete(e.partitions, serverID)
	e.rebuildFlatLocked()
	e.mu.Unlock()
}

// UpdatePartition is equivalent to AddPartition.
func (e *VectorEngine) UpdatePartition(ctx context.Context, serverID string, docs []Document) error {
	return e.AddPartition(ctx, serverID, docs)
}

// Remove removes a document by key.
func (e *VectorEngine) Remove(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for pid, p := range e.partitions {
		idx := -1
		for i, doc := range p.docs {
			if doc.Key == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}

		p.docs = append(p.docs[:idx], p.docs[idx+1:]...)
		p.embeddings = append(p.embeddings[:idx], p.embeddings[idx+1:]...)

		if len(p.docs) == 0 {
			delete(e.partitions, pid)
		}

		e.rebuildFlatLocked()
		return
	}
}

// Ready reports whether the embedding sidecar is reachable.
func (e *VectorEngine) Ready() bool {
	return e.embedder.Ready(context.Background())
}

// embedWithCache computes embeddings, using the cache for unchanged docs.
func (e *VectorEngine) embedWithCache(ctx context.Context, docs []Document) ([][]float32, error) {
	e.mu.RLock()
	cache := e.cache
	e.mu.RUnlock()

	if cache == nil {
		// No cache: embed all documents directly.
		texts := make([]string, len(docs))
		for i, doc := range docs {
			texts[i] = buildEmbedText(doc)
		}
		return e.embedder.Embed(ctx, texts)
	}

	result := make([][]float32, len(docs))
	var needEmbed []int // indexes of docs needing fresh embeddings

	for i, doc := range docs {
		hash := cache.ContentHash(doc.Name, doc.Description)
		if emb, ok := cache.Get(hash); ok {
			result[i] = emb
		} else {
			needEmbed = append(needEmbed, i)
		}
	}

	if len(needEmbed) == 0 {
		return result, nil
	}

	texts := make([]string, len(needEmbed))
	for j, idx := range needEmbed {
		texts[j] = buildEmbedText(docs[idx])
	}

	fresh, err := e.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(needEmbed) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(fresh), len(needEmbed))
	}

	for j, idx := range needEmbed {
		result[idx] = fresh[j]
		hash := cache.ContentHash(docs[idx].Name, docs[idx].Description)
		cache.Set(hash, fresh[j])
	}

	return result, nil
}

// rebuildFlatLocked rebuilds the flat docs/embeddings slices from partitions.
// Must be called under write lock.
func (e *VectorEngine) rebuildFlatLocked() {
	var allDocs []Document
	var allEmb [][]float32

	for _, p := range e.partitions {
		allDocs = append(allDocs, p.docs...)
		allEmb = append(allEmb, p.embeddings...)
	}

	e.docs = allDocs
	e.embeddings = allEmb
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
