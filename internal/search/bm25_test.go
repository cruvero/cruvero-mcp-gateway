package search

import (
	"context"
	"sync"
	"testing"
)

var testCorpus = []Document{
	{Key: "k8s.list_pods", Name: "k8s.list_pods", Title: "List Pods", Description: "List all pods in a Kubernetes namespace.", Tags: []string{"kubernetes", "pods"}},
	{Key: "k8s.get_deployment", Name: "k8s.get_deployment", Title: "Get Deployment", Description: "Retrieve a Kubernetes deployment by name.", Tags: []string{"kubernetes", "deployment"}},
	{Key: "k8s.delete_pod", Name: "k8s.delete_pod", Title: "Delete Pod", Description: "Delete a pod from the cluster.", Tags: []string{"kubernetes"}},
	{Key: "docker.list_containers", Name: "docker.list_containers", Title: "List Containers", Description: "List running Docker containers.", Tags: []string{"docker", "containers"}},
	{Key: "docker.build_image", Name: "docker.build_image", Title: "Build Image", Description: "Build a Docker container image from a Dockerfile.", Tags: []string{"docker"}},
	{Key: "slack.send_message", Name: "slack.send_message", Title: "Send Message", Description: "Send a message to a Slack channel.", Tags: []string{"messaging", "slack"}},
	{Key: "github.create_issue", Name: "github.create_issue", Title: "Create Issue", Description: "Create a new issue in a GitHub repository.", Tags: []string{"github", "issues"}},
	{Key: "pg.run_query", Name: "pg.run_query", Title: "Run Query", Description: "Execute a SQL query against a PostgreSQL database.", Tags: []string{"database", "sql"}},
}

func newIndexedEngine() *BM25Engine {
	e := NewBM25Engine()
	_ = e.Index(context.Background(), testCorpus)
	return e
}

func mustSearch(t *testing.T, e *BM25Engine, query string) []ScoredResult {
	t.Helper()
	results, err := e.Search(context.Background(), query, 0)
	if err != nil {
		t.Fatalf("unexpected search error: %v", err)
	}
	return results
}

func TestBM25Engine_KubernetesPodsViaSynonym(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "kubernetes pods")
	if len(results) == 0 {
		t.Fatal("expected results for 'kubernetes pods', got none")
	}
	if results[0].Key != "k8s.list_pods" {
		t.Errorf("expected first result k8s.list_pods, got %q", results[0].Key)
	}
}

func TestBM25Engine_K8sPods(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "k8s pods")
	if len(results) == 0 {
		t.Fatal("expected results for 'k8s pods', got none")
	}
	if results[0].Key != "k8s.list_pods" {
		t.Errorf("expected first result k8s.list_pods, got %q", results[0].Key)
	}
}

func TestBM25Engine_ExactToolName(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "k8s.list_pods")
	if len(results) == 0 {
		t.Fatal("expected results for exact tool name, got none")
	}
	if results[0].Key != "k8s.list_pods" {
		t.Errorf("expected first result k8s.list_pods, got %q", results[0].Key)
	}
}

func TestBM25Engine_ListReturnsMultiple(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "list")
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results for 'list', got %d", len(results))
	}

	keys := make(map[string]bool)
	for _, r := range results {
		keys[r.Key] = true
	}
	if !keys["k8s.list_pods"] {
		t.Error("expected k8s.list_pods in results")
	}
	if !keys["docker.list_containers"] {
		t.Error("expected docker.list_containers in results")
	}
}

func TestBM25Engine_KubernetesDeployment(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "kubernetes deployment")
	if len(results) == 0 {
		t.Fatal("expected results for 'kubernetes deployment', got none")
	}
	if results[0].Key != "k8s.get_deployment" {
		t.Errorf("expected first result k8s.get_deployment, got %q", results[0].Key)
	}

	// list_pods should also appear (via kubernetes synonym) but ranked lower.
	if len(results) < 2 {
		t.Fatal("expected multiple k8s results")
	}
	foundListPods := false
	for _, r := range results[1:] {
		if r.Key == "k8s.list_pods" {
			foundListPods = true
			break
		}
	}
	if !foundListPods {
		t.Error("expected k8s.list_pods in results (lower ranked)")
	}
}

func TestBM25Engine_Slack(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "Slack")
	if len(results) == 0 {
		t.Fatal("expected results for 'Slack', got none")
	}
	if results[0].Key != "slack.send_message" {
		t.Errorf("expected first result slack.send_message, got %q", results[0].Key)
	}
}

func TestBM25Engine_EmptyQuery(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "")
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestBM25Engine_NoMatch(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results for 'nonexistent', got %d", len(results))
	}
}

func TestBM25Engine_EmptyIndex(t *testing.T) {
	t.Parallel()
	e := NewBM25Engine()

	results := mustSearch(t, e, "anything")
	if results != nil {
		t.Errorf("expected nil from empty index, got %v", results)
	}
}

func TestBM25Engine_PostgresSynonym(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "postgres query")
	if len(results) == 0 {
		t.Fatal("expected results for 'postgres query', got none")
	}
	if results[0].Key != "pg.run_query" {
		t.Errorf("expected first result pg.run_query, got %q", results[0].Key)
	}
}

