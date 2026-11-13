package locale

import (
	"reflect"
	"testing"
)

func TestDefaultLocalesAndProfiles(t *testing.T) {
	locs := DefaultLocales()
	if len(locs) != 22 {
		t.Fatalf("got %d default locales, want 22", len(locs))
	}
	var refCount, enabledCount int
	for _, l := range locs {
		if l.IsRef {
			refCount++
			if l.ID != "zh_CN" {
				t.Fatalf("unexpected ref locale: %s", l.ID)
			}
		}
		if l.Enabled {
			enabledCount++
		}
	}
	if refCount != 1 {
		t.Fatalf("got %d ref locales, want 1", refCount)
	}
	if enabledCount != 9 {
		t.Fatalf("got %d tier-1 enabled locales, want 9", enabledCount)
	}

	profiles := DefaultProfiles()
	wantIDs := []string{"tier1", "cjk", "europe", "sea_mena", "all"}
	if len(profiles) != len(wantIDs) {
		t.Fatalf("got %d profiles, want %d", len(profiles), len(wantIDs))
	}
	for i, p := range profiles {
		if p.ID != wantIDs[i] {
			t.Fatalf("profile[%d].ID = %q, want %q", i, p.ID, wantIDs[i])
		}
	}
}

func TestLocaleSpecFileNameAndClone(t *testing.T) {
	l := LocaleSpec{
		ID:          "ja_JP",
		EngineCode:  "ja",
		EngineCodes: map[string]string{"llm": "ja-JP"},
	}
	if got := l.FileName(""); got != "ja_JP.ts" {
		t.Fatalf("FileName(\"\") = %q, want ja_JP.ts", got)
	}
	if got := l.FileName("app_{id}.ts"); got != "app_ja_JP.ts" {
		t.Fatalf("FileName(\"app_{id}.ts\") = %q, want app_ja_JP.ts", got)
	}

	cloned := l.Clone()
	cloned.EngineCodes["llm"] = "changed"
	if l.EngineCodes["llm"] != "ja-JP" {
		t.Fatalf("Clone did not deep copy EngineCodes")
	}

	if got := ResolveEngineCode(l, "llm"); got != "ja-JP" {
		t.Fatalf("ResolveEngineCode(llm) = %q, want ja-JP", got)
	}
	if got := ResolveEngineCode(l, "google_web"); got != "ja" {
		t.Fatalf("ResolveEngineCode(google_web) = %q, want ja", got)
	}
}

func TestStemLocaleID(t *testing.T) {
	known := DefaultLocales()
	cases := []struct {
		path    string
		pattern string
		want    string
	}{
		{"ja_JP.ts", "", "ja_JP"},
		{"translations/zh-cn.ts", "", "zh_CN"},
		{"app_de_DE.ts", "app_{id}.ts", "de_DE"},
		{"fr_FR_ui.ts", "{id}_ui.ts", "fr_FR"},
		{"nested\\pt_br_strings.ts", "", "pt_BR"},
		{"ko.ts", "", "ko_KR"},
	}
	for _, tc := range cases {
		if got := StemLocaleID(tc.path, tc.pattern, known); got != tc.want {
			t.Errorf("StemLocaleID(%q, %q) = %q, want %q", tc.path, tc.pattern, got, tc.want)
		}
	}
}

