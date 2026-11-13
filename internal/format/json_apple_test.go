package format

import (
	"bytes"
	"strings"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func TestJSONI18nMatchExclusionsAndNested(t *testing.T) {
	a, ok := GetAdapter(FormatJSON)
	if !ok {
		t.Fatalf("FormatJSON adapter not registered")
	}

	excluded := []string{
		"package.json",
		"sub/package-lock.json",
		"tsconfig.json",
		"jsconfig.json",
		"manifest.json",
		"config.json",
		"locloom.json",
		"translation-cache.json",
		"composer.json",
		".vscode/settings.json",
		"app/.vscode/launch.json",
	}
	for _, p := range excluded {
		if score := a.MatchPath(p, []byte(`{"name": "app"}`)); score != 0 {
			t.Errorf("MatchPath(%q) = %d, want 0", p, score)
		}
	}

	// Must yield 0 for .xcstrings and .arb JSON payloads.
	if score := a.MatchPath("Localizable.json", []byte(`{"sourceLanguage": "en", "strings": {}}`)); score != 0 {
		t.Errorf("MatchPath xcstrings-shaped json = %d, want 0", score)
	}
	if score := a.MatchPath("app_en.json", []byte(`{"@@locale": "en", "hello": "Hello"}`)); score != 0 {
		t.Errorf("MatchPath arb-shaped json = %d, want 0", score)
	}

	if score := a.MatchPath("locales/ja/common.json", []byte(`{"auth": {"login": "ログイン"}}`)); score != 60 {
		t.Errorf("MatchPath nested locale json = %d, want 60", score)
	}
}

func TestJSONI18nSurgicalPatchBOMAndCRLF(t *testing.T) {
	a, _ := GetAdapter(FormatJSON)
	bom := "\xEF\xBB\xBF"
	rawStr := bom +
		"{\r\n" +
		"    \"z_first\": \"First\",\r\n" +
		"    \"auth\": {\r\n" +
		"        \"login\": \"Old Login\",\r\n" +
		"        \"html\": \"A & B\"\r\n" +
		"    },\r\n" +
		"    \"errors\": {\r\n" +
		"        \"404.title\": \"Not Found\"\r\n" +
		"    }\r\n" +
		"}\r\n"

	doc, err := a.ParseDoc("locales/ja/common.json", []byte(rawStr), "", nil)
	if err != nil {
		t.Fatalf("ParseDoc error: %v", err)
	}
	if doc.LocaleID != "ja_JP" {
		t.Fatalf("LocaleID = %q, want ja_JP", doc.LocaleID)
	}
	if !doc.HasBOM || doc.LineEnding != "\r\n" {
		t.Fatalf("BOM=%v LineEnding=%q, want true CRLF", doc.HasBOM, doc.LineEnding)
	}
	if len(doc.Entries) != 4 {
		t.Fatalf("len(Entries) = %d, want 4", len(doc.Entries))
	}
	for _, e := range doc.Entries {
		if e.Key.Context != "common" {
			t.Errorf("entry %q Context = %q, want common", e.Key.ID, e.Key.Context)
		}
	}

	patches := []PatchOp{
		{
			Key:      domain.EntryKey{ID: "auth.login", Context: "common"},
			LocaleID: "ja_JP",
			NewText:  "<b>サインイン</b> & 続行",
			Action:   domain.ActionOverwrite,
		},
		{
			Key:      domain.EntryKey{ID: `errors.404\.title`, Context: "common"},
			LocaleID: "ja_JP",
			NewText:  "見つかりません",
			Action:   domain.ActionOverwrite,
		},
		{
			Key:      domain.EntryKey{ID: "auth.logout", Context: "common"},
			LocaleID: "ja_JP",
			NewText:  "ログアウト",
			Action:   domain.ActionAppendNew,
		},
	}

	out, applied, appended, err := a.ApplyPatches(doc, patches)
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}
	if applied != 2 || appended != 1 {
		t.Fatalf("applied=%d appended=%d, want 2, 1", applied, appended)
	}
	if !bytes.HasPrefix(out, []byte{0xEF, 0xBB, 0xBF}) {
		t.Errorf("BOM was not preserved")
	}
	outStr := string(out)
	if !strings.Contains(outStr, "\r\n") {
		t.Errorf("CRLF was not preserved")
	}
	if !strings.Contains(outStr, `"<b>サインイン</b> & 続行"`) {
		t.Errorf("HTML chars were unexpectedly escaped: %s", outStr)
	}
	if !strings.Contains(outStr, "        \"logout\": \"ログアウト\"\r\n") {
		t.Errorf("4-space indent or CRLF not matched on append:\n%s", outStr)
	}
	// Untouched key order & value preserved byte-for-byte.
	idxZ := strings.Index(outStr, `"z_first": "First"`)
	idxAuth := strings.Index(outStr, `"auth": {`)
	if idxZ < 0 || idxAuth < 0 || idxZ > idxAuth {
		t.Errorf("key ordering broken:\n%s", outStr)
	}

	// Test NewLocaleDoc clones structure with empty string values.
	frDoc, err := a.NewLocaleDoc(doc, locale.LocaleSpec{ID: "fr_FR"}, "locales/fr_FR/common.json")
	if err != nil {
		t.Fatalf("NewLocaleDoc error: %v", err)
	}
	for _, e := range frDoc.Entries {
		if e.Text != "" || e.Status != domain.StatusEmpty {
			t.Errorf("cloned entry %q text=%q status=%v, want empty", e.Key.ID, e.Text, e.Status)
		}
	}
}

