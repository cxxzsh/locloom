package domain

import (
	"strings"
	"testing"
)

func TestCatalogContextDisambiguationAndFallback(t *testing.T) {
	cat := NewCatalog()

	kMenuOpen := EntryKey{Context: "FileMenu", Source: "Open"}
	kDoorOpen := EntryKey{Context: "DeviceState", Source: "Open"}
	kSave := EntryKey{Context: "FileMenu", Source: "Save"}

	// Context-agnostic glossary fallback
	cat.Set("zh_CN", EntryKey{Source: "Open"}, "打开(通用)", OriginXLSX)
	cat.Set("zh_CN", EntryKey{Source: "Save"}, "保存", OriginXLSX)

	// Context-specific override for DeviceState
	cat.Set("zh_CN", kDoorOpen, "已开启", OriginManual)

	if got, ok := cat.Lookup("zh_CN", kDoorOpen); !ok || got.Text != "已开启" || got.Origin != OriginManual {
		t.Fatalf("expected context-specific override, got %+v (ok=%v)", got, ok)
	}
	if got, ok := cat.Lookup("zh_CN", kMenuOpen); !ok || got.Text != "打开(通用)" {
		t.Fatalf("expected glossary fallback for FileMenu.Open, got %+v (ok=%v)", got, ok)
	}
	if _, ok := cat.LookupExact("zh_CN", kSave); ok {
		t.Fatalf("LookupExact must not match context-free glossary entry")
	}
	if got, ok := cat.Lookup("zh_CN", kSave); !ok || got.Text != "保存" {
		t.Fatalf("Lookup should match context-free glossary entry, got %+v", got)
	}

	clone := cat.Clone()
	cat.Set("zh_CN", kDoorOpen, "已关闭", OriginLLM)
	if got, _ := clone.Lookup("zh_CN", kDoorOpen); got.Text != "已开启" {
		t.Fatalf("Clone mutated when original changed: %+v", got)
	}

	removed := cat.ClearByPredicate(func(_ string, _ EntryKey, e TranslationEntry) bool {
		return IsMachineOrigin(e.Origin)
	})
	if removed != 1 {
		t.Fatalf("expected 1 machine entry removed, got %d", removed)
	}
}

func TestValidateTranslation(t *testing.T) {
	if w := ValidateTranslation("Save %1 to %2?", "将 %1 保存到 %2？", false); len(w) != 0 {
		t.Fatalf("unexpected warnings: %v", w)
	}
	if w := ValidateTranslation("Save %1?", "保存？", false); len(w) == 0 {
		t.Fatalf("expected missing %%1 warning")
	}
	if w := ValidateTranslation("%n file(s)", "已选择文件", false); len(w) == 0 {
		t.Fatalf("expected missing %%n warning on non-numerus")
	}
	if w := ValidateTranslation("%n file(s)", "1 个文件\n---\n%n 个文件", true); len(w) != 0 {
		t.Fatalf("numerus allows singular without %%n, got warnings: %v", w)
	}
	if w := ValidateTranslation("Hello %1", "你好 [[0]]", false); len(w) == 0 {
		t.Fatalf("expected leaked mask warning")
	}
}

func TestValidateTranslationLang(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		target  string
		numerus bool
		wantEn  string
		wantZh  string
	}{
		{
			name:   "missing placeholder",
			source: "Save %1?",
			target: "Save?",
			wantEn: "Missing placeholder(s): %1",
			wantZh: "缺失占位符: %1",
		},
		{
			name:   "unexpected placeholder",
			source: "Save?",
			target: "Save %2?",
			wantEn: "Unexpected placeholder(s): %2",
			wantZh: "多余占位符: %2",
		},
		{
			name:   "missing plural %n",
			source: "%n file(s)",
			target: "files",
			wantEn: "Missing plural placeholder %n",
			wantZh: "缺失复数占位符 %n",
		},
		{
			name:   "unexpected plural %n",
			source: "files",
			target: "%n files",
			wantEn: "Unexpected plural placeholder %n",
			wantZh: "多余复数占位符 %n",
		},
		{
			name:   "leaked mask token",
			source: "Hello %1",
			target: "Hello [[0]] %1",
			wantEn: "Unrestored mask token [[N]] detected",
			wantZh: "疑似残留未还原的掩码标记 [[N]]",
		},
		{
			name:   "trailing tab mismatch",
			source: "File\t",
			target: "File",
			wantEn: "Trailing tab \\t differs from source",
			wantZh: "末尾制表符 \\t 与原文不一致",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wEn := ValidateTranslationLang(tc.source, tc.target, tc.numerus, "en")
			if len(wEn) != 1 || wEn[0] != tc.wantEn {
				t.Fatalf("en warnings = %v, want [%q]", wEn, tc.wantEn)
			}
			wZh := ValidateTranslationLang(tc.source, tc.target, tc.numerus, "zh")
			if len(wZh) != 1 || wZh[0] != tc.wantZh {
				t.Fatalf("zh warnings = %v, want [%q]", wZh, tc.wantZh)
			}
			wDef := ValidateTranslationLang(tc.source, tc.target, tc.numerus, "")
			if len(wDef) != 1 || wDef[0] != tc.wantZh {
				t.Fatalf("default warnings = %v, want [%q]", wDef, tc.wantZh)
			}
		})
	}
}

