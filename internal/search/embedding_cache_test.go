package search

import (
	"sync"
	"testing"
)

func TestEmbeddingCache_GetSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maxSize   int
		ops       func(c *EmbeddingCache)
		wantLen   int
		checkGet  string
		wantFound bool
	}{
		{
			name:    "basic set and get",
			maxSize: 10,
			ops: func(c *EmbeddingCache) {
				c.Set("a", []float32{1.0, 2.0})
			},
			wantLen:   1,
			checkGet:  "a",
			wantFound: true,
		},
		{
			name:    "cache miss",
			maxSize: 10,
			ops: func(c *EmbeddingCache) {
				c.Set("a", []float32{1.0})
			},
			wantLen:   1,
			checkGet:  "b",
			wantFound: false,
		},
		{
			name:    "overwrite existing key",
			maxSize: 10,
			ops: func(c *EmbeddingCache) {
				c.Set("a", []float32{1.0})
				c.Set("a", []float32{2.0, 3.0})
			},
			wantLen:   1,
			checkGet:  "a",
			wantFound: true,
		},
		{
			name:    "LRU eviction at capacity",
			maxSize: 2,
			ops: func(c *EmbeddingCache) {
				c.Set("a", []float32{1.0})
				c.Set("b", []float32{2.0})
				c.Set("c", []float32{3.0}) // should evict "a"
			},
			wantLen:   2,
			checkGet:  "a",
			wantFound: false,
		},
		{
			name:    "LRU access refreshes order",
			maxSize: 2,
			ops: func(c *EmbeddingCache) {
				c.Set("a", []float32{1.0})
				c.Set("b", []float32{2.0})
				c.Get("a")                 // refresh "a"
				c.Set("c", []float32{3.0}) // should evict "b" (LRU), not "a"
			},
			wantLen:   2,
			checkGet:  "a",
			wantFound: true,
		},
		{
			name:    "empty cache returns miss",
			maxSize: 5,
			ops:     func(c *EmbeddingCache) {},
			wantLen: 0, checkGet: "x", wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewEmbeddingCache(tt.maxSize)
			tt.ops(c)

			if got := c.Len(); got != tt.wantLen {
				t.Errorf("Len() = %d, want %d", got, tt.wantLen)
			}

			_, found := c.Get(tt.checkGet)
			if found != tt.wantFound {
				t.Errorf("Get(%q) found = %v, want %v", tt.checkGet, found, tt.wantFound)
			}
		})
	}
}

func TestEmbeddingCache_ContentHash(t *testing.T) {
	t.Parallel()

	c := NewEmbeddingCache(10)

	tests := []struct {
		name        string
		nameA, descA string
		nameB, descB string
		wantEqual   bool
	}{
		{
			name:      "identical inputs produce same hash",
			nameA:     "tool.a", descA: "does stuff",
			nameB:     "tool.a", descB: "does stuff",
			wantEqual: true,
		},
		{
			name:      "different name produces different hash",
			nameA:     "tool.a", descA: "does stuff",
			nameB:     "tool.b", descB: "does stuff",
			wantEqual: false,
		},
		{
			name:      "different description produces different hash",
			nameA:     "tool.a", descA: "does stuff",
			nameB:     "tool.a", descB: "does other stuff",
			wantEqual: false,
		},
		{
			name:      "empty inputs are consistent",
			nameA:     "", descA: "",
			nameB:     "", descB: "",
			wantEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hashA := c.ContentHash(tt.nameA, tt.descA)
			hashB := c.ContentHash(tt.nameB, tt.descB)

			if (hashA == hashB) != tt.wantEqual {
				t.Errorf("ContentHash equality = %v, want %v (a=%q b=%q)",
					hashA == hashB, tt.wantEqual, hashA, hashB)
			}

			// Hashes should be non-empty hex strings.
			if len(hashA) != 64 {
				t.Errorf("expected 64-char hex hash, got length %d", len(hashA))
			}
		})
	}
}

func TestEmbeddingCache_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	c := NewEmbeddingCache(100)
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Add(2)
		key := string(rune('a' + i%26))
		go func() {
			defer wg.Done()
			c.Set(key, []float32{float32(i)})
		}()
		go func() {
			defer wg.Done()
			c.Get(key)
		}()
	}
	wg.Wait()

	if c.Len() > 100 {
		t.Fatalf("cache exceeded max size: %d", c.Len())
	}
}

func TestEmbeddingCache_LRUEvictionOrder(t *testing.T) {
	t.Parallel()

	c := NewEmbeddingCache(3)
	c.Set("a", []float32{1.0})
	c.Set("b", []float32{2.0})
	c.Set("c", []float32{3.0})

	// Access "a" to make it most recent.
	c.Get("a")
	// Now order is: b, c, a

	// Add "d" -- should evict "b" (oldest).
	c.Set("d", []float32{4.0})

	if _, ok := c.Get("b"); ok {
		t.Fatal("expected 'b' to be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected 'a' to still be present (was accessed recently)")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("expected 'c' to still be present")
	}
	if _, ok := c.Get("d"); !ok {
		t.Fatal("expected 'd' to be present")
	}
}
