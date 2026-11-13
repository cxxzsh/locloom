package planner

import (
	"testing"

	"locloom/internal/domain"
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

	exportOpts := BuildExportRows(planWithAppend, sheet.ExportChanged, locales)
	if len(exportOpts.Rows) != 2 {
		t.Fatalf("BuildExportRows(ExportChanged) = %d rows, want 2", len(exportOpts.Rows))
	}
}
