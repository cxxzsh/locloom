package format

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func TestGettextPORoundTrip(t *testing.T) {
	a, ok := GetAdapter(FormatGettextPO)
	if !ok {
		t.Fatalf("FormatGettextPO adapter not registered")
	}

	raw := "\xef\xbb\xbf" + strings.Join([]string{
		`msgid ""`,
		`msgstr ""`,
		`"Content-Type: text/plain; charset=UTF-8\n"`,
		`"Language: ja_JP\n"`,
		``,
		`#. Greeting button`,
		`#: src/main.cpp:42`,
		`#, c-format, fuzzy`,
		`msgid "Hello %s"`,
		`msgstr "こんにちは"`,
		``,
		`msgctxt "menu"`,
		`msgid "File"`,
		`msgstr ""`,
		``,
		`msgid "%d item"`,
		`msgid_plural "%d items"`,
		`msgstr[0] ""`,
		`msgstr[1] ""`,
		``,
		`#~ msgid "Obsolete"`,
		`#~ msgstr "古い"`,
		``,
	}, "\r\n")

	doc, err := a.ParseDoc("locale/ja.po", []byte(raw), "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("ParseDoc failed: %v", err)
	}
	if !doc.HasBOM || doc.LineEnding != "\r\n" {
		t.Fatalf("expected BOM=true and CRLF, got BOM=%v LE=%q", doc.HasBOM, doc.LineEnding)
	}
	if doc.LocaleID != "ja_JP" {
		t.Fatalf("expected LocaleID=ja_JP, got %q", doc.LocaleID)
	}
	if len(doc.Entries) != 3 {
		t.Fatalf("expected 3 entries (excluding header and #~), got %d", len(doc.Entries))
	}
	if doc.Entries[0].Status != domain.StatusUnfinished || doc.Entries[0].Key.Comment != "Greeting button" {
		t.Fatalf("unexpected entry 0: %+v", doc.Entries[0])
	}
	if doc.Entries[1].Key.Context != "menu" || doc.Entries[1].Status != domain.StatusEmpty {
		t.Fatalf("unexpected entry 1: %+v", doc.Entries[1])
	}
	if !doc.Entries[2].Numerus || len(doc.Entries[2].PluralForms) != 2 {
		t.Fatalf("unexpected plural entry 2: %+v", doc.Entries[2])
	}

	patches := []PatchOp{
		{
			Key:     doc.Entries[0].Key,
			NewText: "こんにちは %s\nようこそ",
			Action:  domain.ActionOverwrite,
		},
		{
			Key:     doc.Entries[1].Key,
			NewText: "ファイル",
			Action:  domain.ActionFillGap,
		},
		{
			Key:         doc.Entries[2].Key,
			PluralForms: []string{"%d 個", "%d 個"},
			Numerus:     true,
			Action:      domain.ActionFillGap,
		},
		{
			Key:     domain.EntryKey{Source: "Quit"},
			NewText: "終了",
			Action:  domain.ActionAppendNew,
		},
	}

	out, applied, appended, err := a.ApplyPatches(doc, patches)
	if err != nil {
		t.Fatalf("ApplyPatches failed: %v", err)
	}
	if applied != 3 || appended != 1 {
		t.Fatalf("expected applied=3 appended=1, got %d/%d", applied, appended)
	}
	if !bytes.HasPrefix(out, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("expected BOM preserved")
	}
	outStr := string(out)
	if !strings.Contains(outStr, "\r\n") {
		t.Fatalf("expected CRLF preserved")
	}
	if strings.Contains(outStr, "fuzzy") {
		t.Fatalf("expected fuzzy flag stripped, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "#, c-format\r\n") {
		t.Fatalf("expected c-format flag preserved, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "#~ msgid \"Obsolete\"") {
		t.Fatalf("expected obsolete block preserved")
	}

	reParsed, err := a.ParseDoc("locale/ja.po", out, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	if len(reParsed.Entries) != 4 {
		t.Fatalf("expected 4 entries after append, got %d", len(reParsed.Entries))
	}
	if reParsed.Entries[0].Status != domain.StatusFinished || reParsed.Entries[0].Text != "こんにちは %s\nようこそ" {
		t.Fatalf("multiline round-trip mismatch: %+v", reParsed.Entries[0])
	}
	if reParsed.Entries[2].PluralForms[0] != "%d 個" || reParsed.Entries[3].Text != "終了" {
		t.Fatalf("plural/append mismatch: %+v / %+v", reParsed.Entries[2], reParsed.Entries[3])
	}
}

func TestAndroidXMLRoundTripAndSiblingTemplate(t *testing.T) {
	dir := t.TempDir()
	valuesDir := filepath.Join(dir, "res", "values")
	jaDir := filepath.Join(dir, "res", "values-ja")
	zhDir := filepath.Join(dir, "res", "values-zh-rCN")
	bcpDir := filepath.Join(dir, "res", "values-b+zh+Hant+TW")
	for _, d := range []string{valuesDir, jaDir, zhDir, bcpDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	baseXML := `<?xml version="1.0" encoding="utf-8"?>
<resources>
    <string name="app_name" translatable="false">LocLoom</string>
    <string name="welcome">It\'s ready</string>
    <plurals name="items">
        <item quantity="one">%d item</item>
        <item quantity="other">%d items</item>
    </plurals>
</resources>
`
	jaXML := `<?xml version="1.0" encoding="utf-8"?>
<resources>
    <string name="welcome"></string>
    <plurals name="items">
        <item quantity="one"></item>
        <item quantity="other"></item>
    </plurals>
</resources>
`
	if err := os.WriteFile(filepath.Join(valuesDir, "strings.xml"), []byte(baseXML), 0o644); err != nil {
		t.Fatal(err)
	}
	jaPath := filepath.Join(jaDir, "strings.xml")
	if err := os.WriteFile(jaPath, []byte(jaXML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify single-file load backfills Key.Source from ../values/strings.xml.
	singleBundle, err := Load(jaPath, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("Load single android xml failed: %v", err)
	}
	if !singleBundle.IsSingleFile || len(singleBundle.Docs) != 1 {
		t.Fatalf("expected single-file bundle with 1 doc, got IsSingleFile=%v docs=%d", singleBundle.IsSingleFile, len(singleBundle.Docs))
	}
	if singleBundle.Docs[0].Entries[0].Key.Source != "It's ready" {
		t.Fatalf("expected backfilled source \"It's ready\", got %q", singleBundle.Docs[0].Entries[0].Key.Source)
	}

	// Verify directory load + locale resolution + Save.
	bundle, err := Load(dir, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("Load dir failed: %v", err)
	}
	if bundle.Format != FormatAndroidXML || len(bundle.Docs) != 2 {
		t.Fatalf("unexpected bundle: format=%s docs=%d", bundle.Format, len(bundle.Docs))
	}

	// Verify BCP-47 and -rCN folder parsing.
	a, _ := GetAdapter(FormatAndroidXML)
	zhDoc, _ := a.ParseDoc("res/values-zh-rCN/strings.xml", []byte(jaXML), "", locale.DefaultLocales())
	if zhDoc.LocaleID != "zh_CN" {
		t.Fatalf("expected zh_CN, got %q", zhDoc.LocaleID)
	}
	twDoc, _ := a.ParseDoc("res/values-b+zh+Hant+TW/strings.xml", []byte(jaXML), "", locale.DefaultLocales())
	if twDoc.LocaleID != "zh_TW" {
		t.Fatalf("expected zh_TW, got %q", twDoc.LocaleID)
	}

	patches := map[string][]PatchOp{
		"ja_JP": {
			{Key: domain.EntryKey{ID: "welcome"}, NewText: "Bob's app", Action: domain.ActionFillGap},
			{Key: domain.EntryKey{ID: "items"}, PluralForms: []string{"%d 個", "%d 個"}, Numerus: true, Action: domain.ActionFillGap},
			{Key: domain.EntryKey{ID: "new_key"}, NewText: "新規", Action: domain.ActionAppendNew},
		},
	}
	report, err := Save(bundle, patches, locale.DefaultLocales(), "", SaveOptions{CreateBackup: true})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if report.FilesWritten != 1 || len(report.BackupPaths) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	updated, err := os.ReadFile(jaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `Bob\'s app`) {
		t.Fatalf("expected escaped apostrophe in:\n%s", string(updated))
	}
	if !strings.Contains(string(updated), `<string name="new_key">新規</string>`) {
		t.Fatalf("expected appended key in:\n%s", string(updated))
	}
}

func TestResXRoundTrip(t *testing.T) {
	a, ok := GetAdapter(FormatResX)
	if !ok {
		t.Fatalf("FormatResX adapter not registered")
	}

	resxRaw := `<?xml version="1.0" encoding="utf-8"?>
<root>
  <xsd:schema id="root" xmlns="" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
    <xsd:element name="root" />
  </xsd:schema>
  <data name="IconBytes" type="System.Drawing.Bitmap, System.Drawing" mimetype="application/x-microsoft.net.object.binary.base64">
    <value>AAAA</value>
  </data>
  <data name="Title" xml:space="preserve">
    <value>Old Title</value>
    <comment>Main window title</comment>
  </data>
</root>
`
	tmpl, err := a.ParseDoc("Resources.resx", []byte(resxRaw), "", locale.DefaultLocales())
	if err != nil {
		t.Fatal(err)
	}
	if !tmpl.IsTemplate || len(tmpl.Entries) != 1 {
		t.Fatalf("expected template with 1 translatable entry (binary skipped), got isTemplate=%v entries=%d", tmpl.IsTemplate, len(tmpl.Entries))
	}
	if tmpl.Entries[0].Key.ID != "Title" || tmpl.Entries[0].Key.Comment != "Main window title" {
		t.Fatalf("unexpected entry: %+v", tmpl.Entries[0])
	}

	out, applied, appended, err := a.ApplyPatches(tmpl, []PatchOp{
		{Key: domain.EntryKey{ID: "Title"}, NewText: "New & Improved", Action: domain.ActionOverwrite},
		{Key: domain.EntryKey{ID: "Cancel"}, NewText: "Cancel", Action: domain.ActionAppendNew},
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || appended != 1 {
		t.Fatalf("expected applied=1 appended=1, got %d/%d", applied, appended)
	}
	outStr := string(out)
	if !strings.Contains(outStr, `<xsd:element name="root" />`) {
		t.Fatalf("expected xsd:schema preserved verbatim")
	}
	if !strings.Contains(outStr, `<value>New &amp; Improved</value>`) {
		t.Fatalf("expected patched value, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, `<data name="Cancel" xml:space="preserve">`) {
		t.Fatalf("expected appended data element")
	}
}

func TestXLIFF12And20RoundTrip(t *testing.T) {
	a, ok := GetAdapter(FormatXLIFF)
	if !ok {
		t.Fatalf("FormatXLIFF adapter not registered")
	}

	xlf12 := `<?xml version="1.0" encoding="utf-8"?>
<xliff version="1.2" xmlns="urn:oasis:names:tc:xliff:document:1.2">
  <file original="app" source-language="en" target-language="fr-FR" datatype="plaintext">
    <body>
      <trans-unit id="greet">
        <source>Hello <x id="1"/> &amp; welcome</source>
        <note>Header</note>
      </trans-unit>
      <trans-unit id="bye">
        <source>Bye</source>
        <target state="needs-translation">Au</target>
      </trans-unit>
    </body>
  </file>
</xliff>
`
	doc12, err := a.ParseDoc("fr.xlf", []byte(xlf12), "", locale.DefaultLocales())
	if err != nil {
		t.Fatal(err)
	}
	if doc12.LocaleID != "fr_FR" || len(doc12.Entries) != 2 {
		t.Fatalf("unexpected doc12: loc=%q entries=%d", doc12.LocaleID, len(doc12.Entries))
	}
	if doc12.Entries[0].Key.Source != `Hello <x id="1"/> & welcome` {
		t.Fatalf("inline tag source mismatch: %q", doc12.Entries[0].Key.Source)
	}
	if doc12.Entries[0].Status != domain.StatusMissing || doc12.Entries[1].Status != domain.StatusUnfinished {
		t.Fatalf("status mismatch: %s / %s", doc12.Entries[0].Status, doc12.Entries[1].Status)
	}

	out12, applied, _, err := a.ApplyPatches(doc12, []PatchOp{
		{Key: doc12.Entries[0].Key, NewText: `Bonjour <x id="1"/> & bienvenue`, Action: domain.ActionFillGap},
		{Key: doc12.Entries[1].Key, NewText: `Au revoir`, Action: domain.ActionOverwrite},
	})
	if err != nil || applied != 2 {
		t.Fatalf("ApplyPatches 1.2 failed: applied=%d err=%v", applied, err)
	}
	if !strings.Contains(string(out12), `<target state="translated">Bonjour <x id="1"/> &amp; bienvenue</target>`) {
		t.Fatalf("expected inserted target with preserved <x/> tag, got:\n%s", string(out12))
	}

	xlf20 := `<?xml version="1.0" encoding="utf-8"?>
<xliff version="2.0" srcLang="en" trgLang="de-DE">
  <file id="f1">
    <unit id="u1">
      <segment state="initial">
        <source>Save</source>
        <target></target>
      </segment>
    </unit>
  </file>
</xliff>
`
	doc20, err := a.ParseDoc("de.xliff", []byte(xlf20), "", locale.DefaultLocales())
	if err != nil {
		t.Fatal(err)
	}
	if doc20.LocaleID != "de_DE" || len(doc20.Entries) != 1 {
		t.Fatalf("unexpected doc20: loc=%q entries=%d", doc20.LocaleID, len(doc20.Entries))
	}
	out20, applied20, _, err := a.ApplyPatches(doc20, []PatchOp{
		{Key: doc20.Entries[0].Key, NewText: "Speichern", Action: domain.ActionFillGap},
	})
	if err != nil || applied20 != 1 {
		t.Fatalf("ApplyPatches 2.0 failed: applied=%d err=%v", applied20, err)
	}
	if !strings.Contains(string(out20), `<segment state="translated">`) || !strings.Contains(string(out20), `<target>Speichern</target>`) {
		t.Fatalf("expected 2.0 segment+target updated, got:\n%s", string(out20))
	}
}

func TestLoaderZipRoundTripAndScaffold(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "project.zip")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w1, _ := zw.Create("i18n/Resources.resx")
	_, _ = w1.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><root><data name="Hello" xml:space="preserve"><value>Hello</value></data></root>`))
	w2, _ := zw.Create("README.txt")
	_, _ = w2.Write([]byte("keep me"))
	_ = zw.Close()
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle, err := Load(zipPath, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("Load zip failed: %v", err)
	}
	if !bundle.IsZip || bundle.Format != FormatResX {
		t.Fatalf("unexpected zip bundle: isZip=%v format=%s", bundle.IsZip, bundle.Format)
	}

	// Patch existing template AND scaffold a brand new ja_JP locale file in the updated zip.
	report, err := Save(bundle, map[string][]PatchOp{
		"ja_JP": {
			{Key: domain.EntryKey{ID: "Hello"}, NewText: "こんにちは", Action: domain.ActionFillGap},
		},
	}, locale.DefaultLocales(), "", SaveOptions{})
	if err != nil {
		t.Fatalf("Save zip failed: %v", err)
	}
	if !strings.HasSuffix(report.OutputPath, "_updated.zip") || report.FilesWritten != 1 {
		t.Fatalf("unexpected zip save report: %+v", report)
	}

	outBytes, err := os.ReadFile(report.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(outBytes), int64(len(outBytes)))
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["README.txt"] || !names["i18n/Resources.resx"] || !names["i18n/Resources.ja-JP.resx"] {
		t.Fatalf("missing expected zip entries: %+v", names)
	}
}

func TestXLIFFNewLocaleDocUpdatesTargetLang(t *testing.T) {
	a, ok := GetAdapter(FormatXLIFF)
	if !ok {
		t.Fatal("FormatXLIFF adapter not registered")
	}

	// XLIFF 1.2: update target-language and clear target/state.
	xlf12 := `<?xml version="1.0" encoding="utf-8"?>
<xliff version="1.2" xmlns="urn:oasis:names:tc:xliff:document:1.2">
  <file original="app" source-language="en" target-language="de-DE" datatype="plaintext">
    <body>
      <trans-unit id="greeting">
        <source>Hello</source>
        <target state="translated">Hallo</target>
      </trans-unit>
    </body>
  </file>
</xliff>
`
	doc12, err := a.ParseDoc("de.xlf", []byte(xlf12), "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("ParseDoc 1.2 failed: %v", err)
	}
	new12, err := a.NewLocaleDoc(doc12, locale.LocaleSpec{ID: "fr_FR"}, "fr_FR.xlf")
	if err != nil {
		t.Fatalf("NewLocaleDoc 1.2 failed: %v", err)
	}
	raw12 := string(new12.Raw)
	if !strings.Contains(raw12, `target-language="fr-FR"`) || strings.Contains(raw12, `target-language="de-DE"`) {
		t.Fatalf("expected XLIFF 1.2 target-language updated to fr-FR, got:\n%s", raw12)
	}
	if strings.Contains(raw12, "Hallo") {
		t.Fatalf("expected old target translation cleared, got:\n%s", raw12)
	}

	// XLIFF 2.0: update trgLang and reset <segment state> to "initial".
	xlf20 := `<?xml version="1.0" encoding="utf-8"?>
<xliff version="2.0" srcLang="en" trgLang="de-DE">
  <file id="f1">
    <unit id="u1">
      <segment state="translated">
        <source>Save</source>
        <target>Speichern</target>
      </segment>
    </unit>
  </file>
</xliff>
`
	doc20, err := a.ParseDoc("de.xliff", []byte(xlf20), "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("ParseDoc 2.0 failed: %v", err)
	}
	new20, err := a.NewLocaleDoc(doc20, locale.LocaleSpec{ID: "ja_JP"}, "ja_JP.xliff")
	if err != nil {
		t.Fatalf("NewLocaleDoc 2.0 failed: %v", err)
	}
	raw20 := string(new20.Raw)
	if !strings.Contains(raw20, `trgLang="ja-JP"`) || strings.Contains(raw20, `trgLang="de-DE"`) {
		t.Fatalf("expected XLIFF 2.0 trgLang updated to ja-JP, got:\n%s", raw20)
	}
	if !strings.Contains(raw20, `<segment state="initial">`) || strings.Contains(raw20, "Speichern") {
		t.Fatalf("expected XLIFF 2.0 segment reset to initial and target cleared, got:\n%s", raw20)
	}
}

func TestPONewLocaleDocUpdatesLanguageHeader(t *testing.T) {
	a, ok := GetAdapter(FormatGettextPO)
	if !ok {
		t.Fatal("FormatGettextPO adapter not registered")
	}

	// Case 1: Existing PO file with Language header.
	poRaw := strings.Join([]string{
		`msgid ""`,
		`msgstr ""`,
		`"Content-Type: text/plain; charset=UTF-8\n"`,
		`"Language: ja_JP\n"`,
		``,
		`#, fuzzy`,
		`msgid "Save"`,
		`msgstr "保存"`,
		``,
	}, "\n")
	docPO, err := a.ParseDoc("ja_JP.po", []byte(poRaw), "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("ParseDoc PO failed: %v", err)
	}
	newPO, err := a.NewLocaleDoc(docPO, locale.LocaleSpec{ID: "fr_FR"}, "fr_FR.po")
	if err != nil {
		t.Fatalf("NewLocaleDoc PO failed: %v", err)
	}
	gotPO := string(newPO.Raw)
	if !strings.Contains(gotPO, `"Language: fr_FR\n"`) || strings.Contains(gotPO, "ja_JP") {
		t.Fatalf("expected Language header updated to fr_FR, got:\n%s", gotPO)
	}
	if strings.Contains(gotPO, "保存") || strings.Contains(gotPO, "fuzzy") {
		t.Fatalf("expected msgstr and fuzzy cleared, got:\n%s", gotPO)
	}

	// Case 2: Headerless .pot template gets a minimal Language header inserted.
	potRaw := "msgid \"Hello\"\nmsgstr \"\"\n"
	docPOT, err := a.ParseDoc("messages.pot", []byte(potRaw), "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("ParseDoc POT failed: %v", err)
	}
	newFromPOT, err := a.NewLocaleDoc(docPOT, locale.LocaleSpec{ID: "de_DE"}, "de_DE.po")
	if err != nil {
		t.Fatalf("NewLocaleDoc from POT failed: %v", err)
	}
	if !strings.Contains(string(newFromPOT.Raw), `"Language: de_DE\n"`) || newFromPOT.LocaleID != "de_DE" {
		t.Fatalf("expected Language header de_DE inserted from POT, got LocaleID=%q raw:\n%s", newFromPOT.LocaleID, string(newFromPOT.Raw))
	}
}

func TestZipPathTraversalAndSizeGuard(t *testing.T) {
	dir := t.TempDir()

	// Mixed zip containing a traversal entry (../evil.json) must fail immediately.
	mixedZip := filepath.Join(dir, "mixed.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	evilW, _ := zw.Create("../evil.json")
	_, _ = evilW.Write([]byte(`{"evil":"1"}`))
	safeW, _ := zw.Create("en.json")
	_, _ = safeW.Write([]byte(`{"hello":"Hello"}`))
	_ = zw.Close()
	if err := os.WriteFile(mixedZip, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(mixedZip, "", locale.DefaultLocales()); err == nil || !strings.Contains(err.Error(), "unsafe zip entry") {
		t.Fatalf("expected unsafe zip entry error on ../evil.json, got err=%v", err)
	}

	// Direct drive-letter path in zip also rejected cleanly.
	driveZip := filepath.Join(dir, "drive.zip")
	buf.Reset()
	zw2 := zip.NewWriter(&buf)
	dw, _ := zw2.Create("C:evil.json")
	_, _ = dw.Write([]byte(`{"evil":"1"}`))
	_ = zw2.Close()
	if err := os.WriteFile(driveZip, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(driveZip, "", locale.DefaultLocales()); err == nil || !strings.Contains(err.Error(), "unsafe zip entry") {
		t.Fatalf("expected unsafe zip entry error on drive-qualified entry, got err=%v", err)
	}
}

func TestAndroidXMLBackslashRoundTrip(t *testing.T) {
	a, ok := GetAdapter(FormatAndroidXML)
	if !ok {
		t.Fatal("FormatAndroidXML adapter not registered")
	}
	raw := `<?xml version="1.0" encoding="utf-8"?>
<resources>
    <string name="path">old</string>
</resources>
`
	doc, err := a.ParseDoc("res/values-ja/strings.xml", []byte(raw), "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("ParseDoc failed: %v", err)
	}
	want := `C:\Users\new\tab`
	out, applied, _, err := a.ApplyPatches(doc, []PatchOp{
		{Key: domain.EntryKey{ID: "path"}, NewText: want, Action: domain.ActionOverwrite},
	})
	if err != nil || applied != 1 {
		t.Fatalf("ApplyPatches failed: applied=%d err=%v", applied, err)
	}
	if !strings.Contains(string(out), `C:\\Users\\new\\tab`) {
		t.Fatalf("expected backslashes escaped in output:\n%s", string(out))
	}
	reparsed, err := a.ParseDoc("res/values-ja/strings.xml", out, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	if len(reparsed.Entries) != 1 || reparsed.Entries[0].Text != want {
		t.Fatalf("backslash round-trip mismatch: got %q, want %q", reparsed.Entries[0].Text, want)
	}
}

func TestSaveOutputFolderCompleteAndCentralizedBackups(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "ts")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	enRaw := `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context><name>App</name><message><source>Save</source><translation>保存</translation></message></context>
</TS>
`
	jaRaw := `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context><name>App</name><message><source>Save</source><translation type="unfinished"></translation></message></context>
</TS>
`
	if err := os.WriteFile(filepath.Join(srcDir, "zh_CN.ts"), []byte(enRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "ja_JP.ts"), []byte(jaRaw), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle, err := Load(srcDir, "{id}.ts", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	outDir := DefaultOutputPath(bundle)
	if outDir != srcDir+"_updated" {
		t.Fatalf("DefaultOutputPath = %q, want %q", outDir, srcDir+"_updated")
	}

	// Save into separate folder: only ja_JP is patched, zh_CN must still be copied untouched.
	patches := map[string][]PatchOp{
		"ja_JP": {
			{Key: domain.EntryKey{Context: "App", Source: "Save"}, NewText: "保存する", Action: domain.ActionFillGap},
		},
	}
	rep, err := Save(bundle, patches, locale.DefaultLocales(), "{id}.ts", SaveOptions{
		CreateBackup: true,
		OutputPath:   outDir,
	})
	if err != nil {
		t.Fatalf("Save to new folder failed: %v", err)
	}
	if rep.FilesWritten != 1 || rep.OutputPath != outDir {
		t.Fatalf("unexpected report: %+v", rep)
	}
	// Originals untouched.
	origJA, _ := os.ReadFile(filepath.Join(srcDir, "ja_JP.ts"))
	if strings.Contains(string(origJA), "保存する") {
		t.Fatalf("original file should not have been modified")
	}
	// Complete copy in outDir (both zh_CN.ts and ja_JP.ts exist).
	if _, err := os.Stat(filepath.Join(outDir, "zh_CN.ts")); err != nil {
		t.Fatalf("untouched zh_CN.ts missing from output folder: %v", err)
	}
	updatedJA, err := os.ReadFile(filepath.Join(outDir, "ja_JP.ts"))
	if err != nil || !strings.Contains(string(updatedJA), "保存する") {
		t.Fatalf("updated ja_JP.ts missing or unpatched: %v / %s", err, string(updatedJA))
	}

	// Second save into the same outDir with CreateBackup=true must put backup inside .locloom_backup/<ts>/,
	// never scatter ja_JP.ts.bak beside ja_JP.ts, and Load(outDir) must ignore .locloom_backup.
	patches2 := map[string][]PatchOp{
		"ja_JP": {
			{Key: domain.EntryKey{Context: "App", Source: "Save"}, NewText: "保存完了", Action: domain.ActionOverwrite},
		},
	}
	rep2, err := Save(bundle, patches2, locale.DefaultLocales(), "{id}.ts", SaveOptions{
		CreateBackup: true,
		OutputPath:   outDir,
	})
	if err != nil {
		t.Fatalf("second Save failed: %v", err)
	}
	if rep2.BackupDir == "" || !strings.Contains(rep2.BackupDir, BackupDirName) || len(rep2.BackupPaths) != 1 {
		t.Fatalf("expected centralized backup inside %s, got %+v", BackupDirName, rep2)
	}
	if _, err := os.Stat(filepath.Join(outDir, "ja_JP.ts.bak")); err == nil {
		t.Fatalf("legacy scattered .bak file should not exist")
	}
	reloaded, err := Load(outDir, "{id}.ts", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("Load(outDir) failed: %v", err)
	}
	if len(reloaded.Docs) != 2 {
		t.Fatalf("Load(outDir) should skip %s and find 2 docs, got %d", BackupDirName, len(reloaded.Docs))
	}

	// Single-file Android XML DefaultOutputPath must preserve values-<lang> parent folder
	// and reuse it without chaining _updated_updated.
	androidBundle := &Bundle{
		Format:       FormatAndroidXML,
		SourcePath:   filepath.Join(dir, "res", "values-ja", "strings.xml"),
		IsSingleFile: true,
	}
	wantAndroidOut := filepath.Join(dir, "res", "values-ja_updated", "values-ja", "strings.xml")
	if got := DefaultOutputPath(androidBundle); got != wantAndroidOut {
		t.Fatalf("Android single-file DefaultOutputPath = %q, want %q", got, wantAndroidOut)
	}
	androidBundle.SourcePath = wantAndroidOut
	if got := DefaultOutputPath(androidBundle); got != wantAndroidOut {
		t.Fatalf("Android single-file repeated DefaultOutputPath = %q, want %q", got, wantAndroidOut)
	}
}
