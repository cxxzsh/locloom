package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xuri/excelize/v2"
	"locloom/internal/config"
	"locloom/internal/domain"
	"locloom/internal/format"
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

func TestUILanguageSwitchENAndZH(t *testing.T) {
	a := newTestApp()
	a.SetHelpMarkdown("# English Guide\n\nEnglish body.\n\n---\n\n<a id=\"简体中文\"></a>\n\n# 中文指南\n\n中文正文。")
	tsDir := writeHermeticTSBundle(t)
	if err := a.LoadTSTarget(tsDir); err != nil {
		t.Fatalf("LoadTSTarget failed: %v", err)
	}

	tt := ui.NewTester(a.View, 1280, 820)

	// Toggle to English via header button
	if err := tt.Click("EN"); err != nil {
		t.Fatalf("click EN toggle failed: %v", err)
	}
	if a.UILang() != "en" {
		t.Fatalf("expected UILang to be en, got %q", a.UILang())
	}
	for _, want := range []string{"Start Translate", "Project", "Pipeline", "Locales", "Focus Locale"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in English UI", want)
		}
	}

	// Trigger a validation warning in English mode
	if err := tt.Click("Save changes to %1?"); err != nil {
		t.Fatalf("click row failed: %v", err)
	}
	a.onEditCell("変更を保存しますか？")
	tt.Frame()
	_, row := a.currentMatrixRow()
	if row == nil || len(row.Cells["ja_JP"].Warnings) == 0 {
		t.Fatal("expected validation warning when dropping %1 placeholder")
	}
	warn := row.Cells["ja_JP"].Warnings[0]
	if !tt.HasText("⚠ " + warn) {
		t.Fatalf("expected English validation warning %q to render in inspector", warn)
	}

	// Verify Help modal slices to English section
	a.SetShowHelp(true)
	tt.Frame()
	if !tt.HasText("LocLoom User Guide") || !tt.HasText("English Guide") {
		t.Fatal("expected English help modal content")
	}
	if tt.HasText("中文指南") {
		t.Fatal("did not expect Chinese help section in English mode")
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatalf("click Close help failed: %v", err)
	}

	// Render full frame in English mode
	if img := ui.Render(a.View, 1280, 820, 1.0); img == nil {
		t.Fatal("expected non-nil rendered image in English mode")
	}

	// Toggle back to Chinese via header button
	if err := tt.Click("中文"); err != nil {
		t.Fatalf("click 中文 toggle failed: %v", err)
	}
	if a.UILang() != "zh" {
		t.Fatalf("expected UILang to be zh, got %q", a.UILang())
	}
	for _, want := range []string{"开始并发翻译", "项目", "管线", "语种", "聚焦单语种"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in Chinese UI", want)
		}
	}
}

func writeHermeticJSONBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	enJSON := `{
  "app.title": "Hello",
  "app.save": "Save"
}
`
	jaJSON := `{
  "app.title": "こんにちは",
  "app.save": ""
}
`
	frJSON := `{
  "app.title": "Bonjour",
  "app.save": ""
}
`
	if err := os.WriteFile(filepath.Join(dir, "en.json"), []byte(enJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ja.json"), []byte(jaJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fr.json"), []byte(frJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSmartLocaleSyncAndNonTSFormatLoad(t *testing.T) {
	a := newTestApp()
	jsonDir := writeHermeticJSONBundle(t)
	if err := a.LoadProject(jsonDir); err != nil {
		t.Fatalf("LoadProject(jsonDir) failed: %v", err)
	}
	if a.bundle == nil || a.bundle.Format != format.FormatJSON {
		t.Fatalf("expected FormatJSON, got %+v", a.bundle)
	}

	// Smart Project Sync should disable de_DE (not in bundle) and enable ja & fr.
	enabledMap := make(map[string]bool)
	for _, loc := range a.cfg.Locales {
		enabledMap[loc.ID] = loc.Enabled
	}
	if enabledMap["de_DE"] {
		t.Fatal("expected de_DE to be disabled after loading jsonDir without de_DE")
	}
	if !enabledMap["ja_JP"] || !enabledMap["fr_FR"] {
		t.Fatalf("expected ja_JP and fr_FR to be enabled, got %+v", enabledMap)
	}
	if enabledMap["en_US"] {
		t.Fatalf("expected source locale en_US not to be enabled as target: %+v", enabledMap)
	}
	if a.focusedLocale != "ja_JP" && a.focusedLocale != "fr_FR" {
		t.Fatalf("expected focusedLocale to be ja_JP or fr_FR, got %q", a.focusedLocale)
	}

	tt := ui.NewTester(a.View, 1280, 820)
	if !tt.HasText("JSON") {
		t.Fatal("expected JSON format badge in header")
	}
	if !tt.HasText("app.title") || !tt.HasText("Hello") {
		t.Fatal("expected JSON key ID and backfilled source text in matrix")
	}
}

func TestCollapsibleProviderCardsAndSecretToggle(t *testing.T) {
	a := newTestApp()
	tt := ui.NewTester(a.View, 1280, 820)

	if err := tt.Click("管线"); err != nil {
		t.Fatalf("click Pipeline tab failed: %v", err)
	}
	if tt.HasText("连通测试") {
		t.Fatal("expected provider cards to be collapsed by default")
	}

	if err := tt.Click("DeepL / DeepLX 翻译"); err != nil {
		t.Fatalf("click DeepL header failed: %v", err)
	}
	if !a.expandedProvider["deepl"] {
		t.Fatal("expected deepl provider card to expand")
	}
	if !tt.HasText("连通测试") || !tt.HasText("显示") {
		t.Fatal("expected expanded provider card to show Test and Show secret buttons")
	}

	if err := tt.Click("显示"); err != nil {
		t.Fatalf("click Show secret button failed: %v", err)
	}
	if !a.revealedSecrets["deepl.api_key"] || !tt.HasText("隐藏") {
		t.Fatal("expected secret toggle to reveal API key and switch label to 隐藏")
	}
}

func TestFullMatrixCellClickAndGapNavigation(t *testing.T) {
	a := newTestApp()
	jsonDir := writeHermeticJSONBundle(t)
	if err := a.LoadProject(jsonDir); err != nil {
		t.Fatalf("LoadProject failed: %v", err)
	}
	tt := ui.NewTester(a.View, 1280, 820)

	if err := tt.Click("全景矩阵"); err != nil {
		t.Fatalf("click Full Matrix failed: %v", err)
	}
	if err := tt.Click("Bonjour"); err != nil {
		t.Fatalf("click Bonjour cell failed: %v", err)
	}
	if a.activeEditLocale != "fr_FR" {
		t.Fatalf("expected activeEditLocale to be fr_FR after clicking Bonjour cell, got %q", a.activeEditLocale)
	}
	if a.editBuf != "Bonjour" {
		t.Fatalf("expected editBuf to be Bonjour, got %q", a.editBuf)
	}

	// Jump to next gap row ("Save")
	if err := tt.Click("下一待办 ▶"); err != nil {
		t.Fatalf("click Next Gap failed: %v", err)
	}
	if a.selectedKey.Source != "Save" {
		t.Fatalf("expected Next Gap to jump to Save, got %q", a.selectedKey.Source)
	}

	// Use source as translation
	if err := tt.Click("填入原文"); err != nil {
		t.Fatalf("click Use Src failed: %v", err)
	}
	if a.editBuf != "Save" {
		t.Fatalf("expected editBuf to be Save after Use Src, got %q", a.editBuf)
	}
}

func TestMatrixRowRightClickContextMenuClearAndRevert(t *testing.T) {
	a := newTestApp()
	jsonDir := writeHermeticJSONBundle(t)
	if err := a.LoadProject(jsonDir); err != nil {
		t.Fatalf("LoadProject failed: %v", err)
	}
	tt := ui.NewTester(a.View, 1280, 820)

	// Right-click row "Hello" (which has existing translations ja_JP="こんにちは", fr_FR="Bonjour")
	if err := tt.RightClick("Hello"); err != nil {
		t.Fatalf("RightClick on Hello failed: %v", err)
	}
	if err := tt.ChooseMenuItem("清空整行所有语种译文"); err != nil {
		t.Fatalf("ChooseMenuItem Clear Entire Row failed: %v", err)
	}

	// Both ja_JP and fr_FR on row 0 should now be cleared (ActionOverwrite with Effective=="")
	row0 := a.plan.Rows[0]
	if row0.Cells["ja_JP"].Effective != "" || row0.Cells["fr_FR"].Effective != "" {
		t.Fatalf("expected row 0 cells cleared, got ja=%q fr=%q", row0.Cells["ja_JP"].Effective, row0.Cells["fr_FR"].Effective)
	}
	if row0.Cells["ja_JP"].Action != domain.ActionOverwrite {
		t.Fatalf("expected ActionOverwrite when clearing finished cell, got %s", row0.Cells["ja_JP"].Action)
	}

	// Right-click again and revert the entire row back to file original
	if err := tt.RightClick("Hello"); err != nil {
		t.Fatalf("RightClick on Hello (2nd) failed: %v", err)
	}
	if err := tt.ChooseMenuItem("还原整行至文件原值"); err != nil {
		t.Fatalf("ChooseMenuItem Revert Entire Row failed: %v", err)
	}
	row0 = a.plan.Rows[0]
	if row0.Cells["ja_JP"].Effective != "こんにちは" || row0.Cells["fr_FR"].Effective != "Bonjour" {
		t.Fatalf("expected row 0 reverted to file originals, got ja=%q fr=%q", row0.Cells["ja_JP"].Effective, row0.Cells["fr_FR"].Effective)
	}

	// Fill source into single cell via right-click on "Save" row (row 1)
	if err := tt.RightClick("Save"); err != nil {
		t.Fatalf("RightClick on Save failed: %v", err)
	}
	if err := tt.ChooseMenuItem("将原文填入此格 (ja_JP)"); err != nil {
		t.Fatalf("ChooseMenuItem Fill Source failed: %v", err)
	}
	if a.plan.Rows[1].Cells["ja_JP"].Effective != "Save" {
		t.Fatalf("expected ja_JP on Save row to be filled with Save, got %q", a.plan.Rows[1].Cells["ja_JP"].Effective)
	}

	// Clear just that single cell via right-click
	if err := tt.RightClick("Save"); err != nil {
		t.Fatalf("RightClick on Save (2nd) failed: %v", err)
	}
	if err := tt.ChooseMenuItem("清空此格译文 (ja_JP)"); err != nil {
		t.Fatalf("ChooseMenuItem Clear Cell failed: %v", err)
	}
	if a.plan.Rows[1].Cells["ja_JP"].Effective != "" {
		t.Fatalf("expected ja_JP on Save row cleared, got %q", a.plan.Rows[1].Cells["ja_JP"].Effective)
	}
}

func TestMatrixRightClickFilterByContextNoPanicInFullMatrix(t *testing.T) {
	a := newTestApp()
	a.cfg.MatrixViewMode = 1 // Full Matrix mode (exact crash scenario)
	dir := t.TempDir()

	// Build a multi-context .ts file with 12 entries where "MainWindow" only has 2 rows,
	// reproducing the exact 12 -> 2 mid-frame shrink that caused index out of range [7].
	zhTS := `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>MainWindow</name>
    <message><source>Open</source><translation>打开</translation></message>
    <message><source>Save</source><translation>保存</translation></message>
</context>
<context>
    <name>SettingsDialog</name>
    <message><source>Item1</source><translation>项1</translation></message>
    <message><source>Item2</source><translation>项2</translation></message>
    <message><source>Item3</source><translation>项3</translation></message>
    <message><source>Item4</source><translation>项4</translation></message>
    <message><source>Item5</source><translation>项5</translation></message>
    <message><source>Item6</source><translation>项6</translation></message>
    <message><source>Item7</source><translation>项7</translation></message>
    <message><source>Item8</source><translation>项8</translation></message>
    <message><source>Item9</source><translation>项9</translation></message>
    <message><source>Item10</source><translation>项10</translation></message>
</context>
</TS>`
	jaTS := `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message><source>Open</source><translation>開く</translation></message>
    <message><source>Save</source><translation type="unfinished"></translation></message>
</context>
</TS>`
	if err := os.WriteFile(filepath.Join(dir, "app_zh_CN.ts"), []byte(zhTS), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app_ja_JP.ts"), []byte(jaTS), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadProject(dir); err != nil {
		t.Fatalf("LoadProject failed: %v", err)
	}
	if len(a.filteredRows) != 12 {
		t.Fatalf("expected 12 initial rows, got %d", len(a.filteredRows))
	}

	tt := ui.NewTester(a.View, 1280, 820)
	if err := tt.RightClick("Save"); err != nil {
		t.Fatalf("RightClick on Save failed: %v", err)
	}
	if err := tt.ChooseMenuItem("仅看此上下文 (MainWindow)"); err != nil {
		t.Fatalf("ChooseMenuItem Filter by Context failed: %v", err)
	}
	if a.contextFilter != "MainWindow" {
		t.Fatalf("expected contextFilter MainWindow, got %q", a.contextFilter)
	}
	if len(a.filteredRows) != 2 {
		t.Fatalf("expected 2 filtered rows for MainWindow, got %d", len(a.filteredRows))
	}
	if a.selectedKey.Source != "Save" {
		t.Fatalf("expected clicked row Save to stay selected, got %q", a.selectedKey.Source)
	}

	// Clear context filter back to all 12 rows via right-click
	if err := tt.RightClick("Save"); err != nil {
		t.Fatalf("RightClick on Save (unfilter) failed: %v", err)
	}
	if err := tt.ChooseMenuItem("取消上下文筛选（显示全部）"); err != nil {
		t.Fatalf("ChooseMenuItem Clear Context Filter failed: %v", err)
	}
	if len(a.filteredRows) != 12 {
		t.Fatalf("expected 12 rows after clearing context filter, got %d", len(a.filteredRows))
	}
	if a.WindowTitle() != "LocLoom" {
		t.Fatalf("expected clean WindowTitle 'LocLoom', got %q", a.WindowTitle())
	}
}
