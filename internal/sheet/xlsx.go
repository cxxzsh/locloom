package sheet

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/platform"
)

// RowRecord represents a single parsed row from an Excel translation sheet.
type RowRecord struct {
	RowNumber    int
	Context      string
	Source       string
	Comment      string
	Translations map[string]string // localeID -> text
}

// WorkbookData holds parsed sheet metadata and rows from an Excel workbook.
type WorkbookData struct {
	FilePath          string
	SheetName         string
	SheetNames        []string
	HasContextColumn  bool
	HasCommentColumn  bool
	DiscoveredLocales []string
	RefLocales        map[string]bool
	Rows              []RowRecord
}

// ExportScope controls which rows are included when exporting to Excel.
type ExportScope int

const (
	ExportAll ExportScope = iota
	ExportUntranslated
	ExportChanged
	ExportWarnings
)

// ExportColumn describes a locale column in ExportExcel.
type ExportColumn struct {
	LocaleID string
	Header   string
	IsRef    bool
}

// ExportCell holds one cell's value and visual status for Excel export.
type ExportCell struct {
	Text       string
	Status     domain.TransStatus
	Changed    bool
	HasWarning bool
}

// ExportRow holds one row of data for Excel export.
type ExportRow struct {
	Context string
	Source  string
	Comment string
	Cells   map[string]ExportCell
}

// ExportOptions configures Excel export formatting and data.
type ExportOptions struct {
	IncludeContext bool
	IncludeComment bool
	SheetName      string
	Columns        []ExportColumn
	Rows           []ExportRow
}

type localeColInfo struct {
	colIdx   int
	localeID string
	isRef    bool
}

