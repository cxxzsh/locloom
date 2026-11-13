package planner

import (
	"fmt"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/sheet"
	"locloom/internal/tsxml"
)

// CellPlan describes the current and planned translation state for one (row, locale) cell.
type CellPlan struct {
	LocaleID  string
	OldText   string
	NewText   string
	Effective string
	Origin    string
	Status    domain.TransStatus
	Action    domain.PatchActionType
	Numerus   bool
	Warnings  []string
}

// MatrixRow represents a single unique (Context, Source, Comment) entry across all locales.
type MatrixRow struct {
	Index        int
	Key          domain.EntryKey
	ExtraComment string
	Location     string
	Numerus      bool
	RefText      string
	InTS         bool
	Cells        map[string]CellPlan
	HasGap       bool
	HasChange    bool
	HasWarning   bool
}

// LocaleStats aggregates translation metrics for one locale.
type LocaleStats struct {
	LocaleID   string
	Total      int
	Translated int
	GapsBefore int
	GapsAfter  int
	Changed    int
	Appended   int
	Warnings   int
	Completion float64
}

// PlanSummary aggregates matrix-wide metrics for UI display and export.
type PlanSummary struct {
	TotalRows     int
	MultiContext  bool
	ContextNames  []string
	ContextCounts map[string]int
	TotalGaps     int
	TotalChanges  int
	TotalAppends  int
	TotalWarnings int
	LocaleStats   map[string]LocaleStats
}

// PlanInput configures BuildPlan.
type PlanInput struct {
	Bundle        *tsxml.Bundle
	ExtraEntries  []domain.EntryKey
	DefaultCtx    string
	Catalog       *domain.Catalog
	Locales       []locale.LocaleSpec
	WriteMode     domain.WriteMode
	AppendMissing bool
	UILang        string
}

// MatrixPlan contains the computed translation matrix and summary statistics.
type MatrixPlan struct {
	Rows        []MatrixRow
	Summary     PlanSummary
	WriteMode   domain.WriteMode
	RefLocaleID string
	EnabledIDs  []string
	UILang      string
}

