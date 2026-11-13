package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/provider"
)

type tmCacheKey struct {
	path        string
	modUnixNano int64
	size        int64
}

type tsTMProvider struct {
	mu     sync.RWMutex
	tmPath string

	loadMu       sync.Mutex
	cacheKey     tmCacheKey
	cachedBundle *format.Bundle
}

// NewTSTMProvider creates the built-in local multi-format translation memory provider.
func NewTSTMProvider() provider.Provider {
	return &tsTMProvider{}
}

func (p *tsTMProvider) ID() string   { return domain.OriginTM }
func (p *tsTMProvider) Name() string { return "本地翻译记忆库 (多格式 TM)" }
func (p *tsTMProvider) Description() string {
	return "从历史翻译目录、.zip 归档或单文件（.ts/.po/.xcstrings/.json/.arb/.xml/.resx/.xliff）提取已完成译文（多语言并发时仅解析一次）"
}

func (p *tsTMProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "Local Translation Memory (Multi-Format TM)"
	}
	return p.Name()
}

func (p *tsTMProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Extract finished translations from historical localization folders, .zip archives, or single files (.ts/.po/.xcstrings/.json/.arb/.xml/.resx/.xliff) (parsed once across concurrent locales)"
	}
	return p.Description()
}
func (p *tsTMProvider) Kind() provider.Kind { return provider.KindLocal }

func (p *tsTMProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:         "tm_path",
			Label:       "参考翻译路径 (目录 / .zip / 单文件)",
			Type:        provider.FieldFile,
			Placeholder: "选择历史翻译目录、.zip 归档或支持的本地化文件",
			Extensions:  format.SupportedExtensions(),
		},
	}
}

func (p *tsTMProvider) LocalizedConfigSchema(lang string) []provider.ConfigField {
	if lang != "en" {
		return p.ConfigSchema()
	}
	return []provider.ConfigField{
		{
			Key:         "tm_path",
			Label:       "Reference Localization Path (Folder / .zip / File)",
			Type:        provider.FieldFile,
			Placeholder: "Select historical localization folder, .zip archive, or supported file",
			Extensions:  format.SupportedExtensions(),
		},
	}
}

func (p *tsTMProvider) Configure(values map[string]string, _ *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tmPath = strings.TrimSpace(values["tm_path"])
	return nil
}

func (p *tsTMProvider) loadBundleCached() (*format.Bundle, error) {
	p.mu.RLock()
	tmPath := p.tmPath
	p.mu.RUnlock()

	if tmPath == "" {
		return nil, fmt.Errorf("translation memory path is not configured")
	}

	info, err := os.Stat(tmPath)
	if err != nil {
		return nil, err
	}

	key := tmCacheKey{
		path:        tmPath,
		modUnixNano: info.ModTime().UnixNano(),
		size:        info.Size(),
	}

	p.loadMu.Lock()
	defer p.loadMu.Unlock()
	if p.cachedBundle != nil && p.cacheKey == key {
		return p.cachedBundle, nil
	}

	bundle, err := format.Load(tmPath, "{id}.ts", locale.DefaultLocales())
	if err != nil {
		return nil, err
	}

	p.cacheKey = key
	p.cachedBundle = bundle
	return bundle, nil
}

func (p *tsTMProvider) Check(_ context.Context) error {
	_, err := p.loadBundleCached()
	return err
}

func matchesTMLocale(loc string, target locale.LocaleSpec) bool {
	if loc == "" {
		return false
	}
	if loc == target.ID || strings.EqualFold(loc, target.ID) {
		return true
	}
	if target.EngineCode != "" && strings.EqualFold(loc, target.EngineCode) {
		return true
	}
	normLoc := strings.ReplaceAll(strings.ToLower(loc), "-", "_")
	normTarget := strings.ReplaceAll(strings.ToLower(target.ID), "-", "_")
	if normLoc == normTarget {
		return true
	}
	known := locale.DefaultLocales()
	canonLoc, _, ok1 := locale.ParseHeaderLocale(loc, known)
	canonTarget, _, ok2 := locale.ParseHeaderLocale(target.ID, known)
	if ok1 && canonLoc == target.ID {
		return true
	}
	if ok2 && loc == canonTarget {
		return true
	}
	return ok1 && ok2 && canonLoc == canonTarget
}

func (p *tsTMProvider) Translate(_ context.Context, req provider.Request) (map[string]string, error) {
	bundle, err := p.loadBundleCached()
	if err != nil {
		return nil, err
	}

	type ctxSrc struct {
		ctx string
		src string
	}
	byIDCtx := make(map[domain.EntryKey]string)
	byCtxSrc := make(map[ctxSrc]string)
	bySrc := make(map[string]string)

	for _, doc := range bundle.Docs {
		if doc == nil {
			continue
		}
		if bundle.Topology == format.TopologyPerLocale && doc.LocaleID != "" && !matchesTMLocale(doc.LocaleID, req.TargetLocale) {
			continue
		}
		for _, e := range doc.Entries {
			entryLoc := e.LocaleID
			if entryLoc == "" {
				entryLoc = doc.LocaleID
			}
			if !matchesTMLocale(entryLoc, req.TargetLocale) {
				continue
			}
			if e.Status != domain.StatusFinished {
				continue
			}
			text := e.Text
			if text == "" && e.Numerus && len(e.PluralForms) > 0 {
				text = domain.JoinNumerus(e.PluralForms)
			}
			if text == "" {
				continue
			}
			if e.Key.ID != "" {
				ik := e.Key.IDContext()
				if _, exists := byIDCtx[ik]; !exists {
					byIDCtx[ik] = text
				}
			}
			if e.Key.Source != "" {
				cs := ctxSrc{ctx: e.Key.Context, src: e.Key.Source}
				if _, exists := byCtxSrc[cs]; !exists {
					byCtxSrc[cs] = text
				}
				if _, exists := bySrc[e.Key.Source]; !exists {
					bySrc[e.Key.Source] = text
				}
			}
		}
	}

	out := make(map[string]string, len(req.Items))
	for _, item := range req.Items {
		if item.Key.ID != "" {
			if v, ok := byIDCtx[item.Key.IDContext()]; ok && v != "" {
				out[item.ID] = v
				continue
			}
		}
		if v, ok := byCtxSrc[ctxSrc{ctx: item.Key.Context, src: item.Key.Source}]; ok && v != "" {
			out[item.ID] = v
			continue
		}
		if v, ok := bySrc[item.Key.Source]; ok && v != "" {
			out[item.ID] = v
		}
	}
	return out, nil
}
