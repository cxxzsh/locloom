package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"

	"locloom/internal/app"
	"locloom/internal/config"
	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/planner"
	"locloom/internal/provider"
	"locloom/internal/providers"
	"locloom/internal/sheet"
	"locloom/internal/tsxml"
)

// TestSurgicalTSXMLRoundTrip verifies UTF-8 BOM + CRLF preservation, XML comments,
// <location> tags, entity safety (no double-unescape), <byte value="xa"/> control chars,
// <numerusform> plural round-trips, and CloneSkeleton header fidelity.
func TestSurgicalTSXMLRoundTrip(t *testing.T) {
	utf8BOM := "\xEF\xBB\xBF"
	rawLF := utf8BOM + `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
<context>
    <name>MainWindow</name>
    <!-- Navigation strings -->
    <message>
        <location filename="../ui/mainwindow.cpp" line="42"/>
        <source>Save &amp; Exit</source>
        <translation type="unfinished"></translation>
    </message>
    <message>
        <source>Line 1<byte value="xa"/>Line 2</source>
        <translation type="unfinished"></translation>
    </message>
    <message numerus="yes">
        <source>%n file(s) selected</source>
        <translation type="unfinished">
            <numerusform></numerusform>
        </translation>
    </message>
</context>
</TS>
`
	rawCRLF := strings.ReplaceAll(rawLF, "\n", "\r\n")

	tf, err := tsxml.ParseBytes("ja_JP.ts", []byte(rawCRLF))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if !tf.HasBOM {
		t.Fatalf("expected HasBOM=true")
	}
	if tf.LineEnding != "\r\n" {
		t.Fatalf("expected LineEnding=\\r\\n, got %q", tf.LineEnding)
	}
	if tf.SourceLang != "en_US" || tf.Language != "ja_JP" {
		t.Fatalf("unexpected header langs: language=%q sourcelanguage=%q", tf.Language, tf.SourceLang)
	}

	patches := []tsxml.PatchItem{
		{
			Key:     domain.EntryKey{Context: "MainWindow", Source: "Save & Exit"},
			NewText: "保存 & 終了 <v2>",
			Action:  domain.ActionFillGap,
		},
		{
			Key:     domain.EntryKey{Context: "MainWindow", Source: "Line 1\nLine 2"},
			NewText: "1行目\n2行目",
			Action:  domain.ActionFillGap,
		},
		{
			Key:          domain.EntryKey{Context: "MainWindow", Source: "%n file(s) selected"},
			Numerus:      true,
			NumerusForms: []string{"%n 件のファイルを選択中"},
			Action:       domain.ActionFillGap,
		},
	}

	res, err := tsxml.ApplyPatches(tf, patches)
	if err != nil {
		t.Fatalf("ApplyPatches failed: %v", err)
	}
	if res.Applied != 3 {
		t.Fatalf("expected Applied=3, got %d", res.Applied)
	}

	out := res.OutputRaw
	if !bytes.HasPrefix(out, []byte(utf8BOM)) {
		t.Fatalf("output lost UTF-8 BOM")
	}
	outNoBOM := bytes.TrimPrefix(out, []byte(utf8BOM))
	// Every \n must be preceded by \r (CRLF preservation).
	for i := range outNoBOM {
		if outNoBOM[i] == '\n' && (i == 0 || outNoBOM[i-1] != '\r') {
			t.Fatalf("found bare LF at byte %d after CRLF round-trip", i)
		}
	}

	outStr := string(outNoBOM)
	for _, want := range []string{
		`<!-- Navigation strings -->`,
		`<location filename="../ui/mainwindow.cpp" line="42"/>`,
		`<source>Save &amp; Exit</source>`,
		`<translation>保存 &amp; 終了 &lt;v2&gt;</translation>`,
		`<byte value="xa"/>`,
		`<numerusform>%n 件のファイルを選択中</numerusform>`,
	} {
		if !strings.Contains(outStr, want) {
			t.Fatalf("patched output missing %q\n--- OUTPUT ---\n%s", want, outStr)
		}
	}
	if strings.Contains(outStr, "&amp;amp;") {
		t.Fatalf("detected double-escaped &amp;amp; in output:\n%s", outStr)
	}

	// Validate XML syntax via encoding/xml.
	dec := xml.NewDecoder(bytes.NewReader(outNoBOM))
	for {
		if _, err := dec.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("output failed XML validation: %v", err)
		}
	}

	// Verify CloneSkeleton preserves version="2.1" and sourcelanguage="en_US".
	cloned, err := tsxml.CloneSkeleton(tf, locale.LocaleSpec{ID: "fr_FR", Name: "French"}, "fr_FR.ts")
	if err != nil {
		t.Fatalf("CloneSkeleton failed: %v", err)
	}
	if cloned.Language != "fr_FR" || cloned.SourceLang != "en_US" {
		t.Fatalf("unexpected cloned langs: language=%q sourcelanguage=%q", cloned.Language, cloned.SourceLang)
	}
	clonedStr := string(cloned.Raw)
	if !strings.Contains(clonedStr, `version="2.1"`) || !strings.Contains(clonedStr, `sourcelanguage="en_US"`) || !strings.Contains(clonedStr, `language="fr_FR"`) {
		t.Fatalf("CloneSkeleton did not preserve TS header attributes:\n%s", clonedStr)
	}
}

