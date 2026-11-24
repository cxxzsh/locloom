package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
)

type stubProvider struct {
	id        string
	kind      Kind
	calls     atomic.Int32
	fail      bool
	translate func(ctx context.Context, req Request) (map[string]string, error)
}

func (s *stubProvider) ID() string                  { return s.id }
func (s *stubProvider) Name() string                { return s.id }
func (s *stubProvider) Description() string         { return s.id }
func (s *stubProvider) Kind() Kind                  { return s.kind }
func (s *stubProvider) ConfigSchema() []ConfigField { return nil }
func (s *stubProvider) Configure(map[string]string, *netpool.Pool) error {
	return nil
}
func (s *stubProvider) Check(context.Context) error { return nil }
func (s *stubProvider) Translate(ctx context.Context, req Request) (map[string]string, error) {
	s.calls.Add(1)
	if s.fail {
		return nil, errors.New("boom")
	}
	if s.translate != nil {
		return s.translate(ctx, req)
	}
	return map[string]string{}, nil
}

func TestRegistryOrdered(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubProvider{id: "a", kind: KindLocal})
	reg.Register(&stubProvider{id: "b", kind: KindEngine})
	reg.Register(&stubProvider{id: "c", kind: KindEngine})

	if _, ok := reg.Get("b"); !ok {
		t.Fatal("expected provider b")
	}
	if len(reg.All()) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(reg.All()))
	}

	ordered := reg.Ordered([]string{"c", "unknown", "a", "c"})
	if len(ordered) != 3 {
		t.Fatalf("expected 3 ordered providers, got %d", len(ordered))
	}
	if ordered[0].ID() != "c" || ordered[1].ID() != "a" || ordered[2].ID() != "b" {
		t.Fatalf("unexpected order: %s, %s, %s", ordered[0].ID(), ordered[1].ID(), ordered[2].ID())
	}
}

func TestMaskAndRestorePlaceholders(t *testing.T) {
	src := "  Save %1 of %L2 (%n files)\nNext:\t "
	m := MaskPlaceholders(src)
	if m.LeadingWS != "  " || m.TrailingWS != " " {
		t.Fatalf("unexpected edge whitespace: lead=%q trail=%q", m.LeadingWS, m.TrailingWS)
	}
	if len(m.Tokens) != 5 {
		t.Fatalf("expected 5 tokens, got %d: %v", len(m.Tokens), m.Tokens)
	}
	if m.Masked != "Save [[0]] of [[1]] ([[2]] files)[[3]]Next:[[4]]" {
		t.Fatalf("unexpected masked string: %q", m.Masked)
	}

	// Simulate MT engine producing spaced, full-width, and stray edge spaces
	mtOut := " 保存 [[ 0 ]] / 【【1】】 (［［2］］ 个文件)[[3]]下一步：[[4]] "
	restored := RestorePlaceholders(mtOut, m)
	want := "  保存 %1 / %L2 (%n 个文件)\n下一步：\t "
	if restored != want {
		t.Fatalf("restored=%q, want=%q", restored, want)
	}

	// Trailing colon + space preservation
	m2 := MaskPlaceholders("Name: ")
	if got := RestorePlaceholders("名称：", m2); got != "名称： " {
		t.Fatalf("expected trailing space preserved, got %q", got)
	}

	// Single-bracket fallback
	m3 := MaskPlaceholders("Open %1")
	if got := RestorePlaceholders("打开 [0]", m3); got != "打开 %1" {
		t.Fatalf("expected single-bracket fallback, got %q", got)
	}
}

func TestMaskMultiFormatPlaceholders(t *testing.T) {
	src := `Hello {{user_name}}, file %1$s has {count} items (%@) <x id="1"/>`
	m := MaskPlaceholders(src)
	if len(m.Tokens) != 5 {
		t.Fatalf("expected 5 multi-format tokens, got %d: %v", len(m.Tokens), m.Tokens)
	}
	restored := RestorePlaceholders(`こんにちは [[0]]、ファイル [[1]] には [[2]] 個 ([[3]]) [[4]]`, m)
	want := `こんにちは {{user_name}}、ファイル %1$s には {count} 個 (%@) <x id="1"/>`
	if restored != want {
		t.Fatalf("restored = %q, want %q", restored, want)
	}
}

func TestCachePersistenceAndFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "translation-cache.json")
	c := NewCache(path)

	c.Put("google_web", "ja", "Save", "MainWindow", "保存")
	c.Put("google_web", "ja", "Cancel", "", "キャンセル")

	if v, ok := c.Get("google_web", "ja", "Save", "MainWindow"); !ok || v != "保存" {
		t.Fatalf("exact lookup failed: ok=%v v=%q", ok, v)
	}
	if v, ok := c.Get("google_web", "ja", "Cancel", "SettingsDialog"); !ok || v != "キャンセル" {
		t.Fatalf("empty-context fallback failed: ok=%v v=%q", ok, v)
	}
	if _, ok := c.Get("google_web", "ja", "Save", "OtherContext"); ok {
		t.Fatal("should not match different context when only MainWindow is cached")
	}
	if c.Len() != 2 {
		t.Fatalf("expected Len 2, got %d", c.Len())
	}

	if err := c.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	c2 := NewCache(path)
	if v, ok := c2.Get("google_web", "ja", "Save", "MainWindow"); !ok || v != "保存" {
		t.Fatalf("reloaded cache miss: ok=%v v=%q", ok, v)
	}

	if err := c2.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	if c2.Len() != 0 {
		t.Fatalf("expected Len 0 after Clear, got %d", c2.Len())
	}
}

func TestRunConcurrentWaterfallCacheAndNumerus(t *testing.T) {
	reg := NewRegistry()
	localProv := &stubProvider{
		id:   "local_tm",
		kind: KindLocal,
		translate: func(_ context.Context, req Request) (map[string]string, error) {
			out := make(map[string]string)
			for _, it := range req.Items {
				if it.Key.Source == "OK" {
					out[it.ID] = "确定"
				}
			}
			return out, nil
		},
	}
	engineProv := &stubProvider{
		id:   "mock_engine",
		kind: KindEngine,
		translate: func(_ context.Context, req Request) (map[string]string, error) {
			out := make(map[string]string)
			for _, it := range req.Items {
				out[it.ID] = "T:" + it.Key.Source
			}
			return out, nil
		},
	}
	reg.Register(localProv)
	reg.Register(engineProv)

	cache := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	loc := locale.LocaleSpec{ID: "zh_CN", Name: "简体中文", Enabled: true}
	tasks := map[string][]SourceItem{
		"zh_CN": {
			{ID: "1", Key: domain.EntryKey{Context: "MainWindow", Source: "OK"}, RefText: "OK"},
			{ID: "2", Key: domain.EntryKey{Context: "MainWindow", Source: "File %1 "}, RefText: "File %1 "},
			{
				ID:      "3",
				Key:     domain.EntryKey{Context: "MainWindow", Source: "%n item(s)"},
				RefText: domain.JoinNumerus([]string{"%n item", "%n items"}),
				Numerus: true,
			},
		},
	}

	var updateCount atomic.Int32
	res := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"local_tm", "mock_engine"},
		Locales:     []locale.LocaleSpec{loc},
		Tasks:       tasks,
		Concurrency: 2,
		OnUpdate: func(statuses map[string]LocaleStatus, newCells []TranslatedCell, newEvents []LogEvent) {
			updateCount.Add(1)
		},
	})

	if updateCount.Load() == 0 {
		t.Fatal("expected OnUpdate callbacks")
	}
	st := res.Statuses["zh_CN"]
	if st.Phase != PhaseDone || st.Completed != 3 || st.CacheHits != 0 {
		t.Fatalf("unexpected status: %+v", st)
	}

	cellBySrc := make(map[string]TranslatedCell)
	for _, c := range res.Cells {
		cellBySrc[c.Key.Source] = c
	}
	if cellBySrc["OK"].Text != "确定" || cellBySrc["OK"].ProviderID != "local_tm" {
		t.Fatalf("unexpected OK cell: %+v", cellBySrc["OK"])
	}
	if cellBySrc["File %1 "].Text != "T:File %1 " || cellBySrc["File %1 "].ProviderID != "mock_engine" {
		t.Fatalf("unexpected File cell: %+v", cellBySrc["File %1 "])
	}
	wantPlural := domain.JoinNumerus([]string{"T:%n item", "T:%n items"})
	if cellBySrc["%n item(s)"].Text != wantPlural {
		t.Fatalf("unexpected plural cell: got %q, want %q", cellBySrc["%n item(s)"].Text, wantPlural)
	}

	// Second run should hit cache for engine items and not call mock_engine
	callsBefore := engineProv.calls.Load()
	res2 := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"local_tm", "mock_engine"},
		Locales:     []locale.LocaleSpec{loc},
		Tasks:       tasks,
		Concurrency: 2,
	})
	if engineProv.calls.Load() != callsBefore {
		t.Fatal("expected mock_engine not to be called on cache hit")
	}
	if res2.Statuses["zh_CN"].CacheHits != 2 {
		t.Fatalf("expected 2 cache hits, got %d", res2.Statuses["zh_CN"].CacheHits)
	}

	// BypassCache forces engine call
	res3 := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"local_tm", "mock_engine"},
		Locales:     []locale.LocaleSpec{loc},
		Tasks:       tasks,
		Concurrency: 2,
		BypassCache: true,
	})
	if engineProv.calls.Load() != callsBefore+1 {
		t.Fatal("expected mock_engine to be called when BypassCache=true")
	}
	if res3.Statuses["zh_CN"].CacheHits != 0 {
		t.Fatalf("expected 0 cache hits with BypassCache, got %d", res3.Statuses["zh_CN"].CacheHits)
	}
}

