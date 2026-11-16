package provider

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
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

// ClearByLocale removes all cached entries for the given targetLocale and returns the number removed.
func (c *Cache) ClearByLocale(targetLocale string) int {
	return c.clearWhere(func(providerID, locale string) bool {
		return locale == targetLocale
	})
}

// ClearByProvider removes all cached entries for the given providerID and returns the number removed.
func (c *Cache) ClearByProvider(providerID string) int {
	return c.clearWhere(func(prov, locale string) bool {
		return prov == providerID
	})
}

func (c *Cache) clearWhere(match func(providerID, targetLocale string) bool) int {
	c.mu.Lock()
	removed := 0
	kept := make([]string, 0, len(c.order))
	for _, k := range c.order {
		parts := strings.SplitN(k, "\x00", 4)
		prov, loc := "", ""
		if len(parts) >= 2 {
			prov, loc = parts[0], parts[1]
		}
		if match(prov, loc) {
			delete(c.data, k)
			removed++
		} else {
			kept = append(kept, k)
		}
	}
	for k := range c.data {
		parts := strings.SplitN(k, "\x00", 4)
		prov, loc := "", ""
		if len(parts) >= 2 {
			prov, loc = parts[0], parts[1]
		}
		if match(prov, loc) {
			delete(c.data, k)
			removed++
		}
	}
	c.order = kept
	if removed > 0 {
		c.dirty = true
	}
	c.mu.Unlock()
	if removed > 0 {
		_ = c.Save()
	}
	return removed
}

// CountsByLocale returns the number of cached entries per target locale.
func (c *Cache) CountsByLocale() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int)
	for k := range c.data {
		if parts := strings.SplitN(k, "\x00", 4); len(parts) >= 2 && parts[1] != "" {
			out[parts[1]]++
		}
	}
	return out
}

// CountsByProvider returns the number of cached entries per provider ID.
func (c *Cache) CountsByProvider() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int)
	for k := range c.data {
		if parts := strings.SplitN(k, "\x00", 4); len(parts) >= 1 && parts[0] != "" {
			out[parts[0]]++
		}
	}
	return out
}

// CacheEntry represents a single cached translation entry for inspection/deletion.
type CacheEntry struct {
	Key          string
	ProviderID   string
	TargetLocale string
	ContextName  string
	Source       string
	Translated   string
}

// Entries returns all cached entries sorted by key.
func (c *Cache) Entries() []CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]CacheEntry, 0, len(c.data))
	for k, v := range c.data {
		parts := strings.SplitN(k, "\x00", 4)
		e := CacheEntry{Key: k, Translated: v}
		if len(parts) >= 1 {
			e.ProviderID = parts[0]
		}
		if len(parts) >= 2 {
			e.TargetLocale = parts[1]
		}
		if len(parts) >= 3 {
			e.ContextName = parts[2]
		}
		if len(parts) >= 4 {
			e.Source = parts[3]
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b CacheEntry) int {
		return strings.Compare(a.Key, b.Key)
	})
	return out
}

// DeleteKey removes a single cached entry by its exact key.
func (c *Cache) DeleteKey(key string) bool {
	return c.DeleteKeys([]string{key}) > 0
}

// DeleteKeys removes all specified keys from the cache and saves to disk.
func (c *Cache) DeleteKeys(keys []string) int {
	if len(keys) == 0 {
		return 0
	}
	delSet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		delSet[k] = struct{}{}
	}
	c.mu.Lock()
	removed := 0
	for k := range delSet {
		if _, ok := c.data[k]; ok {
			delete(c.data, k)
			removed++
		}
	}
	if removed > 0 {
		kept := make([]string, 0, len(c.order))
		for _, k := range c.order {
			if _, ok := delSet[k]; !ok {
				kept = append(kept, k)
			}
		}
		c.order = kept
		c.dirty = true
	}
	c.mu.Unlock()
	if removed > 0 {
		_ = c.Save()
	}
	return removed
}
