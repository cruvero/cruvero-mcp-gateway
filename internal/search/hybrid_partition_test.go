package search

import (
	"context"
	"fmt"
	"testing"
)

func TestHybridEngine_UpdatePartition(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	hybrid, _, _ := newTestHybridEngine(embedder)

	docsA := []Document{
		{Key: "a.tool1", Name: "a.tool1", Title: "Tool One", Description: "Alpha tool one."},
	}
	docsB := []Document{
		{Key: "b.tool1", Name: "b.tool1", Title: "Tool Two", Description: "Beta tool two."},
	}

	if err := hybrid.UpdatePartition(context.Background(), "server-a", docsA); err != nil {
		t.Fatalf("UpdatePartition server-a: %v", err)
	}
	if err := hybrid.UpdatePartition(context.Background(), "server-b", docsB); err != nil {
		t.Fatalf("UpdatePartition server-b: %v", err)
	}

	// BM25 search should find docs from both partitions.
	results, err := hybrid.bm25.Search(context.Background(), "tool", 0)
	if err != nil {
		t.Fatalf("BM25 Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 BM25 results, got %d", len(results))
	}
}

func TestHybridEngine_RemovePartition(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	hybrid, _, _ := newTestHybridEngine(embedder)

	_ = hybrid.UpdatePartition(context.Background(), "server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Alpha."},
	})
	_ = hybrid.UpdatePartition(context.Background(), "server-b", []Document{
		{Key: "b.tool", Name: "b.tool", Description: "Beta."},
	})

	hybrid.RemovePartition("server-a")

	results, _ := hybrid.bm25.Search(context.Background(), "alpha", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'alpha' after removing server-a, got %d", len(results))
	}

	results, _ = hybrid.bm25.Search(context.Background(), "beta", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'beta', got %d", len(results))
	}
}

func TestHybridEngine_UpdatePartitionVectorError(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, fmt.Errorf("embedding service down")
		},
	}

	hybrid, _, _ := newTestHybridEngine(embedder)

	// UpdatePartition should succeed even when vector fails (BM25 succeeds).
	err := hybrid.UpdatePartition(context.Background(), "server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Alpha tool."},
	})
	if err != nil {
		t.Fatalf("expected no error (BM25 should work), got: %v", err)
	}

	// BM25 should still have the docs.
	results, _ := hybrid.bm25.Search(context.Background(), "alpha", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 BM25 result, got %d", len(results))
	}

	if hybrid.VectorIndexFallbacks() != 1 {
		t.Fatalf("expected 1 vector fallback, got %d", hybrid.VectorIndexFallbacks())
	}
}

func TestHybridEngine_UpdatePartitionReplace(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	hybrid, _, _ := newTestHybridEngine(embedder)

	// Initial partition.
	_ = hybrid.UpdatePartition(context.Background(), "server-a", []Document{
		{Key: "a.old", Name: "a.old", Description: "Old tool."},
	})

	// Replace with new docs.
	_ = hybrid.UpdatePartition(context.Background(), "server-a", []Document{
		{Key: "a.new", Name: "a.new", Description: "New tool."},
	})

	results, _ := hybrid.bm25.Search(context.Background(), "old", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'old' after update, got %d", len(results))
	}

	results, _ = hybrid.bm25.Search(context.Background(), "new", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'new' after update, got %d", len(results))
	}
}