type localizedStubProvider struct {
	stubProvider
}

func (l *localizedStubProvider) LocalizedName(lang string) string {
	if lang == "en" {
		return "English Name"
	}
	return "中文名称"
}

func (l *localizedStubProvider) LocalizedDescription(lang string) string {
	if lang == "en" {
		return "English Description"
	}
	return "中文描述"
}

func (l *localizedStubProvider) LocalizedConfigSchema(lang string) []ConfigField {
	label := "中文标签"
	if lang == "en" {
		label = "English Label"
	}
	return []ConfigField{{Key: "k", Label: label, Type: FieldText}}
}

func TestLocalizedProviderHelpers(t *testing.T) {
	if got := ProviderName(nil, "en"); got != "" {
		t.Fatalf("ProviderName(nil) = %q, want empty", got)
	}
	if got := ProviderDescription(nil, "en"); got != "" {
		t.Fatalf("ProviderDescription(nil) = %q, want empty", got)
	}
	if got := ProviderSchema(nil, "en"); got != nil {
		t.Fatalf("ProviderSchema(nil) = %v, want nil", got)
	}

	plain := &stubProvider{id: "plain"}
	if got := ProviderName(plain, "en"); got != "plain" {
		t.Fatalf("plain ProviderName = %q, want plain", got)
	}
	if got := ProviderDescription(plain, "en"); got != "plain" {
		t.Fatalf("plain ProviderDescription = %q, want plain", got)
	}
	if got := ProviderSchema(plain, "en"); got != nil {
		t.Fatalf("plain ProviderSchema = %v, want nil", got)
	}

	loc := &localizedStubProvider{stubProvider: stubProvider{id: "loc"}}
	if got := ProviderName(loc, "en"); got != "English Name" {
		t.Fatalf("loc ProviderName(en) = %q", got)
	}
	if got := ProviderName(loc, "zh"); got != "中文名称" {
		t.Fatalf("loc ProviderName(zh) = %q", got)
	}
	if got := ProviderDescription(loc, "en"); got != "English Description" {
		t.Fatalf("loc ProviderDescription(en) = %q", got)
	}
	if got := ProviderSchema(loc, "en"); len(got) != 1 || got[0].Label != "English Label" {
		t.Fatalf("loc ProviderSchema(en) = %+v", got)
	}
	if got := ProviderSchema(loc, "zh"); len(got) != 1 || got[0].Label != "中文标签" {
		t.Fatalf("loc ProviderSchema(zh) = %+v", got)
	}
}

func TestRestorePlaceholdersSinglePassNoCascade(t *testing.T) {
	// NBSP (\u00a0) at edges must be preserved and restored cleanly.
	nbspSrc := "\u00a0Wait %1 then %s\u00a0"
	m1 := MaskPlaceholders(nbspSrc)
	if m1.LeadingWS != "\u00a0" || m1.TrailingWS != "\u00a0" {
		t.Fatalf("expected NBSP edge spaces preserved, got lead=%q trail=%q", m1.LeadingWS, m1.TrailingWS)
	}
	restored1 := RestorePlaceholders(" 请等待 [[0]] 再看 [[1]] ", m1)
	if restored1 != "\u00a0请等待 %1 再看 %s\u00a0" {
		t.Fatalf("unexpected NBSP restore: %q", restored1)
	}

	// Single-pass restoration must not cascade when token [0] contains literal "[1]".
	cascadeMask := MaskedText{
		Masked:     "A [[0]] B [[1]]",
		Tokens:     []string{"[1]", "%s"},
		LeadingWS:  "\u00a0",
		TrailingWS: "\u00a0",
	}
	restoredCascade := RestorePlaceholders(" A [[0]] B [[1]] ", cascadeMask)
	if restoredCascade != "\u00a0A [1] B %s\u00a0" {
		t.Fatalf("cascade corruption occurred: got %q, want %q", restoredCascade, "\u00a0A [1] B %s\u00a0")
	}

	// Literal single-bracket [0] in source must stay literal even in single-bracket fallback pass.
	litMask := MaskPlaceholders("Array [0] %1 %2")
	restoredLit := RestorePlaceholders("数组 [0] %1 [1]", litMask)
	if restoredLit != "数组 [0] %1 %2" {
		t.Fatalf("literal [0] or fallback restore failed: %q", restoredLit)
	}
}

