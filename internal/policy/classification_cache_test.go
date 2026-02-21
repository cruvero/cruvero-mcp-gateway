package policy

import (
	"sync"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestClassificationCacheHitMiss(t *testing.T) {
	t.Parallel()

	cache := NewClassificationCache(time.Minute)

	if got := cache.Get("missing"); got != nil {
		t.Fatalf("expected nil for cache miss, got %+v", got)
	}

	tc := &types.ToolClassification{
		ToolName:  "github.delete_repo",
		RiskLevel: types.RiskDestructive,
		Reason:    "test",
	}
	cache.Set("github.delete_repo", tc)

	got := cache.Get("github.delete_repo")
	if got == nil {
		t.Fatal("expected cache hit")
	}
	if got.RiskLevel != types.RiskDestructive {
		t.Fatalf("expected destructive, got %q", got.RiskLevel)
	}
}

func TestClassificationCacheTTLExpiry(t *testing.T) {
	t.Parallel()

	cache := NewClassificationCache(time.Millisecond)

	tc := &types.ToolClassification{
		ToolName:  "tool.a",
		RiskLevel: types.RiskReadOnly,
	}
	cache.Set("tool.a", tc)

	time.Sleep(5 * time.Millisecond)

	if got := cache.Get("tool.a"); got != nil {
		t.Fatalf("expected nil after TTL expiry, got %+v", got)
	}
}

func TestClassificationCacheInvalidate(t *testing.T) {
	t.Parallel()

	cache := NewClassificationCache(time.Minute)
	cache.Set("tool.a", &types.ToolClassification{ToolName: "tool.a", RiskLevel: types.RiskWrite})
	cache.Set("tool.b", &types.ToolClassification{ToolName: "tool.b", RiskLevel: types.RiskReadOnly})

	cache.Invalidate("tool.a")
	if got := cache.Get("tool.a"); got != nil {
		t.Fatal("expected nil after invalidation")
	}
	if got := cache.Get("tool.b"); got == nil {
		t.Fatal("expected tool.b to still be cached")
	}
}

func TestClassificationCacheInvalidateAll(t *testing.T) {
	t.Parallel()

	cache := NewClassificationCache(time.Minute)
	cache.Set("tool.a", &types.ToolClassification{ToolName: "tool.a", RiskLevel: types.RiskWrite})
	cache.Set("tool.b", &types.ToolClassification{ToolName: "tool.b", RiskLevel: types.RiskReadOnly})

	cache.InvalidateAll()
	if got := cache.Get("tool.a"); got != nil {
		t.Fatal("expected nil after invalidate all")
	}
	if got := cache.Get("tool.b"); got != nil {
		t.Fatal("expected nil after invalidate all")
	}
}

func TestClassificationCacheConcurrent(t *testing.T) {
	t.Parallel()

	cache := NewClassificationCache(time.Minute)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Set("tool.a", &types.ToolClassification{ToolName: "tool.a", RiskLevel: types.RiskWrite})
			_ = cache.Get("tool.a")
			cache.Invalidate("tool.a")
		}()
	}
	wg.Wait()
}
