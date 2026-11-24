package app

import (
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/planner"
	"locloom/internal/tsxml"
)

// A cell cleared via SetCleared keeps Status=Finished but Effective="",
// so ModeFillUnfinished must still enqueue it (the old code skipped on status).
func TestBuildTranslationTasksIncludesClearedFinished(t *testing.T) {
	zhTS, err := tsxml.ParseBytes("zh_CN.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>M</name>
    <message>
        <source>Save</source>
        <translation>保存</translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	zhTS.LocaleID = "zh_CN"
	jaTS, err := tsxml.ParseBytes("ja_JP.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>M</name>
    <message>
        <source>Save</source>
        <translation>保存済み</translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	jaTS.LocaleID = "ja_JP"

	locales := []locale.LocaleSpec{
		{ID: "zh_CN", Enabled: true, IsRef: true},
		{ID: "ja_JP", Enabled: true},
	}
	plan := planner.BuildPlan(planner.PlanInput{
		Bundle:    &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS}},
		Catalog:   domain.NewCatalog(),
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
	})
	key := plan.Rows[0].Key
	if cell := plan.Rows[0].Cells["ja_JP"]; cell.Status != domain.StatusFinished || cell.Effective != "保存済み" {
		t.Fatalf("pre: cell=%+v", cell)
	}

	// Clear the cell like clearCellAt does.
	plan2 := planner.BuildPlan(planner.PlanInput{
		Bundle: &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS}},
		Catalog: func() *domain.Catalog {
			c := domain.NewCatalog()
			c.SetCleared("ja_JP", key, domain.OriginManual)
			return c
		}(),
		Locales: locales, WriteMode: domain.ModeFillUnfinished,
	})
	cell := plan2.Rows[0].Cells["ja_JP"]
	if cell.Effective != "" || cell.Status != domain.StatusFinished {
		t.Fatalf("cleared cell = %+v, want Effective empty + Status Finished", cell)
	}

	tasks, n := buildTranslationTasks(plan2, []locale.LocaleSpec{locales[1]}, domain.ModeFillUnfinished, "zh_CN")
	if n != 1 || len(tasks["ja_JP"]) != 1 {
		t.Fatalf("tasks=%v n=%d, want 1 re-translation task for cleared cell", tasks, n)
	}
}

// Default "translate from" must be the file's source text, not the reference
// locale's own translation: row.RefText carries the ref locale's Effective
// (e.g. zh_CN "患者查看报告次数"), and feeding that to every other locale is
// what made translations lose the source's trailing punctuation wholesale.
func TestLocaleSourceDefaultsToFileSource(t *testing.T) {
	zhTS, err := tsxml.ParseBytes("zh_CN.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="zh_CN">
<context>
    <name>M</name>
    <message>
        <source>Patient Views:</source>
        <translation>患者查看报告次数</translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	zhTS.LocaleID = "zh_CN"
	jaTS, err := tsxml.ParseBytes("ja_JP.ts", []byte(`<?xml version="1.0" encoding="utf-8"?>
<TS version="2.1" language="ja_JP">
<context>
    <name>M</name>
    <message>
        <source>Patient Views:</source>
        <translation type="unfinished"></translation>
    </message>
</context>
</TS>`))
	if err != nil {
		t.Fatal(err)
	}
	jaTS.LocaleID = "ja_JP"

	locales := []locale.LocaleSpec{
		{ID: "zh_CN", Enabled: true, IsRef: true},
		{ID: "ja_JP", Enabled: true},
		{ID: "zh_TW", Enabled: true, SourceLocale: "zh_CN"},
	}
	plan := planner.BuildPlan(planner.PlanInput{
		Bundle:    &tsxml.Bundle{Files: []*tsxml.TSFile{zhTS, jaTS}},
		Catalog:   domain.NewCatalog(),
		Locales:   locales,
		WriteMode: domain.ModeFillUnfinished,
	})
	row := &plan.Rows[0]
	if row.RefText != "患者查看报告次数" {
		t.Fatalf("pre: RefText=%q, want ref locale's text", row.RefText)
	}

	// ja_JP (default source) must get the file source, not the zh_CN text.
	src, _ := localeSource(plan, row, locales[1])
	if src != "Patient Views:" {
		t.Fatalf("default localeSource = %q, want file source %q", src, "Patient Views:")
	}
	// zh_TW pinned to zh_CN gets the ref locale's text as intended.
	srcTW, locTW := localeSource(plan, row, locales[2])
	if srcTW != "患者查看报告次数" || locTW != "zh_CN" {
		t.Fatalf("pinned localeSource = %q/%q, want zh_CN text", srcTW, locTW)
	}
}