func TestIDKeyCatalogAndMultiFormatValidation(t *testing.T) {
	cat := NewCatalog()
	kBtnA := EntryKey{ID: "menu.open", Context: "common", Source: "Open"}
	kBtnB := EntryKey{ID: "state.open", Context: "common", Source: "Open"}

	if kBtnA.Canonical() == kBtnB.Canonical() {
		t.Fatalf("distinct IDs must produce distinct Canonical keys")
	}

	cat.Set("ja_JP", EntryKey{ID: "menu.open", Context: "common"}, "開く", OriginManual)
	cat.Set("ja_JP", EntryKey{ID: "state.open"}, "接続中", OriginXLSX)

	if got, ok := cat.Lookup("ja_JP", kBtnA); !ok || got.Text != "開く" {
		t.Fatalf("expected ID+Context lookup 開く, got %+v (ok=%v)", got, ok)
	}
	if got, ok := cat.Lookup("ja_JP", kBtnB); !ok || got.Text != "接続中" {
		t.Fatalf("expected ID-only lookup 接続中, got %+v (ok=%v)", got, ok)
	}

	// Multi-format placeholders: printf, Apple, i18next, .NET/ARB, Rails, XLIFF
	validPairs := [][2]string{
		{"Hello {{user}}, you have {count} messages", "こんにちは {{user}}、{count} 件のメッセージがあります"},
		{"Downloaded %1$d of %2$s (%@)", "%2$s 中 %1$d をダウンロード (%@)"},
		{"Total: {0:N2} for %{name} <x id=\"1\"/>", "合計: %{name} は {0:N2} <x id=\"1\"/>"},
		{"Terms & Privacy <v2>", "利用規約 <v2>"},
	}
	for _, pair := range validPairs {
		if w := ValidateTranslationLang(pair[0], pair[1], false, "en"); len(w) != 0 {
			t.Fatalf("unexpected warning for %q -> %q: %v", pair[0], pair[1], w)
		}
	}

	if w := ValidateTranslationLang("Hello {{user}}!", "こんにちは!", false, "en"); len(w) == 0 {
		t.Fatal("expected warning when {{user}} is missing")
	}
	if w := ValidateTranslationLang("Count: {0:N2}", "Count: 100", false, "en"); len(w) == 0 {
		t.Fatal("expected warning when {0:N2} is missing")
	}
}

// Trailing punctuation parity: engines can drop a source's final colon, which
// then cascades into "translate from" outputs. ASCII/CJK forms are equivalent.
func TestValidateTrailingPunctuation(t *testing.T) {
	cases := []struct {
		source, target string
		wantWarn       bool
	}{
		{"Patient Views:", "患者查看报告次数", true},   // colon dropped
		{"Patient Views:", "患者查看报告次数：", false}, // ASCII : -> CJK ：
		{"Save?", "Save？", false},              // ? -> ？
		{"Done.", "完成。", false},                // . -> 。
		{"Hi!", "你好！", false},                  // ! -> ！
		{"Name", "名前：", true},                  // colon added
		{"Hello", "こんにちは", false},              // no terminator either side
	}
	for _, tc := range cases {
		w := ValidateTranslationLang(tc.source, tc.target, false, "en")
		hasPunctWarn := false
		for _, msg := range w {
			if strings.HasPrefix(msg, "Trailing punctuation differs") {
				hasPunctWarn = true
			}
		}
		if hasPunctWarn != tc.wantWarn {
			t.Fatalf("Validate(%q -> %q) punctWarn=%v, want %v (warnings=%v)",
				tc.source, tc.target, hasPunctWarn, tc.wantWarn, w)
		}
	}
}

func TestValidateTrailingPunctuationEllipsis(t *testing.T) {
	cases := []struct {
		source, target string
		wantWarn       bool
	}{
		{"Hi...", "你好…", false},   // ... -> … same class
		{"Hi…", "你好...", false},   // … -> ... same class
		{"Hi...", "你好", true},     // ellipsis dropped
		{"Hi", "你好…", true},       // ellipsis added
		{"Wait。", "等等...", true},  // 。 vs ... are different classes
	}
	for _, tc := range cases {
		w := ValidateTranslationLang(tc.source, tc.target, false, "en")
		has := false
		for _, m := range w {
			if strings.HasPrefix(m, "Trailing punctuation differs") {
				has = true
			}
		}
		if has != tc.wantWarn {
			t.Fatalf("(%q,%q) warn=%v want %v (%v)", tc.source, tc.target, has, tc.wantWarn, w)
		}
	}
}
