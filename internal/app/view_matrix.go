package app

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/planner"
)

func (a *App) viewMatrix(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(1.5)).Children(func() {
		// Filter & View Mode Toolbar
		ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Wrap().Children(func() {
			if ui.Segmented(c, &a.statusFilter, "全部", "待补齐", "待写入", "有告警").Changed() {
				a.refilterRows()
			}

			if ui.Segmented(c, &a.cfg.MatrixViewMode, "聚焦单语种", "全景矩阵").Changed() {
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
				Placeholder("搜索原文、上下文、译文 (Cmd/Ctrl+F)").
				Grow(1).
				MinWidth(180).
				Changed() {
				a.refilterRows()
			}

			ui.MenuButton(c, "清空...", func(m *ui.Menu) {
				if m.Item("清空机翻结果").Chosen() {
					a.clearMachineTranslations()
				}
				if m.Item("清空全部暂存译文").Chosen() {
					a.clearAllTranslations()
				}
			})
		})

		if a.plan == nil || len(a.filteredRows) == 0 {
			ui.Column(c).Grow(1).Center().Gap(t.Space(1.5)).Children(func() {
				if a.plan == nil || a.plan.Summary.TotalRows == 0 {
					ui.Text(c, "尚未加载词条数据").FontSize(14).FontWeight(600).TextColor(t.TextMuted)
					ui.Text(c, "请点击顶部「打开目录」/「打开 ZIP」/「导入 Excel」，或直接将文件拖拽到窗口").FontSize(12).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "当前筛选条件下没有匹配词条").FontSize(13).TextColor(t.TextMuted)
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
	targetLoc := a.focusedLocale
	targetHeader := "目标译文"
	if targetLoc != "" {
		targetHeader = "目标译文 (" + targetLoc + ")"
	}
	refHeader := "参考译文"
	if a.plan != nil && a.plan.RefLocaleID != "" {
		refHeader = "参考 (" + a.plan.RefLocaleID + ")"
	}

	cols := []ui.TableColumn{
		{ID: "idx", Title: "#", Width: 48, Align: ui.End, Fixed: true, RowHeader: true},
		{ID: "ctx", Title: "上下文", Width: 140, MinWidth: 90, Sortable: false},
		{ID: "src", Title: "英文原文 (Source)", MinWidth: 160},
		{ID: "ref", Title: refHeader, MinWidth: 140},
		{ID: "tgt", Title: targetHeader, MinWidth: 180},
		{ID: "status", Title: "状态/来源", Width: 120, MinWidth: 90},
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
				txt = "(空)"
				colr = t.TextMuted
			}
			ui.Text(c, txt).FontSize(12).TextColor(colr).SingleLine().Tooltip(cell.Effective)
		case 5:
			if targetLoc == "" {
				return
			}
			cell := r.Cells[targetLoc]
			badge, fg := formatCellStatusBadge(c, cell)
			ui.Text(c, badge).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
		}
	}).Grow(1).MinHeight(0)

	if tbl.Changed() {
		a.syncEditorFromSelection()
	}
}

func (a *App) viewFullMatrixTable(c *ui.Context) {
	t := c.Theme()
	locs := a.enabledNonRefLocales()

	refHeader := "参考"
	if a.plan != nil && a.plan.RefLocaleID != "" {
		refHeader = "参考 (" + a.plan.RefLocaleID + ")"
	}

	cols := make([]ui.TableColumn, 0, 4+len(locs))
	cols = append(cols,
		ui.TableColumn{ID: "idx", Title: "#", Width: 48, Align: ui.End, Fixed: true, RowHeader: true},
		ui.TableColumn{ID: "ctx", Title: "上下文", Width: 120, MinWidth: 80},
		ui.TableColumn{ID: "src", Title: "英文原文", Width: 180, MinWidth: 120},
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
			tag, fg := shortMatrixTag(c, cell)
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

func formatCellStatusBadge(c *ui.Context, cell planner.CellPlan) (string, ui.Color) {
	t := c.Theme()
	if len(cell.Warnings) > 0 {
		return fmt.Sprintf("⚠ 告警(%d)", len(cell.Warnings)), t.Danger
	}
	switch cell.Action {
	case domain.ActionFillGap:
		return "补齐 · " + originLabel(cell.Origin), t.Accent
	case domain.ActionOverwrite:
		return "覆盖 · " + originLabel(cell.Origin), t.Warning
	case domain.ActionAppendNew:
		return "新增 · " + originLabel(cell.Origin), t.Success
	case domain.ActionSkipFinished:
		return "已完成 (跳过)", t.TextMuted
	default:
		if cell.Effective == "" {
			return "待补齐", t.Warning
		}
		return "已就绪", t.Success
	}
}

func shortMatrixTag(c *ui.Context, cell planner.CellPlan) (string, ui.Color) {
	t := c.Theme()
	if len(cell.Warnings) > 0 {
		return "[⚠]", t.Danger
	}
	switch cell.Action {
	case domain.ActionFillGap:
		return "[补]", t.Accent
	case domain.ActionOverwrite:
		return "[改]", t.Warning
	case domain.ActionAppendNew:
		return "[新]", t.Success
	default:
		return "", t.TextMuted
	}
}

func originLabel(origin string) string {
	switch origin {
	case domain.OriginTS:
		return "TS"
	case domain.OriginXLSX:
		return "Excel"
	case domain.OriginTM:
		return "语料库"
	case domain.OriginGoogle:
		return "Google"
	case domain.OriginLLM:
		return "LLM"
	case domain.OriginManual:
		return "手工"
	case domain.OriginCache:
		return "缓存"
	default:
		if origin == "" {
			return "-"
		}
		return origin
	}
}
