package app

import (
	"fmt"
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

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(1.5)).Children(func() {
		// Filter & View Mode Toolbar (kept on 1 crisp line at 1280px)
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Wrap().Children(func() {
			if ui.Segmented(c, &a.statusFilter, tr.FilterAll, tr.FilterGaps, tr.FilterPending, tr.FilterWarnings).Shrink(0).Changed() {
				a.refilterRows()
			}

			if ui.Segmented(c, &a.cfg.MatrixViewMode, tr.ViewModeFocus, tr.ViewModeFull).Shrink(0).Changed() {
				a.scheduleConfigSave()
			}

			if a.cfg.MatrixViewMode == 0 {
				ids := a.enabledNonRefIDs()
				if len(ids) > 0 {
					if ui.Select(c, &a.focusedLocale, ids).Width(92).Shrink(0).Changed() {
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
			}).Shrink(0)
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

		if a.cfg.MatrixViewMode == 1 {
			a.viewFullMatrixTable(c)
		} else {
			a.viewFocusedMatrixTable(c)
		}
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

	tbl := ui.Table(c, &a.tableState, cols, len(a.filteredRows), func(row, col int) {
		r := &a.plan.Rows[a.filteredRows[row]]
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
			pillBg := fg.Alpha(0.12)
			pillFg := fg
			if sel {
				pillBg = t.AccentText.Alpha(0.22)
				pillFg = t.AccentText
			}
			pill := ui.Row(c).
				Padding(t.Space(0.5), t.Space(1.5)).
				Radius(t.Radius).
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

	tbl := ui.Table(c, &a.tableState, cols, len(a.filteredRows), func(row, col int) {
		r := &a.plan.Rows[a.filteredRows[row]]
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
			bg := ui.Transparent
			if a.activeEditLocale == locID {
				if sel {
					bg = t.AccentText.Alpha(0.18)
				} else {
					bg = t.Accent.Alpha(0.08)
				}
			}
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
				FillWidth().
				Gap(t.Space(1)).
				Padding(t.Space(0.5), t.Space(1)).
				Radius(t.Radius).
				Background(bg).
				AlignItems(ui.Center).
				Cursor(ui.CursorPointer).
				Tooltip(tip)
			cellRow.Children(func() {
				if tag != "" {
					tagFg := fg
					if sel {
						tagFg = t.AccentText
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