// TestMultiContextDisambiguationAndPhantomProtection verifies that identical <source>
// strings in distinct <context> blocks receive distinct translations without leakage,
// and Excel-only rows do not pollute .ts plans unless AppendMissing is enabled
// (including appending into an empty <TS> file).
func TestMultiContextDisambiguationAndPhantomProtection(t *testing.T) {
	dir := t.TempDir()
	jaPath := filepath.Join(dir, "ja_JP.ts")
	tsContent := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
<context>
    <name>FileMenu</name>
    <message>
        <source>Open</source>
        <translation type="unfinished"></translation>
    </message>
</context>
<context>
    <name>ConnectionState</name>
    <message>
        <source>Open</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>
`
	if err := os.WriteFile(jaPath, []byte(tsContent), 0o644); err != nil {
		t.Fatalf("write ja_JP.ts: %v", err)
	}

	locs := []locale.LocaleSpec{{ID: "ja_JP", Name: "Japanese", Enabled: true, EngineCode: "ja"}}
	bundle, err := tsxml.LoadBundle(dir, "{id}.ts", locs)
	if err != nil {
		t.Fatalf("LoadBundle failed: %v", err)
	}

	cat := domain.NewCatalog()
	cat.Set("ja_JP", domain.EntryKey{Context: "FileMenu", Source: "Open"}, "開く", domain.OriginXLSX)
	cat.Set("ja_JP", domain.EntryKey{Context: "ConnectionState", Source: "Open"}, "接続中", domain.OriginXLSX)
	phantomKey := domain.EntryKey{Context: "SettingsDialog", Source: "Apply"}
	cat.Set("ja_JP", phantomKey, "適用", domain.OriginXLSX)

	// AppendMissing == false: phantomKey must not appear in plan.
	planOff := planner.BuildPlan(planner.PlanInput{
		Bundle:        bundle,
		ExtraEntries:  []domain.EntryKey{phantomKey},
		DefaultCtx:    "FileMenu",
		Catalog:       cat,
		Locales:       locs,
		WriteMode:     domain.ModeFillUnfinished,
		AppendMissing: false,
	})
	if !planOff.Summary.MultiContext {
		t.Fatalf("expected MultiContext=true")
	}
	if len(planOff.Rows) != 2 {
		t.Fatalf("expected 2 rows with AppendMissing=false, got %d", len(planOff.Rows))
	}
	gotByCtx := make(map[string]string)
	for _, r := range planOff.Rows {
		if r.Key.Source == "Apply" {
			t.Fatalf("phantom row leaked into plan when AppendMissing=false")
		}
		gotByCtx[r.Key.Context] = r.Cells["ja_JP"].NewText
	}
	if gotByCtx["FileMenu"] != "開く" || gotByCtx["ConnectionState"] != "接続中" {
		t.Fatalf("context disambiguation failed: %+v", gotByCtx)
	}

	// AppendMissing == true: phantomKey is appended.
	planOn := planner.BuildPlan(planner.PlanInput{
		Bundle:        bundle,
		ExtraEntries:  []domain.EntryKey{phantomKey},
		DefaultCtx:    "SettingsDialog",
		Catalog:       cat,
		Locales:       locs,
		WriteMode:     domain.ModeFillUnfinished,
		AppendMissing: true,
	})
	if len(planOn.Rows) != 3 {
		t.Fatalf("expected 3 rows with AppendMissing=true, got %d", len(planOn.Rows))
	}
	var foundAppend bool
	for _, r := range planOn.Rows {
		if r.Key.Context == "SettingsDialog" && r.Key.Source == "Apply" {
			if r.Cells["ja_JP"].Action != domain.ActionAppendNew {
				t.Fatalf("expected ActionAppendNew, got %v", r.Cells["ja_JP"].Action)
			}
			foundAppend = true
		}
	}
	if !foundAppend {
		t.Fatalf("expected appended row for SettingsDialog/Apply")
	}

	// Verify appending into an empty <TS> file works cleanly.
	emptyDir := t.TempDir()
	emptyPath := filepath.Join(emptyDir, "ja_JP.ts")
	emptyTS := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
</TS>
`
	if err := os.WriteFile(emptyPath, []byte(emptyTS), 0o644); err != nil {
		t.Fatalf("write empty ja_JP.ts: %v", err)
	}
	emptyBundle, err := tsxml.LoadBundle(emptyDir, "{id}.ts", locs)
	if err != nil {
		t.Fatalf("LoadBundle empty failed: %v", err)
	}
	emptyPlan := planner.BuildPlan(planner.PlanInput{
		Bundle:        emptyBundle,
		ExtraEntries:  []domain.EntryKey{phantomKey},
		DefaultCtx:    "SettingsDialog",
		Catalog:       cat,
		Locales:       locs,
		WriteMode:     domain.ModeFillUnfinished,
		AppendMissing: true,
	})
	patches := planner.BuildPatchesForFile(emptyPlan, emptyBundle.Files[0], "ja_JP")
	res, err := tsxml.ApplyPatches(emptyBundle.Files[0], patches)
	if err != nil {
		t.Fatalf("ApplyPatches to empty TS failed: %v", err)
	}
	if res.Appended != 1 || !strings.Contains(string(res.OutputRaw), "<name>SettingsDialog</name>") || !strings.Contains(string(res.OutputRaw), "<translation>適用</translation>") {
		t.Fatalf("unexpected empty TS append result (appended=%d):\n%s", res.Appended, string(res.OutputRaw))
	}
}

