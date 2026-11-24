package sheet

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"locloom/internal/domain"
)

func TestExportWarningCellComment(t *testing.T) {
	dir := t.TempDir()
	xlsxPath := filepath.Join(dir, "warn.xlsx")

	opts := ExportOptions{
		SheetName: "Warnings",
		Columns: []ExportColumn{
			{LocaleID: "zh_CN", Header: "zh_CN (参考)", IsRef: true},
			{LocaleID: "ja_JP", Header: "日本語"},
		},
		Rows: []ExportRow{
			{
				Source: "Save %1",
				Cells: map[string]ExportCell{
					"zh_CN": {Text: "保存 %1", Status: domain.StatusFinished},
					"ja_JP": {
						Text:       "保存",
						Status:     domain.StatusFinished,
						HasWarning: true,
						Warnings:   []string{"Missing placeholder(s): %1", "Unexpected placeholder(s): %2"},
					},
				},
			},
		},
	}
	if err := ExportExcel(xlsxPath, opts); err != nil {
		t.Fatalf("ExportExcel failed: %v", err)
	}

	f, err := excelize.OpenFile(xlsxPath)
	if err != nil {
		t.Fatalf("open export: %v", err)
	}
	defer f.Close()
	comments, err := f.GetComments("Warnings")
	if err != nil {
		t.Fatalf("GetComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d: %+v", len(comments), comments)
	}
	c := comments[0]
	// Row 2, third column: Key? no KeyID/Context/Comment -> Source=A, zh_CN=B, ja_JP=C
	if c.Cell != "C2" {
		t.Fatalf("comment cell = %q, want C2", c.Cell)
	}
	if !strings.Contains(c.Text, "Missing placeholder(s): %1") || !strings.Contains(c.Text, "%2") {
		t.Fatalf("comment text missing warnings: %q", c.Text)
	}
}
