package provider

import (
	"context"
	"errors"
	"path/filepath"
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
