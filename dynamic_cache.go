package smol

import (
	"container/list"
	"sync"
)

// Simple thread-safe LRU cache for fastTables keyed by string.
// This keeps a bounded number of entries to limit memory.

type cacheEntry struct {
	key string
	val fastTables
}

type lruCache struct {
	mu       sync.Mutex
	list     *list.List
	entries  map[string]*list.Element
	capacity int
	hits     int64
	misses   int64
}

// newLRUCache constructs a new lruCache with the provided capacity.
// Uses the conventional LRU initialism in the constructor name for clarity.
func newLRUCache(capacity int) *lruCache {
	if capacity <= 0 {
		capacity = 64
	}
	return &lruCache{list: list.New(), entries: make(map[string]*list.Element), capacity: capacity}
}

func (c *lruCache) Get(key string) (fastTables, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.list.MoveToFront(el)
		c.hits++
		return el.Value.(*cacheEntry).val, true
	}
	c.misses++
	return fastTables{}, false
}

func (c *lruCache) Put(key string, val fastTables) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		el.Value.(*cacheEntry).val = val
		c.list.MoveToFront(el)
		return
	}
	entry := &cacheEntry{key: key, val: val}
	el := c.list.PushFront(entry)
	c.entries[key] = el
	if c.list.Len() > c.capacity {
		// evict oldest
		back := c.list.Back()
		if back != nil {
			be := back.Value.(*cacheEntry)
			delete(c.entries, be.key)
			c.list.Remove(back)
		}
	}
}

func (c *lruCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

func (c *lruCache) Stats() (hits, misses int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}

func (c *lruCache) SetCapacity(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= 0 {
		n = 64
	}
	c.capacity = n
	for c.list.Len() > c.capacity {
		back := c.list.Back()
		if back == nil {
			break
		}
		be := back.Value.(*cacheEntry)
		delete(c.entries, be.key)
		c.list.Remove(back)
	}
}

// Initialize default cache on package init
func init() {
	fastTableCache = newLRUCache(64)
}

// SetFastTableCacheCapacity allows tests or callers to configure the cache size at runtime.
// This also resets hit/miss statistics so tests have a reproducible starting state.
func SetFastTableCacheCapacity(n int) {
	if fastTableCache == nil {
		fastTableCache = newLRUCache(n)
		return
	}
	// Update capacity (will acquire the cache lock internally) and then reset stats
	fastTableCache.SetCapacity(n)
	fastTableCache.mu.Lock()
	fastTableCache.hits = 0
	fastTableCache.misses = 0
	fastTableCache.mu.Unlock()
}

// FastTableCacheStats returns hit/miss counts for the cache.
func FastTableCacheStats() (hits, misses int64, size int) {
	if fastTableCache == nil {
		return 0, 0, 0
	}
	h, m := fastTableCache.Stats()
	return h, m, fastTableCache.Size()
}
