package search

import (
	"context"
	"fmt"
	"testing"
)

func newTestHybridEngine(embedder *mockEmbedder) (*HybridEngine, *BM25Engine, *VectorEngine) {
	bm25 := NewBM25Engine()
	vector := NewVectorEngine(embedder)
	hybrid := NewHybridEngine(bm25, vector)
	return hybrid, bm25, vector
}

func TestHybridEngine_FusionOrdering(t *testing.T) {
	t.Parallel()

	callNum := 0
	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			callNum++
			if callNum == 1 {
				// Index: doc[0] = high vector match, doc[1] = low
				return [][]float32{
					{1.0, 0.0},
					{0.1, 0.9},
				}, nil
			}
			// Search query: matches doc[0]
			return [][]float32{{1.0, 0.0}}, nil
		},
	}

	hybrid, _, _ := newTestHybridEngine(embedder)

	docs := []Document{
		{Key: "k8s.list_pods", Name: "k8s.list_pods", Title: "List Pods", Description: "List all Kubernetes pods."},
		{Key: "docker.list_containers", Name: "docker.list_containers", Title: "List Containers", Description: "List Docker containers."},
	}

	if err := hybrid.Index(context.Background(), docs); err != nil {
		t.Fatalf("Index error: %v", err)
	}

	results, err := hybrid.Search(context.Background(), "list kubernetes pods", 0)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}

	// Both BM25 and vector should agree k8s.list_pods ranks first.
	if results[0].Key != "k8s.list_pods" {
		t.Errorf("expected first result k8s.list_pods, got %q", results[0].Key)
	}
}

func TestHybridEngine_GracefulDegradation(t *testing.T) {
	t.Parallel()

	callNum := 0
	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			callNum++
			if callNum == 1 {
				// Index succeeds.
				return make([][]float32, len(texts)), nil
			}
			// Search fails.
			return nil, fmt.Errorf("sidecar down")
		},
	}

	hybrid, _, _ := newTestHybridEngine(embedder)

	docs := []Document{
		{Key: "k8s.list_pods", Name: "k8s.list_pods", Description: "List Kubernetes pods."},
	}

	if err := hybrid.Index(context.Background(), docs); err != nil {
		t.Fatalf("Index error: %v", err)
	}

	// Search should fall back to BM25-only.
	results, err := hybrid.Search(context.Background(), "kubernetes pods", 0)
	if err != nil {
		t.Fatalf("expected no error with graceful degradation, got: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected BM25 fallback results, got none")
	}
	if results[0].Key != "k8s.list_pods" {
		t.Errorf("expected k8s.list_pods from BM25 fallback, got %q", results[0].Key)
	}
	if hybrid.VectorSearchFallbacks() != 1 {
		t.Fatalf("expected vector search fallback count 1, got %d", hybrid.VectorSearchFallbacks())
	}
}

