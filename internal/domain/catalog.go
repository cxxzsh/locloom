package domain

import "sort"

// TranslationEntry holds a candidate translation and its provenance.
type TranslationEntry struct {
	Text   string `json:"text"`
	Origin string `json:"origin"`
}

// Catalog stores workspace translations per locale keyed by EntryKey.
// Lookup resolves exact (Context, Source, Comment) first, then (Context, Source, ""),
// and finally ("", Source, "") for context-less glossary imports.
type Catalog struct {
	byLocale map[string]map[EntryKey]TranslationEntry
}

// NewCatalog creates an empty translation catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		byLocale: make(map[string]map[EntryKey]TranslationEntry),
	}
}

// Clone returns a deep copy of the catalog safe for concurrent readers/planners.
func (c *Catalog) Clone() *Catalog {
	out := NewCatalog()
	if c == nil {
		return out
	}
	for loc, entries := range c.byLocale {
		if len(entries) == 0 {
			continue
		}
		dst := make(map[EntryKey]TranslationEntry, len(entries))
		for k, v := range entries {
			dst[k] = v
		}
		out.byLocale[loc] = dst
	}
	return out
}

// Set stores or updates a translation for localeID and key.
// Empty text removes the entry rather than storing blank overrides.
func (c *Catalog) Set(localeID string, key EntryKey, text, origin string) {
	if localeID == "" || key.Source == "" {
		return
	}
	if text == "" {
		c.Delete(localeID, key)
		return
	}
	m, ok := c.byLocale[localeID]
	if !ok {
		m = make(map[EntryKey]TranslationEntry)
		c.byLocale[localeID] = m
	}
	m[key] = TranslationEntry{Text: text, Origin: origin}
}

// Delete removes an exact key override for localeID.
func (c *Catalog) Delete(localeID string, key EntryKey) {
	if c == nil {
		return
	}
	if m, ok := c.byLocale[localeID]; ok {
		delete(m, key)
		if len(m) == 0 {
			delete(c.byLocale, localeID)
		}
	}
}

// LookupExact checks exact (Context, Source, Comment) and, when Comment != "",
// falls back to (Context, Source, ""), without crossing into other contexts.
func (c *Catalog) LookupExact(localeID string, key EntryKey) (TranslationEntry, bool) {
	if c == nil {
		return TranslationEntry{}, false
	}
	m, ok := c.byLocale[localeID]
	if !ok {
		return TranslationEntry{}, false
	}
	if v, found := m[key]; found {
		return v, true
	}
	if key.Comment != "" {
		if v, found := m[key.ContextSource()]; found {
			return v, true
		}
	}
	return TranslationEntry{}, false
}

// Lookup resolves a translation for row key, falling back to context-free ("", Source, "")
// glossary entries if no context-specific translation is present.
func (c *Catalog) Lookup(localeID string, key EntryKey) (TranslationEntry, bool) {
	if v, ok := c.LookupExact(localeID, key); ok {
		return v, true
	}
	if key.Context != "" || key.Comment != "" {
		if m, ok := c.byLocale[localeID]; ok {
			if v, found := m[key.SourceOnly()]; found {
				return v, true
			}
		}
	}
	return TranslationEntry{}, false
}

// ClearAll removes all workspace translations across all locales.
func (c *Catalog) ClearAll() {
	if c == nil {
		return
	}
	clear(c.byLocale)
}

// ClearLocale removes all workspace translations for a single locale.
func (c *Catalog) ClearLocale(localeID string) {
	if c == nil {
		return
	}
	delete(c.byLocale, localeID)
}

// ClearByPredicate deletes entries matching fn and returns the number removed.
func (c *Catalog) ClearByPredicate(fn func(localeID string, key EntryKey, entry TranslationEntry) bool) int {
	if c == nil || fn == nil {
		return 0
	}
	removed := 0
	for loc, m := range c.byLocale {
		for k, v := range m {
			if fn(loc, k, v) {
				delete(m, k)
				removed++
			}
		}
		if len(m) == 0 {
			delete(c.byLocale, loc)
		}
	}
	return removed
}

// Locales returns sorted locale IDs currently present in the catalog.
func (c *Catalog) Locales() []string {
	if c == nil || len(c.byLocale) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.byLocale))
	for loc := range c.byLocale {
		out = append(out, loc)
	}
	sort.Strings(out)
	return out
}

// EntriesForLocale returns a shallow copy of entries for localeID.
func (c *Catalog) EntriesForLocale(localeID string) map[EntryKey]TranslationEntry {
	if c == nil {
		return nil
	}
	src := c.byLocale[localeID]
	if len(src) == 0 {
		return nil
	}
	dst := make(map[EntryKey]TranslationEntry, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Count returns total stored entries across all locales.
func (c *Catalog) Count() int {
	if c == nil {
		return 0
	}
	total := 0
	for _, m := range c.byLocale {
		total += len(m)
	}
	return total
}