// LoadExcel reads an Excel workbook using shared file access and parses translation rows.
func LoadExcel(path, sheetName string, known []locale.LocaleSpec, skipRef bool) (*WorkbookData, error) {
	raw, err := platform.ReadFileShared(path)
	if err != nil {
		return nil, fmt.Errorf("read excel %q: %w", path, err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("open excel %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sheetNames := f.GetSheetList()
	if len(sheetNames) == 0 {
		return nil, fmt.Errorf("workbook %q has no sheets", path)
	}

	targetSheet := strings.TrimSpace(sheetName)
	if targetSheet == "" {
		targetSheet = sheetNames[0]
	} else {
		found := false
		for _, s := range sheetNames {
			if s == targetSheet {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("sheet %q not found in %q", targetSheet, path)
		}
	}

	rows, err := f.GetRows(targetSheet)
	if err != nil {
		return nil, fmt.Errorf("read rows from sheet %q: %w", targetSheet, err)
	}

	wb := &WorkbookData{
		FilePath:   path,
		SheetName:  targetSheet,
		SheetNames: sheetNames,
		RefLocales: make(map[string]bool),
	}
	if len(rows) == 0 {
		return wb, nil
	}

	header := rows[0]
	sourceCol := -1
	contextCol := -1
	commentCol := -1
	var localeCols []localeColInfo
	seenDiscovered := make(map[string]bool)

	for colIdx, h := range header {
		switch {
		case sourceCol < 0 && locale.IsSourceHeader(h):
			sourceCol = colIdx
		case contextCol < 0 && locale.IsContextHeader(h):
			contextCol = colIdx
			wb.HasContextColumn = true
		case commentCol < 0 && locale.IsCommentHeader(h):
			commentCol = colIdx
			wb.HasCommentColumn = true
		default:
			if locID, isRef, ok := locale.ParseHeaderLocale(h, known); ok {
				if isRef {
					wb.RefLocales[locID] = true
				}
				localeCols = append(localeCols, localeColInfo{
					colIdx:   colIdx,
					localeID: locID,
					isRef:    isRef,
				})
				if !(skipRef && isRef) && !seenDiscovered[locID] {
					seenDiscovered[locID] = true
					wb.DiscoveredLocales = append(wb.DiscoveredLocales, locID)
				}
			}
		}
	}

	if sourceCol < 0 {
		return nil, fmt.Errorf("sheet %q is missing a Source column", targetSheet)
	}

	for rIdx := 1; rIdx < len(rows); rIdx++ {
		row := rows[rIdx]
		src := cellAt(row, sourceCol)
		if src == "" {
			continue
		}
		rec := RowRecord{
			RowNumber:    rIdx + 1,
			Context:      strings.TrimSpace(cellAt(row, contextCol)),
			Source:       src,
			Comment:      strings.TrimSpace(cellAt(row, commentCol)),
			Translations: make(map[string]string, len(localeCols)),
		}
		for _, lc := range localeCols {
			if skipRef && lc.isRef {
				continue
			}
			val := cellAt(row, lc.colIdx)
			if val != "" {
				rec.Translations[lc.localeID] = val
			}
		}
		wb.Rows = append(wb.Rows, rec)
	}

	return wb, nil
}

func cellAt(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return row[idx]
}

// PopulateCatalog imports workbook translations into cat.
// Uses EntryKey{Context, Source, Comment} when HasContextColumn is true,
// or glossary key EntryKey{Source: src} when HasContextColumn is false.
func PopulateCatalog(wb *WorkbookData, cat *domain.Catalog, skipRef bool) int {
	if wb == nil || cat == nil {
		return 0
	}
	imported := 0
	for _, row := range wb.Rows {
		var key domain.EntryKey
		if wb.HasContextColumn {
			key = domain.EntryKey{
				Context: row.Context,
				Source:  row.Source,
				Comment: row.Comment,
			}
		} else {
			key = domain.EntryKey{Source: row.Source}
		}

		for locID, text := range row.Translations {
			if text == "" {
				continue
			}
			if skipRef && wb.RefLocales[locID] {
				continue
			}
			cat.Set(locID, key, text, domain.OriginXLSX)
			imported++
		}
	}
	return imported
}

// ExportExcel writes translation data to a styled Excel workbook atomically.
func ExportExcel(path string, opts ExportOptions) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheetName := strings.TrimSpace(opts.SheetName)
	if sheetName == "" {
		sheetName = "Translations"
	}
	if sheetName != "Sheet1" {
		if err := f.SetSheetName("Sheet1", sheetName); err != nil {
			return fmt.Errorf("rename sheet: %w", err)
		}
	}

	border := []excelize.Border{
		{Type: "left", Color: "D9E1F2", Style: 1},
		{Type: "right", Color: "D9E1F2", Style: 1},
		{Type: "top", Color: "D9E1F2", Style: 1},
		{Type: "bottom", Color: "D9E1F2", Style: 1},
	}

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F4E78"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left", WrapText: true},
		Border:    border,
	})
	defaultStyle, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    border,
	})
	missingStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FCE4D6"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    border,
	})
	changedStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"E2EFDA"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    border,
	})
	warningStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"FFF2CC"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    border,
	})

	headers := make([]string, 0, 3+len(opts.Columns))
	if opts.IncludeContext {
		headers = append(headers, "Context")
	}
	headers = append(headers, "Source")
	if opts.IncludeComment {
		headers = append(headers, "Comment")
	}
	for _, col := range opts.Columns {
		h := strings.TrimSpace(col.Header)
		if h == "" {
			h = col.LocaleID
		}
		if col.IsRef && !strings.Contains(h, "参考") && !strings.Contains(strings.ToLower(h), "ref") {
			h += " (参考)"
		}
		headers = append(headers, h)
	}

	colWidths := make([]int, len(headers))
	for cIdx, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(cIdx+1, 1)
		_ = f.SetCellValue(sheetName, cell, h)
		_ = f.SetCellStyle(sheetName, cell, cell, headerStyle)
		colWidths[cIdx] = len([]rune(h)) + 4
	}
	_ = f.SetRowHeight(sheetName, 1, 24)

	for rIdx, row := range opts.Rows {
		excelRow := rIdx + 2
		cIdx := 0
		writeCell := func(val string, styleID int) {
			cell, _ := excelize.CoordinatesToCellName(cIdx+1, excelRow)
			_ = f.SetCellValue(sheetName, cell, val)
			_ = f.SetCellStyle(sheetName, cell, cell, styleID)
			w := len([]rune(val)) + 2
			if w > colWidths[cIdx] {
				colWidths[cIdx] = w
			}
			cIdx++
		}

		if opts.IncludeContext {
			writeCell(row.Context, defaultStyle)
		}
		writeCell(row.Source, defaultStyle)
		if opts.IncludeComment {
			writeCell(row.Comment, defaultStyle)
		}
		for _, col := range opts.Columns {
			ec := row.Cells[col.LocaleID]
			style := defaultStyle
			switch {
			case ec.HasWarning:
				style = warningStyle
			case ec.Changed:
				style = changedStyle
			case ec.Text == "" && (ec.Status == domain.StatusMissing || ec.Status == domain.StatusUnfinished || ec.Status == domain.StatusEmpty):
				style = missingStyle
			}
			writeCell(ec.Text, style)
		}
	}

	for cIdx, w := range colWidths {
		colName, _ := excelize.ColumnNumberToName(cIdx + 1)
		if w < 16 {
			w = 16
		}
		if w > 52 {
			w = 52
		}
		_ = f.SetColWidth(sheetName, colName, colName, float64(w))
	}

	_ = f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		Split:       true,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	lastCol, _ := excelize.ColumnNumberToName(len(headers))
	lastRow := len(opts.Rows) + 1
	if lastRow < 1 {
		lastRow = 1
	}
	_ = f.AutoFilter(sheetName, fmt.Sprintf("A1:%s%d", lastCol, lastRow), nil)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return fmt.Errorf("encode excel: %w", err)
	}
	return platform.AtomicWriteFile(path, buf.Bytes(), 0o644)
}
