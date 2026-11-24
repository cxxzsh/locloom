package planner

import (
	"strings"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/locale"
	"locloom/internal/sheet"
	"locloom/internal/tsxml"
)

func makeTestTSFile(t *testing.T, locID, xmlBody string) *tsxml.TSFile {
	t.Helper()
	tf, err := tsxml.ParseBytes(locID+".ts", []byte(xmlBody))
	if err != nil {
		t.Fatalf("ParseBytes(%s) failed: %v", locID, err)
	}
	tf.LocaleID = locID
	return tf
}

func TestBuildPlanPhantomProtectionAndUpdateCell(t *testing.T) {
	jaTS := makeTestTSFile(t, "ja_JP", `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save %1</source>
        <translation type="unfinished"></translation>
    </message>
    <message>
        <source>Exit</source>
        <translation>終了</translation>
    </message>
</context>
</TS>`)

	zhTS := makeTestTSFile(t, "zh_CN", `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save %1</source>
        <translation>保存 %1</translation>
    </message>
    <message>
        <source>Exit</source>
        <translation>退出</translation>
    </message>
</context>
</TS>`)

	bundle := &tsxml.Bundle{
		Files: []*tsxml.TSFile{zhTS, jaTS},
	}

	locales := []locale.LocaleSpec{
		{ID: "zh_CN", Name: "简体中文", Enabled: true, IsRef: true},
		{ID: "ja_JP", Name: "日本語", Enabled: true},
	}

	cat := domain.NewCatalog()
	// Intentional missing placeholder warning (%1 missing)
	cat.Set("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Save %1"}, "保存", domain.OriginXLSX)

	extra := []domain.EntryKey{
		{Context: "SettingsDialog", Source: "PhantomRow"},
	}

	// 1. AppendMissing == false -> PhantomRow must NOT appear.
	plan := BuildPlan(PlanInput{
		Bundle:        bundle,
		ExtraEntries:  extra,
		Catalog:       cat,
		Locales:       locales,
		WriteMode:     domain.ModeFillUnfinished,
		AppendMissing: false,
	})
	if plan.Summary.TotalRows != 2 {
		t.Fatalf("TotalRows = %d, want 2 (phantom row protection)", plan.Summary.TotalRows)
	}
	if plan.Summary.TotalChanges != 1 || plan.Summary.TotalWarnings != 1 {
		t.Fatalf("unexpected summary: changes=%d warnings=%d", plan.Summary.TotalChanges, plan.Summary.TotalWarnings)
	}

	// 2. UpdateCell in O(1): fix the translation in catalog and update row 0.
	cat.Set("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Save %1"}, "%1 を保存", domain.OriginManual)
	if ok := UpdateCell(plan, 0, "ja_JP", cat); !ok {
		t.Fatal("UpdateCell returned false")
	}
	if plan.Summary.TotalWarnings != 0 {
		t.Fatalf("expected TotalWarnings = 0 after UpdateCell fix, got %d", plan.Summary.TotalWarnings)
	}
	if plan.Rows[0].HasWarning {
		t.Fatal("expected row 0 HasWarning = false")
	}

	patches := BuildPatchesForFile(plan, jaTS, "ja_JP")
	if len(patches) != 1 || patches[0].NewText != "%1 を保存" {
		t.Fatalf("unexpected patches: %+v", patches)
	}

	// 3. AppendMissing == true -> ExtraEntries appear with ActionAppendNew if translated.
	cat.Set("ja_JP", domain.EntryKey{Context: "SettingsDialog", Source: "PhantomRow"}, "追加行", domain.OriginLLM)
	planWithAppend := BuildPlan(PlanInput{
		Bundle:        bundle,
		ExtraEntries:  extra,
		Catalog:       cat,
		Locales:       locales,
		WriteMode:     domain.ModeFillUnfinished,
		AppendMissing: true,
	})
	if planWithAppend.Summary.TotalRows != 3 {
		t.Fatalf("TotalRows with AppendMissing = %d, want 3", planWithAppend.Summary.TotalRows)
	}
	if !planWithAppend.Summary.MultiContext {
		t.Fatal("expected MultiContext = true")
	}
	if planWithAppend.Summary.TotalAppends != 1 {
		t.Fatalf("TotalAppends = %d, want 1", planWithAppend.Summary.TotalAppends)
	}

	exportOpts := BuildExportRows(planWithAppend, sheet.ExportChanged, locales, false)
	if len(exportOpts.Rows) != 2 {
		t.Fatalf("BuildExportRows(ExportChanged) = %d rows, want 2", len(exportOpts.Rows))
	}
}