// BuildPlan constructs a MatrixPlan by merging all .ts bundle entries, optional extra entries
// (only when AppendMissing is true), and candidate translations from Catalog.
func BuildPlan(in PlanInput) *MatrixPlan {
	plan := &MatrixPlan{
		WriteMode: in.WriteMode,
		UILang:    in.UILang,
		Summary: PlanSummary{
			ContextCounts: make(map[string]int),
			LocaleStats:   make(map[string]LocaleStats),
		},
	}

	var trackedIDs []string
	trackedSet := make(map[string]bool)
	for _, l := range in.Locales {
		if l.IsRef && plan.RefLocaleID == "" {
			plan.RefLocaleID = l.ID
		}
		if l.Enabled {
			plan.EnabledIDs = append(plan.EnabledIDs, l.ID)
			if !trackedSet[l.ID] {
				trackedSet[l.ID] = true
				trackedIDs = append(trackedIDs, l.ID)
			}
		}
	}
	if plan.RefLocaleID != "" && !trackedSet[plan.RefLocaleID] {
		trackedSet[plan.RefLocaleID] = true
		trackedIDs = append(trackedIDs, plan.RefLocaleID)
	}

	// Index messages per locale by exact EntryKey and context-scoped (Context, Source).
	type localeIndex struct {
		exact   map[domain.EntryKey]*tsxml.MessageEntry
		ctxSrc  map[domain.EntryKey]*tsxml.MessageEntry
	}
	idxByLocale := make(map[string]*localeIndex)

	if in.Bundle != nil {
		for _, tf := range in.Bundle.Files {
			if tf == nil {
				continue
			}
			li := idxByLocale[tf.LocaleID]
			if li == nil {
				li = &localeIndex{
					exact:  make(map[domain.EntryKey]*tsxml.MessageEntry),
					ctxSrc: make(map[domain.EntryKey]*tsxml.MessageEntry),
				}
				idxByLocale[tf.LocaleID] = li
			}
			for ci := range tf.Contexts {
				ctx := &tf.Contexts[ci]
				for mi := range ctx.Messages {
					m := &ctx.Messages[mi]
					k := m.Key()
					if _, exists := li.exact[k]; !exists {
						li.exact[k] = m
					}
					cs := k.ContextSource()
					if _, exists := li.ctxSrc[cs]; !exists {
						li.ctxSrc[cs] = m
					}
				}
			}
		}
	}

	// Collect unique keys in deterministic order from Bundle.Files.
	var rows []MatrixRow
	seenKeys := make(map[domain.EntryKey]int)
	seenContexts := make(map[string]bool)

	addRow := func(k domain.EntryKey, extraCmt, loc string, numerus, inTS bool) {
		if k.Source == "" {
			return
		}
		if idx, exists := seenKeys[k]; exists {
			if rows[idx].ExtraComment == "" && extraCmt != "" {
				rows[idx].ExtraComment = extraCmt
			}
			if rows[idx].Location == "" && loc != "" {
				rows[idx].Location = loc
			}
			if numerus {
				rows[idx].Numerus = true
			}
			return
		}
		seenKeys[k] = len(rows)
		if k.Context != "" {
			if !seenContexts[k.Context] {
				seenContexts[k.Context] = true
				plan.Summary.ContextNames = append(plan.Summary.ContextNames, k.Context)
			}
			plan.Summary.ContextCounts[k.Context]++
		}
		rows = append(rows, MatrixRow{
			Index:        len(rows),
			Key:          k,
			ExtraComment: extraCmt,
			Location:     loc,
			Numerus:      numerus,
			InTS:         inTS,
			Cells:        make(map[string]CellPlan, len(trackedIDs)),
		})
	}

	if in.Bundle != nil {
		for _, tf := range in.Bundle.Files {
			if tf == nil {
				continue
			}
			for _, ctx := range tf.Contexts {
				for _, m := range ctx.Messages {
					loc := ""
					if m.LocationFile != "" {
						if m.LocationLine != "" {
							loc = fmt.Sprintf("%s:%s", m.LocationFile, m.LocationLine)
						} else {
							loc = m.LocationFile
						}
					}
					addRow(m.Key(), m.ExtraComment, loc, m.Numerus, true)
				}
			}
		}
	}

	// Phantom-row protection: only append ExtraEntries not in .ts when AppendMissing is true.
	if in.AppendMissing {
		for _, extra := range in.ExtraEntries {
			k := extra
			if k.Context == "" && in.DefaultCtx != "" {
				k.Context = in.DefaultCtx
			}
			if _, exists := seenKeys[k]; !exists {
				addRow(k, "", "", false, false)
			}
		}
	}

	plan.Summary.TotalRows = len(rows)
	plan.Summary.MultiContext = len(plan.Summary.ContextNames) > 1

	for _, id := range trackedIDs {
		plan.Summary.LocaleStats[id] = LocaleStats{
			LocaleID: id,
			Total:    len(rows),
		}
	}

	for rIdx := range rows {
		row := &rows[rIdx]
		for _, locID := range trackedIDs {
			var msgPtr *tsxml.MessageEntry
			if li := idxByLocale[locID]; li != nil {
				if m, ok := li.exact[row.Key]; ok {
					msgPtr = m
				} else if m, ok := li.ctxSrc[row.Key.ContextSource()]; ok {
					msgPtr = m
				}
			}

			oldText := ""
			status := domain.StatusMissing
			numerus := row.Numerus
			if msgPtr != nil {
				oldText = msgPtr.Translation
				status = msgPtr.Status
				if msgPtr.Numerus {
					numerus = true
				}
			}

			cp := evalCellPlan(row.Key, oldText, status, numerus, locID, in.Catalog, in.WriteMode, in.UILang)
			row.Cells[locID] = cp
			applyCellToStats(plan, locID, cp, +1)
		}

		if plan.RefLocaleID != "" {
			if refCell, ok := row.Cells[plan.RefLocaleID]; ok {
				row.RefText = refCell.Effective
			}
		}
		recomputeRowFlags(row)
	}

	finalizeCompletions(&plan.Summary)
	plan.Rows = rows
	return plan
}

