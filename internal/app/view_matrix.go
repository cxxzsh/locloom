package app

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/i18n"
	"locloom/internal/planner"
)

func (a *App) viewMatrix(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(1.5)).Children(func() {
		// Filter & View Mode Toolbar
		ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Wrap().Children(func() {
			if ui.Segmented(c, &a.statusFilter, tr.FilterAll, tr.FilterGaps, tr.FilterPending, tr.FilterWarnings).Changed() {
				a.refilterRows()
			}

			if ui.Segmented(c, &a.cfg.MatrixViewMode, tr.ViewModeFocus, tr.ViewModeFull).Changed() {
				a.scheduleConfigSave()
			}

			if a.cfg.MatrixViewMode == 0 {
				ids := a.enabledNonRefIDs()
				if len(ids) > 0 {
					if ui.Select(c, &a.focusedLocale, ids).Width(115).Changed() {
						a.activeEditLocale = a.focusedLocale
						a.syncEditorFromSelection()
					}
				}
			}

			if ui.SearchField(c, &a.searchQuery).
				Bind(&a.searchHandle).
				Placeholder(tr.PlaceholderSearch).
				Grow(1).
				MinWidth(180).
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
			})
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

	cols := []ui.TableColumn{
		{ID: "idx", Title: "#", Width: 44, Align: ui.End, Fixed: true, RowHeader: true},
		{ID: "ctx", Title: tr.ColContext, Width: 120, MinWidth: 85, Sortable: false},
		{ID: "src", Title: tr.ColSource, Width: 210, MinWidth: 140},
		{ID: "ref", Title: refHeader, Width: 170, MinWidth: 120},
		{ID: "tgt", Title: targetHeader, Width: 210, MinWidth: 140},
		{ID: "status", Title: tr.ColStatus, Width: 125, MinWidth: 95},
	}

	tbl := ui.Table(c, &a.tableState, cols, len(a.filteredRows), func(row, col int) {
		r := &a.plan.Rows[a.filteredRows[row]]
		switch col {
		case 0:
			ui.Text(c, strconv.Itoa(r.Index+1)).FontSize(12).TextColor(t.TextMuted).SingleLine()
		case 1:
			ctxName := r.Key.Context
			if ctxName == "" {
				ctxName = "-"
			}
			ui.Text(c, ctxName).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(ctxName)
		case 2:
			ui.Text(c, r.Key.Source).FontSize(12).SingleLine().Tooltip(r.Key.Source)
		case 3:
			ui.Text(c, r.RefText).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(r.RefText)
		case 4:
			if targetLoc == "" {
				ui.Text(c, "-").FontSize(12).TextColor(t.TextMuted).SingleLine()
				return
			}
			cell := r.Cells[targetLoc]
			txt := cell.Effective
			colr := t.Text
			if txt == "" {
				txt = tr.EmptyCell
				colr = t.TextMuted
			}
			ui.Text(c, txt).FontSize(12).TextColor(colr).SingleLine().Tooltip(cell.Effective)
		case 5:
			if targetLoc == "" {
				return
			}
			cell := r.Cells[targetLoc]
			badge, fg := formatCellStatusBadge(c, cell, tr)
			ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
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

	cols := make([]ui.TableColumn, 0, 4+len(locs))
	cols = append(cols,
		ui.TableColumn{ID: "idx", Title: "#", Width: 48, Align: ui.End, Fixed: true, RowHeader: true},
		ui.TableColumn{ID: "ctx", Title: tr.ColContext, Width: 120, MinWidth: 80},
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
		switch col {
		case 0:
			ui.Text(c, strconv.Itoa(r.Index+1)).FontSize(12).TextColor(t.TextMuted).SingleLine()
		case 1:
			ctxName := r.Key.Context
			if ctxName == "" {
				ctxName = "-"
			}
			ui.Text(c, ctxName).FontSize(12).TextColor(t.TextMuted).SingleLine()
		case 2:
			ui.Text(c, r.Key.Source).FontSize(12).SingleLine().Tooltip(r.Key.Source)
		case 3:
			ui.Text(c, r.RefText).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(r.RefText)
		default:
			locIdx := col - 4
			if locIdx < 0 || locIdx >= len(locs) {
				return
			}
			locID := locs[locIdx].ID
			cell := r.Cells[locID]
			tag, fg := shortMatrixTag(c, cell, tr)
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				if tag != "" {
					ui.Text(c, tag).FontSize(11).FontWeight(700).TextColor(fg).SingleLine()
				}
				txt := cell.Effective
				if txt == "" {
					txt = "-"
				}
				ui.Text(c, txt).FontSize(12).SingleLine().Shrink(1).Tooltip(cell.Effective)
			})
		}
	}).Grow(1).MinHeight(0)

	if tbl.Changed() {
		a.syncEditorFromSelection()
	}
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
	default:
		if origin == "" {
			return "-"
		}
		return origin
	}
}
