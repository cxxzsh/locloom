package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xuri/excelize/v2"

	"locloom/internal/config"
	"locloom/internal/locale"
	"locloom/internal/sheet"
)

func newTestApp() *App {
	cfg := config.Default()
	cfg.Locales = []locale.LocaleSpec{
		{ID: "zh_CN", Name: "简体中文", Enabled: true, IsRef: true, EngineCode: "zh-CN"},
		{ID: "ja_JP", Name: "日语", Enabled: true, EngineCode: "ja"},
		{ID: "de_DE", Name: "德语", Enabled: true, EngineCode: "de"},
	}
	return NewWithConfig(cfg)
}

func writeHermeticTSBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	zhTS := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="zh_CN">
<context>
    <name>MainWindow</name>
    <message>
        <source>Open File</source>
        <translation>打开文件</translation>
    </message>
    <message>
        <source>Save changes to %1?</source>
        <translation>是否保存对 %1 的更改？</translation>
    </message>
</context>
<context>
    <name>SettingsDialog</name>
    <message numerus="yes">
        <source>%n file(s) selected</source>
        <translation>
            <numerusform>已选择 %n 个文件</numerusform>
        </translation>
    </message>
</context>
</TS>
`
	jaTS := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message>
        <source>Open File</source>
        <translation type="unfinished"></translation>
    </message>
    <message>
        <source>Save changes to %1?</source>
        <translation type="unfinished"></translation>
    </message>
</context>
<context>
    <name>SettingsDialog</name>
    <message numerus="yes">
        <source>%n file(s) selected</source>
        <translation type="unfinished">
            <numerusform></numerusform>
        </translation>
    </message>
</context>
</TS>
`
	if err := os.WriteFile(filepath.Join(dir, "zh_CN.ts"), []byte(zhTS), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ja_JP.ts"), []byte(jaTS), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeHermeticXLSX(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "glossary.xlsx")

	f := excelize.NewFile()
	defer f.Close()
	sh := f.GetSheetName(0)
	_ = f.SetSheetRow(sh, "A1", &[]string{"Context", "Source", "zh_CN", "ja_JP", "de_DE"})
	_ = f.SetSheetRow(sh, "A2", &[]string{"MainWindow", "Open File", "打开文件", "ファイルを開く", "Datei öffnen"})
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitialRenderHeadless(t *testing.T) {
	a := newTestApp()
	img := ui.Render(a.View, 1280, 820, 1.0)
	if img == nil || img.Bounds().Dx() != 1280 || img.Bounds().Dy() != 820 {
		t.Fatalf("unexpected rendered image bounds: %v", img.Bounds())
	}

	tt := ui.NewTester(a.View, 1280, 820)
	if !tt.HasText("LocLoom") {
		t.Fatal("expected header brand LocLoom")
	}
	if !tt.HasText("开始并发翻译") {
		t.Fatal("expected primary CTA 开始并发翻译")
	}
	if !tt.HasText("尚未加载词条数据") {
		t.Fatal("expected empty state message before loading project")
	}
}

func TestLoadTSAndXLSXSelectionAndEdit(t *testing.T) {
	a := newTestApp()
	tsDir := writeHermeticTSBundle(t)
	xlsxPath := writeHermeticXLSX(t)

	if err := a.LoadTSTarget(tsDir); err != nil {
		t.Fatalf("LoadTSTarget failed: %v", err)
	}
	if err := a.ImportFromExcelFile(xlsxPath, "", true); err != nil {
		t.Fatalf("ImportFromExcelFile failed: %v", err)
	}

	tt := ui.NewTester(a.View, 1280, 820)
	if !tt.HasText("Open File") {
		t.Fatal("expected Open File row in translation matrix")
	}
	if !tt.HasText("Save changes to %1?") {
		t.Fatal("expected Save changes to %1? row in translation matrix")
	}

	// Click second row and verify inspector updates to match selection.
	if err := tt.Click("Save changes to %1?"); err != nil {
		t.Fatalf("click row failed: %v", err)
	}
	if a.selectedKey.Source != "Save changes to %1?" {
		t.Fatalf("expected selectedKey.Source to be Save changes to %%1?, got %q", a.selectedKey.Source)
	}

	// Perform O(1) edit via onEditCell and verify matrix reflects it.
	a.onEditCell("%1 への変更を保存しますか？")
	tt.Frame()
	if !tt.HasText("%1 への変更を保存しますか？") {
		t.Fatal("expected edited translation to appear after O(1) cell update")
	}

	// Export to temp xlsx to verify end-to-end export path.
	exportPath := filepath.Join(t.TempDir(), "out.xlsx")
	if err := a.ExportToExcelFile(exportPath, sheet.ExportAll); err != nil {
		t.Fatalf("ExportToExcelFile failed: %v", err)
	}
	if _, err := os.Stat(exportPath); err != nil {
		t.Fatalf("exported xlsx missing: %v", err)
	}
}

func TestThemeSwitchingAndModals(t *testing.T) {
	a := newTestApp()
	a.SetHelpMarkdown("# Help Title\n\nGeneric software localization guide.")
	tt := ui.NewTester(a.View, 1280, 820)

	// Switch theme via header button
	a.SetThemeMode(2)
	tt.Frame()
	if a.cfg.ThemeMode != 2 {
		t.Fatalf("expected dark theme mode 2, got %d", a.cfg.ThemeMode)
	}
	a.SetThemeMode(1)
	tt.Frame()
	if a.cfg.ThemeMode != 1 {
		t.Fatalf("expected light theme mode 1, got %d", a.cfg.ThemeMode)
	}

	// Open Help modal
	if err := tt.Click("帮助"); err != nil {
		t.Fatalf("click help button failed: %v", err)
	}
	if !a.showHelp || !tt.HasText("LocLoom 使用指南") {
		t.Fatal("expected help modal to open")
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatalf("click close help failed: %v", err)
	}
	if a.showHelp {
		t.Fatal("expected help modal to close")
	}

	// Open Export modal
	if err := tt.Click("导出 Excel"); err != nil {
		t.Fatalf("click export button failed: %v", err)
	}
	if !a.showExportModal || !tt.HasText("导出 Excel 词条表 (.xlsx)") {
		t.Fatal("expected export modal to open")
	}
}