func isUntranslatedStatus(s domain.TransStatus) bool {
	return s == domain.StatusMissing || s == domain.StatusEmpty || s == domain.StatusUnfinished
}

func evalCellPlan(
	key domain.EntryKey,
	oldText string,
	status domain.TransStatus,
	numerus bool,
	localeID string,
	cat *domain.Catalog,
	mode domain.WriteMode,
	uiLang string,
) CellPlan {
	cp := CellPlan{
		LocaleID:  localeID,
		OldText:   oldText,
		Effective: oldText,
		Status:    status,
		Action:    domain.ActionNone,
		Numerus:   numerus,
	}
	if status != domain.StatusMissing {
		cp.Origin = domain.OriginTS
	}

	var cand domain.TranslationEntry
	var hasCand bool
	if cat != nil {
		cand, hasCand = cat.Lookup(localeID, key)
	}

	switch {
	case status == domain.StatusMissing:
		if hasCand && cand.Text != "" {
			cp.Action = domain.ActionAppendNew
			cp.NewText = cand.Text
			cp.Effective = cand.Text
			cp.Origin = cand.Origin
		}
	case isUntranslatedStatus(status):
		if hasCand && cand.Text != "" {
			cp.Action = domain.ActionFillGap
			cp.NewText = cand.Text
			cp.Effective = cand.Text
			cp.Origin = cand.Origin
		}
	case status == domain.StatusFinished:
		if hasCand && cand.Text != "" && cand.Text != oldText {
			if mode == domain.ModeOverwriteExisting {
				cp.Action = domain.ActionOverwrite
				cp.NewText = cand.Text
				cp.Effective = cand.Text
				cp.Origin = cand.Origin
			} else {
				cp.Action = domain.ActionSkipFinished
				cp.NewText = cand.Text
				cp.Effective = oldText
				cp.Origin = domain.OriginTS
			}
		}
	}

	if cp.Effective != "" {
		cp.Warnings = domain.ValidateTranslationLang(key.Source, cp.Effective, numerus, uiLang)
	}
	return cp
}

func cellIsGapAfter(cp CellPlan) bool {
	if cp.Status == domain.StatusVanished {
		return false
	}
	return isUntranslatedStatus(cp.Status) && !cp.Action.Mutates()
}

func applyCellToStats(plan *MatrixPlan, locID string, cp CellPlan, sign int) {
	st := plan.Summary.LocaleStats[locID]
	st.LocaleID = locID
	st.Total = plan.Summary.TotalRows

	if isUntranslatedStatus(cp.Status) {
		st.GapsBefore += sign
	}
	if cellIsGapAfter(cp) {
		st.GapsAfter += sign
		plan.Summary.TotalGaps += sign
	} else if cp.Status != domain.StatusVanished {
		st.Translated += sign
	}

	switch cp.Action {
	case domain.ActionFillGap, domain.ActionOverwrite:
		st.Changed += sign
		plan.Summary.TotalChanges += sign
	case domain.ActionAppendNew:
		st.Appended += sign
		plan.Summary.TotalAppends += sign
	}

	if len(cp.Warnings) > 0 {
		st.Warnings += sign
		plan.Summary.TotalWarnings += sign
	}

	plan.Summary.LocaleStats[locID] = st
}

func finalizeCompletions(sum *PlanSummary) {
	for id, st := range sum.LocaleStats {
		if st.Total > 0 {
			st.Completion = float64(st.Translated) / float64(st.Total)
		}
		sum.LocaleStats[id] = st
	}
}

func recomputeRowFlags(row *MatrixRow) {
	row.HasGap = false
	row.HasChange = false
	row.HasWarning = false
	for _, cp := range row.Cells {
		if cellIsGapAfter(cp) {
			row.HasGap = true
		}
		if cp.Action.Mutates() {
			row.HasChange = true
		}
		if len(cp.Warnings) > 0 {
			row.HasWarning = true
		}
	}
}

