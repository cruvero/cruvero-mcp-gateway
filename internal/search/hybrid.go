package search

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
)

// rrfK is the constant for Reciprocal Rank Fusion scoring.
// score(d) = 1/(k + rank_bm25) + 1/(k + rank_vec)
const rrfK = 60

// HybridEngine combines BM25 lexical search with vector semantic search
// using Reciprocal Rank Fusion (RRF). When the vector engine fails, it
// falls back to BM25-only results.
type HybridEngine struct {
	bm25                  *BM25Engine
	vector                *VectorEngine
	vectorSearchFallbacks atomic.Uint64
	vectorIndexFallbacks  atomic.Uint64
}

// NewHybridEngine creates a hybrid search engine that fuses BM25 and vector
// results using Reciprocal Rank Fusion.
func NewHybridEngine(bm25 *BM25Engine, vector *VectorEngine) *HybridEngine {
	return &HybridEngine{bm25: bm25, vector: vector}
}

// Index indexes documents in both the BM25 and vector engines.
func (e *HybridEngine) Index(ctx context.Context, docs []Document) error {
	if err := e.bm25.Index(ctx, docs); err != nil {
		return err
	}
	if err := e.vector.Index(ctx, docs); err != nil {
		e.vectorIndexFallbacks.Add(1)
		slog.Warn("hybrid engine: vector index failed, BM25 index succeeded",
			slog.String("error", err.Error()))
	}
	return nil
}

// Search runs both engines concurrently and fuses results with RRF.
// Falls back to BM25-only if the vector engine fails.
func (e *HybridEngine) Search(ctx context.Context, query string, limit int) ([]ScoredResult, error) {
	var (
		bm25Results   []ScoredResult
		vectorResults []ScoredResult
		bm25Err       error
		vectorErr     error
		wg            sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		bm25Results, bm25Err = e.bm25.Search(ctx, query, 0)
	}()
	go func() {
		defer wg.Done()
		vectorResults, vectorErr = e.vector.Search(ctx, query, 0)
	}()
	wg.Wait()

	if bm25Err != nil {
		return nil, bm25Err
	}

	if vectorErr != nil {
		e.vectorSearchFallbacks.Add(1)
		slog.Warn("hybrid engine: vector search failed, using BM25 only",
			slog.String("error", vectorErr.Error()))
		if limit > 0 && len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, nil
	}

	fused := fuseRRF(bm25Results, vectorResults)
	if len(fused) == 0 {
		return nil, nil
	}

	if limit > 0 && len(fused) > limit {
		fused = fused[:limit]
	}

	return fused, nil
}

// Remove removes a document from both engines.
func (e *HybridEngine) Remove(key string) {
	e.bm25.Remove(key)
	e.vector.Remove(key)
}

// UpdatePartition updates documents for a server in both engines.
// Vector failures are logged but do not fail the operation.
func (e *HybridEngine) UpdatePartition(ctx context.Context, serverID string, docs []Document) error {
	if err := e.bm25.UpdatePartition(serverID, docs); err != nil {
		return err
	}
	if err := e.vector.UpdatePartition(ctx, serverID, docs); err != nil {
		e.vectorIndexFallbacks.Add(1)
		slog.Warn("hybrid engine: vector partition update failed",
			slog.String("server_id", serverID),
			slog.String("error", err.Error()))
	}
	return nil
}

// RemovePartition removes all documents for a server from both engines.
func (e *HybridEngine) RemovePartition(serverID string) {
	e.bm25.RemovePartition(serverID)
	e.vector.RemovePartition(serverID)
}

// Ready reports whether both engines are operational. If only vector is
// down, the engine is still partially operational (handled at readyz level).
func (e *HybridEngine) Ready() bool {
	return e.bm25.Ready()
}

// VectorReady reports whether the vector sub-engine is operational.
func (e *HybridEngine) VectorReady() bool {
	return e.vector.Ready()
}

// VectorSearchFallbacks reports how often hybrid search degraded to BM25 due
// to vector query failures.
func (e *HybridEngine) VectorSearchFallbacks() uint64 {
	return e.vectorSearchFallbacks.Load()
}

// VectorIndexFallbacks reports how often hybrid indexing degraded to BM25 due
// to vector indexing failures.
func (e *HybridEngine) VectorIndexFallbacks() uint64 {
	return e.vectorIndexFallbacks.Load()
}

// fuseRRF merges two ranked result lists using Reciprocal Rank Fusion.
func fuseRRF(a, b []ScoredResult) []ScoredResult {
	scores := make(map[string]float64)

	for rank, r := range a {
		scores[r.Key] += 1.0 / float64(rrfK+rank+1)
	}
	for rank, r := range b {
		scores[r.Key] += 1.0 / float64(rrfK+rank+1)
	}

	results := make([]ScoredResult, 0, len(scores))
	for key, score := range scores {
		results = append(results, ScoredResult{Key: key, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Key < results[j].Key
	})

	return results
}
