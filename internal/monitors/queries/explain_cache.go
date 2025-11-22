package queries

import (
	"container/list"
	"sync"
	"time"
)

// ExplainResult holds a cached EXPLAIN plan result.
type ExplainResult struct {
	Query       string
	Plan        string // JSON formatted plan
	ExecutedAt  time.Time
	Error       string
	Fingerprint uint64
}

// ExplainCache is an LRU cache for EXPLAIN results.
type ExplainCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[uint64]*list.Element
	order    *list.List // Front = most recent, Back = least recent
}

type cacheEntry struct {
	fingerprint uint64
	result      *ExplainResult
}

// NewExplainCache creates a new EXPLAIN cache with the given capacity.
func NewExplainCache(capacity int) *ExplainCache {
	return &ExplainCache{
		capacity: capacity,
		items:    make(map[uint64]*list.Element),
		order:    list.New(),
	}
}

// Get retrieves a cached EXPLAIN result by fingerprint.
func (c *ExplainCache) Get(fingerprint uint64) (*ExplainResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if elem, ok := c.items[fingerprint]; ok {
		// Move to front (most recently used)
		c.order.MoveToFront(elem)
		return elem.Value.(*cacheEntry).result, true
	}
	return nil, false
}

// Put stores an EXPLAIN result in the cache.
func (c *ExplainCache) Put(fingerprint uint64, result *ExplainResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if elem, ok := c.items[fingerprint]; ok {
		// Update and move to front
		elem.Value.(*cacheEntry).result = result
		c.order.MoveToFront(elem)
		return
	}

	// Evict if at capacity
	if c.order.Len() >= c.capacity {
		c.evict()
	}

	// Add new entry at front
	entry := &cacheEntry{
		fingerprint: fingerprint,
		result:      result,
	}
	elem := c.order.PushFront(entry)
	c.items[fingerprint] = elem
}

// evict removes the least recently used entry.
func (c *ExplainCache) evict() {
	if elem := c.order.Back(); elem != nil {
		entry := elem.Value.(*cacheEntry)
		delete(c.items, entry.fingerprint)
		c.order.Remove(elem)
	}
}

// Clear removes all entries from the cache.
func (c *ExplainCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[uint64]*list.Element)
	c.order.Init()
}

// Len returns the number of cached entries.
func (c *ExplainCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}
