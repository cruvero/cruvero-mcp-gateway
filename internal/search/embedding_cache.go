package search

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// EmbeddingCache is a concurrency-safe LRU cache for document embeddings.
// Keys are content hashes; the most recently accessed entry is at the end
// of the order slice.
type EmbeddingCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	order   []string // LRU order, most recent at end
	maxSize int
}

type cacheEntry struct {
	embedding []float32
}

// NewEmbeddingCache creates an LRU embedding cache with the given capacity.
// A maxSize <= 0 disables eviction (unbounded).
func NewEmbeddingCache(maxSize int) *EmbeddingCache {
	if maxSize <= 0 {
		maxSize = 0
	}
	return &EmbeddingCache{
		entries: make(map[string]*cacheEntry),
		maxSize: maxSize,
	}
}

// Get retrieves a cached embedding. Returns (nil, false) on cache miss.
func (c *EmbeddingCache) Get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// Move to end (most recently used).
	c.moveToEndLocked(key)
	return e.embedding, true
}

// Set stores an embedding under the given key, evicting the LRU entry if
// the cache is at capacity.
func (c *EmbeddingCache) Set(key string, embedding []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		c.entries[key] = &cacheEntry{embedding: embedding}
		c.moveToEndLocked(key)
		return
	}

	// Evict oldest entry if at capacity.
	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		c.evictLocked()
	}

	c.entries[key] = &cacheEntry{embedding: embedding}
	c.order = append(c.order, key)
}

// ContentHash returns a SHA-256 hex digest of the concatenation of name and
// description, suitable as a cache key for detecting content changes.
func (c *EmbeddingCache) ContentHash(name, description string) string {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0}) // separator
	h.Write([]byte(description))
	return hex.EncodeToString(h.Sum(nil))
}

// Len returns the number of cached entries.
func (c *EmbeddingCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// moveToEndLocked moves key to the end of the order slice (most recently used).
// Must be called under write lock.
func (c *EmbeddingCache) moveToEndLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

// evictLocked removes the least recently used entry. Must be called under write lock.
func (c *EmbeddingCache) evictLocked() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}
