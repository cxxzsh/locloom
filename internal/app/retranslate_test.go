package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"locloom/internal/config"
	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/planner"
	"locloom/internal/provider"
	"locloom/internal/tsxml"
)

// A cell cleared via SetCleared keeps Status=Finished but Effective="",
// so ModeFillUnfinished must still enqueue it (the old code skipped on status).
func TestBuildTranslationTasksIncludesClearedFinished(t *testing.T) {
	zhTS, err := tsxml.ParseBytes("zh_CN.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>M</name>
    <message>
        <source>Save</source>
        <translation>保存</translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	zhTS.LocaleID = "zh_CN"
	jaTS, err := tsxml.ParseBytes("ja_JP.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>M</name>
    <message>
        <source>Save</source>
        <translation>保存済み</translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	jaTS.LocaleID = "ja_JP"

	locales := []locale.LocaleSpec{
		{ID: "zh_CN", Enabled: true, IsRef: true},
		{ID: "ja_JP", Enabled: true},
	}
	plan := planner.BuildPlan(planner.PlanInput{
		Bundle:    &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS}},
		Catalog:   domain.NewCatalog(),
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
	})
	key := plan.Rows[0].Key
	if cell := plan.Rows[0].Cells["ja_JP"]; cell.Status != domain.StatusFinished || cell.Effective != "保存済み" {
		t.Fatalf("pre: cell=%+v", cell)
	}

	// Clear the cell like clearCellAt does.
	plan2 := planner.BuildPlan(planner.PlanInput{
		Bundle: &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS}},
		Catalog: func() *domain.Catalog {
			c := domain.NewCatalog()
			c.SetCleared("ja_JP", key, domain.OriginManual)
			return c
		}(),
		Locales: locales, WriteMode: domain.ModeFillUnfinished,
	})
	cell := plan2.Rows[0].Cells["ja_JP"]
	if cell.Effective != "" || cell.Status != domain.StatusFinished {
		t.Fatalf("cleared cell = %+v, want Effective empty + Status Finished", cell)
	}

	tasks, n := buildTranslationTasks(plan2, []locale.LocaleSpec{locales[1]}, domain.ModeFillUnfinished, "zh_CN")
	if n != 1 || len(tasks["ja_JP"]) != 1 {
		t.Fatalf("tasks=%v n=%d, want 1 re-translation task for cleared cell", tasks, n)
	}
}

// Default "translate from" must be the file's source text, not the reference
// locale's own translation: row.RefText carries the ref locale's Effective
// (e.g. zh_CN "患者查看报告次数"), and feeding that to every other locale is
// what made translations lose the source's trailing punctuation wholesale.
func TestLocaleSourceDefaultsToFileSource(t *testing.T) {
	zhTS, err := tsxml.ParseBytes("zh_CN.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>M</name>
    <message>
        <source>Patient Views:</source>
        <translation>患者查看报告次数</translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	zhTS.LocaleID = "zh_CN"
	jaTS, err := tsxml.ParseBytes("ja_JP.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>M</name>
    <message>
        <source>Patient Views:</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	jaTS.LocaleID = "ja_JP"

	locales := []locale.LocaleSpec{
		{ID: "zh_CN", Enabled: true, IsRef: true},
		{ID: "ja_JP", Enabled: true},
		{ID: "zh_TW", Enabled: true, SourceLocale: "zh_CN"},
	}
	plan := planner.BuildPlan(planner.PlanInput{
		Bundle:    &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS}},
		Catalog:   domain.NewCatalog(),
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
	})
	row := &plan.Rows[0]
	if row.RefText != "患者查看报告次数" {
		t.Fatalf("pre: RefText=%q, want ref locale's text", row.RefText)
	}

	// ja_JP (default source) must get the file source, not the zh_CN text.
	src, _ := localeSource(plan, row, locales[1])
	if src != "Patient Views:" {
		t.Fatalf("default localeSource = %q, want file source %q", src, "Patient Views:")
	}
	// zh_TW pinned to zh_CN gets the ref locale's text as intended.
	srcTW, locTW := localeSource(plan, row, locales[2])
	if srcTW != "患者查看报告次数" || locTW != "zh_CN" {
		t.Fatalf("pinned localeSource = %q/%q, want zh_CN text", srcTW, locTW)
	}
}

type mockRowProvider struct{}

