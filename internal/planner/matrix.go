package planner

import (
	"fmt"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/format"
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
	Numerus     bool
	PluralCount int
	Warnings    []string
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
	FormatBundle  *format.Bundle
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
	keyIndex    map[domain.EntryKey]int
	idCtxIndex  map[domain.EntryKey]int
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

	var fb *format.Bundle
	if in.FormatBundle != nil {
		fb = in.FormatBundle
	} else if in.Bundle != nil {
		fb = format.FromTSBundle(in.Bundle)
	}

	// Index entries per locale by exact EntryKey, IDContext (when ID != ""), and ContextSource (when ID == "").
	type localeIndex struct {
		exact  map[domain.EntryKey]*format.NormalizedEntry
		idCtx  map[domain.EntryKey]*format.NormalizedEntry
		ctxSrc map[domain.EntryKey]*format.NormalizedEntry
	}
	idxByLocale := make(map[string]*localeIndex)
	getLocaleIdx := func(locID string) *localeIndex {
		li := idxByLocale[locID]
		if li == nil {
			li = &localeIndex{
				exact:  make(map[domain.EntryKey]*format.NormalizedEntry),
				idCtx:  make(map[domain.EntryKey]*format.NormalizedEntry),
				ctxSrc: make(map[domain.EntryKey]*format.NormalizedEntry),
			}
			idxByLocale[locID] = li
		}
		return li
	}

	if fb != nil {
		for _, doc := range fb.Docs {
			if doc == nil {
				continue
			}
			for ei := range doc.Entries {
				e := &doc.Entries[ei]
				entryLoc := e.LocaleID
				if entryLoc == "" {
					entryLoc = doc.LocaleID
				}
				if entryLoc == "" {
					continue
				}
				li := getLocaleIdx(entryLoc)
				k := e.Key
				if _, exists := li.exact[k]; !exists {
					li.exact[k] = e
				}
				if k.ID != "" {
					ik := k.IDContext()
					if _, exists := li.idCtx[ik]; !exists {
						li.idCtx[ik] = e
					}
				} else {
					cs := k.ContextSource()
					if _, exists := li.ctxSrc[cs]; !exists {
						li.ctxSrc[cs] = e
					}
				}
			}
		}
	}

	// Collect unique keys in deterministic order from FormatBundle.Docs.
	var rows []MatrixRow
	seenKeys := make(map[domain.EntryKey]int)
	seenIDCtx := make(map[domain.EntryKey]int)
	seenContexts := make(map[string]bool)

	addRow := func(k domain.EntryKey, extraCmt, loc string, numerus, inTS bool) {
		if k.Source == "" && k.ID == "" {
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
		if k.ID != "" {
			ik := k.IDContext()
			if idx, exists := seenIDCtx[ik]; exists {
				if (rows[idx].Key.Source == "" || rows[idx].Key.Source == rows[idx].Key.ID) && k.Source != "" && k.Source != k.ID {
					rows[idx].Key.Source = k.Source
				}
				if rows[idx].Key.Comment == "" && k.Comment != "" {
					rows[idx].Key.Comment = k.Comment
				}
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
			seenIDCtx[ik] = len(rows)
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

	if fb != nil {
		for _, doc := range fb.Docs {
			if doc == nil {
				continue
			}
			for _, e := range doc.Entries {
				addRow(e.Key, e.ExtraComment, e.Location, e.Numerus, true)
			}
		}
	}

	// Phantom-row protection: only append ExtraEntries not in bundle when AppendMissing is true.
	if in.AppendMissing {
		for _, extra := range in.ExtraEntries {
			k := extra
			if k.Context == "" && in.DefaultCtx != "" {
				k.Context = in.DefaultCtx
			}
			_, exists := seenKeys[k]
			if !exists && k.ID != "" {
				_, exists = seenIDCtx[k.IDContext()]
			}
			if !exists {
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

	isMultiLocale := fb != nil && fb.Topology == format.TopologyMultiLocale
	for rIdx := range rows {
		row := &rows[rIdx]
		for _, locID := range trackedIDs {
			var entryPtr *format.NormalizedEntry
			if li := idxByLocale[locID]; li != nil {
				if e, ok := li.exact[row.Key]; ok {
					entryPtr = e
				} else if row.Key.ID != "" {
					if e, ok := li.idCtx[row.Key.IDContext()]; ok {
						entryPtr = e
					}
				} else if e, ok := li.ctxSrc[row.Key.ContextSource()]; ok {
					entryPtr = e
				}
			}

			oldText := ""
			status := domain.StatusMissing
			if isMultiLocale && row.InTS {
				// Multi-locale files (e.g. .xcstrings) treat absent localizations as empty gaps so they fill without AppendMissing.
				status = domain.StatusEmpty
			}
			numerus := row.Numerus
			pluralCount := 0
			if entryPtr != nil {
				oldText = entryPtr.Text
				if len(entryPtr.PluralForms) > 0 {
					pluralCount = len(entryPtr.PluralForms)
				}
				if oldText == "" && entryPtr.Numerus && len(entryPtr.PluralForms) > 0 {
					oldText = domain.JoinNumerus(entryPtr.PluralForms)
				}
				status = entryPtr.Status
				if entryPtr.Numerus {
					numerus = true
				}
			}
			cp := evalCellPlan(row.Key, oldText, status, numerus, locID, in.Catalog, in.WriteMode, in.UILang)
			cp.PluralCount = pluralCount
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
	plan.keyIndex = seenKeys
	plan.idCtxIndex = seenIDCtx
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

	if hasCand && cand.Cleared {
		if oldText != "" {
			cp.Action = domain.ActionOverwrite
			cp.NewText = ""
			cp.Effective = ""
			cp.Origin = cand.Origin
		} else {
			cp.Action = domain.ActionNone
			cp.NewText = ""
			cp.Effective = ""
		}
		return cp
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
			if mode == domain.ModeOverwriteExisting || cand.Origin == domain.OriginManual {
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
	if cp.Effective == "" && cp.Action == domain.ActionOverwrite {
		return true
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
	newCell.PluralCount = oldCell.PluralCount
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

// UpdateCellByKey updates a single cell by EntryKey and localeID in O(1) via BuildPlan's key index.
func UpdateCellByKey(plan *MatrixPlan, key domain.EntryKey, localeID string, cat *domain.Catalog) bool {
	if plan == nil {
		return false
	}
	if plan.keyIndex == nil {
		plan.keyIndex = make(map[domain.EntryKey]int, len(plan.Rows))
		plan.idCtxIndex = make(map[domain.EntryKey]int, len(plan.Rows))
		for i := range plan.Rows {
			rk := plan.Rows[i].Key
			plan.keyIndex[rk] = i
			if rk.ID != "" {
				plan.idCtxIndex[rk.IDContext()] = i
			}
		}
	}
	idx, ok := plan.keyIndex[key]
	if !ok && key.ID != "" && plan.idCtxIndex != nil {
		idx, ok = plan.idCtxIndex[key.IDContext()]
	}
	if !ok {
		return false
	}
	return UpdateCell(plan, idx, localeID, cat)
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

// BuildFormatPatches extracts per-locale PatchOps for all enabled locales with mutating actions.
func BuildFormatPatches(plan *MatrixPlan) map[string][]format.PatchOp {
	if plan == nil {
		return nil
	}
	out := make(map[string][]format.PatchOp)
	for _, locID := range plan.EnabledIDs {
		var ops []format.PatchOp
		for _, row := range plan.Rows {
			cp, ok := row.Cells[locID]
			if !ok || !cp.Action.Mutates() {
				continue
			}
			isNumerus := row.Numerus || cp.Numerus
			var forms []string
			if isNumerus {
				forms = domain.SplitNumerus(cp.NewText)
			}
			ops = append(ops, format.PatchOp{
				Key:         row.Key,
				LocaleID:    locID,
				NewText:     cp.NewText,
				PluralForms: forms,
				Action:      cp.Action,
				Numerus:     isNumerus,
			})
		}
		if len(ops) > 0 {
			out[locID] = ops
		}
	}
	return out
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
		if r.Key.ID != "" {
			opts.IncludeKeyID = true
		}
		if r.Key.Comment != "" {
			opts.IncludeComment = true
		}
		if opts.IncludeKeyID && opts.IncludeComment {
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
			KeyID:   r.Key.ID,
			Context: r.Key.Context,
			Source:  r.Key.Source,
			Comment: r.Key.Comment,
			Cells:   cells,
		})
	}

	return opts
}
