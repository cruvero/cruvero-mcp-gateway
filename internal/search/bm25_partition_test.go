package search

import (
	"context"
	"sync"
	"testing"
)

func TestBM25Engine_AddPartition(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()

	docsA := []Document{
		{Key: "a.tool1", Name: "a.tool1", Description: "Alpha tool one."},
		{Key: "a.tool2", Name: "a.tool2", Description: "Alpha tool two."},
	}
	docsB := []Document{
		{Key: "b.tool1", Name: "b.tool1", Description: "Beta tool one."},
	}

	if err := e.AddPartition("server-a", docsA); err != nil {
		t.Fatalf("AddPartition server-a: %v", err)
	}
	if err := e.AddPartition("server-b", docsB); err != nil {
		t.Fatalf("AddPartition server-b: %v", err)
	}

	// Search across both partitions.
	results, err := e.Search(context.Background(), "tool", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results across partitions, got %d", len(results))
	}
}

func TestBM25Engine_RemovePartition(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()

	docsA := []Document{
		{Key: "a.tool1", Name: "a.tool1", Description: "Alpha tool."},
	}
	docsB := []Document{
		{Key: "b.tool1", Name: "b.tool1", Description: "Beta tool."},
	}

	_ = e.AddPartition("server-a", docsA)
	_ = e.AddPartition("server-b", docsB)

	e.RemovePartition("server-a")

	results, _ := e.Search(context.Background(), "tool", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result after removing server-a, got %d", len(results))
	}
	if results[0].Key != "b.tool1" {
		t.Fatalf("expected b.tool1, got %q", results[0].Key)
	}
}

func TestBM25Engine_UpdatePartition(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()
	_ = e.AddPartition("server-a", []Document{
		{Key: "a.old", Name: "a.old", Description: "Old tool."},
	})

	// Update replaces the partition entirely.
	_ = e.UpdatePartition("server-a", []Document{
		{Key: "a.new", Name: "a.new", Description: "New tool."},
	})

	results, _ := e.Search(context.Background(), "old", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'old' after update, got %d", len(results))
	}

	results, _ = e.Search(context.Background(), "new", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'new' after update, got %d", len(results))
	}
}

func TestBM25Engine_IndexClearsPartitions(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()

	// Add server-specific partitions.
	_ = e.AddPartition("server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Alpha."},
	})

	// Index() should clear all partitions and create a default one.
	_ = e.Index(context.Background(), []Document{
		{Key: "default.tool", Name: "default.tool", Description: "Default tool."},
	})

	results, _ := e.Search(context.Background(), "alpha", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'alpha' after Index(), got %d", len(results))
	}

	results, _ = e.Search(context.Background(), "default", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'default' after Index(), got %d", len(results))
	}
}

func TestBM25Engine_GlobalIDFAcrossPartitions(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()

	// "kubernetes" appears in server-a only; "list" appears in both.
	_ = e.AddPartition("server-a", []Document{
		{Key: "a.k8s", Name: "k8s.list_pods", Description: "List kubernetes pods."},
	})
	_ = e.AddPartition("server-b", []Document{
		{Key: "b.list", Name: "docker.list_containers", Description: "List docker containers."},
	})

	// "kubernetes" is rarer (1 doc) than "list" (2 docs), so it should have higher IDF
	// and k8s.list_pods should rank higher for "kubernetes list".
	results, err := e.Search(context.Background(), "kubernetes list", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].Key != "a.k8s" {
		t.Errorf("expected a.k8s to rank first (kubernetes is rarer), got %q", results[0].Key)
	}
}

func TestBM25Engine_PartitionConcurrentAccess(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(3)
		serverID := string(rune('a' + i%5))
		go func() {
			defer wg.Done()
			_ = e.AddPartition(serverID, []Document{
				{Key: serverID + ".tool", Name: serverID + ".tool", Description: "A tool."},
			})
		}()
		go func() {
			defer wg.Done()
			_, _ = e.Search(context.Background(), "tool", 0)
		}()
		go func() {
			defer wg.Done()
			e.RemovePartition(serverID)
		}()
	}
	wg.Wait()
}

func TestBM25Engine_RemoveFromPartition(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()
	_ = e.AddPartition("server-a", []Document{
		{Key: "a.tool1", Name: "a.tool1", Description: "First tool."},
		{Key: "a.tool2", Name: "a.tool2", Description: "Second tool."},
	})

	e.Remove("a.tool1")

	results, _ := e.Search(context.Background(), "first", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'first' after remove, got %d", len(results))
	}

	results, _ = e.Search(context.Background(), "second", 0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'second', got %d", len(results))
	}
}

func TestBM25Engine_RemoveLastDocDeletesPartition(t *testing.T) {
	t.Parallel()

	e := NewBM25Engine()
	_ = e.AddPartition("server-a", []Document{
		{Key: "a.tool", Name: "a.tool", Description: "Only tool."},
	})

	e.Remove("a.tool")

	// Partition should be completely gone.
	results, _ := e.Search(context.Background(), "tool", 0)
	if len(results) != 0 {
		t.Fatalf("expected 0 results after removing last doc, got %d", len(results))
	}
}
