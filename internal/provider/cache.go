package provider

import (
	"encoding/json"
	"os"
	"slices"
	"sync"

	"locloom/internal/platform"
)

const (
	cacheFileName   = "translation-cache.json"
	maxCacheEntries = 50000
)

// Cache provides thread-safe in-memory and persistent disk caching for engine translations.
type Cache struct {
	mu    sync.RWMutex
	dirty bool
	path  string
	data  map[string]string // key: cacheProviderID + "\x00" + targetLocale + "\x00" + contextName + "\x00" + source
	order []string
}

var (
	globalCache     *Cache
	globalCacheOnce sync.Once
)

// NewCache creates a Cache backed by path and loads any existing entries from disk.
func NewCache(path string) *Cache {
	c := &Cache{
		path: path,
		data: make(map[string]string),
	}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var loaded map[string]string
			if json.Unmarshal(raw, &loaded) == nil && loaded != nil {
				c.data = loaded
				c.order = make([]string, 0, len(loaded))
				for k := range loaded {
					c.order = append(c.order, k)
				}
				c.evictExcessLocked()
			}
		}
	}
	return c
}

func (c *Cache) evictExcessLocked() {
	if len(c.data) <= maxCacheEntries {
		return
	}
	head := 0
	for len(c.data) > maxCacheEntries && head < len(c.order) {
		k := c.order[head]
		head++
		delete(c.data, k)
	}
	if head > 0 {
		if head >= len(c.order) {
			c.order = nil
		} else {
			c.order = slices.Clone(c.order[head:])
		}
	}
	for k := range c.data {
		if len(c.data) <= maxCacheEntries {
			break
		}
		delete(c.data, k)
	}
	c.dirty = true
}

// GlobalCache returns the singleton translation cache persisted at platform.ResolveCachePath("translation-cache.json").
func GlobalCache() *Cache {
	globalCacheOnce.Do(func() {
		globalCache = NewCache(platform.ResolveCachePath(cacheFileName))
	})
	return globalCache
}

func cacheKey(providerID, targetLocale, contextName, source string) string {
	return providerID + "\x00" + targetLocale + "\x00" + contextName + "\x00" + source
}

// Get looks up a cached translation, falling back to an empty contextName if context-specific lookup misses.
func (c *Cache) Get(providerID, targetLocale, source, contextName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if v, ok := c.data[cacheKey(providerID, targetLocale, contextName, source)]; ok && v != "" {
		return v, true
	}
	if contextName != "" {
		if v, ok := c.data[cacheKey(providerID, targetLocale, "", source)]; ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// Put stores a translation in memory and marks the cache dirty.
func (c *Cache) Put(providerID, targetLocale, source, contextName, translated string) {
	if translated == "" {
		return
	}
	k := cacheKey(providerID, targetLocale, contextName, source)
	c.mu.Lock()
	if _, exists := c.data[k]; !exists {
		c.order = append(c.order, k)
	}
	c.data[k] = translated
	c.evictExcessLocked()
	c.dirty = true
	c.mu.Unlock()
}

// Save flushes cached translations atomically to disk if modified.
func (c *Cache) Save() error {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return nil
	}
	snapshot := make(map[string]string, len(c.data))
	for k, v := range c.data {
		snapshot[k] = v
	}
	c.dirty = false
	path := c.path
	c.mu.Unlock()

	if path == "" {
		return nil
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return err
	}
	if err := platform.AtomicWriteFile(path, raw, 0o600); err != nil {
		c.mu.Lock()
		c.dirty = true
		c.mu.Unlock()
		return err
	}
	return nil
}

// Clear empties all cached translations in memory and removes the cache file on disk.
func (c *Cache) Clear() error {
	c.mu.Lock()
	c.data = make(map[string]string)
	c.order = nil
	c.dirty = false
	path := c.path
	c.mu.Unlock()

	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}