func TestBuildPlanAndUpdateCellUILang(t *testing.T) {
	jaTS := makeTestTSFile(t, "ja_JP", `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save %1</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>`)

	bundle := &tsxml.Bundle{Files: []*tsxml.TSFile{jaTS}}
	locales := []locale.LocaleSpec{{ID: "ja_JP", Enabled: true}}

	cat := domain.NewCatalog()
	cat.Set("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Save %1"}, "保存", domain.OriginXLSX)

	planEn := BuildPlan(PlanInput{
		Bundle:    bundle,
		Catalog:   cat,
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
		UILang:    "en",
	})
	wEn := planEn.Rows[0].Cells["ja_JP"].Warnings
	if len(wEn) != 1 || wEn[0] != "Missing placeholder(s): %1" {
		t.Fatalf("en warnings = %v, want [Missing placeholder(s): %%1]", wEn)
	}

	// UpdateCell preserves plan.UILang ("en")
	cat.Set("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Save %1"}, "保存 %2", domain.OriginManual)
	if !UpdateCell(planEn, 0, "ja_JP", cat) {
		t.Fatal("UpdateCell returned false")
	}
	wEn2 := planEn.Rows[0].Cells["ja_JP"].Warnings
	if len(wEn2) != 2 || wEn2[0] != "Missing placeholder(s): %1" || wEn2[1] != "Unexpected placeholder(s): %2" {
		t.Fatalf("en warnings after UpdateCell = %v", wEn2)
	}

	planZh := BuildPlan(PlanInput{
		Bundle:    bundle,
		Catalog:   cat,
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
		UILang:    "zh",
	})
	wZh := planZh.Rows[0].Cells["ja_JP"].Warnings
	if len(wZh) != 2 || wZh[0] != "缺失占位符: %1" {
		t.Fatalf("zh warnings = %v", wZh)
	}
}

func TestBuildPlanIDKeyedFormatBundleAndBuildFormatPatches(t *testing.T) {
	fb := &format.Bundle{
		Format:       format.FormatJSON,
		Topology:     format.TopologyPerLocale,
		SourceLocale: "en",
		Docs: []*format.DocRecord{
			{
				FormatID:   format.FormatJSON,
				LocaleID:   "en",
				IsTemplate: true,
				Entries: []format.NormalizedEntry{
					{Key: domain.EntryKey{ID: "menu.save", Source: "Save"}, LocaleID: "en", Text: "Save", Status: domain.StatusFinished},
					{Key: domain.EntryKey{ID: "menu.exit", Source: "Exit"}, LocaleID: "en", Text: "Exit", Status: domain.StatusFinished},
				},
			},
			{
				FormatID: format.FormatJSON,
				LocaleID: "ja",
				Entries: []format.NormalizedEntry{
					{Key: domain.EntryKey{ID: "menu.save", Source: "Save"}, LocaleID: "ja", Text: "", Status: domain.StatusEmpty},
				},
			},
		},
	}

	locales := []locale.LocaleSpec{
		{ID: "en", Name: "English", Enabled: false, IsRef: true},
		{ID: "ja", Name: "Japanese", Enabled: true},
	}

	cat := domain.NewCatalog()
	cat.Set("ja", domain.EntryKey{ID: "menu.save", Source: "Save"}, "保存", domain.OriginManual)
	cat.Set("ja", domain.EntryKey{ID: "menu.exit", Source: "Exit"}, "終了", domain.OriginLLM)

	plan := BuildPlan(PlanInput{
		FormatBundle: fb,
		Catalog:      cat,
		Locales:      locales,
		WriteMode:    domain.ModeFillUnfinished,
	})

	if plan.Summary.TotalRows != 2 {
		t.Fatalf("TotalRows = %d, want 2", plan.Summary.TotalRows)
	}
	if plan.Rows[0].RefText != "Save" || plan.Rows[1].RefText != "Exit" {
		t.Fatalf("unexpected RefTexts: %q, %q", plan.Rows[0].RefText, plan.Rows[1].RefText)
	}
	if got := plan.Rows[0].Cells["ja"].Action; got != domain.ActionFillGap {
		t.Fatalf("row 0 ja action = %s, want %s", got, domain.ActionFillGap)
	}
	if got := plan.Rows[1].Cells["ja"].Action; got != domain.ActionAppendNew {
		t.Fatalf("row 1 ja action = %s, want %s", got, domain.ActionAppendNew)
	}

	patches := BuildFormatPatches(plan)
	if len(patches["ja"]) != 2 {
		t.Fatalf("expected 2 patches for ja, got %d", len(patches["ja"]))
	}
	if _, hasEn := patches["en"]; hasEn {
		t.Fatal("disabled ref locale en should not have patches")
	}

	exportOpts := BuildExportRows(plan, sheet.ExportAll, locales, false)
	if !exportOpts.IncludeKeyID {
		t.Fatal("expected IncludeKeyID = true")
	}
	if len(exportOpts.Rows) != 2 || exportOpts.Rows[0].KeyID != "menu.save" {
		t.Fatalf("unexpected export rows: %+v", exportOpts.Rows)
	}
}

