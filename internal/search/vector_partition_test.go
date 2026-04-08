package search

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestVectorEngine_AddPartition(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	docsA := []Document{
		{Key: "a.tool1", Name: "a.tool1", Description: "Alpha tool."},
	}
	docsB := []Document{
		{Key: "b.tool1", Name: "b.tool1", Description: "Beta tool."},
	}

	if err := e.AddPartition(context.Background(), "server-a", docsA); err != nil {
		t.Fatalf("AddPartition server-a: %v", err)
	}
	if err := e.AddPartition(context.Background(), "server-b", docsB); err != nil {
		t.Fatalf("AddPartition server-b: %v", err)
	}

	e.mu.RLock()
	if len(e.partitions) != 2 {
		t.Fatalf("expected 2 partitions, got %d", len(e.partitions))
	}
	if len(e.docs) != 2 {
		t.Fatalf("expected 2 flat docs, got %d", len(e.docs))
	}
	e.mu.RUnlock()
}

func TestVectorEngine_RemovePartition(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	_ = e.AddPartition(context.Background(), "server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Alpha."},
	})
	_ = e.AddPartition(context.Background(), "server-b", []Document{
		{Key: "b.tool", Name: "b.tool", Description: "Beta."},
	})

	e.RemovePartition("server-a")

	e.mu.RLock()
	if len(e.partitions) != 1 {
		t.Fatalf("expected 1 partition, got %d", len(e.partitions))
	}
	if len(e.docs) != 1 {
		t.Fatalf("expected 1 flat doc, got %d", len(e.docs))
	}
	e.mu.RUnlock()
}

func TestVectorEngine_UpdatePartition(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	_ = e.AddPartition(context.Background(), "server-a", []Document{
		{Key: "a.old", Name: "a.old", Description: "Old."},
	})

	_ = e.UpdatePartition(context.Background(), "server-a", []Document{
		{Key: "a.new", Name: "a.new", Description: "New."},
	})

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.partitions) != 1 {
		t.Fatalf("expected 1 partition, got %d", len(e.partitions))
	}
	p := e.partitions["server-a"]
	if p == nil || len(p.docs) != 1 || p.docs[0].Key != "a.new" {
		t.Fatal("partition should contain only the new doc")
	}
}

func TestVectorEngine_AddPartitionEmpty(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	_ = e.AddPartition(context.Background(), "server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Tool."},
	})

	// Adding empty docs should remove the partition.
	_ = e.AddPartition(context.Background(), "server-a", nil)

	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.partitions) != 0 {
		t.Fatalf("expected 0 partitions after adding empty docs, got %d", len(e.partitions))
	}
}

func TestVectorEngine_WithEmbeddingCache(t *testing.T) {
	t.Parallel()

	embedCalls := 0
	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			embedCalls++
			result := make([][]float32, len(texts))
			for i := range texts {
				result[i] = []float32{0.1, 0.2, 0.3}
			}
			return result, nil
		},
	}

	cache := NewEmbeddingCache(100)
	e := NewVectorEngine(embedder)
	e.SetEmbeddingCache(cache)

	docs := []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Alpha tool."},
		{Key: "b.tool", Name: "b.tool", Description: "Beta tool."},
	}

	// First call: cache is empty, should embed both docs.
	_ = e.AddPartition(context.Background(), "server-a", docs)
	if embedCalls != 1 {
		t.Fatalf("expected 1 embed call, got %d", embedCalls)
	}
	if cache.Len() != 2 {
		t.Fatalf("expected 2 cache entries, got %d", cache.Len())
	}

	// Second call with same docs: should use cache, no new embed call.
	_ = e.AddPartition(context.Background(), "server-a", docs)
	if embedCalls != 1 {
		t.Fatalf("expected still 1 embed call (cached), got %d", embedCalls)
	}

	// Third call with changed description: should re-embed only the changed doc.
	changedDocs := []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Alpha tool."},          // same
		{Key: "b.tool", Name: "b.tool", Description: "Beta tool MODIFIED."}, // changed
	}
	_ = e.AddPartition(context.Background(), "server-a", changedDocs)
	if embedCalls != 2 {
		t.Fatalf("expected 2 embed calls (one cached, one new), got %d", embedCalls)
	}
}

func TestVectorEngine_PartitionEmbedderError(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, fmt.Errorf("sidecar down")
		},
	}

	e := NewVectorEngine(embedder)
	err := e.AddPartition(context.Background(), "server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Tool."},
	})
	if err == nil {
		t.Fatal("expected error from embedder failure")
	}
}

func TestVectorEngine_PartitionConcurrentAccess(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(2)
		serverID := fmt.Sprintf("server-%d", i%3)
		go func() {
			defer wg.Done()
			_ = e.AddPartition(context.Background(), serverID, []Document{
				{Key: serverID + ".tool", Name: serverID + ".tool", Description: "A tool."},
			})
		}()
		go func() {
			defer wg.Done()
			e.RemovePartition(serverID)
		}()
	}
	wg.Wait()
}
