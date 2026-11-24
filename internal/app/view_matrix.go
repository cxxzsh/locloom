package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/i18n"
	"locloom/internal/planner"
)

func (a *App) viewMatrix(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	card(c, t).Fill().Margin(0, 0, t.Space(1.5), 0).Padding(t.Space(2)).Gap(t.Space(1.5)).Children(func() {
		// Filter & View Mode Toolbar (kept on 1 crisp line at 1280px)
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Wrap().Children(func() {
			warnLabel := tr.FilterWarnings
			if a.plan != nil {
				totalWarns := 0
				for id, st := range a.plan.Summary.LocaleStats {
					if id != a.plan.RefLocaleID {
						totalWarns += st.Warnings
					}
				}
				if totalWarns > 0 {
					warnLabel = fmt.Sprintf("%s (%d)", tr.FilterWarnings, totalWarns)
				}
			}
			if ui.Segmented(c, &a.statusFilter, tr.FilterAll, tr.FilterGaps, tr.FilterPending, warnLabel).Shrink(0).Tooltip(tr.TipStatusFilter).Changed() {
				a.refilterRows()
			}

			if ui.Segmented(c, &a.cfg.MatrixViewMode, tr.ViewModeFocus, tr.ViewModeFull).Shrink(0).Tooltip(tr.TipMatrixViewMode).Changed() {
				a.scheduleConfigSave()
			}

			if a.cfg.MatrixViewMode == 0 {
				ids := a.enabledNonRefIDs()
				if len(ids) > 0 {
					labels := make([]string, len(ids))
					idToLabel := make(map[string]string, len(ids))
					for i, id := range ids {
						lbl := a.localeSelectLabel(id)
						labels[i] = lbl
						idToLabel[id] = lbl
					}
					selectedLabel := idToLabel[a.focusedLocale]
					if selectedLabel == "" && len(labels) > 0 {
						selectedLabel = labels[0]
					}
					sel := ui.Select(c, &selectedLabel, labels).Width(168).Shrink(0).Tooltip(tr.TipFocusedLocale)
					changed := sel.Changed()
					if changed {
						if idx := slices.Index(labels, selectedLabel); idx >= 0 {
							a.focusedLocale = ids[idx]
						}
					}
					sel.HandleInput(func(ev ui.InputEvent) bool {
						if ev.Kind == ui.InputScroll && ev.DY != 0 {
							cur := slices.Index(ids, a.focusedLocale)
							if cur < 0 {
								cur = 0
							}
							if ev.DY < 0 {
								cur = (cur - 1 + len(ids)) % len(ids)
							} else {
								cur = (cur + 1) % len(ids)
							}
							a.focusedLocale = ids[cur]
							changed = true
							return true
						}
						return false
					})
					if changed {
						a.activeEditLocale = a.focusedLocale
						a.syncEditorFromSelection()
					}
				}
			}

			if ui.SearchField(c, &a.searchQuery).
				Bind(&a.searchHandle).
				Placeholder(tr.PlaceholderSearch).
				Grow(1).
				MinWidth(90).
				Tooltip(tr.TipSearchField).
				Changed() {
				a.refilterRows()
			}
			ui.MenuButton(c, tr.MenuClear, func(m *ui.Menu) {
				if m.Item(tr.MenuClearMachine).Chosen() {
					a.clearMachineTranslations()
				}
				if m.Item(tr.MenuClearAll).Chosen() {
					a.clearAllTranslations()
				}
			}).Shrink(0).Radius(pillRadius).Tooltip(tr.TipMenuClear)
		})

		if a.plan == nil || len(a.filteredRows) == 0 {
			ui.Column(c).Grow(1).Center().Gap(t.Space(1.5)).Children(func() {
				if a.plan == nil || a.plan.Summary.TotalRows == 0 {
					ui.Text(c, tr.EmptyNoDataTitle).FontSize(14).FontWeight(600).TextColor(t.TextMuted)
					ui.Text(c, tr.EmptyNoDataHint).FontSize(12).TextColor(t.TextMuted)
				} else {
					ui.Text(c, tr.EmptyNoMatch).FontSize(13).TextColor(t.TextMuted)
				}
			})
			return
		}

		a.syncTableSelection()
		if a.cfg.MatrixViewMode == 1 {
			a.viewFullMatrixTable(c)
		} else {
			a.viewFocusedMatrixTable(c)
		}
		a.flushTableOps(c)
	})
}