// TestZIPSubdirectoryAndExtraFileFidelity verifies nested subdirectory relative paths
// and verbatim preservation of non-.ts entries in *_updated.zip.
func TestZIPSubdirectoryAndExtraFileFidelity(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "translations.zip")

	makeTS := func(ctx, src string) string {
		return `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
<context>
    <name>` + ctx + `</name>
    <message>
        <source>` + src + `</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>
`
	}

	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(zf)
	entries := map[string]string{
		"i18n/module_a/ja_JP.ts": makeTS("ModuleA", "Export"),
		"i18n/module_b/ja_JP.ts": makeTS("ModuleB", "Import"),
		"README.txt":             "Bundle metadata\n",
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	zf.Close()

	locs := []locale.LocaleSpec{{ID: "ja_JP", Name: "Japanese", Enabled: true, EngineCode: "ja"}}
	bundle, err := tsxml.LoadBundle(zipPath, "{id}.ts", locs)
	if err != nil {
		t.Fatalf("LoadBundle zip failed: %v", err)
	}
	if !bundle.IsZip || len(bundle.Files) != 2 {
		t.Fatalf("expected zip bundle with 2 .ts files, got isZip=%v files=%d", bundle.IsZip, len(bundle.Files))
	}
	if string(bundle.ZipExtras["README.txt"]) != "Bundle metadata\n" {
		t.Fatalf("unexpected ZipExtras[README.txt]: %q", string(bundle.ZipExtras["README.txt"]))
	}

	patched := make(map[string][]byte)
	for _, tf := range bundle.Files {
		var p tsxml.PatchItem
		if strings.Contains(tf.RelPath, "module_a") {
			p = tsxml.PatchItem{Key: domain.EntryKey{Context: "ModuleA", Source: "Export"}, NewText: "エクスポート", Action: domain.ActionFillGap}
		} else {
			p = tsxml.PatchItem{Key: domain.EntryKey{Context: "ModuleB", Source: "Import"}, NewText: "インポート", Action: domain.ActionFillGap}
		}
		res, err := tsxml.ApplyPatches(tf, []tsxml.PatchItem{p})
		if err != nil {
			t.Fatalf("ApplyPatches %s: %v", tf.RelPath, err)
		}
		patched[tf.RelPath] = res.OutputRaw
	}

	report, err := tsxml.SaveBundle(bundle, patched, tsxml.SaveOptions{})
	if err != nil {
		t.Fatalf("SaveBundle zip failed: %v", err)
	}
	if !strings.HasSuffix(report.OutputPath, "_updated.zip") {
		t.Fatalf("expected *_updated.zip output, got %q", report.OutputPath)
	}

	zr, err := zip.OpenReader(report.OutputPath)
	if err != nil {
		t.Fatalf("open updated zip: %v", err)
	}
	defer zr.Close()

	gotFiles := make(map[string]string)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		gotFiles[f.Name] = string(b)
	}

	if gotFiles["README.txt"] != "Bundle metadata\n" {
		t.Fatalf("README.txt not preserved verbatim: %q", gotFiles["README.txt"])
	}
	if !strings.Contains(gotFiles["i18n/module_a/ja_JP.ts"], "<translation>エクスポート</translation>") {
		t.Fatalf("module_a translation missing in updated zip")
	}
	if !strings.Contains(gotFiles["i18n/module_b/ja_JP.ts"], "<translation>インポート</translation>") {
		t.Fatalf("module_b translation missing in updated zip")
	}
}

