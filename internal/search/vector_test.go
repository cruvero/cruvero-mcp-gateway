package search

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
)

// mockEmbedder returns predetermined embeddings for testing.
type mockEmbedder struct {
	embedFn func(ctx context.Context, texts []string) ([][]float32, error)
	ready   bool
}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, texts)
	}
	return nil, fmt.Errorf("no embed function set")
}

func (m *mockEmbedder) Ready(_ context.Context) bool {
	return m.ready
}

// newMockEmbedder creates a mock that returns simple deterministic vectors.
// Each document gets a unique vector based on its position + a shared component.
func newMockEmbedder() *mockEmbedder {
	return &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			result := make([][]float32, len(texts))
			for i := range texts {
				// Create a 4-dim vector with a unique component.
				vec := make([]float32, 4)
				vec[0] = 0.5 // shared component
				vec[1+i%3] = 1.0
				result[i] = vec
			}
			return result, nil
		},
	}
}

func TestVectorEngine_IndexAndSearch(t *testing.T) {
	t.Parallel()

	// Return vectors by call order: first call (Index) gets 3 doc vectors,
	// second call (Search) gets the query vector matching doc[0].
	callNum := 0
	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			callNum++
			if callNum == 1 {
				// Index: 3 docs → doc[0]=[1,0,0] doc[1]=[0,1,0] doc[2]=[0,0,1]
				return [][]float32{
					{1.0, 0.0, 0.0},
					{0.0, 1.0, 0.0},
					{0.0, 0.0, 1.0},
				}, nil
			}
			// Search: query → matches doc[0]
			return [][]float32{{1.0, 0.0, 0.0}}, nil
		},
	}

	docs := []Document{
		{Key: "k8s.list_pods", Name: "k8s.list_pods", Title: "List Pods", Description: "List all pods in a Kubernetes namespace.", Tags: []string{"kubernetes", "pods"}},
		{Key: "docker.list_containers", Name: "docker.list_containers", Title: "List Containers", Description: "List running Docker containers.", Tags: []string{"docker"}},
		{Key: "slack.send_message", Name: "slack.send_message", Title: "Send Message", Description: "Send a message to a Slack channel.", Tags: []string{"messaging"}},
	}

	e := NewVectorEngine(embedder)
	if err := e.Index(context.Background(), docs); err != nil {
		t.Fatalf("Index error: %v", err)
	}

	results, err := e.Search(context.Background(), "kubernetes pods", 0)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].Key != "k8s.list_pods" {
		t.Errorf("expected first result k8s.list_pods, got %q", results[0].Key)
	}
}

func TestVectorEngine_EmptyIndex(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	results, err := e.Search(context.Background(), "test", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil from empty index, got %v", results)
	}
}

func TestVectorEngine_EmptyQuery(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)
	_ = e.Index(context.Background(), []Document{
		{Key: "test", Name: "test", Description: "test doc"},
	})

	results, err := e.Search(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestVectorEngine_EmbedderError(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{
		ready: false,
		embedFn: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, fmt.Errorf("sidecar unavailable")
		},
	}

	e := NewVectorEngine(embedder)

	// Index should propagate error.
	err := e.Index(context.Background(), []Document{
		{Key: "test", Name: "test"},
	})
	if err == nil {
		t.Fatal("expected error from embedder failure during Index")
	}

	// Search should propagate error.
	_, err = e.Search(context.Background(), "test", 0)
	if err == nil {
		t.Fatal("expected error from embedder failure during Search")
	}
}

func TestVectorEngine_Remove(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)
	_ = e.Index(context.Background(), []Document{
		{Key: "a", Name: "a", Description: "first"},
		{Key: "b", Name: "b", Description: "second"},
	})

	e.Remove("a")

	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.docs) != 1 {
		t.Fatalf("expected 1 doc after removal, got %d", len(e.docs))
	}
	if e.docs[0].Key != "b" {
		t.Fatalf("expected remaining doc to be 'b', got %q", e.docs[0].Key)
	}
	if len(e.embeddings) != 1 {
		t.Fatalf("expected 1 embedding after removal, got %d", len(e.embeddings))
	}
}

func TestVectorEngine_RemoveNonexistent(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)
	_ = e.Index(context.Background(), []Document{
		{Key: "a", Name: "a"},
	})

	e.Remove("nonexistent")

	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.docs) != 1 {
		t.Fatalf("expected 1 doc after removing nonexistent, got %d", len(e.docs))
	}
}

func TestVectorEngine_Ready(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{ready: true}
	e := NewVectorEngine(embedder)
	if !e.Ready() {
		t.Fatal("expected Ready() = true when embedder is ready")
	}

	embedder.ready = false
	if e.Ready() {
		t.Fatal("expected Ready() = false when embedder is not ready")
	}
}

func TestVectorEngine_Limit(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)
	_ = e.Index(context.Background(), []Document{
		{Key: "a", Name: "a", Description: "first"},
		{Key: "b", Name: "b", Description: "second"},
		{Key: "c", Name: "c", Description: "third"},
	})

	results, err := e.Search(context.Background(), "test", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with limit=1, got %d", len(results))
	}
}

func TestVectorEngine_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	embedder := newMockEmbedder()
	e := NewVectorEngine(embedder)

	docs := []Document{
		{Key: "a", Name: "a", Description: "first"},
		{Key: "b", Name: "b", Description: "second"},
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = e.Index(context.Background(), docs)
		}()
		go func() {
			defer wg.Done()
			_, _ = e.Search(context.Background(), "test", 0)
		}()
	}
	wg.Wait()
}

func TestVectorEngine_IndexEmbeddingCountMismatch(t *testing.T) {
	t.Parallel()

	embedder := &mockEmbedder{
		ready: true,
		embedFn: func(_ context.Context, texts []string) ([][]float32, error) {
			// Return fewer embeddings than documents.
			return [][]float32{{0.1, 0.2}}, nil
		},
	}

	e := NewVectorEngine(embedder)
	err := e.Index(context.Background(), []Document{
		{Key: "a", Name: "a"},
		{Key: "b", Name: "b"},
	})
	if err == nil {
		t.Fatal("expected error for embedding count mismatch")
	}
}

func TestCosineSimilarity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{name: "identical", a: []float32{1, 0, 0}, b: []float32{1, 0, 0}, want: 1.0},
		{name: "orthogonal", a: []float32{1, 0, 0}, b: []float32{0, 1, 0}, want: 0.0},
		{name: "opposite", a: []float32{1, 0, 0}, b: []float32{-1, 0, 0}, want: -1.0},
		{name: "mismatched length", a: []float32{1, 0}, b: []float32{1, 0, 0}, want: 0.0},
		{name: "empty", a: nil, b: nil, want: 0.0},
		{name: "zero vector", a: []float32{0, 0, 0}, b: []float32{1, 0, 0}, want: 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("cosineSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestBuildEmbedText(t *testing.T) {
	t.Parallel()

	doc := Document{
		Key:         "k8s.list_pods",
		Name:        "k8s.list_pods",
		Title:       "List Pods",
		Description: "List all pods.",
		Tags:        []string{"kubernetes", "pods"},
	}

	text := buildEmbedText(doc)
	// Name should have dots/underscores replaced with spaces.
	if text == "" {
		t.Fatal("expected non-empty text")
	}
	// Should contain all parts.
	for _, want := range []string{"k8s list pods", "List Pods", "List all pods", "kubernetes pods"} {
		if !contains(text, want) {
			t.Errorf("expected text to contain %q, got %q", want, text)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