func (a *App) viewFocusedMatrixTable(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()
	targetLoc := a.focusedLocale
	targetHeader := tr.ColTarget
	if targetLoc != "" {
		targetHeader = fmt.Sprintf(tr.ColTargetFmt, targetLoc)
	}
	refHeader := tr.ColRef
	if a.plan != nil && a.plan.RefLocaleID != "" {
		refHeader = fmt.Sprintf(tr.ColRefFmt, a.plan.RefLocaleID)
	}

	ctxTitle := tr.ColContext
	if a.bundle != nil && a.bundle.Format != format.FormatQtTS && a.bundle.Format != format.FormatGettextPO {
		ctxTitle = tr.ColKeyID
	}

	cols := []ui.TableColumn{
		{ID: "idx", Title: "#", Width: 38, Align: ui.End, Fixed: true, RowHeader: true},
		{ID: "ctx", Title: ctxTitle, Width: 115, MinWidth: 80, Sortable: false},
		{ID: "src", Title: tr.ColSource, Width: 175, MinWidth: 120},
		{ID: "ref", Title: refHeader, Width: 145, MinWidth: 100},
		{ID: "tgt", Title: targetHeader, Width: 175, MinWidth: 120},
		{ID: "status", Title: tr.ColStatus, Width: 105, MinWidth: 85},
	}

	plan := a.plan
	rows := a.filteredRows
	tbl := ui.Table(c, &a.tableState, cols, len(rows), func(row, col int) {
		if plan == nil || row < 0 || row >= len(rows) {
			return
		}
		rowIdx := rows[row]
		if rowIdx < 0 || rowIdx >= len(plan.Rows) {
			return
		}
		r := &plan.Rows[rowIdx]
		sel := row == a.selectedRow
		mutedOrSel := func(el ui.Element) ui.Element {
			if !sel {
				el.TextColor(t.TextMuted)
			}
			return el
		}
		switch col {
		case 0:
			mutedOrSel(ui.Text(c, strconv.Itoa(r.Index+1)).FontSize(12).SingleLine())
		case 1:
			ctxName := formatRowCtxOrKey(r)
			mutedOrSel(ui.Text(c, ctxName).FontSize(12).SingleLine()).Tooltip(ctxName)
		case 2:
			ui.Text(c, r.Key.Source).FontSize(12).SingleLine().Tooltip(r.Key.Source)
		case 3:
			mutedOrSel(ui.Text(c, r.RefText).FontSize(12).SingleLine()).Tooltip(r.RefText)
		case 4:
			if targetLoc == "" {
				mutedOrSel(ui.Text(c, "-").FontSize(12).SingleLine())
				return
			}
			cell := r.Cells[targetLoc]
			txt := cell.Effective
			if r.Numerus && txt != "" {
				if a.cfg.EnablePluralForms {
					txt = domain.FormatNumerusPreview(txt)
				} else if parts := domain.SplitNumerus(txt); len(parts) > 0 {
					txt = parts[0]
				}
			}
			el := ui.Text(c, txt).FontSize(12).SingleLine()
			if txt == "" {
				el = mutedOrSel(ui.Text(c, tr.EmptyCell).FontSize(12).SingleLine())
			}
			el.Tooltip(txt)
		case 5:
			if targetLoc == "" {
				return
			}
			cell := r.Cells[targetLoc]
			badge, fg := formatCellStatusBadge(c, cell, tr)
			pillBg := statusTint(t, fg)
			pillFg := fg
			if sel {
				pillBg = t.Background
			}
			pill := ui.Row(c).
				Padding(t.Space(0.5), t.Space(1.5)).
				Radius(pillRadius).
				Background(pillBg).
				AlignItems(ui.Center)
			if len(cell.Warnings) > 0 {
				pill.Tooltip(strings.Join(cell.Warnings, "\n"))
			}
			pill.Children(func() {
				ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(pillFg).SingleLine()
			})
		}
	}).Grow(1).MinHeight(0)

	if tbl.Changed() {
		a.syncEditorFromSelection()
	}
}

