package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"locloom/internal/config"
	"locloom/internal/locale"
	"locloom/internal/updater"
)

// The sidebar locale list is scoped to the project (bundle ∪ enabled);
// searching bypasses the scope so a hidden preset can be found and enabled
// as a manual "add", and select-all must not re-enable hidden presets.
func TestSidebarLocaleScope(t *testing.T) {
	cfg := config.Default()
	cfg.Locales = locale.DefaultLocales()
	a := NewWithConfig(cfg)
	dir := writeHermeticJSONBundle(t)
	if err := a.LoadProject(dir); err != nil {
		t.Fatal(err)
	}
	if scope := a.sidebarLocaleScope(); len(scope) != 3 {
		t.Fatalf("scope = %v", scope)
	}

	// Searching "de" must surface hidden de_DE so it can be re-enabled.
	a.localeSearchQuery = "de"
	a.sidebarTab = 2
	tt := ui.NewTester(a.View, 1280, 820)
	if !tt.HasText("de_DE") {
		t.Fatal("expected hidden de_DE to appear under search")
	}

	// Enabling it brings it into the (unfiltered) scope.
	for i := range a.cfg.Locales {
		if a.cfg.Locales[i].ID == "de_DE" {
			a.cfg.Locales[i].Enabled = true
		}
	}
	a.localeSearchQuery = ""
	if scope := a.sidebarLocaleScope(); !scope["de_DE"] {
		t.Fatalf("expected de_DE in scope after enable, scope=%v", scope)
	}

	// Select-all stays scoped: hidden presets must remain disabled.
	for i := range a.cfg.Locales {
		if !a.cfg.Locales[i].IsRef {
			a.cfg.Locales[i].Enabled = false
		}
	}
	a.selectAllLocales(true)
	scope := a.sidebarLocaleScope()
	for _, l := range a.cfg.Locales {
		if l.IsRef {
			continue
		}
		if !scope[l.ID] && l.Enabled {
			t.Fatalf("selectAll enabled hidden locale %s", l.ID)
		}
	}
}

