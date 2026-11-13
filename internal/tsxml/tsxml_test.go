package tsxml

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func TestParseEntitiesAndByteTagsNoDoubleUnescape(t *testing.T) {
	raw := "\xEF\xBB\xBF" + `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
<context>
    <name>MainWindow</name>
    <message>
        <location filename="mainwindow.cpp" line="42"/>
        <source>A &amp; B &amp;lt;tag&amp;gt;<byte value="x7"/></source>
        <comment>menu item</comment>
        <extracomment>shown in File menu</extracomment>
        <translation type="unfinished"></translation>
    </message>
    <message numerus="yes">
        <source>%n file(s) selected</source>
        <translation>
            <numerusform>%n 個のファイルを選択</numerusform>
        </translation>
    </message>
</context>
</TS>
`
	tf, err := ParseBytes("ja_JP.ts", []byte(raw))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if !tf.HasBOM {
		t.Fatal("expected HasBOM = true")
	}
	if tf.Language != "ja_JP" || tf.SourceLang != "en_US" {
		t.Fatalf("unexpected langs: %q / %q", tf.Language, tf.SourceLang)
	}
	if len(tf.Contexts) != 1 || len(tf.Contexts[0].Messages) != 2 {
		t.Fatalf("unexpected contexts/messages: %+v", tf.Contexts)
	}
	m0 := tf.Contexts[0].Messages[0]
	wantSrc := "A & B &lt;tag&gt;\x07"
	if m0.Source != wantSrc {
		t.Fatalf("Source = %q, want %q (double unescape bug?)", m0.Source, wantSrc)
	}
	if m0.Comment != "menu item" || m0.ExtraComment != "shown in File menu" {
		t.Fatalf("unexpected comments: %q / %q", m0.Comment, m0.ExtraComment)
	}
	if m0.Status != domain.StatusUnfinished {
		t.Fatalf("Status = %v, want unfinished", m0.Status)
	}

	m1 := tf.Contexts[0].Messages[1]
	if !m1.Numerus || len(m1.NumerusForms) != 1 || m1.Status != domain.StatusFinished {
		t.Fatalf("unexpected numerus message: %+v", m1)
	}
}

func TestParseUTF16BOMAndCRLF(t *testing.T) {
	xmlText := "<?xml version=\"1.0\" encoding=\"utf-16\"?>\r\n<TS version=\"2.1\" language=\"de_DE\">\r\n<context>\r\n    <name>SettingsDialog</name>\r\n    <message>\r\n        <source>Save</source>\r\n        <translation>Speichern</translation>\r\n    </message>\r\n</context>\r\n</TS>\r\n"
	encoded := utf16.Encode([]rune(xmlText))
	buf := make([]byte, 2+len(encoded)*2)
	buf[0], buf[1] = 0xFF, 0xFE
	for i, u := range encoded {
		binary.LittleEndian.PutUint16(buf[2+i*2:], u)
	}

	tf, err := ParseBytes("de_DE.ts", buf)
	if err != nil {
		t.Fatalf("ParseBytes UTF-16LE failed: %v", err)
	}
	if tf.LineEnding != "\r\n" {
		t.Fatalf("LineEnding = %q, want \\r\\n", tf.LineEnding)
	}
	if len(tf.Contexts) != 1 || tf.Contexts[0].Messages[0].Translation != "Speichern" {
		t.Fatalf("unexpected parsed UTF-16 content: %+v", tf.Contexts)
	}
}