type namespacedStubProvider struct {
	stubProvider
	ns string
}

func (n *namespacedStubProvider) CacheNamespace() string { return n.ns }

func TestCacheNamespaceIsolationAndPermissions(t *testing.T) {
	baseDir := t.TempDir()
	cacheDir := filepath.Join(baseDir, "locloom")
	cachePath := filepath.Join(cacheDir, "translation-cache.json")
	cache := NewCache(cachePath)

	nsProv := &namespacedStubProvider{
		stubProvider: stubProvider{id: "test_ns", kind: KindEngine},
		ns:           "gpt-4o|https://api.openai.com/v1|promptA",
	}
	nsProv.translate = func(_ context.Context, req Request) (map[string]string, error) {
		out := make(map[string]string, len(req.Items))
		for _, it := range req.Items {
			out[it.ID] = nsProv.ns + ":" + it.RefText
		}
		return out, nil
	}

	reg := NewRegistry()
	reg.Register(nsProv)

	item := SourceItem{
		ID:        domain.EntryKey{Source: "Open"}.Canonical(),
		Key:       domain.EntryKey{Source: "Open"},
		RefLocale: "en",
		RefText:   "Open",
	}
	locs := []locale.LocaleSpec{{ID: "ja", Enabled: true}}
	tasks := map[string][]SourceItem{"ja": {item}}

	res1 := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"test_ns"},
		Locales:     locs,
		Tasks:       tasks,
		Concurrency: 1,
	})
	if len(res1.Cells) != 1 || nsProv.calls.Load() != 1 {
		t.Fatalf("expected 1 cell & 1 call, got cells=%d calls=%d", len(res1.Cells), nsProv.calls.Load())
	}

	// Switching CacheNamespace must miss old cache and compute a fresh translation.
	nsProv.ns = "gpt-4o-mini|https://api.openai.com/v1|promptB"
	res2 := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"test_ns"},
		Locales:     locs,
		Tasks:       tasks,
		Concurrency: 1,
	})
	if len(res2.Cells) != 1 || nsProv.calls.Load() != 2 || !containsSubstring(res2.Cells[0].Text, "gpt-4o-mini") {
		t.Fatalf("expected cache miss on namespace change, got calls=%d cells=%+v", nsProv.calls.Load(), res2.Cells)
	}

	// Same new namespace hits cache without incrementing call count.
	res3 := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"test_ns"},
		Locales:     locs,
		Tasks:       tasks,
		Concurrency: 1,
	})
	if nsProv.calls.Load() != 2 || res3.Statuses["ja"].CacheHits != 1 {
		t.Fatalf("expected cache hit on matching namespace, got calls=%d statuses=%+v", nsProv.calls.Load(), res3.Statuses)
	}

	if err := cache.Save(); err != nil {
		t.Fatalf("cache.Save() failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		dirInfo, err := os.Stat(cacheDir)
		if err != nil {
			t.Fatalf("stat cache dir failed: %v", err)
		}
		if dirInfo.Mode().Perm()&0o077 != 0 {
			t.Fatalf("expected 0700 cache dir perms, got %o", dirInfo.Mode().Perm())
		}
		fileInfo, err := os.Stat(cachePath)
		if err != nil {
			t.Fatalf("stat cache file failed: %v", err)
		}
		if fileInfo.Mode().Perm()&0o077 != 0 {
			t.Fatalf("expected 0600 cache file perms, got %o", fileInfo.Mode().Perm())
		}
	}

	// Verify FIFO eviction keeps cache bounded at maxCacheEntries.
	bounded := NewCache("")
	for i := 0; i < maxCacheEntries+3; i++ {
		bounded.Put("p", "ja", fmt.Sprintf("src-%d", i), "", fmt.Sprintf("tr-%d", i))
	}
	if len(bounded.data) != maxCacheEntries {
		t.Fatalf("expected cache bounded at %d entries, got %d", maxCacheEntries, len(bounded.data))
	}
	if _, ok := bounded.Get("p", "ja", "src-0", ""); ok {
		t.Fatal("expected oldest entry src-0 to be evicted")
	}
	if _, ok := bounded.Get("p", "ja", fmt.Sprintf("src-%d", maxCacheEntries+2), ""); !ok {
		t.Fatal("expected newest entry to remain in cache")
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

func TestRunConcurrentPreservesPartialResultsAndPinnedSourceCache(t *testing.T) {
	reg := NewRegistry()
	primary := &stubProvider{
		id:   "primary",
		kind: KindEngine,
		translate: func(_ context.Context, req Request) (map[string]string, error) {
			// Succeed on item 1, fail mid-batch with rate limit error.
			return map[string]string{req.Items[0].ID: "P1"}, errors.New("HTTP 429 rate limited")
		},
	}
	fallback := &stubProvider{
		id:   "fallback",
		kind: KindEngine,
		translate: func(_ context.Context, req Request) (map[string]string, error) {
			out := make(map[string]string, len(req.Items))
			for _, it := range req.Items {
				out[it.ID] = "FB:" + it.SrcText
			}
			return out, nil
		},
	}
	reg.Register(primary)
	reg.Register(fallback)

	cache := NewCache("")
	loc := locale.LocaleSpec{ID: "ja_JP", Enabled: true}
	tasks := map[string][]SourceItem{
		"ja_JP": {
			{ID: "1", Key: domain.EntryKey{Context: "M", Source: "One"}, SrcText: "One"},
			{ID: "2", Key: domain.EntryKey{Context: "M", Source: "Two"}, SrcText: "Two"},
		},
	}

	res := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"primary", "fallback"},
		Locales:     []locale.LocaleSpec{loc},
		Tasks:       tasks,
		Concurrency: 1,
	})
	if len(res.Cells) != 2 {
		t.Fatalf("expected 2 cells (1 harvested partial + 1 fallback), got %+v", res.Cells)
	}
	bySrc := map[string]TranslatedCell{}
	for _, c := range res.Cells {
		bySrc[c.Key.Source] = c
	}
	if bySrc["One"].Text != "P1" || bySrc["One"].ProviderID != "primary" {
		t.Fatalf("partial result from primary was lost: %+v", bySrc["One"])
	}
	if bySrc["Two"].Text != "FB:Two" || bySrc["Two"].ProviderID != "fallback" {
		t.Fatalf("expected remaining item to fall back cleanly: %+v", bySrc["Two"])
	}

	// Pinned SourceLocale cache isolation: translating from zh_CN must not return English-source cached entry.
	nsProv := &namespacedStubProvider{
		stubProvider: stubProvider{id: "openai_compat", kind: KindEngine},
		ns:           "gpt-4o|url",
	}
	nsProv.translate = func(_ context.Context, req Request) (map[string]string, error) {
		out := make(map[string]string, len(req.Items))
		for _, it := range req.Items {
			out[it.ID] = "TR:" + it.SrcText
		}
		return out, nil
	}
	reg.Register(nsProv)
	twLoc := locale.LocaleSpec{ID: "zh_TW", Enabled: true}
	resEN := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"openai_compat"},
		Locales:     []locale.LocaleSpec{twLoc},
		Tasks: map[string][]SourceItem{
			"zh_TW": {{ID: "k1", Key: domain.EntryKey{Context: "M", Source: "Save"}, SrcText: "Save"}},
		},
		Concurrency: 1,
	})
	if resEN.Cells[0].Text != "TR:Save" {
		t.Fatalf("unexpected EN-source result: %+v", resEN.Cells)
	}
	resZH := RunConcurrent(context.Background(), DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{"openai_compat"},
		Locales:     []locale.LocaleSpec{twLoc},
		Tasks: map[string][]SourceItem{
			"zh_TW": {{ID: "k1", Key: domain.EntryKey{Context: "M", Source: "Save"}, SourceLocale: "zh_CN", SrcText: "保存"}},
		},
		Concurrency: 1,
	})
	if resZH.Cells[0].Text != "TR:保存" {
		t.Fatalf("pinned SourceLocale hit stale English cache: %+v", resZH.Cells)
	}

	// ClearByProvider must match namespaced "openai_compat@..." keys and bump Revision().
	revBefore := cache.Revision()
	removed := cache.ClearByProvider("openai_compat")
	if removed != 2 {
		t.Fatalf("ClearByProvider(openai_compat) removed %d, want 2", removed)
	}
	if cache.Revision() <= revBefore {
		t.Fatalf("expected cache Revision to increase after ClearByProvider")
	}
}
