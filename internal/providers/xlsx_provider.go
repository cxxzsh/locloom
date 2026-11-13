package providers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/provider"
	"locloom/internal/sheet"
)

type xlsxCacheKey struct {
	filePath    string
	sheetName   string
	skipRef     bool
	modUnixNano int64
	size        int64
}

type xlsxProvider struct {
	mu       sync.RWMutex
	filePath string
	sheet    string
	skipRef  bool

	loadMu   sync.Mutex
	cacheKey xlsxCacheKey
	cachedWB *sheet.WorkbookData
}

// NewXLSXProvider creates the built-in Excel (.xlsx/.xls) local translation provider.
func NewXLSXProvider() provider.Provider {
	return &xlsxProvider{skipRef: true}
}

func (p *xlsxProvider) ID() string   { return domain.OriginXLSX }
func (p *xlsxProvider) Name() string { return "Excel 词条表 (.xlsx)" }
func (p *xlsxProvider) Description() string {
	return "从本地 Excel 多语言词条表中精确匹配并回填译文（多语言并发时仅解析一次）"
}
func (p *xlsxProvider) Kind() provider.Kind { return provider.KindLocal }

func (p *xlsxProvider) ConfigSchema() []provider.ConfigField {
	return []provider.ConfigField{
		{
			Key:         "file_path",
			Label:       "Excel 文件",
			Type:        provider.FieldFile,
			Placeholder: "选择 .xlsx / .xls 多语言词条表",
			Extensions:  []string{"xlsx", "xls"},
		},
		{
			Key:         "sheet",
			Label:       "工作表名",
			Type:        provider.FieldText,
			Placeholder: "留空则自动读取第一个工作表",
		},
		{
			Key:     "skip_ref",
			Label:   "跳过参考语言列",
			Type:    provider.FieldBool,
			Default: "true",
			Help:    "开启后不把参考语言（如中文）当作译文写入目标列",
		},
	}
}

func (p *xlsxProvider) Configure(values map[string]string, _ *netpool.Pool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filePath = strings.TrimSpace(values["file_path"])
	p.sheet = strings.TrimSpace(values["sheet"])
	p.skipRef = values["skip_ref"] != "false"
	return nil
}

func (p *xlsxProvider) loadWorkbookCached() (*sheet.WorkbookData, error) {
	p.mu.RLock()
	fp, sh, skipRef := p.filePath, p.sheet, p.skipRef
	p.mu.RUnlock()

	if fp == "" {
		return nil, fmt.Errorf("未配置 Excel 文件路径")
	}
	info, err := os.Stat(fp)
	if err != nil {
		return nil, err
	}

	key := xlsxCacheKey{
		filePath:    fp,
		sheetName:   sh,
		skipRef:     skipRef,
		modUnixNano: info.ModTime().UnixNano(),
		size:        info.Size(),
	}

	p.loadMu.Lock()
	defer p.loadMu.Unlock()
	if p.cachedWB != nil && p.cacheKey == key {
		return p.cachedWB, nil
	}

	wb, err := sheet.LoadExcel(fp, sh, locale.DefaultLocales(), skipRef)
	if err != nil {
		return nil, err
	}
	p.cacheKey = key
	p.cachedWB = wb
	return wb, nil
}

func (p *xlsxProvider) Check(_ context.Context) error {
	_, err := p.loadWorkbookCached()
	return err
}

func (p *xlsxProvider) Translate(_ context.Context, req provider.Request) (map[string]string, error) {
	wb, err := p.loadWorkbookCached()
	if err != nil {
		return nil, err
	}

	locCandidates := []string{req.TargetLocale.ID}
	if canonID, _, ok := locale.ParseHeaderLocale(req.TargetLocale.ID, locale.DefaultLocales()); ok && canonID != "" && canonID != req.TargetLocale.ID {
		locCandidates = append(locCandidates, canonID)
	}
	if req.TargetLocale.EngineCode != "" && req.TargetLocale.EngineCode != req.TargetLocale.ID {
		locCandidates = append(locCandidates, req.TargetLocale.EngineCode)
	}
	exactMap := make(map[domain.EntryKey]string, len(wb.Rows))
	ctxSrcMap := make(map[domain.EntryKey]string, len(wb.Rows))
	ctxSrcExact := make(map[domain.EntryKey]bool, len(wb.Rows))
	srcOnlyMap := make(map[string]string, len(wb.Rows))

	for _, row := range wb.Rows {
		var trans string
		for _, cand := range locCandidates {
			if v := row.Translations[cand]; v != "" {
				trans = v
				break
			}
		}
		if trans == "" {
			continue
		}
		k := domain.EntryKey{
			Context: row.Context,
			Source:  row.Source,
			Comment: row.Comment,
		}
		if _, exists := exactMap[k]; !exists {
			exactMap[k] = trans
		}
		csKey := k.ContextSource()
		if row.Comment == "" {
			if !ctxSrcExact[csKey] {
				ctxSrcMap[csKey] = trans
				ctxSrcExact[csKey] = true
			}
		} else if _, exists := ctxSrcMap[csKey]; !exists {
			ctxSrcMap[csKey] = trans
		}
		if row.Context == "" {
			if _, exists := srcOnlyMap[row.Source]; !exists {
				srcOnlyMap[row.Source] = trans
			}
		}
	}

	out := make(map[string]string, len(req.Items))
	for _, item := range req.Items {
		if v, ok := exactMap[item.Key]; ok && v != "" {
			out[item.ID] = v
			continue
		}
		if item.Key.Comment != "" {
			if v, ok := ctxSrcMap[item.Key.ContextSource()]; ok && v != "" {
				out[item.ID] = v
				continue
			}
		}
		if !wb.HasContextColumn {
			if v, ok := srcOnlyMap[item.Key.Source]; ok && v != "" {
				out[item.ID] = v
				continue
			}
		}
	}
	return out, nil
}
