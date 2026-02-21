package policy

import (
	"sync"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const defaultClassificationCacheTTL = 5 * time.Minute

type cacheEntry struct {
	classification *types.ToolClassification
	expiresAt      time.Time
}

// ClassificationCache provides a TTL-based in-memory cache for tool classifications.
type ClassificationCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

// NewClassificationCache creates a classification cache with the given TTL.
func NewClassificationCache(ttl time.Duration) *ClassificationCache {
	if ttl <= 0 {
		ttl = defaultClassificationCacheTTL
	}
	return &ClassificationCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
}

// Get returns a cached classification, or nil if not found or expired.
func (c *ClassificationCache) Get(toolName string) *types.ToolClassification {
	c.mu.RLock()
	entry, ok := c.entries[toolName]
	c.mu.RUnlock()

	if !ok {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.entries, toolName)
		c.mu.Unlock()
		return nil
	}

	return entry.classification
}

// Set stores a classification in the cache.
func (c *ClassificationCache) Set(toolName string, classification *types.ToolClassification) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[toolName] = &cacheEntry{
		classification: classification,
		expiresAt:      time.Now().Add(c.ttl),
	}
}

// Invalidate removes a specific entry from the cache.
func (c *ClassificationCache) Invalidate(toolName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, toolName)
}

// InvalidateAll clears the entire cache.
func (c *ClassificationCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}
