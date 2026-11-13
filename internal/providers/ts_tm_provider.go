package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/platform"
	"locloom/internal/provider"
	"locloom/internal/tsxml"
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
	cachedBundle *tsxml.Bundle
}

// NewTSTMProvider creates the built-in local Qt .ts / .zip translation memory provider.
func NewTSTMProvider() provider.Provider {
	return &tsTMProvider{}
}

func (p *tsTMProvider) ID() string   { return domain.OriginTM }
func (p *tsTMProvider) Name() string { return "本地 .ts 翻译记忆库 (TM)" }
func (p *tsTMProvider) Description() string {
	return "从历史 .ts 目录、.zip 归档或单个 .ts 文件提取已完成译文（多语言并发时仅解析一次）"
}

func (p *tsTMProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "Local .ts Translation Memory (TM)"
	}
	return p.Name()
}

func (p *tsTMProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "Extract finished translations from historical .ts folders, .zip archives, or single .ts files (parsed once across concurrent locales)"
	}
	return p.Description()
}
func (p *tsTMProvider) Kind() provider.Kind { return provider.KindLocal }

func (p *tsTMProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:         "tm_path",
			Label:       "参考 .ts / .zip 路径",
			Type:        provider.FieldFile,
			Placeholder: "选择历史 .ts 翻译目录、.zip 归档或单个 .ts 文件",
			Extensions:  []string{"zip", "ts"},
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
			Label:       "Reference .ts / .zip Path",
			Type:        provider.FieldFile,
			Placeholder: "Select historical .ts folder, .zip archive, or single .ts file",
			Extensions:  []string{"zip", "ts"},
		},
	}
}

func (p *tsTMProvider) Configure(values map[string]string, _ *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tmPath = strings.TrimSpace(values["tm_path"])
	return nil
}

func (p *tsTMProvider) loadBundleCached() (*tsxml.Bundle, error) {
	p.mu.RLock()
	tmPath := p.tmPath
	p.mu.RUnlock()

	if tmPath == "" {
		return nil, fmt.Errorf("translation memory (.ts/.zip) path is not configured")
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

	var bundle *tsxml.Bundle
	if !info.IsDir() && strings.EqualFold(filepath.Ext(tmPath), ".ts") {
		raw, rerr := platform.ReadFileShared(tmPath)
		if rerr != nil {
			return nil, rerr
		}
		tf, perr := tsxml.ParseBytes(filepath.Base(tmPath), raw)
		if perr != nil {
			return nil, perr
		}
		tf.FilePath = tmPath
		if tf.LocaleID == "" {
			tf.LocaleID = locale.StemLocaleID(filepath.Base(tmPath), "{id}.ts", locale.DefaultLocales())
		}
		bundle = &tsxml.Bundle{
			SourcePath: tmPath,
			Files:      []*tsxml.TSFile{tf},
		}
	} else {
		var lerr error
		bundle, lerr = tsxml.LoadBundle(tmPath, "{id}.ts", locale.DefaultLocales())
		if lerr != nil {
			return nil, lerr
		}
	}

	p.cacheKey = key
	p.cachedBundle = bundle
	return bundle, nil
}

func (p *tsTMProvider) Check(_ context.Context) error {
	_, err := p.loadBundleCached()
	return err
}

func (p *tsTMProvider) Translate(_ context.Context, req provider.Request) (map[string]string, error) {
	bundle, err := p.loadBundleCached()
	if err != nil {
		return nil, err
	}

	targetID := req.TargetLocale.ID
	var matchedFile *tsxml.TSFile
	for _, tf := range bundle.Files {
		if tf.LocaleID == targetID ||
			strings.EqualFold(tf.Language, targetID) ||
			strings.EqualFold(tf.FileName, targetID+".ts") {
			matchedFile = tf
			break
		}
	}
	if matchedFile == nil {
		return map[string]string{}, nil
	}

	type ctxSrc struct {
		ctx string
		src string
	}
	byCtxSrc := make(map[ctxSrc]string)
	bySrc := make(map[string]string)

	for _, ctxBlock := range matchedFile.Contexts {
		for _, msg := range ctxBlock.Messages {
			if msg.Status != domain.StatusFinished || msg.Translation == "" {
				continue
			}
			cs := ctxSrc{ctx: ctxBlock.Name, src: msg.Source}
			if _, exists := byCtxSrc[cs]; !exists {
				byCtxSrc[cs] = msg.Translation
			}
			if _, exists := bySrc[msg.Source]; !exists {
				bySrc[msg.Source] = msg.Translation
			}
		}
	}

	out := make(map[string]string, len(req.Items))
	for _, item := range req.Items {
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