func (a *App) viewFullMatrixTable(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()
	locs := a.enabledNonRefLocales()

	refHeader := tr.ColRefShort
	if a.plan != nil && a.plan.RefLocaleID != "" {
		refHeader = fmt.Sprintf(tr.ColRefFmt, a.plan.RefLocaleID)
	}

	ctxTitle := tr.ColContext
	if a.bundle != nil && a.bundle.Format != format.FormatQtTS && a.bundle.Format != format.FormatGettextPO {
		ctxTitle = tr.ColKeyID
	}

	cols := make([]ui.TableColumn, 0, 4+len(locs))
	cols = append(cols,
		ui.TableColumn{ID: "idx", Title: "#", Width: 48, Align: ui.End, Fixed: true, RowHeader: true},
		ui.TableColumn{ID: "ctx", Title: ctxTitle, Width: 130, MinWidth: 80},
		ui.TableColumn{ID: "src", Title: tr.ColSourceShort, Width: 180, MinWidth: 120},
		ui.TableColumn{ID: "ref", Title: refHeader, Width: 150, MinWidth: 100},
	)
	for _, l := range locs {
		cols = append(cols, ui.TableColumn{
			ID:       "loc_" + l.ID,
			Title:    l.ID,
			Width:    145,
			MinWidth: 90,
		})
	}

	plan := a.plan
	rows := a.filteredRows
	tbl := ui.Table(c, &a.tableState, cols, len(rows), func(row, col int) {
		if plan == nil || row < 0 || row >= len(rows) {
			return
		}
		rowIdx := rows[row]
		if rowIdx < 0 || rowIdx >= len(plan.Rows) {
			return
		}
		r := &plan.Rows[rowIdx]
		sel := row == a.selectedRow
		mutedOrSel := func(el ui.Element) ui.Element {
			if !sel {
				el.TextColor(t.TextMuted)
			}
			return el
		}
		switch col {
		case 0:
			mutedOrSel(ui.Text(c, strconv.Itoa(r.Index+1)).FontSize(12).SingleLine())
		case 1:
			ctxName := formatRowCtxOrKey(r)
			mutedOrSel(ui.Text(c, ctxName).FontSize(12).SingleLine()).Tooltip(ctxName)
		case 2:
			ui.Text(c, r.Key.Source).FontSize(12).SingleLine().Tooltip(r.Key.Source)
		case 3:
			mutedOrSel(ui.Text(c, r.RefText).FontSize(12).SingleLine()).Tooltip(r.RefText)
		default:
			locIdx := col - 4
			if locIdx < 0 || locIdx >= len(locs) {
				return
			}
			locID := locs[locIdx].ID
			cell := r.Cells[locID]
			tag, fg := shortMatrixTag(c, cell, tr)
			tip := cell.Effective
			if len(cell.Warnings) > 0 {
				warnTip := strings.Join(cell.Warnings, "\n")
				if tip != "" {
					tip = warnTip + "\n" + tip
				} else {
					tip = warnTip
				}
			}
			cellRow := ui.Row(c).
				Margin(-t.Space(1.5), -t.Space(2.5)).
				Grow(1).
				AlignSelf(ui.Stretch).
				Gap(t.Space(1)).
				Padding(t.Space(0.5), t.Space(1)).
				AlignItems(ui.Center).
				Cursor(ui.CursorPointer).
				Tooltip(tip)
			cellRow.Children(func() {
				if tag != "" {
					tagFg := fg
					if sel {
						tagFg = accentInk(t)
					}
					ui.Text(c, tag).FontSize(11).FontWeight(700).TextColor(tagFg).SingleLine().Shrink(0)
				}
				txt := cell.Effective
				if r.Numerus && txt != "" {
					if a.cfg.EnablePluralForms {
						txt = domain.FormatNumerusPreview(txt)
					} else if parts := domain.SplitNumerus(txt); len(parts) > 0 {
						txt = parts[0]
					}
				}
				if txt == "" {
					txt = "-"
				}
				ui.Text(c, txt).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
			})
			if cellRow.Clicked() {
				a.selectedRow = row
				a.focusedLocale = locID
				a.activeEditLocale = locID
				a.syncEditorFromSelection()
			}
		}
	}).Grow(1).MinHeight(0)
	if tbl.Changed() {
		a.syncEditorFromSelection()
	}
}