func TestHybridEngine_EmptyResults(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			result := make([][]float32, len(texts))
			for i := range result {
				result[i] = []float32{0.1, 0.1}
			}
			return result, nil
		},
	}

	hybrid, _, _ := newTestHybridEngine(embedder)
	_ = hybrid.Index(context.Background(), []Document{
		{Key: "a", Name: "a", Description: "some tool"},
	})

	results, err := hybrid.Search(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestHybridEngine_Limit(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	hybrid, _, _ := newTestHybridEngine(embedder)

	_ = hybrid.Index(context.Background(), []Document{
		{Key: "a", Name: "a", Description: "tool alpha"},
		{Key: "b", Name: "b", Description: "tool beta"},
		{Key: "c", Name: "c", Description: "tool gamma"},
	})

	results, err := hybrid.Search(context.Background(), "tool", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with limit=1, got %d", len(results))
	}
}

func TestHybridEngine_Remove(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	hybrid, _, _ := newTestHybridEngine(embedder)

	_ = hybrid.Index(context.Background(), []Document{
		{Key: "a", Name: "a", Description: "tool alpha"},
		{Key: "b", Name: "b", Description: "tool beta"},
	})

	hybrid.Remove("a")

	// BM25 should no longer find "a".
	bm25Results, _ := hybrid.bm25.Search(context.Background(), "alpha", 0)
	for _, r := range bm25Results {
		if r.Key == "a" {
			t.Fatal("expected 'a' removed from BM25 engine")
		}
	}
}

func TestHybridEngine_Ready(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{ready: true}
	hybrid, _, _ := newTestHybridEngine(embedder)

	// Ready() only depends on BM25 (always true).
	if !hybrid.Ready() {
		t.Fatal("expected Ready() = true")
	}

	// VectorReady() depends on embedder.
	if !hybrid.VectorReady() {
		t.Fatal("expected VectorReady() = true when embedder is ready")
	}

	embedder.ready = false
	if !hybrid.Ready() {
		t.Fatal("expected Ready() = true even when vector is down")
	}
	if hybrid.VectorReady() {
		t.Fatal("expected VectorReady() = false when embedder is down")
	}
}

func TestHybridEngine_IndexVectorError(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, fmt.Errorf("embedding service down")
		},
	}

	hybrid, _, _ := newTestHybridEngine(embedder)

	docs := []Document{
		{Key: "a", Name: "a", Description: "tool alpha"},
	}

	// hybrid.Index should succeed even when vector index fails.
	if err := hybrid.Index(context.Background(), docs); err != nil {
		t.Fatalf("expected no error when vector index fails, got: %v", err)
	}

	// BM25 should still work.
	results, err := hybrid.bm25.Search(context.Background(), "alpha", 0)
	if err != nil {
		t.Fatalf("unexpected BM25 search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected BM25 results after vector index failure")
	}
	if hybrid.VectorIndexFallbacks() != 1 {
		t.Fatalf("expected vector index fallback count 1, got %d", hybrid.VectorIndexFallbacks())
	}
}

func TestFuseRRF(t *testing.T) {
	t.Parallel()

	a := []ScoredResult{
		{Key: "doc1", Score: 10.0},
		{Key: "doc2", Score: 5.0},
		{Key: "doc3", Score: 1.0},
	}
	b := []ScoredResult{
		{Key: "doc2", Score: 10.0},
		{Key: "doc1", Score: 5.0},
		{Key: "doc4", Score: 1.0},
	}

	fused := fuseRRF(a, b)

	// doc1: rank 1 in a, rank 2 in b → 1/(61) + 1/(62)
	// doc2: rank 2 in a, rank 1 in b → 1/(62) + 1/(61)
	// Both should have the same score (symmetrical).
	if len(fused) < 2 {
		t.Fatalf("expected at least 2 fused results, got %d", len(fused))
	}

	// doc1 and doc2 should both appear in top results.
	keys := make(map[string]bool)
	for _, r := range fused[:2] {
		keys[r.Key] = true
	}
	if !keys["doc1"] || !keys["doc2"] {
		t.Errorf("expected doc1 and doc2 in top 2, got %v", fused[:2])
	}

	// doc3 should rank lower (only in one list).
	for _, r := range fused {
		if r.Key == "doc3" {
			if r.Score >= fused[0].Score {
				t.Error("doc3 should rank lower than docs appearing in both lists")
			}
			break
		}
	}
}

func TestFuseRRF_EmptyInputs(t *testing.T) {
	t.Parallel()

	fused := fuseRRF(nil, nil)
	if len(fused) != 0 {
		t.Fatalf("expected 0 results from empty inputs, got %d", len(fused))
	}

	fused = fuseRRF([]ScoredResult{{Key: "a", Score: 1.0}}, nil)
	if len(fused) != 1 {
		t.Fatalf("expected 1 result, got %d", len(fused))
	}
	if fused[0].Key != "a" {
		t.Errorf("expected key 'a', got %q", fused[0].Key)
	}
}
