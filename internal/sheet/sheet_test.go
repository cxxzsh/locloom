package sheet

import (
	"path/filepath"
	"testing"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func TestExportLoadAndPopulateCatalog(t *testing.T) {
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "translations.xlsx")

	opts := ExportOptions{
		IncludeContext: true,
		IncludeComment: true,
		SheetName:      "UI",
		Columns: []ExportColumn{
			{LocaleID: "zh_CN", Header: "zh_CN (参考)", IsRef: true},
			{LocaleID: "ja_JP", Header: "日本語"},
			{LocaleID: "de_DE", Header: "de_DE"},
		},
		Rows: []ExportRow{
			{
				Context: "MainWindow",
				Source:  "Save changes to %1?",
				Comment: "confirm dialog",
				Cells: map[string]ExportCell{
					"zh_CN": {Text: "是否保存对 %1 的更改？", Status: domain.StatusFinished},
					"ja_JP": {Text: "%1 への変更を保存しますか？", Status: domain.StatusFinished, Changed: true},
					"de_DE": {Text: "", Status: domain.StatusUnfinished},
				},
			},
			{
				Context: "SettingsDialog",
				Source:  "Cancel",
				Cells: map[string]ExportCell{
					"zh_CN": {Text: "取消", Status: domain.StatusFinished},
					"ja_JP": {Text: "キャンセル", Status: domain.StatusFinished},
					"de_DE": {Text: "Abbrechen", Status: domain.StatusFinished, HasWarning: true},
				},
			},
		},
	}

	if err := ExportExcel(xlsxPath, opts); err != nil {
		t.Fatalf("ExportExcel failed: %v", err)
	}

	wb, err := LoadExcel(xlsxPath, "UI", locale.DefaultLocales(), true)
	if err != nil {
		t.Fatalf("LoadExcel failed: %v", err)
	}
	if !wb.HasContextColumn || !wb.HasCommentColumn {
		t.Fatalf("expected context and comment columns to be detected")
	}
	if !wb.RefLocales["zh_CN"] {
		t.Fatalf("expected zh_CN to be marked as ref locale")
	}
	if len(wb.DiscoveredLocales) != 2 {
		t.Fatalf("expected 2 non-ref discovered locales, got %v", wb.DiscoveredLocales)
	}
	if len(wb.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(wb.Rows))
	}

	cat := domain.NewCatalog()
	imported := PopulateCatalog(wb, cat, true)
	if imported != 3 {
		t.Fatalf("imported = %d, want 3 (ja_JP x2 + de_DE x1, skipping zh_CN ref and empty de_DE)", imported)
	}

	entry, ok := cat.Lookup("ja_JP", domain.EntryKey{Context: "MainWindow", Source: "Save changes to %1?", Comment: "confirm dialog"})
	if !ok || entry.Text != "%1 への変更を保存しますか？" || entry.Origin != domain.OriginXLSX {
		t.Fatalf("unexpected catalog lookup: %+v, ok=%v", entry, ok)
	}
}

func TestGlossaryWorkbookWithoutContextColumn(t *testing.T) {
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "glossary.xlsx")

	opts := ExportOptions{
		IncludeContext: false,
		IncludeComment: false,
		Columns: []ExportColumn{
			{LocaleID: "fr_FR", Header: "fr_FR"},
		},
		Rows: []ExportRow{
			{
				Source: "Open",
				Cells: map[string]ExportCell{
					"fr_FR": {Text: "Ouvrir", Status: domain.StatusFinished},
				},
			},
		},
	}
	if err := ExportExcel(xlsxPath, opts); err != nil {
		t.Fatalf("ExportExcel failed: %v", err)
	}

	wb, err := LoadExcel(xlsxPath, "", locale.DefaultLocales(), true)
	if err != nil {
		t.Fatalf("LoadExcel failed: %v", err)
	}
	if wb.HasContextColumn {
		t.Fatal("expected HasContextColumn = false")
	}

	cat := domain.NewCatalog()
	if n := PopulateCatalog(wb, cat, true); n != 1 {
		t.Fatalf("PopulateCatalog = %d, want 1", n)
	}

	// Lookup with any context should fall back to glossary ("", "Open", "").
	entry, ok := cat.Lookup("fr_FR", domain.EntryKey{Context: "FileManager", Source: "Open"})
	if !ok || entry.Text != "Ouvrir" {
		t.Fatalf("expected glossary fallback to find Ouvrir, got %+v ok=%v", entry, ok)
	}
}