func formatRowCtxOrKey(r *planner.MatrixRow) string {
	if r.Key.ID != "" && r.Key.Context != "" {
		return r.Key.Context + " · " + r.Key.ID
	}
	if r.Key.ID != "" {
		return r.Key.ID
	}
	if r.Key.Context != "" {
		return r.Key.Context
	}
	return "-"
}

func formatCellStatusBadge(c *ui.Context, cell planner.CellPlan, tr *i18n.Catalog) (string, ui.Color) {
	t := c.Theme()
	if len(cell.Warnings) > 0 {
		return fmt.Sprintf(tr.BadgeWarnFmt, len(cell.Warnings)), t.Danger
	}
	switch cell.Action {
	case domain.ActionFillGap:
		return tr.BadgeFillPrefix + originLabel(cell.Origin, tr), t.Accent
	case domain.ActionOverwrite:
		return tr.BadgeOverPrefix + originLabel(cell.Origin, tr), t.Warning
	case domain.ActionAppendNew:
		return tr.BadgeAddPrefix + originLabel(cell.Origin, tr), t.Success
	case domain.ActionSkipFinished:
		return tr.BadgeSkipped, t.TextMuted
	default:
		if cell.Effective == "" {
			return tr.BadgePending, t.Warning
		}
		return tr.BadgeReady, t.Success
	}
}

func shortMatrixTag(c *ui.Context, cell planner.CellPlan, tr *i18n.Catalog) (string, ui.Color) {
	t := c.Theme()
	if len(cell.Warnings) > 0 {
		return tr.TagWarn, t.Danger
	}
	switch cell.Action {
	case domain.ActionFillGap:
		return tr.TagFill, t.Accent
	case domain.ActionOverwrite:
		return tr.TagOver, t.Warning
	case domain.ActionAppendNew:
		return tr.TagAdd, t.Success
	default:
		return "", t.TextMuted
	}
}

func originLabel(origin string, tr *i18n.Catalog) string {
	switch origin {
	case domain.OriginTS:
		return tr.OriginTS
	case domain.OriginXLSX:
		return tr.OriginXLSX
	case domain.OriginTM:
		return tr.OriginTM
	case domain.OriginGoogle:
		return tr.OriginGoogle
	case domain.OriginLLM:
		return tr.OriginLLM
	case domain.OriginManual:
		return tr.OriginManual
	case domain.OriginCache:
		return tr.OriginCache
	case domain.OriginBing:
		return "Bing"
	case domain.OriginDeepL:
		return "DeepL"
	case domain.OriginMyMemory:
		return "MyMemory"
	case domain.OriginLibre:
		return "Lingva"
	case domain.OriginBaidu:
		return "Baidu"
	case domain.OriginNiutrans:
		return "Niutrans"
	default:
		if origin == "" {
			return "-"
		}
		return origin
	}
}

// tableCursorColumnID returns the table column holding the locale under
// edit: "tgt" in the focused matrix, its loc_* column in the full matrix.
func (a *App) tableCursorColumnID() string {
	if a.cfg.MatrixViewMode == 0 {
		return "tgt"
	}
	if a.activeEditLocale == "" {
		return ""
	}
	return "loc_" + a.activeEditLocale
}

func (a *App) deferTableOp(fn func()) {
	a.pendingTableOps = append(a.pendingTableOps, fn)
}

// syncTableSelection mirrors the single app-level selection into the
// table's Selection so CellsStyle paints the chosen row. The app has no
// multi-row operations; clicks that CellsStyle would have chosen keep
// the same key, so normalization here is idempotent.
func (a *App) syncTableSelection() {
	want := ""
	if a.selectedRow >= 0 && a.selectedRow < len(a.filteredRows) && a.plan != nil {
		want = a.plan.Rows[a.filteredRows[a.selectedRow]].Key.Canonical()
	}
	cur := ""
	for k := range a.rowSel.All() {
		cur = k
		break
	}
	if a.rowSel.Len() != 1 || cur != want {
		a.rowSel.Clear()
		if want != "" {
			a.rowSel.Add(want)
		}
	}
}

