package provider

import (
	"context"
	"sync"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
)

// Kind distinguishes local deterministic sources from remote/AI translation engines.
type Kind string

const (
	KindLocal  Kind = "local"
	KindEngine Kind = "engine"
)

// FieldType defines the UI control rendered for a provider's configuration option.
type FieldType string

const (
	FieldText   FieldType = "text"
	FieldSecret FieldType = "secret"
	FieldBool   FieldType = "bool"
	FieldSelect FieldType = "select"
	FieldFile   FieldType = "file"
	FieldDir    FieldType = "dir"
)

// ConfigField declaratively describes one configuration parameter of a Provider.
type ConfigField struct {
	Key         string
	Label       string
	Type        FieldType
	Default     string
	Placeholder string
	Options     []string
	Extensions  []string
	Help        string
}

// SourceItem represents a single string to be translated.
type SourceItem struct {
	ID           string
	Key          domain.EntryKey
	SourceLocale string
	RefLocale    string
	RefText      string
	Numerus      bool
}

// Request is passed to a Provider for translating a batch of strings into one target locale.
type Request struct {
	TargetLocale locale.LocaleSpec
	EngineCode   string
	Items        []SourceItem
}

// Provider is the pluggable interface for any translation source.
type Provider interface {
	ID() string
	Name() string
	Description() string
	Kind() Kind
	ConfigSchema() []ConfigField
	Configure(values map[string]string, pool *netpool.Pool) error
	Check(ctx context.Context) error
	Translate(ctx context.Context, req Request) (map[string]string, error)
}

// ModelDiscoverer is an optional interface implemented by AI providers capable of
// querying available models from their server.
type ModelDiscoverer interface {
	DiscoverModels(ctx context.Context) ([]string, error)
}

// LocalizedProvider is an optional interface implemented by providers that support
// localized display names, descriptions, and configuration schemas.
type LocalizedProvider interface {
	LocalizedName(lang string) string
	LocalizedDescription(lang string) string
	LocalizedConfigSchema(lang string) []ConfigField
}

// CacheKeyer is an optional interface implemented by providers whose output depends on
// configuration parameters (such as model name, API mode, endpoint, or domain prompt).
// The returned namespace differentiates cache entries without storing secrets.
type CacheKeyer interface {
	CacheNamespace() string
}

// ProviderCacheID returns the cache key prefix for p, scoped by CacheNamespace() when non-empty.
func ProviderCacheID(p Provider) string {
	if p == nil {
		return ""
	}
	id := p.ID()
	if ck, ok := p.(CacheKeyer); ok {
		if ns := ck.CacheNamespace(); ns != "" {
			return id + "@" + ns
		}
	}
	return id
}

// ProviderName returns the localized name of p for lang, falling back to p.Name().
func ProviderName(p Provider, lang string) string {
	if p == nil {
		return ""
	}
	if lp, ok := p.(LocalizedProvider); ok {
		return lp.LocalizedName(lang)
	}
	return p.Name()
}

// ProviderDescription returns the localized description of p for lang, falling back to p.Description().
func ProviderDescription(p Provider, lang string) string {
	if p == nil {
		return ""
	}
	if lp, ok := p.(LocalizedProvider); ok {
		return lp.LocalizedDescription(lang)
	}
	return p.Description()
}

// ProviderSchema returns the localized config schema of p for lang, falling back to p.ConfigSchema().
func ProviderSchema(p Provider, lang string) []ConfigField {
	if p == nil {
		return nil
	}
	if lp, ok := p.(LocalizedProvider); ok {
		return lp.LocalizedConfigSchema(lang)
	}
	return p.ConfigSchema()
}

// Registry is a thread-safe collection of translation providers.
type Registry struct {
	mu    sync.RWMutex
	byID  map[string]Provider
	order []string
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		byID: make(map[string]Provider),
	}
}

var defaultRegistry = NewRegistry()

// DefaultRegistry returns the global default provider registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Register adds or replaces a Provider in the registry, preserving registration order.
func (r *Registry) Register(p Provider) {
	if p == nil || p.ID() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := p.ID()
	if _, exists := r.byID[id]; !exists {
		r.order = append(r.order, id)
	}
	r.byID[id] = p
}

// Get retrieves a registered Provider by ID.
func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	return p, ok
}

// All returns all registered Providers in registration order.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.order))
	for _, id := range r.order {
		if p, ok := r.byID[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Ordered returns all registered Providers ordered by order first,
// followed by any remaining registered providers in registration order.
func (r *Registry) Ordered(order []string) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]bool, len(r.order))
	out := make([]Provider, 0, len(r.order))
	for _, id := range order {
		if seen[id] {
			continue
		}
		if p, ok := r.byID[id]; ok {
			seen[id] = true
			out = append(out, p)
		}
	}
	for _, id := range r.order {
		if seen[id] {
			continue
		}
		if p, ok := r.byID[id]; ok {
			seen[id] = true
			out = append(out, p)
		}
	}
	return out
}

// Register adds a Provider to DefaultRegistry().
func Register(p Provider) {
	defaultRegistry.Register(p)
}

// Get retrieves a Provider by ID from DefaultRegistry().
func Get(id string) (Provider, bool) {
	return defaultRegistry.Get(id)
}

// All returns all Providers from DefaultRegistry() in registration order.
func All() []Provider {
	return defaultRegistry.All()
}

// Ordered returns all Providers from DefaultRegistry() ordered by order.
func Ordered(order []string) []Provider {
	return defaultRegistry.Ordered(order)
}
