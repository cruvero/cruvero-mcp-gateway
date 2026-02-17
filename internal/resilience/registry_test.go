package resilience

import (
	"sync"
	"testing"
	"time"
)

func TestBreakerRegistryConcurrentGetOrCreate(t *testing.T) {
	t.Parallel()

	registry := NewBreakerRegistry()
	const workers = 64

	breakers := make([]*CircuitBreaker, workers)
	wg := sync.WaitGroup{}
	wg.Add(workers)
	for i := range workers {
		go func(idx int) {
			defer wg.Done()
			breakers[idx] = registry.GetOrCreate("server-a", 3, 2*time.Second)
		}(i)
	}
	wg.Wait()

	first := breakers[0]
	if first == nil {
		t.Fatal("expected breaker")
	}
	for i := 1; i < workers; i++ {
		if breakers[i] != first {
			t.Fatalf("expected all goroutines to get same breaker pointer, mismatch at %d", i)
		}
	}
}

func TestBreakerRegistryRemoveAndListOpen(t *testing.T) {
	t.Parallel()

	registry := NewBreakerRegistry()
	openBreaker := registry.GetOrCreate("server-open", 1, time.Second)
	_ = openBreaker.Execute(t.Context(), func() error { return assertErr{} })
	registry.GetOrCreate("server-closed", 2, time.Second)

	openServers := registry.ListOpen()
	if len(openServers) != 1 || openServers[0] != "server-open" {
		t.Fatalf("expected only server-open in ListOpen, got %#v", openServers)
	}

	registry.Remove("server-open")
	openServers = registry.ListOpen()
	if len(openServers) != 0 {
		t.Fatalf("expected no open servers after remove, got %#v", openServers)
	}
}

type assertErr struct{}

func (assertErr) Error() string {
	return "failure"
}
