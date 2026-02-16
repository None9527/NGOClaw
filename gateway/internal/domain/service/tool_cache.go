package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// ToolResultCache caches tool execution results for short-term deduplication.
// When the agent calls the same tool with identical arguments within the TTL,
// the cached result is returned without re-executing the tool.
//
// This prevents wasted work when the LLM retries or loops on the same tool call.
type ToolResultCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
	ttl     time.Duration
	maxSize int
}

type cacheEntry struct {
	output    string
	success   bool
	createdAt time.Time
}

// NewToolResultCache creates a cache with the given TTL and max entries.
func NewToolResultCache(ttl time.Duration, maxSize int) *ToolResultCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if maxSize <= 0 {
		maxSize = 100
	}
	return &ToolResultCache{
		entries: make(map[string]*cacheEntry, maxSize),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// Get returns a cached result if present and not expired.
func (c *ToolResultCache) Get(toolName string, args map[string]interface{}) (output string, success bool, hit bool) {
	key := c.makeKey(toolName, args)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return "", false, false
	}

	if time.Since(entry.createdAt) > c.ttl {
		// Expired — evict
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return "", false, false
	}

	return entry.output, entry.success, true
}

// Put stores a tool result in the cache.
func (c *ToolResultCache) Put(toolName string, args map[string]interface{}, output string, success bool) {
	key := c.makeKey(toolName, args)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = &cacheEntry{
		output:    output,
		success:   success,
		createdAt: time.Now(),
	}
}

// Clear empties the cache.
func (c *ToolResultCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry, c.maxSize)
}

// Size returns the number of entries in the cache.
func (c *ToolResultCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// makeKey produces a deterministic hash from tool name + arguments.
func (c *ToolResultCache) makeKey(toolName string, args map[string]interface{}) string {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte{0}) // separator
	if args != nil {
		// JSON-encode for deterministic ordering? Not guaranteed by Go.
		// Use sorted keys instead for stability.
		argsBytes, _ := json.Marshal(args)
		h.Write(argsBytes)
	}
	return hex.EncodeToString(h.Sum(nil))[:16] // 16-char prefix is enough
}

// evictOldest removes the oldest entry from the cache.
func (c *ToolResultCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, v := range c.entries {
		if oldestKey == "" || v.createdAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
