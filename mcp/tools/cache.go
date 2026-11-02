package tools

import (
	"sync"
	"time"
)

// ttlCache is a tiny per-process cache for responses that are stable for the
// life of a session but needed by many tool calls — today only
// GET /api/custom/me, whose scan_period_seconds every stack listing uses to
// resolve a stack's inherited sync interval.
//
// Keyed by API key, never shared across keys: the cached value includes the
// caller's own role and capabilities, so a shared entry would report one key's
// permissions to another.
type ttlCache struct {
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]ttlEntry
}

type ttlEntry struct {
	value    map[string]interface{}
	storedAt time.Time
	expires  time.Time
}

func newTTLCache(ttl time.Duration) *ttlCache {
	return &ttlCache{ttl: ttl, entries: make(map[string]ttlEntry)}
}

func (c *ttlCache) get(key string) (map[string]interface{}, bool) {
	value, _, ok := c.getWithAge(key)
	return value, ok
}

// getWithAge also reports how long ago the entry was stored, so callers that
// surface freshness (whoami) can say exactly how stale a cache hit is.
func (c *ttlCache) getWithAge(key string) (map[string]interface{}, time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expires) {
		return nil, 0, false
	}
	return entry.value, time.Since(entry.storedAt), true
}

func (c *ttlCache) put(key string, value map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Opportunistic sweep: the key set is bounded by the number of distinct
	// API keys that have connected, but a long-lived process should not hold
	// an entry for a credential that stopped calling.
	now := time.Now()
	for k, entry := range c.entries {
		if now.After(entry.expires) {
			delete(c.entries, k)
		}
	}
	c.entries[key] = ttlEntry{value: value, storedAt: now, expires: now.Add(c.ttl)}
}