func TestLocaleProfilesProjectScopingAndRefToggle(t *testing.T) {
	dir := t.TempDir()
	// Create a project mimicking test_data/ts: English source, zh_CN + ja_JP + da_DK + nb_NO + pt_PT (no zh_HK, no pt_BR, no cs_CZ).
	files := []string{"zh_CN.ts", "ja_JP.ts", "da_DK.ts", "nb_NO.ts", "pt_PT.ts"}
	for _, name := range files {
		lang := name[:len(name)-3]
		xml := `<?xml version="1.0" encoding="utf-8"?><TS version="2.1" language="` + lang + `"><context><name>M</name><message><source>Save</source><translation type="unfinished"></translation></message></context></TS>`
		if err := os.WriteFile(filepath.Join(dir, name), []byte(xml), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	a := NewWithConfig(cfg)
	if err := a.LoadProject(dir); err != nil {
		t.Fatal(err)
	}

	findProfile := func(id string) locale.Profile {
		for _, p := range a.cfg.Profiles {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("profile %q not found", id)
		return locale.Profile{}
	}
	enabledMap := func() map[string]bool {
		m := map[string]bool{}
		for _, l := range a.enabledNonRefLocales() {
			m[l.ID] = true
		}
		return m
	}

	// 1. Europe profile must select da_DK, nb_NO, pt_PT and NOT inject non-project ghost locales (pt_BR, de_DE, cs_CZ).
	a.applyLocaleProfile(findProfile("europe"))
	em := enabledMap()
	if !em["da_DK"] || !em["nb_NO"] || !em["pt_PT"] || len(em) != 3 {
		t.Fatalf("europe profile enabled = %v, want exactly {da_DK, nb_NO, pt_PT}", em)
	}
	if !a.isProfileActive(findProfile("europe")) {
		t.Fatal("expected europe profile to report active")
	}
	// focusedLocale must snap to one of the enabled locales (not stay on disabled ja_JP).
	if !em[a.focusedLocale] {
		t.Fatalf("focusedLocale = %q should be in enabled set %v", a.focusedLocale, em)
	}

	// 2. CJK profile must select ja_JP only (zh_CN is ref) and NOT inject ghost zh_HK.
	a.applyLocaleProfile(findProfile("cjk"))
	em = enabledMap()
	if !em["ja_JP"] || em["zh_HK"] || len(em) != 1 {
		t.Fatalf("cjk profile enabled = %v, want {ja_JP}", em)
	}

	// 3. All profile must re-enable ALL project locales including discovered da_DK & nb_NO, without ghost locales.
	a.applyLocaleProfile(findProfile("all"))
	em = enabledMap()
	if !em["ja_JP"] || !em["da_DK"] || !em["nb_NO"] || !em["pt_PT"] || len(em) != 4 {
		t.Fatalf("all profile enabled = %v, want 4 project target locales", em)
	}

	// 4. Toggling Ref off on zh_CN makes zh_CN a normal translatable target locale!
	a.setRefLocale("zh_CN")
	em = enabledMap()
	if !em["zh_CN"] || len(em) != 5 {
		t.Fatalf("after clearing ref on zh_CN, enabledNonRef = %v, want 5 locales including zh_CN", em)
	}
	if a.plan.RefLocaleID != "" {
		t.Fatalf("expected RefLocaleID == \"\", got %q", a.plan.RefLocaleID)
	}

	// 5. syncBundleLocales on a JSON project with SourceLocale="en" must not enable "en" as a target.
	jsonDir := writeHermeticJSONBundle(t)
	if err := a.LoadProject(jsonDir); err != nil {
		t.Fatal(err)
	}
	a.syncBundleLocales()
	em = enabledMap()
	if em["en"] {
		t.Fatalf("syncBundleLocales unexpectedly enabled source locale 'en': %v", em)
	}
}

func TestRedesignedHelpModalNavigationZHAndEN(t *testing.T) {
	cfg := config.Default()
	cfg.HasSeenOnboarding = true
	a := NewWithConfig(cfg)
	a.SetHelpMarkdowns("# EN README", "# ZH README")
	a.SetShowHelp(true)

	tt := ui.NewTester(a.View, 1280, 820)
	if !tt.HasText("LocLoom 使用指南") || !tt.HasText("4 步完成多语种无损本地化") {
		t.Fatal("expected redesigned Chinese Quick Start page in Help modal")
	}

	// Click through all 5 navigation chapters.
	for _, tc := range []struct {
		nav  string
		want string
	}{
		{"翻译与语种", "十源并发瀑布流推荐层级"},
		{"保存与格式", "原生支持的 8 大无损格式"},
		{"快捷键与技巧", "Cmd / Ctrl + S"},
		{"常见问题与路径", "便携模式"},
	} {
		if err := tt.Click(tc.nav); err != nil {
			t.Fatalf("click help nav %q failed: %v", tc.nav, err)
		}
		if !tt.HasText(tc.want) {
			t.Fatalf("after clicking %q, expected text %q", tc.nav, tc.want)
		}
	}

	// Switch to English and verify English chapters render.
	a.SetUILang("en")
	a.helpTab = 0
	tt.Frame()
	if !tt.HasText("LocLoom User Guide") || !tt.HasText("4-Step Lossless Localization Workflow") {
		t.Fatal("expected redesigned English Quick Start page in Help modal")
	}
}

func TestAutoUpdateHeaderPillsAndSettingsUI(t *testing.T) {
	cfg := config.Default()
	cfg.HasSeenOnboarding = true
	if !cfg.AutoDownloadUpdates {
		t.Fatal("expected AutoDownloadUpdates to default to true")
	}
	a := NewWithConfig(cfg)
	tt := ui.NewTester(a.View, 1280, 820)

	// 1. Update available pill in header opens Settings -> Updates tab.
	a.updateInfo = &updater.ReleaseInfo{
		CurrentVersion: "0.1.7",
		LatestVersion:  "0.1.8",
		HasUpdate:      true,
		Notes:          "### Added\n- Auto download",
	}
	tt.Frame()
	if !tt.HasText("⬆ v0.1.8") {
		t.Fatal("expected header update available pill ⬆ v0.1.8")
	}
	if err := tt.Click("⬆ v0.1.8"); err != nil {
		t.Fatalf("click update pill failed: %v", err)
	}
	if !a.showSettings || a.settingsTab != 2 {
		t.Fatalf("expected settings modal tab 2 open, got show=%v tab=%d", a.showSettings, a.settingsTab)
	}
	if !tt.HasText(a.tr().HintSelfUpdateDev) {
		t.Fatal("expected dev-mode self-update explanation in Settings -> Updates")
	}
	a.showSettings = false

	// 2. Downloading state shows progress pill in header.
	a.installingUpd = true
	a.updateDL = 512 * 1024
	a.updateTotal = 1024 * 1024
	tt.Frame()
	if !tt.HasText("⬇ 50%") {
		t.Fatal("expected header download progress pill ⬇ 50%")
	}

	// 3. Staged ready-to-relaunch update shows green ready pill and Restart button in Settings.
	a.installingUpd = false
	a.updateReadyVersion = "0.1.8"
	a.updateReadyNotes = "### Added\n- Ready to restart"
	tt.Frame()
	if !tt.HasText("↻ 更新 v0.1.8") {
		t.Fatal("expected header ready pill ↻ 更新 v0.1.8")
	}
	if err := tt.Click("↻ 更新 v0.1.8"); err != nil {
		t.Fatalf("click ready pill failed: %v", err)
	}
	if !tt.HasText("立即重启更新 v0.1.8") {
		t.Fatal("expected Restart to Apply button in Settings -> Updates")
	}
}