// UpdateCell updates a single (rowIdx, localeID) cell in O(1) and adjusts PlanSummary deltas.
func UpdateCell(plan *MatrixPlan, rowIdx int, localeID string, cat *domain.Catalog) bool {
	if plan == nil || rowIdx < 0 || rowIdx >= len(plan.Rows) {
		return false
	}
	row := &plan.Rows[rowIdx]
	oldCell, ok := row.Cells[localeID]
	if !ok {
		return false
	}

	applyCellToStats(plan, localeID, oldCell, -1)
	newCell := evalCellPlan(row.Key, oldCell.OldText, oldCell.Status, oldCell.Numerus, localeID, cat, plan.WriteMode, plan.UILang)
	row.Cells[localeID] = newCell
	applyCellToStats(plan, localeID, newCell, +1)

	if st, exists := plan.Summary.LocaleStats[localeID]; exists && st.Total > 0 {
		st.Completion = float64(st.Translated) / float64(st.Total)
		plan.Summary.LocaleStats[localeID] = st
	}

	if localeID == plan.RefLocaleID {
		row.RefText = newCell.Effective
	}
	recomputeRowFlags(row)
	return true
}

// BuildPatchesForFile extracts ordered tsxml.PatchItems for localeID from plan.
func BuildPatchesForFile(plan *MatrixPlan, tf *tsxml.TSFile, localeID string) []tsxml.PatchItem {
	if plan == nil {
		return nil
	}
	if localeID == "" && tf != nil {
		localeID = tf.LocaleID
	}
	var patches []tsxml.PatchItem
	for _, row := range plan.Rows {
		cp, ok := row.Cells[localeID]
		if !ok || !cp.Action.Mutates() {
			continue
		}
		isNumerus := row.Numerus || cp.Numerus
		var forms []string
		if isNumerus {
			forms = domain.SplitNumerus(cp.NewText)
		}
		patches = append(patches, tsxml.PatchItem{
			Key:          row.Key,
			NewText:      cp.NewText,
			NumerusForms: forms,
			Action:       cp.Action,
			Numerus:      isNumerus,
		})
	}
	return patches
}

// BuildExportRows converts a MatrixPlan into sheet.ExportOptions filtered by scope.
func BuildExportRows(plan *MatrixPlan, scope sheet.ExportScope, locales []locale.LocaleSpec) sheet.ExportOptions {
	opts := sheet.ExportOptions{
		SheetName: "Translations",
	}
	if plan == nil {
		return opts
	}

	opts.IncludeContext = plan.Summary.MultiContext
	for _, r := range plan.Rows {
		if r.Key.Comment != "" {
			opts.IncludeComment = true
			break
		}
	}

	for _, l := range locales {
		if !l.Enabled && !l.IsRef {
			continue
		}
		header := l.ID
		if strings.TrimSpace(l.Name) != "" && l.Name != l.ID {
			header = fmt.Sprintf("%s (%s)", l.Name, l.ID)
		}
		opts.Columns = append(opts.Columns, sheet.ExportColumn{
			LocaleID: l.ID,
			Header:   header,
			IsRef:    l.IsRef,
		})
	}

	for _, r := range plan.Rows {
		switch scope {
		case sheet.ExportUntranslated:
			if !r.HasGap {
				continue
			}
		case sheet.ExportChanged:
			if !r.HasChange {
				continue
			}
		case sheet.ExportWarnings:
			if !r.HasWarning {
				continue
			}
		}

		cells := make(map[string]sheet.ExportCell, len(opts.Columns))
		for _, col := range opts.Columns {
			cp := r.Cells[col.LocaleID]
			cells[col.LocaleID] = sheet.ExportCell{
				Text:       cp.Effective,
				Status:     cp.Status,
				Changed:    cp.Action.Mutates(),
				HasWarning: len(cp.Warnings) > 0,
			}
		}
		opts.Rows = append(opts.Rows, sheet.ExportRow{
			Context: r.Key.Context,
			Source:  r.Key.Source,
			Comment: r.Key.Comment,
			Cells:   cells,
		})
	}

	return opts
}