func TestParseHeaderLocaleAndColumnDetectors(t *testing.T) {
	if !IsSourceHeader("Source") || !IsSourceHeader("原文") || IsSourceHeader("ja_JP") {
		t.Fatal("IsSourceHeader mismatch")
	}
	if !IsContextHeader("Context") || !IsContextHeader("上下文") || IsContextHeader("Source") {
		t.Fatal("IsContextHeader mismatch")
	}
	if !IsCommentHeader("Comment") || !IsCommentHeader("备注") || IsCommentHeader("Source") {
		t.Fatal("IsCommentHeader mismatch")
	}

	known := DefaultLocales()
	cases := []struct {
		header  string
		wantID  string
		wantRef bool
		wantOK  bool
	}{
		{"zh_CN (参考)", "zh_CN", true, true},
		{"Simplified Chinese [ref]", "zh_CN", true, true},
		{"日本語", "ja_JP", false, true},
		{"German (de_DE)", "de_DE", false, true},
		{"fr-fr", "fr_FR", false, true},
		{"Source", "", false, false},
		{"Context", "", false, false},
		{"UnknownColumnXYZ", "", false, false},
	}
	for _, tc := range cases {
		id, isRef, ok := ParseHeaderLocale(tc.header, known)
		if id != tc.wantID || isRef != tc.wantRef || ok != tc.wantOK {
			t.Errorf("ParseHeaderLocale(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tc.header, id, isRef, ok, tc.wantID, tc.wantRef, tc.wantOK)
		}
	}
}

func TestMergeDiscovered(t *testing.T) {
	existing := []LocaleSpec{
		{ID: "zh_CN", Name: "简体中文", Enabled: true, IsRef: true, EngineCode: "zh-CN"},
	}
	merged, added := MergeDiscovered(existing, []string{"zh_CN", "ja_JP", "ca_ES"})
	if !reflect.DeepEqual(added, []string{"ja_JP", "ca_ES"}) {
		t.Fatalf("added = %v, want [ja_JP ca_ES]", added)
	}
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	if merged[1].Name != "日本語" || !merged[1].Enabled {
		t.Fatalf("expected ja_JP to inherit default metadata and be enabled: %+v", merged[1])
	}
	if merged[2].EngineCode != "ca" {
		t.Fatalf("expected ca_ES derived EngineCode 'ca', got %q", merged[2].EngineCode)
	}
}

func TestDisplayNameAndEnglishNames(t *testing.T) {
	for _, l := range DefaultLocales() {
		if l.EnglishName == "" {
			t.Fatalf("locale %s missing EnglishName", l.ID)
		}
		if got := l.DisplayName("en"); got != l.EnglishName {
			t.Fatalf("locale %s DisplayName(en) = %q, want %q", l.ID, got, l.EnglishName)
		}
		if got := l.DisplayName("zh"); got != l.Name {
			t.Fatalf("locale %s DisplayName(zh) = %q, want %q", l.ID, got, l.Name)
		}
	}

	fallbackLoc := LocaleSpec{ID: "xx_XX", Name: "Custom"}
	if got := fallbackLoc.DisplayName("en"); got != "Custom" {
		t.Fatalf("fallback DisplayName(en) = %q, want Custom", got)
	}
	idFallback := LocaleSpec{ID: "yy_YY"}
	if got := idFallback.DisplayName("zh"); got != "yy_YY" {
		t.Fatalf("id fallback DisplayName(zh) = %q, want yy_YY", got)
	}

	for _, p := range DefaultProfiles() {
		if p.EnglishName == "" {
			t.Fatalf("profile %s missing EnglishName", p.ID)
		}
		if got := p.DisplayName("en"); got != p.EnglishName {
			t.Fatalf("profile %s DisplayName(en) = %q, want %q", p.ID, got, p.EnglishName)
		}
		if got := p.DisplayName("zh"); got != p.Name {
			t.Fatalf("profile %s DisplayName(zh) = %q, want %q", p.ID, got, p.Name)
		}
	}

	merged, added := MergeDiscovered(nil, []string{"zh_CN"})
	if len(added) != 1 || len(merged) != 1 || merged[0].EnglishName != "Simplified Chinese" {
		t.Fatalf("MergeDiscovered did not preserve EnglishName: %+v", merged)
	}
}

func TestResolveEngineCodeByProvider(t *testing.T) {
	zhCN := LocaleSpec{ID: "zh_CN", EngineCode: "zh-CN"}
	zhTW := LocaleSpec{ID: "zh_TW", EngineCode: "zh-TW"}
	jaJP := LocaleSpec{ID: "ja_JP", EngineCode: "ja"}
	frFR := LocaleSpec{ID: "fr_FR", EngineCode: "fr"}
	ptBR := LocaleSpec{ID: "pt_BR", EngineCode: "pt"}

	cases := []struct {
		spec       LocaleSpec
		providerID string
		want       string
	}{
		{zhCN, "bing_web", "zh-Hans"},
		{zhTW, "bing_web", "zh-Hant"},
		{zhCN, "deepl", "ZH"},
		{zhTW, "deepl", "ZH-HANT"},
		{ptBR, "deepl", "PT-BR"},
		{jaJP, "baidu_api", "jp"},
		{frFR, "baidu_api", "fra"},
		{zhTW, "baidu_api", "cht"},
		{zhTW, "niutrans", "cht"},
		{jaJP, "mymemory", "ja-JP"},
		{zhTW, "libretranslate", "zt"},
	}
	for _, tc := range cases {
		if got := ResolveEngineCode(tc.spec, tc.providerID); got != tc.want {
			t.Errorf("ResolveEngineCode(%s, %s) = %q, want %q", tc.spec.ID, tc.providerID, got, tc.want)
		}
	}
}
