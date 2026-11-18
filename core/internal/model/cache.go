package model

import (
	"container/list"
	"sync"
)

// cacheSize bounds a Cache. A view needs about five entries per branch.
const cacheSize = 8192

// Cache keeps answers across builds of one repository. Every key names all
// the inputs of its answer as commit hashes (and, for unpushed counts, a
// fingerprint of the exact set of remote-tracking refs), so a cached answer
// is the one git would give again. The least recently used entries go first.
// It is safe for concurrent use; a nil *Cache keeps nothing.
type Cache struct {
	mu    sync.Mutex
	max   int
	items map[string]*list.Element
	order *list.List // front: most recently used
}

type cacheItem struct {
	key string
	val any
}

// NewCache returns an empty cache.
func NewCache() *Cache { return newCacheSize(cacheSize) }

func newCacheSize(max int) *Cache {
	return &Cache{max: max, items: map[string]*list.Element{}, order: list.New()}
}

// get returns the value stored under key. Values are never changed after
// put, so callers must copy slices before handing them out.
func (c *Cache) get(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*cacheItem).val, true
}

func (c *Cache) put(key string, val any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		e.Value.(*cacheItem).val = val
		c.order.MoveToFront(e)
		return
	}
	c.items[key] = c.order.PushFront(&cacheItem{key, val})
	for c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*cacheItem).key)
	}
}