func TestApplyPatchesStrictContextAndAppendAndNumerus(t *testing.T) {
	raw := `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message>
        <source>Open</source>
        <comment>file</comment>
        <translation type="unfinished"></translation>
    </message>
    <message>
        <source>Open</source>
        <comment>network</comment>
        <translation type="unfinished"></translation>
    </message>
    <message numerus="yes">
        <source>%n item(s)</source>
        <translation type="unfinished">
            <numerusform></numerusform>
        </translation>
    </message>
</context>
<context>
    <name>SettingsDialog</name>
    <message>
        <source>Save</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>
`
	tf, err := ParseBytes("ja_JP.ts", []byte(raw))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	patches := []PatchItem{
		// Same source, different comments in MainWindow
		{
			Key:     domain.EntryKey{Context: "MainWindow", Source: "Open", Comment: "file"},
			NewText: "開く (ファイル)",
			Action:  domain.ActionFillGap,
		},
		// Cross-context should NOT match MainWindow's "Save" (doesn't exist in MainWindow)
		{
			Key:     domain.EntryKey{Context: "MainWindow", Source: "Save"},
			NewText: "WRONG",
			Action:  domain.ActionFillGap,
		},
		// Numerus update
		{
			Key:          domain.EntryKey{Context: "MainWindow", Source: "%n item(s)"},
			NumerusForms: []string{"%n 個の項目"},
			Action:       domain.ActionFillGap,
			Numerus:      true,
		},
		// Append into existing context
		{
			Key:     domain.EntryKey{Context: "SettingsDialog", Source: "Cancel"},
			NewText: "キャンセル",
			Action:  domain.ActionAppendNew,
		},
		// Append into brand new context
		{
			Key:     domain.EntryKey{Context: "FileManager", Source: "Delete\x07"},
			NewText: "削除",
			Action:  domain.ActionAppendNew,
		},
	}

	res, err := ApplyPatches(tf, patches)
	if err != nil {
		t.Fatalf("ApplyPatches failed: %v", err)
	}
	if res.Applied != 2 || res.Appended != 2 || res.Skipped != 1 {
		t.Fatalf("unexpected counts: applied=%d appended=%d skipped=%d", res.Applied, res.Appended, res.Skipped)
	}

	outStr := string(res.OutputRaw)
	if strings.Contains(outStr, "WRONG") {
		t.Fatal("cross-context fallback occurred when it must not")
	}
	if !strings.Contains(outStr, `<byte value="x7"/>`) {
		t.Fatal("expected control byte <byte value=\"x7\"/> in appended source")
	}

	// Re-parse patched output to verify round-trip
	tf2, err := ParseBytes("ja_JP.ts", res.OutputRaw)
	if err != nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	if len(tf2.Contexts) != 3 {
		t.Fatalf("expected 3 contexts after new context append, got %d", len(tf2.Contexts))
	}
}

func TestCloneSkeletonAndNewEmptyTSFile(t *testing.T) {
	raw := `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN" sourcelanguage="en_US">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save changes to %1?</source>
        <translation>保存对 %1 的更改？</translation>
    </message>
</context>
</TS>
`
	tmpl, err := ParseBytes("zh_CN.ts", []byte(raw))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	target := locale.LocaleSpec{ID: "fr_FR"}
	cloned, err := CloneSkeleton(tmpl, target, "fr_FR.ts")
	if err != nil {
		t.Fatalf("CloneSkeleton failed: %v", err)
	}
	if cloned.Language != "fr_FR" || cloned.SourceLang != "en_US" {
		t.Fatalf("unexpected cloned header langs: %q / %q", cloned.Language, cloned.SourceLang)
	}
	if cloned.Contexts[0].Messages[0].Status != domain.StatusUnfinished || cloned.Contexts[0].Messages[0].Translation != "" {
		t.Fatalf("expected unfinished empty translation, got %+v", cloned.Contexts[0].Messages[0])
	}

	empty := NewEmptyTSFile(locale.LocaleSpec{ID: "es_ES"}, "es_ES.ts", "\n")
	if empty == nil || empty.Language != "es_ES" || empty.CloseTagStart == 0 {
		t.Fatalf("unexpected NewEmptyTSFile: %+v", empty)
	}
}

