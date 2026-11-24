package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"locloom/internal/config"
	"locloom/internal/locale"
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