// TestEndToEndWaterfallExcelAndHeadlessUI exercises the 4-stage waterfall
// (XLSX -> TS TM -> mock OpenAI-compat LLM with placeholder masking & cache hits),
// .xlsx export/re-import round-trip, and headless UI RGBA frame rendering.
func TestEndToEndWaterfallExcelAndHeadlessUI(t *testing.T) {
	dir := t.TempDir()

	// 1. Placeholder guard round-trip check.
	masked := provider.MaskPlaceholders(`Save changes to "%1"?` + "\n" + `%2 remaining`)
	if strings.Contains(masked.Masked, "%1") || strings.Contains(masked.Masked, "%2") {
		t.Fatalf("expected placeholders masked, got %q", masked.Masked)
	}
	restored := provider.RestorePlaceholders(`"[[0]]" への変更を保存しますか?[[1]]残り [[ 2 ]]`, masked)
	if !strings.Contains(restored, "%1") || !strings.Contains(restored, "%2") || !strings.Contains(restored, "\n") {
		t.Fatalf("placeholder restore failed: %q", restored)
	}
	if warns := domain.ValidateTranslation(`Save changes to "%1"?`+"\n"+`%2 remaining`, restored, false); len(warns) != 0 {
		t.Fatalf("unexpected validation warnings: %v", warns)
	}

	// 2. Prepare target .ts directory, TM .ts directory, and .xlsx glossary.
	targetDir := filepath.Join(dir, "target_ts")
	tmDir := filepath.Join(dir, "tm_ts")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targetTS := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
<context>
    <name>FileManager</name>
    <message>
        <source>Glossary Hit</source>
        <translation type="unfinished"></translation>
    </message>
    <message>
        <source>TM Hit</source>
        <translation type="unfinished"></translation>
    </message>
    <message>
        <source>Save changes to %1?</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>
`
	tmTS := `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE TS>
<TS version="2.1" language="ja_JP" sourcelanguage="en_US">
<context>
    <name>FileManager</name>
    <message>
        <source>TM Hit</source>
        <translation>TM一致</translation>
    </message>
</context>
</TS>
`
	if err := os.WriteFile(filepath.Join(targetDir, "ja_JP.ts"), []byte(targetTS), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmDir, "ja_JP.ts"), []byte(tmTS), 0o644); err != nil {
		t.Fatal(err)
	}

	glossaryXLSX := filepath.Join(dir, "glossary.xlsx")
	if err := sheet.ExportExcel(glossaryXLSX, sheet.ExportOptions{
		IncludeContext: true,
		SheetName:      "Glossary",
		Columns: []sheet.ExportColumn{
			{LocaleID: "ja_JP", Header: "ja_JP"},
		},
		Rows: []sheet.ExportRow{
			{
				Context: "FileManager",
				Source:  "Glossary Hit",
				Cells: map[string]sheet.ExportCell{
					"ja_JP": {Text: "用語集一致", Status: domain.StatusFinished},
				},
			},
		},
	}); err != nil {
		t.Fatalf("ExportExcel glossary failed: %v", err)
	}

	// 3. Start mock OpenAI-compatible server and count requests.
	var llmHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"mock-model"}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			llmHits.Add(1)
			var reqBody struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			userContent := reqBody.Messages[len(reqBody.Messages)-1].Content
			var inEntries []struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			}
			_ = json.Unmarshal([]byte(userContent), &inEntries)
			outMap := make(map[string]string, len(inEntries))
			for _, e := range inEntries {
				if strings.Contains(e.Source, "[[0]]") {
					outMap[e.ID] = "[[0]] への変更を保存しますか?"
				} else {
					outMap[e.ID] = "翻訳: " + e.Source
				}
			}
			outJSON, _ := json.Marshal(outMap)
			resp := map[string]any{
				"choices": []map[string]any{
					{"message": map[string]string{"content": string(outJSON)}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// 4. Configure isolated provider registry, pool, and cache.
	reg := provider.NewRegistry()
	providers.RegisterBuiltins(reg)
	pool := netpool.New(netpool.ProxyConfig{})

	if p, ok := reg.Get(domain.OriginXLSX); ok {
		if err := p.Configure(map[string]string{"file_path": glossaryXLSX, "skip_ref": "true"}, pool); err != nil {
			t.Fatalf("configure xlsx: %v", err)
		}
	}
	if p, ok := reg.Get(domain.OriginTM); ok {
		if err := p.Configure(map[string]string{"tm_path": tmDir}, pool); err != nil {
			t.Fatalf("configure ts_tm: %v", err)
		}
	}
	if p, ok := reg.Get(domain.OriginLLM); ok {
		if err := p.Configure(map[string]string{
			"base_url":  srv.URL + "/v1",
			"model":     "mock-model",
			"use_proxy": "false",
		}, pool); err != nil {
			t.Fatalf("configure openai_compat: %v", err)
		}
	}

	cache := provider.NewCache(filepath.Join(dir, "cache.json"))
	locs := []locale.LocaleSpec{{ID: "ja_JP", Name: "Japanese", Enabled: true, EngineCode: "ja"}}
	items := []provider.SourceItem{
		{ID: "0", Key: domain.EntryKey{Context: "FileManager", Source: "Glossary Hit"}},
		{ID: "1", Key: domain.EntryKey{Context: "FileManager", Source: "TM Hit"}},
		{ID: "2", Key: domain.EntryKey{Context: "FileManager", Source: "Save changes to %1?"}},
	}

	opts := provider.DispatchOptions{
		Registry:    reg,
		Cache:       cache,
		ProviderIDs: []string{domain.OriginXLSX, domain.OriginTM, domain.OriginLLM},
		Locales:     locs,
		Tasks:       map[string][]provider.SourceItem{"ja_JP": items},
		Concurrency: 2,
	}

	res1 := provider.RunConcurrent(context.Background(), opts)
	if res1.Statuses["ja_JP"].Phase != provider.PhaseDone {
		t.Fatalf("expected PhaseDone, got %v (errors=%v)", res1.Statuses["ja_JP"].Phase, res1.Statuses["ja_JP"].Errors)
	}
	if llmHits.Load() != 1 {
		t.Fatalf("expected 1 LLM HTTP call on first run, got %d", llmHits.Load())
	}

	gotBySource := make(map[string]provider.TranslatedCell)
	for _, c := range res1.Cells {
		gotBySource[c.Key.Source] = c
	}
	if gotBySource["Glossary Hit"].Text != "用語集一致" || gotBySource["Glossary Hit"].ProviderID != domain.OriginXLSX {
		t.Fatalf("unexpected Glossary Hit cell: %+v", gotBySource["Glossary Hit"])
	}
	if gotBySource["TM Hit"].Text != "TM一致" || gotBySource["TM Hit"].ProviderID != domain.OriginTM {
		t.Fatalf("unexpected TM Hit cell: %+v", gotBySource["TM Hit"])
	}
	if gotBySource["Save changes to %1?"].Text != "%1 への変更を保存しますか?" || gotBySource["Save changes to %1?"].ProviderID != domain.OriginLLM {
		t.Fatalf("unexpected LLM cell: %+v", gotBySource["Save changes to %1?"])
	}

	// Second run must hit persistent cache with 0 additional LLM HTTP calls.
	res2 := provider.RunConcurrent(context.Background(), opts)
	if llmHits.Load() != 1 {
		t.Fatalf("expected 0 additional LLM HTTP calls on cached second run, total=%d", llmHits.Load())
	}
	if res2.Statuses["ja_JP"].CacheHits == 0 {
		t.Fatalf("expected CacheHits > 0 on second run")
	}

	// 5. Build MatrixPlan, export to .xlsx, and re-import to verify round-trip.
	bundle, err := tsxml.LoadBundle(targetDir, "{id}.ts", locs)
	if err != nil {
		t.Fatalf("LoadBundle failed: %v", err)
	}
	cat := domain.NewCatalog()
	for _, c := range res1.Cells {
		cat.Set(c.LocaleID, c.Key, c.Text, c.ProviderID)
	}
	plan := planner.BuildPlan(planner.PlanInput{
		Bundle:    bundle,
		Catalog:   cat,
		Locales:   locs,
		WriteMode: domain.ModeFillUnfinished,
	})
	exportPath := filepath.Join(dir, "exported.xlsx")
	exportOpts := planner.BuildExportRows(plan, sheet.ExportAll, locs, false)
	if err := sheet.ExportExcel(exportPath, exportOpts); err != nil {
		t.Fatalf("ExportExcel failed: %v", err)
	}
	wb, err := sheet.LoadExcel(exportPath, "", locs, true)
	if err != nil {
		t.Fatalf("LoadExcel failed: %v", err)
	}
	reimportedCat := domain.NewCatalog()
	if n := sheet.PopulateCatalog(wb, reimportedCat, true); n != 3 {
		t.Fatalf("expected 3 reimported entries, got %d", n)
	}
	entry, ok := reimportedCat.Lookup("ja_JP", domain.EntryKey{Context: "FileManager", Source: "Save changes to %1?"})
	if !ok || entry.Text != "%1 への変更を保存しますか?" {
		t.Fatalf("reimported Excel entry mismatch: ok=%v entry=%+v", ok, entry)
	}

	// 6. Headless UI render via app.NewWithConfig + ui.Render.
	cfg := config.Default()
	cfg.LastTSPath = ""
	cfg.LastXLSXPath = ""
	application := app.NewWithConfig(cfg)
	application.SetHelpMarkdown("# LocLoom\n\nIntegration test help content.")
	if err := application.LoadTSTarget(targetDir); err != nil {
		t.Fatalf("app.LoadTSTarget failed: %v", err)
	}
	img := ui.Render(application.View, 1280, 820, 1.0)
	if img == nil {
		t.Fatalf("ui.Render returned nil image")
	}
	b := img.Bounds()
	if b.Dx() != 1280 || b.Dy() != 820 {
		t.Fatalf("unexpected render bounds: %v", b)
	}
	// Verify the rendered RGBA frame is non-blank (contains more than one color).
	first := img.RGBAAt(b.Min.X, b.Min.Y)
	nonUniform := false
	for y := b.Min.Y; y < b.Max.Y && !nonUniform; y += 8 {
		for x := b.Min.X; x < b.Max.X; x += 8 {
			if img.RGBAAt(x, y) != first {
				nonUniform = true
				break
			}
		}
	}
	if !nonUniform {
		t.Fatalf("ui.Render produced a uniform/blank frame")
	}
}