func TestBuildPlanMultiLocaleXCStringsMissingLocalesFillGap(t *testing.T) {
	fb := &format.Bundle{
		Format:       format.FormatXCStrings,
		Topology:     format.TopologyMultiLocale,
		SourceLocale: "en",
		Docs: []*format.DocRecord{
			{
				FormatID: format.FormatXCStrings,
				Locales:  []string{"en", "ja"},
				Entries: []format.NormalizedEntry{
					{Key: domain.EntryKey{ID: "greeting", Source: "Hello"}, LocaleID: "en", Text: "Hello", Status: domain.StatusFinished},
					{Key: domain.EntryKey{ID: "greeting", Source: "Hello"}, LocaleID: "ja", Text: "こんにちは", Status: domain.StatusFinished},
				},
			},
		},
	}

	locales := []locale.LocaleSpec{
		{ID: "ja", Name: "Japanese", Enabled: true},
		{ID: "fr", Name: "French", Enabled: true},
	}

	cat := domain.NewCatalog()
	cat.Set("fr", domain.EntryKey{ID: "greeting", Source: "Hello"}, "Bonjour", domain.OriginLLM)

	plan := BuildPlan(PlanInput{
		FormatBundle:  fb,
		Catalog:       cat,
		Locales:       locales,
		WriteMode:     domain.ModeFillUnfinished,
		AppendMissing: false,
	})

	if plan.Summary.TotalRows != 1 {
		t.Fatalf("TotalRows = %d, want 1", plan.Summary.TotalRows)
	}
	frCell := plan.Rows[0].Cells["fr"]
	if frCell.Status != domain.StatusEmpty {
		t.Fatalf("fr Status = %s, want %s", frCell.Status, domain.StatusEmpty)
	}
	if frCell.Action != domain.ActionFillGap {
		t.Fatalf("fr Action = %s, want %s", frCell.Action, domain.ActionFillGap)
	}
}

func TestUpdateCellByKey(t *testing.T) {
	fb := &format.Bundle{
		Format:       format.FormatXLIFF,
		Topology:     format.TopologyPerLocale,
		SourceLocale: "en",
		Docs: []*format.DocRecord{
			{
				FormatID: format.FormatXLIFF,
				LocaleID: "en",
				Entries: []format.NormalizedEntry{
					{Key: domain.EntryKey{ID: "menu.save", Source: "Save"}, LocaleID: "en", Text: "Save", Status: domain.StatusFinished},
					{Key: domain.EntryKey{Source: "Plain Source"}, LocaleID: "en", Text: "Plain Source", Status: domain.StatusFinished},
				},
			},
			{
				FormatID: format.FormatXLIFF,
				LocaleID: "ja",
				Entries: []format.NormalizedEntry{
					{Key: domain.EntryKey{ID: "menu.save", Source: "Save"}, LocaleID: "ja", Text: "", Status: domain.StatusEmpty},
					{Key: domain.EntryKey{Source: "Plain Source"}, LocaleID: "ja", Text: "", Status: domain.StatusEmpty},
				},
			},
		},
	}
	locales := []locale.LocaleSpec{
		{ID: "en", Name: "English", Enabled: true, IsRef: true},
		{ID: "ja", Name: "Japanese", Enabled: true},
	}
	cat := domain.NewCatalog()
	plan := BuildPlan(PlanInput{
		FormatBundle: fb,
		Catalog:      cat,
		Locales:      locales,
		WriteMode:    domain.ModeFillUnfinished,
	})
	if plan.Summary.LocaleStats["ja"].Translated != 0 {
		t.Fatalf("expected 0 initial translated ja cells, got %d", plan.Summary.LocaleStats["ja"].Translated)
	}

	// Update via direct EntryKey.
	plainKey := domain.EntryKey{Source: "Plain Source"}
	cat.Set("ja", plainKey, "プレーン原文", domain.OriginLLM)
	if !UpdateCellByKey(plan, plainKey, "ja", cat) {
		t.Fatal("expected UpdateCellByKey to match direct EntryKey")
	}
	if plan.Rows[1].Cells["ja"].Effective != "プレーン原文" {
		t.Fatalf("unexpected row 1 effective text %q", plan.Rows[1].Cells["ja"].Effective)
	}

	// Update via IDContext fallback when Source differs.
	idKey := domain.EntryKey{ID: "menu.save", Source: "Save"}
	cat.Set("ja", idKey, "保存", domain.OriginManual)
	if !UpdateCellByKey(plan, domain.EntryKey{ID: "menu.save"}, "ja", cat) {
		t.Fatal("expected UpdateCellByKey to match IDContext fallback")
	}
	if plan.Rows[0].Cells["ja"].Effective != "保存" || plan.Summary.LocaleStats["ja"].Translated != 2 {
		t.Fatalf("unexpected id row update: cell=%+v stats=%+v", plan.Rows[0].Cells["ja"], plan.Summary.LocaleStats["ja"])
	}

	// Unknown key returns false without mutating plan.
	if UpdateCellByKey(plan, domain.EntryKey{ID: "missing"}, "ja", cat) {
		t.Fatal("expected false for unknown EntryKey")
	}
}