func (a *App) flushTableOps(c *ui.Context) {
	if len(a.pendingTableOps) == 0 {
		return
	}
	ops := a.pendingTableOps
	a.pendingTableOps = nil
	for _, fn := range ops {
		fn()
	}
	c.Invalidate()
}

func (a *App) buildMatrixCellMenu(m *ui.Menu, row, col int) {
	if a.plan == nil || row < 0 || row >= len(a.filteredRows) {
		return
	}
	planRowIdx := a.filteredRows[row]
	if planRowIdx < 0 || planRowIdx >= len(a.plan.Rows) {
		return
	}
	r := &a.plan.Rows[planRowIdx]
	rowKey := r.Key
	rowSource := r.Key.Source
	rowContext := r.Key.Context
	rowRefText := r.RefText
	ctxOrKey := formatRowCtxOrKey(r)
	tr := a.tr()

	locID := a.focusedLocale
	if a.cfg.MatrixViewMode == 1 {
		locs := a.enabledNonRefLocales()
		if col >= 4 && col-4 < len(locs) {
			locID = locs[col-4].ID
		} else if a.activeEditLocale != "" {
			locID = a.activeEditLocale
		}
	}

	focusRowKey := func() {
		a.selectedKey = rowKey
		for idx, rIdx := range a.filteredRows {
			if rIdx == planRowIdx {
				a.selectedRow = idx
				break
			}
		}
		if locID != "" {
			a.focusedLocale = locID
			a.activeEditLocale = locID
		}
		a.editBoundLocale = ""
		a.syncEditorFromSelection()
	}

	busy := a.translating || a.translatingRow
	if m.Item(tr.CtxTranslateRow).Disabled(busy).Chosen() {
		a.deferTableOp(func() {
			focusRowKey()
			a.translateRowAtAsync(planRowIdx, "")
		})
	}
	if m.Item(tr.CtxRetranslateRow).Disabled(busy).Chosen() {
		a.deferTableOp(func() {
			focusRowKey()
			a.retranslateRowAtAsync(planRowIdx)
		})
	}
	if locID != "" {
		if m.Item(fmt.Sprintf(tr.CtxRetranslateCellFmt, locID)).Disabled(busy).Chosen() {
			targetLoc := locID
			a.deferTableOp(func() {
				focusRowKey()
				a.translateRowAtAsync(planRowIdx, targetLoc)
			})
		}
	}

	m.Separator()

	if locID != "" {
		cell := r.Cells[locID]
		_, hasExact := a.catalog.LookupExact(locID, rowKey)
		canClearCell := cell.Effective != "" || hasExact
		if m.Item(fmt.Sprintf(tr.CtxClearCellFmt, locID)).Disabled(!canClearCell).Chosen() {
			targetLoc := locID
			a.deferTableOp(func() {
				focusRowKey()
				a.clearCellAt(planRowIdx, targetLoc)
			})
		}
	}
	rowHasAnyText := false
	rowHasAnyStaged := false
	for _, loc := range a.enabledNonRefLocales() {
		if c, ok := r.Cells[loc.ID]; ok && c.Effective != "" {
			rowHasAnyText = true
		}
		if _, ok := a.catalog.LookupExact(loc.ID, rowKey); ok {
			rowHasAnyStaged = true
		}
	}
	if m.Item(tr.CtxClearRow).Disabled(!rowHasAnyText && !rowHasAnyStaged).Chosen() {
		a.deferTableOp(func() {
			focusRowKey()
			a.clearRowAt(planRowIdx)
		})
	}

	if locID != "" {
		_, hasStagedCell := a.catalog.LookupExact(locID, rowKey)
		if m.Item(fmt.Sprintf(tr.CtxRevertCellFmt, locID)).Disabled(!hasStagedCell).Chosen() {
			targetLoc := locID
			a.deferTableOp(func() {
				focusRowKey()
				a.revertCellAt(planRowIdx, targetLoc)
			})
		}
	}
	if m.Item(tr.CtxRevertRow).Disabled(!rowHasAnyStaged).Chosen() {
		a.deferTableOp(func() {
			focusRowKey()
			a.revertRowAt(planRowIdx)
		})
	}

	m.Separator()

	if locID != "" {
		if m.Item(fmt.Sprintf(tr.CtxUseSrcFmt, locID)).Disabled(rowSource == "").Chosen() {
			targetLoc := locID
			a.deferTableOp(func() {
				focusRowKey()
				a.catalog.Set(targetLoc, rowKey, rowSource, domain.OriginManual)
				planner.UpdateCell(a.plan, planRowIdx, targetLoc, a.catalog)
				a.editBoundLocale = ""
				a.syncEditorFromSelection()
			})
		}
	}
	if m.Item(tr.CtxCopySource).Disabled(rowSource == "").Chosen() {
		a.deferTableOp(func() {
			focusRowKey()
			a.copySourceToClipboard()
		})
	}
	if locID != "" {
		targetTxt := r.Cells[locID].Effective
		if m.Item(fmt.Sprintf(tr.CtxCopyTargetFmt, locID)).Disabled(targetTxt == "").Chosen() {
			txt := targetTxt
			a.deferTableOp(func() {
				focusRowKey()
				a.copyText(txt)
			})
		}
	}
	if m.Item(tr.CtxCopyRef).Disabled(rowRefText == "").Chosen() {
		a.deferTableOp(func() {
			focusRowKey()
			a.copyText(rowRefText)
		})
	}
	if ctxOrKey != "-" {
		if m.Item(tr.CtxCopyKeyCtx).Chosen() {
			val := ctxOrKey
			a.deferTableOp(func() {
				focusRowKey()
				a.copyText(val)
			})
		}
	}

	if rowContext != "" {
		m.Separator()
		if a.contextFilter == rowContext {
			if m.Item(tr.CtxClearContextFilter).Chosen() {
				a.deferTableOp(func() {
					a.selectedKey = rowKey
					a.contextFilter = ""
					a.refilterRows()
				})
			}
		} else {
			if m.Item(fmt.Sprintf(tr.CtxFilterContextFmt, rowContext)).Chosen() {
				a.deferTableOp(func() {
					a.selectedKey = rowKey
					a.contextFilter = rowContext
					a.refilterRows()
				})
			}
		}
	}
}