func TestBM25Engine_DatabaseSynonym(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "database")
	if len(results) == 0 {
		t.Fatal("expected results for 'database', got none")
	}
	found := false
	for _, r := range results {
		if r.Key == "pg.run_query" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected pg.run_query in 'database' results")
	}
}

func TestBM25Engine_ContainerSynonym(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "container")
	if len(results) == 0 {
		t.Fatal("expected results for 'container', got none")
	}
	found := false
	for _, r := range results {
		if r.Key == "docker.list_containers" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected docker.list_containers in 'container' results")
	}
}

func TestBM25Engine_Reindex(t *testing.T) {
	t.Parallel()
	e := NewBM25Engine()

	_ = e.Index(context.Background(), []Document{
		{Key: "old.tool", Name: "old.tool", Description: "Old tool description."},
	})
	results := mustSearch(t, e, "old")
	if len(results) != 1 {
		t.Fatalf("expected 1 result before reindex, got %d", len(results))
	}

	_ = e.Index(context.Background(), []Document{
		{Key: "new.tool", Name: "new.tool", Description: "New tool description."},
	})
	results = mustSearch(t, e, "old")
	if len(results) != 0 {
		t.Fatalf("expected 0 results for 'old' after reindex, got %d", len(results))
	}
	results = mustSearch(t, e, "new")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'new' after reindex, got %d", len(results))
	}
}

func TestBM25Engine_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = e.Index(context.Background(), testCorpus)
		}()
		go func() {
			defer wg.Done()
			_, _ = e.Search(context.Background(), "kubernetes pods", 0)
		}()
	}
	wg.Wait()
}

func TestBM25Engine_ScoresDescending(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	results := mustSearch(t, e, "kubernetes")
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: score[%d]=%f > score[%d]=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestBM25Engine_DeleteSynonym(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	// "remove" should match k8s.delete_pod via delete/remove synonym.
	results := mustSearch(t, e, "remove pod")
	if len(results) == 0 {
		t.Fatal("expected results for 'remove pod', got none")
	}
	if results[0].Key != "k8s.delete_pod" {
		t.Errorf("expected first result k8s.delete_pod, got %q", results[0].Key)
	}
}

func TestBM25Engine_TagsSearchable(t *testing.T) {
	t.Parallel()
	e := NewBM25Engine()
	_ = e.Index(context.Background(), []Document{
		{Key: "tool.a", Name: "tool.a", Description: "Does something.", Tags: []string{"unique-tag-xyz"}},
		{Key: "tool.b", Name: "tool.b", Description: "Does something else."},
	})

	results := mustSearch(t, e, "unique-tag-xyz")
	if len(results) != 1 {
		t.Fatalf("expected 1 result matching tag, got %d", len(results))
	}
	if results[0].Key != "tool.a" {
		t.Errorf("expected tool.a, got %q", results[0].Key)
	}
}

func TestBM25Engine_Limit(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	// "kubernetes" matches multiple docs via synonym expansion.
	all := mustSearch(t, e, "kubernetes")
	if len(all) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(all))
	}

	limited, err := e.Search(context.Background(), "kubernetes", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 result with limit=1, got %d", len(limited))
	}
	if limited[0].Key != all[0].Key {
		t.Errorf("limited result should match top result: got %q, want %q", limited[0].Key, all[0].Key)
	}
}

func TestBM25Engine_Remove(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()

	// Verify k8s.list_pods is found.
	results := mustSearch(t, e, "k8s.list_pods")
	if len(results) == 0 {
		t.Fatal("expected k8s.list_pods before removal")
	}

	e.Remove("k8s.list_pods")

	// Should no longer match exact name.
	results = mustSearch(t, e, "k8s.list_pods")
	for _, r := range results {
		if r.Key == "k8s.list_pods" {
			t.Fatal("k8s.list_pods should not appear after removal")
		}
	}
}

func TestBM25Engine_RemoveNonexistent(t *testing.T) {
	t.Parallel()
	e := newIndexedEngine()
	before := mustSearch(t, e, "kubernetes")

	e.Remove("nonexistent.tool")

	after := mustSearch(t, e, "kubernetes")
	if len(before) != len(after) {
		t.Fatalf("removing nonexistent key changed results: before=%d after=%d", len(before), len(after))
	}
}

func TestBM25Engine_Ready(t *testing.T) {
	t.Parallel()
	e := NewBM25Engine()
	if !e.Ready() {
		t.Fatal("BM25Engine should always be ready")
	}
}

func TestNewBM25EngineWithSynonyms(t *testing.T) {
	t.Parallel()

	t.Run("nil provider uses static", func(t *testing.T) {
		t.Parallel()
		e := NewBM25EngineWithSynonyms(nil)
		if e.synonyms == nil {
			t.Fatal("expected non-nil synonym provider when passing nil")
		}
	})

	t.Run("custom provider", func(t *testing.T) {
		t.Parallel()
		custom := NewMapSynonymProvider(map[string][]string{"foo": {"bar"}})
		e := NewBM25EngineWithSynonyms(custom)
		syns := e.synonyms.Lookup("foo")
		if len(syns) != 1 || syns[0] != "bar" {
			t.Fatalf("expected [bar], got %v", syns)
		}
	})
}
