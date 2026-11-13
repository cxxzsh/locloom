package domain

import "testing"

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