func TestJSONI18nFlatAndTemplate(t *testing.T) {
	a, _ := GetAdapter(FormatJSON)
	raw := []byte("{\n  \"btn.save\": \"Save\",\n  \"btn.cancel\": \"Cancel\"\n}\n")
	doc, err := a.ParseDoc("en.json", raw, "", nil)
	if err != nil {
		t.Fatalf("ParseDoc error: %v", err)
	}
	if !doc.IsTemplate {
		t.Errorf("en.json should be marked IsTemplate=true")
	}
	if doc.Entries[0].Key.ID != "btn.save" || doc.Entries[0].Key.Source != "Save" {
		t.Errorf("unexpected flat template entry: %+v", doc.Entries[0])
	}
}

func TestARBSurgicalPatchAndMetadataPreservation(t *testing.T) {
	a, ok := GetAdapter(FormatARB)
	if !ok {
		t.Fatalf("FormatARB adapter not registered")
	}
	if s := a.MatchPath("lib/l10n/app_en.arb", []byte(`{}`)); s != 95 {
		t.Errorf("MatchPath .arb = %d, want 95", s)
	}
	if s := a.MatchPath("app_en.json", []byte(`{"@@locale": "en"}`)); s != 90 {
		t.Errorf("MatchPath .json with @@locale = %d, want 90", s)
	}

	raw := `{
  "@@locale": "zh_CN",
  "greetUser": "你好 {name}",
  "@greetUser": {
    "description": "Greeting shown on home screen",
    "placeholders": {
      "name": {
        "type": "String"
      }
    }
  },
  "saveBtn": ""
}`
	doc, err := a.ParseDoc("app_zh_CN.arb", []byte(raw), "", nil)
	if err != nil {
		t.Fatalf("ParseDoc error: %v", err)
	}
	if doc.LocaleID != "zh_CN" {
		t.Fatalf("LocaleID = %q, want zh_CN", doc.LocaleID)
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(doc.Entries))
	}
	if doc.Entries[0].Key.Comment != "Greeting shown on home screen" {
		t.Errorf("Comment = %q, want Greeting shown on home screen", doc.Entries[0].Key.Comment)
	}

	out, applied, appended, err := a.ApplyPatches(doc, []PatchOp{
		{
			Key:      domain.EntryKey{ID: "saveBtn"},
			LocaleID: "zh_CN",
			NewText:  "保存",
			Action:   domain.ActionFillGap,
		},
		{
			Key:      domain.EntryKey{ID: "cancelBtn"},
			LocaleID: "zh_CN",
			NewText:  "取消",
			Action:   domain.ActionAppendNew,
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}
	if applied != 1 || appended != 1 {
		t.Fatalf("applied=%d appended=%d, want 1, 1", applied, appended)
	}
	outStr := string(out)
	if !strings.Contains(outStr, `"@@locale": "zh_CN"`) {
		t.Errorf("@@locale lost:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"@greetUser": {`) || !strings.Contains(outStr, `"placeholders": {`) {
		t.Errorf("@greetUser metadata lost:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"saveBtn": "保存"`) || !strings.Contains(outStr, `"cancelBtn": "取消"`) {
		t.Errorf("patches missing:\n%s", outStr)
	}

	// Test NewLocaleDoc updates @@locale and clears values.
	jaDoc, err := a.NewLocaleDoc(doc, locale.LocaleSpec{ID: "ja_JP"}, "app_ja_JP.arb")
	if err != nil {
		t.Fatalf("NewLocaleDoc error: %v", err)
	}
	if !strings.Contains(string(jaDoc.Raw), `"@@locale": "ja_JP"`) {
		t.Errorf("NewLocaleDoc did not update @@locale:\n%s", string(jaDoc.Raw))
	}
	for _, e := range jaDoc.Entries {
		if e.Text != "" {
			t.Errorf("NewLocaleDoc entry %q not cleared: %q", e.Key.ID, e.Text)
		}
	}
}

func TestXCStringsMultiLocalePluralsAndNewLocale(t *testing.T) {
	a, ok := GetAdapter(FormatXCStrings)
	if !ok {
		t.Fatalf("FormatXCStrings adapter not registered")
	}
	if a.Topology() != TopologyMultiLocale {
		t.Fatalf("Topology = %v, want TopologyMultiLocale", a.Topology())
	}
	if s := a.MatchPath("Localizable.xcstrings", []byte(`{}`)); s != 100 {
		t.Errorf("MatchPath .xcstrings = %d, want 100", s)
	}

	raw := `{
  "sourceLanguage" : "en",
  "strings" : {
    "app_name" : {
      "comment" : "App title",
      "shouldTranslate" : false,
      "localizations" : {
        "en" : {
          "stringUnit" : {
            "state" : "translated",
            "value" : "LocLoom"
          }
        }
      }
    },
    "greeting" : {
      "comment" : "Welcome message",
      "extractionState" : "manual",
      "localizations" : {
        "en" : {
          "stringUnit" : {
            "state" : "translated",
            "value" : "Hello!"
          }
        },
        "ja" : {
          "stringUnit" : {
            "state" : "needs_review",
            "value" : "こんにちは"
          }
        }
      }
    },
    "items_count" : {
      "localizations" : {
        "en" : {
          "variations" : {
            "plural" : {
              "one" : {
                "stringUnit" : {
                  "state" : "translated",
                  "value" : "%lld item"
                }
              },
              "other" : {
                "stringUnit" : {
                  "state" : "translated",
                  "value" : "%lld items"
                }
              }
            }
          }
        }
      }
    },
    "no_locs_yet" : {
      "comment" : "Fresh key"
    }
  },
  "version" : "1.0"
}`

	doc, err := a.ParseDoc("Localizable.xcstrings", []byte(raw), "", nil)
	if err != nil {
		t.Fatalf("ParseDoc error: %v", err)
	}
	if doc.SourceLang != "en" {
		t.Fatalf("SourceLang = %q, want en", doc.SourceLang)
	}
	// app_name has shouldTranslate: false -> must be skipped.
	for _, e := range doc.Entries {
		if e.Key.ID == "app_name" {
			t.Fatalf("shouldTranslate: false entry app_name was not skipped")
		}
	}

	// Check CLDR plural entry.
	var pluralEntry *NormalizedEntry
	for i := range doc.Entries {
		if doc.Entries[i].Key.ID == "items_count" && doc.Entries[i].LocaleID == "en" {
			pluralEntry = &doc.Entries[i]
			break
		}
	}
	if pluralEntry == nil || !pluralEntry.Numerus || len(pluralEntry.PluralKeys) != 2 {
		t.Fatalf("plural entry not parsed properly: %+v", pluralEntry)
	}
	if pluralEntry.Text != domain.JoinNumerus([]string{"%lld item", "%lld items"}) {
		t.Errorf("plural Text = %q", pluralEntry.Text)
	}

	patches := []PatchOp{
		// Update existing ja stringUnit (needs_review -> translated).
		{
			Key:      domain.EntryKey{ID: "greeting"},
			LocaleID: "ja",
			NewText:  "ようこそ！",
			Action:   domain.ActionOverwrite,
		},
		// Insert brand-new locale fr into existing localizations.
		{
			Key:      domain.EntryKey{ID: "greeting"},
			LocaleID: "fr",
			NewText:  "Bonjour !",
			Action:   domain.ActionAppendNew,
		},
		// Insert brand-new plural locale ja into items_count.
		{
			Key:         domain.EntryKey{ID: "items_count"},
			LocaleID:    "ja",
			PluralForms: []string{"%lld 件"},
			Numerus:     true,
			Action:      domain.ActionAppendNew,
		},
		// Insert localizations onto no_locs_yet which had none.
		{
			Key:      domain.EntryKey{ID: "no_locs_yet"},
			LocaleID: "fr",
			NewText:  "Nouvelle clé",
			Action:   domain.ActionAppendNew,
		},
	}

	out, applied, appended, err := a.ApplyPatches(doc, patches)
	if err != nil {
		t.Fatalf("ApplyPatches error: %v", err)
	}
	if applied != 1 || appended != 3 {
		t.Fatalf("applied=%d appended=%d, want 1, 3", applied, appended)
	}

	// Re-parse output to verify structural integrity.
	reparsed, err := a.ParseDoc("Localizable.xcstrings", out, "", nil)
	if err != nil {
		t.Fatalf("re-parse output error: %v\n%s", err, string(out))
	}
	outStr := string(out)
	if !strings.Contains(outStr, `"extractionState" : "manual"`) || !strings.Contains(outStr, `"version" : "1.0"`) {
		t.Errorf("metadata fields lost:\n%s", outStr)
	}

	foundFRGreeting := false
	foundJAGreeting := false
	foundFRNoLocs := false
	for _, e := range reparsed.Entries {
		if e.Key.ID == "greeting" && e.LocaleID == "fr" && e.Text == "Bonjour !" && e.Status == domain.StatusFinished {
			foundFRGreeting = true
		}
		if e.Key.ID == "greeting" && e.LocaleID == "ja" && e.Text == "ようこそ！" && e.Status == domain.StatusFinished {
			foundJAGreeting = true
		}
		if e.Key.ID == "no_locs_yet" && e.LocaleID == "fr" && e.Text == "Nouvelle clé" && e.Status == domain.StatusFinished {
			foundFRNoLocs = true
		}
	}
	if !foundFRGreeting || !foundJAGreeting || !foundFRNoLocs {
		t.Errorf("missing expected reparsed entries: frGreeting=%v jaGreeting=%v frNoLocs=%v",
			foundFRGreeting, foundJAGreeting, foundFRNoLocs)
	}
}