// BuildExportRows with relevantOnly drops locale columns that contain no cell
// matching the scope; ref columns always survive, warnings carry their text.
func TestBuildExportRowsRelevantColumns(t *testing.T) {
	jaTS := makeTestTSFile(t, "ja_JP", `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save %1</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>`)
	zhTS := makeTestTSFile(t, "zh_CN", `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save %1</source>
        <translation>保存 %1</translation>
    </message>
</context>
</TS>`)
	deTS := makeTestTSFile(t, "de_DE", `<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="de_DE">
<context>
    <name>MainWindow</name>
    <message>
        <source>Save %1</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>`)

	bundle := &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS, deTS}}
	locales := []locale.LocaleSpec{
		{ID: "zh_CN", Name: "简体中文", Enabled: true, IsRef: true},
		{ID: "ja_JP", Name: "日本語", Enabled: true},
		{ID: "de_DE", Name: "Deutsch", Enabled: true},
	}
	cat := domain.NewCatalog()
	// ja_JP staged text missing %1 → warning cell; de_DE stays clean.
	cat.Set("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Save %1"}, "保存", domain.OriginXLSX)

	plan := BuildPlan(PlanInput{
		Bundle:    bundle,
		Catalog:   cat,
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
	})
	if plan.Summary.TotalWarnings != 1 {
		t.Fatalf("TotalWarnings = %d, want 1", plan.Summary.TotalWarnings)
	}

	warnOpts := BuildExportRows(plan, sheet.ExportWarnings, locales, true)
	if len(warnOpts.Rows) != 1 {
		t.Fatalf("warning rows = %d, want 1", len(warnOpts.Rows))
	}
	if len(warnOpts.Columns) != 2 || warnOpts.Columns[0].LocaleID != "zh_CN" || warnOpts.Columns[1].LocaleID != "ja_JP" {
		t.Fatalf("relevant columns = %+v, want [zh_CN ja_JP]", warnOpts.Columns)
	}
	cell := warnOpts.Rows[0].Cells["ja_JP"]
	if len(cell.Warnings) == 0 || !strings.Contains(cell.Warnings[0], "%1") {
		t.Fatalf("expected carried warning text, got %+v", cell)
	}

	// Same scope without relevantOnly keeps every enabled column.
	warnAll := BuildExportRows(plan, sheet.ExportWarnings, locales, false)
	if len(warnAll.Columns) != 3 {
		t.Fatalf("unfiltered columns = %+v, want 3", warnAll.Columns)
	}

	// Gap scope: de_DE unfinished is the only gap; ja_JP staged fill mutates it.
	gapOpts := BuildExportRows(plan, sheet.ExportUntranslated, locales, true)
	if len(gapOpts.Columns) != 2 || gapOpts.Columns[1].LocaleID != "de_DE" {
		t.Fatalf("gap-relevant columns = %+v, want [zh_CN de_DE]", gapOpts.Columns)
	}
}