func (mockRowProvider) ID() string                          { return "test_mock_row" }
func (mockRowProvider) Name() string                        { return "Mock" }
func (mockRowProvider) Description() string                 { return "Mock" }
func (mockRowProvider) Kind() provider.Kind                 { return provider.KindEngine }
func (mockRowProvider) ConfigSchema() []provider.ConfigField { return nil }
func (mockRowProvider) Configure(map[string]string, *netpool.Pool) error {
	return nil
}
func (mockRowProvider) Check(context.Context) error { return nil }
func (mockRowProvider) Translate(_ context.Context, req provider.Request) (map[string]string, error) {
	out := make(map[string]string, len(req.Items))
	for _, it := range req.Items {
		out[it.ID] = req.TargetLocale.ID + ":" + it.SrcText
	}
	return out, nil
}

func TestRetranslateRowAllLocalesAndClearedCellAndAppBugFixes(t *testing.T) {
	provider.Register(mockRowProvider{})

	cfg := config.Default()
	cfg.Locales = []locale.LocaleSpec{
		{ID: "zh_CN", Name: "简体中文", Enabled: true, IsRef: true, EngineCode: "zh-CN"},
		{ID: "ja_JP", Name: "日语", Enabled: true, EngineCode: "ja"},
		{ID: "de_DE", Name: "德语", Enabled: true, EngineCode: "de"},
	}
	cfg.ActiveProviders = []string{"test_mock_row"}
	cfg.ProviderOrder = []string{"test_mock_row"}

	a := NewWithConfig(cfg)
	if a.defaultCtx != cfg.AppendContext {
		t.Fatalf("defaultCtx = %q, want %q", a.defaultCtx, cfg.AppendContext)
	}

	dir := t.TempDir()
	for _, f := range []struct{ name, lang, trans string }{
		{"zh_CN.ts", "zh_CN", "保存"},
		{"ja_JP.ts", "ja_JP", "保存済み"},
		{"de_DE.ts", "de_DE", "Speichern"},
	} {
		xml := `<?xml version="1.0" encoding="utf-8"?><TS version="2.1" language="` + f.lang + `"><context><name>M</name><message><source>Save</source><translation>` + f.trans + `</translation></message></context></TS>`
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(xml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.LoadProject(dir); err != nil {
		t.Fatal(err)
	}

	// 1. retranslateRowAtAsync must re-translate ALL enabled non-ref locales (ja_JP and de_DE).
	a.retranslateRowAtAsync(0)
	a.bgWG.Wait()
	if got := a.plan.Rows[0].Cells["ja_JP"].Effective; got != "ja_JP:Save" {
		t.Fatalf("retranslateRowAtAsync ja_JP = %q, want ja_JP:Save (status=%q)", got, a.statusBanner)
	}
	if got := a.plan.Rows[0].Cells["de_DE"].Effective; got != "de_DE:Save" {
		t.Fatalf("retranslateRowAtAsync de_DE = %q, want de_DE:Save", got)
	}

	// 2. onEditCell("") on a Finished cell must mark it Cleared instead of reverting to OldText.
	a.selectedRow = 0
	a.activeEditLocale = "ja_JP"
	a.onEditCell("")
	if cell := a.plan.Rows[0].Cells["ja_JP"]; cell.Effective != "" || cell.Action != domain.ActionOverwrite {
		t.Fatalf("onEditCell(\"\") on Finished cell = %+v, want Effective=\"\" ActionOverwrite", cell)
	}

	// 3. Single-row translate (non-force) must re-translate that cleared Finished cell in Fill mode.
	a.translateRowAtAsync(0, "")
	a.bgWG.Wait()
	if got := a.plan.Rows[0].Cells["ja_JP"].Effective; got != "ja_JP:Save" {
		t.Fatalf("translateRowAtAsync on cleared Finished cell = %q, want ja_JP:Save", got)
	}

	// 4. ImportFromExcelFile must rebuild a.plan immediately.
	xlsxPath := writeHermeticXLSX(t)
	a.clearAllTranslations()
	if err := a.ImportFromExcelFile(xlsxPath, "", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.catalog.Lookup("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Open File"}); !ok {
		t.Fatal("expected xlsx catalog entry")
	}

	// 5. Removing a locale in sidebar Locales tab must not panic.
	a.sidebarTab = 2
	tt := ui.NewTester(a.View, 1280, 820)
	if err := tt.Click("×"); err != nil {
		t.Fatalf("click remove locale failed: %v", err)
	}
	if len(a.cfg.Locales) != 2 {
		t.Fatalf("expected 2 locales remaining after removal, got %d", len(a.cfg.Locales))
	}

	// 6. DeepL official mode must be included in cloud proxy bulk toggle.
	a.cfg.ProviderConfigs[domain.OriginDeepL] = map[string]string{
		"mode":     "official",
		"base_url": "http://127.0.0.1:1188/translate",
	}
	a.setCloudProvidersUseProxy(true)
	if got := a.cfg.ProviderConfigs[domain.OriginDeepL]["use_proxy"]; got != "true" {
		t.Fatalf("expected DeepL official mode to receive use_proxy=true, got %q", got)
	}
}

func TestWriteBackAsyncSaveTargetFolderAndInPlaceBackup(t *testing.T) {
	cfg := config.Default()
	cfg.Locales = []locale.LocaleSpec{
		{ID: "zh_CN", Name: "简体中文", Enabled: true, IsRef: true, EngineCode: "zh-CN"},
		{ID: "ja_JP", Name: "日语", Enabled: true, EngineCode: "ja"},
	}
	if cfg.SaveTargetMode != config.SaveTargetNewFolder {
		t.Fatalf("default SaveTargetMode = %d, want SaveTargetNewFolder", cfg.SaveTargetMode)
	}

	parent := t.TempDir()
	srcDir := filepath.Join(parent, "ts")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	zhXML := `<?xml version="1.0" encoding="utf-8"?><TS version="2.1" language="zh_CN"><context><name>M</name><message><source>Save</source><translation>保存</translation></message></context></TS>`
	jaXML := `<?xml version="1.0" encoding="utf-8"?><TS version="2.1" language="ja_JP"><context><name>M</name><message><source>Save</source><translation type="unfinished"></translation></message></context></TS>`
	if err := os.WriteFile(filepath.Join(srcDir, "zh_CN.ts"), []byte(zhXML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "ja_JP.ts"), []byte(jaXML), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewWithConfig(cfg)
	if err := a.LoadProject(srcDir); err != nil {
		t.Fatal(err)
	}
	a.selectedRow = 0
	a.activeEditLocale = "ja_JP"
	a.onEditCell("保存する")

	// Default SaveTargetNewFolder writes complete bundle into ts_updated/ without modifying ts/.
	a.writeBackAsync()
	a.bgWG.Wait()

	outDir := srcDir + "_updated"
	origJA, _ := os.ReadFile(filepath.Join(srcDir, "ja_JP.ts"))
	if strings.Contains(string(origJA), "保存する") {
		t.Fatalf("original ts/ja_JP.ts was modified in SaveTargetNewFolder mode")
	}
	outJA, err := os.ReadFile(filepath.Join(outDir, "ja_JP.ts"))
	if err != nil || !strings.Contains(string(outJA), "保存する") {
		t.Fatalf("expected updated ts_updated/ja_JP.ts, err=%v raw=%s", err, string(outJA))
	}
	if _, err := os.Stat(filepath.Join(outDir, "zh_CN.ts")); err != nil {
		t.Fatalf("expected untouched zh_CN.ts copied into ts_updated/: %v", err)
	}
	if a.bundle.SourcePath != outDir {
		t.Fatalf("SourcePath should switch to saved output folder %q so Refresh/next saves stay consistent, got %q", outDir, a.bundle.SourcePath)
	}
	if a.plan.Summary.TotalChanges != 0 {
		t.Fatalf("expected 0 pending changes after reload, got %d", a.plan.Summary.TotalChanges)
	}

	// Second edit + Save must reuse ts_updated/ (NOT create ts_updated_updated/) and back up into .locloom_backup/.
	a.onEditCell("保存完了")
	a.writeBackAsync()
	a.bgWG.Wait()
	if _, err := os.Stat(outDir + "_updated"); err == nil {
		t.Fatalf("second save must not chain %s_updated", outDir)
	}
	outJA2, err := os.ReadFile(filepath.Join(outDir, "ja_JP.ts"))
	if err != nil || !strings.Contains(string(outJA2), "保存完了") {
		t.Fatalf("second save did not update ts_updated/ja_JP.ts: err=%v raw=%s", err, string(outJA2))
	}
	if _, err := os.Stat(filepath.Join(outDir, ".locloom_backup")); err != nil {
		t.Fatalf("expected backup folder in ts_updated/.locloom_backup: %v", err)
	}
}