func TestLoadAndSaveZipBundleWithWindowsSeparatorsAndExtras(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "locales.zip")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	tsEntry, _ := zw.Create(`sub\ja_JP.ts`)
	_, _ = tsEntry.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><TS version="2.1" language="ja_JP"><context><name>App</name><message><source>OK</source><translation type="unfinished"></translation></message></context></TS>`))
	extraEntry, _ := zw.Create("README.txt")
	_, _ = extraEntry.Write([]byte("keep me"))
	_ = zw.Close()

	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	bundle, err := LoadBundle(zipPath, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("LoadBundle zip failed: %v", err)
	}
	if !bundle.IsZip || len(bundle.Files) != 1 || bundle.Files[0].RelPath != "sub/ja_JP.ts" {
		t.Fatalf("unexpected bundle files: %+v", bundle.Files)
	}
	if string(bundle.ZipExtras["README.txt"]) != "keep me" {
		t.Fatalf("missing ZipExtras")
	}

	patchRes, err := ApplyPatches(bundle.Files[0], []PatchItem{
		{Key: domain.EntryKey{Context: "App", Source: "OK"}, NewText: "はい", Action: domain.ActionFillGap},
	})
	if err != nil {
		t.Fatalf("ApplyPatches failed: %v", err)
	}

	report, err := SaveBundle(bundle, map[string][]byte{bundle.Files[0].RelPath: patchRes.OutputRaw}, SaveOptions{})
	if err != nil {
		t.Fatalf("SaveBundle zip failed: %v", err)
	}
	if !strings.HasSuffix(report.OutputPath, "_updated.zip") || report.FilesWritten != 1 {
		t.Fatalf("unexpected SaveReport: %+v", report)
	}

	reloaded, err := LoadBundle(report.OutputPath, "", locale.DefaultLocales())
	if err != nil {
		t.Fatalf("reload updated zip failed: %v", err)
	}
	if reloaded.Files[0].Contexts[0].Messages[0].Translation != "はい" {
		t.Fatalf("unexpected reloaded translation: %q", reloaded.Files[0].Contexts[0].Messages[0].Translation)
	}
	if string(reloaded.ZipExtras["README.txt"]) != "keep me" {
		t.Fatal("ZipExtras lost on SaveBundle")
	}
}

func TestApplyPatchesCRLFMultilineNoBareLF(t *testing.T) {
	raw := strings.Join([]string{
		`<?xml version="1.0" encoding="utf-8"?>`,
		`<!DOCTYPE TS>`,
		`<TS version="2.1" language="ja_JP">`,
		`<context>`,
		`    <name>MainWindow</name>`,
		`    <message>`,
		`        <source>Hello</source>`,
		`        <translation type="unfinished"></translation>`,
		`    </message>`,
		`    <message numerus="yes">`,
		`        <source>%n items</source>`,
		`        <translation type="unfinished">`,
		`            <numerusform></numerusform>`,
		`        </translation>`,
		`    </message>`,
		`</context>`,
		`</TS>`,
		``,
	}, "\r\n")

	tf, err := ParseBytes("ja_JP.ts", []byte(raw))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if tf.LineEnding != "\r\n" {
		t.Fatalf("expected CRLF line ending, got %q", tf.LineEnding)
	}

	res, err := ApplyPatches(tf, []PatchItem{
		{
			Key:     domain.EntryKey{Context: "MainWindow", Source: "Hello"},
			NewText: "Line 1\nLine 2\r\nLine 3",
			Action:  domain.ActionFillGap,
		},
		{
			Key:          domain.EntryKey{Context: "MainWindow", Source: "%n items"},
			Numerus:      true,
			NumerusForms: []string{"%n item\nfirst line", "%n items\nsecond line"},
			Action:       domain.ActionFillGap,
		},
		{
			Key:     domain.EntryKey{Context: "MainWindow", Source: "Multi\nSource", Comment: "Multi\nComment"},
			NewText: "Appended\nLine 2",
			Action:  domain.ActionAppendNew,
		},
	})
	if err != nil {
		t.Fatalf("ApplyPatches failed: %v", err)
	}
	if !bytes.Contains(res.OutputRaw, []byte("\r\n")) {
		t.Fatal("expected CRLF in OutputRaw")
	}
	stripped := bytes.ReplaceAll(res.OutputRaw, []byte("\r\n"), nil)
	if bytes.Contains(stripped, []byte("\n")) {
		t.Fatalf("found bare LF in CRLF-patched output:\n%q", string(res.OutputRaw))
	}
}