func (a *App) buildMatrixHeaderMenu(m *ui.Menu, col int) {
	if a.plan == nil {
		return
	}
	tr := a.tr()
	var locID string
	if a.cfg.MatrixViewMode == 1 {
		locs := a.enabledNonRefLocales()
		if col >= 4 && col-4 < len(locs) {
			locID = locs[col-4].ID
		}
	} else if col == 4 {
		locID = a.focusedLocale
	}
	if locID == "" {
		return
	}
	if a.cfg.MatrixViewMode == 1 {
		if m.Item(fmt.Sprintf(tr.CtxHeaderFocusFmt, locID)).Chosen() {
			targetLoc := locID
			a.deferTableOp(func() {
				a.cfg.MatrixViewMode = 0
				a.focusedLocale = targetLoc
				a.activeEditLocale = targetLoc
				a.editBoundLocale = ""
				a.syncEditorFromSelection()
				a.scheduleConfigSave()
			})
		}
	}
	if m.Item(fmt.Sprintf(tr.CtxHeaderRevertColFmt, locID)).Chosen() {
		targetLoc := locID
		a.deferTableOp(func() {
			a.catalog.ClearLocale(targetLoc)
			a.editBoundLocale = ""
			a.rebuildPlan()
			a.syncEditorFromSelection()
			a.setStatus(fmt.Sprintf(tr.StatusRevertedColFmt, targetLoc), false)
		})
	}
}

func (a *App) localeSelectLabel(id string) string {
	for _, spec := range a.cfg.Locales {
		if spec.ID == id {
			name := spec.DisplayName(a.UILang())
			if name != "" && name != id {
				return id + " · " + name
			}
			break
		}
	}
	return id
}
