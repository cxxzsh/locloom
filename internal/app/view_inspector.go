package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/tsxml"
)

func (a *App) viewInspector(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(1.5)).Children(func() {
		logTabLabel := fmt.Sprintf("运行日志与诊断 (%d)", len(a.logs))
		ui.Tabs(c, &a.bottomTab, "词条精修与 XML 对比", logTabLabel)

		switch a.bottomTab {
		case 0:
			a.viewInspectorDetail(c)
		case 1:
			a.viewDiagnosticsLogs(c)
		}
	})
}

func (a *App) viewInspectorDetail(c *ui.Context) {
	t := c.Theme()
	_, row := a.currentMatrixRow()
	if row == nil {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, "在上方表格中点击任意词条行，即可在此处精修译文并预览 XML 回填差异").FontSize(12).TextColor(t.TextMuted)
		})
		return
	}

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(0.5)).Children(func() {
			// Metadata strip
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Wrap().Children(func() {
				ctxName := row.Key.Context
				if ctxName == "" {
					ctxName = "(无上下文)"
				}
				ui.Badge(c, "上下文: "+ctxName)
				if row.Location != "" {
					ui.Text(c, "位置: "+row.Location).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
				if row.Key.Comment != "" {
					ui.Text(c, "注释: "+row.Key.Comment).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
				if row.Numerus {
					ui.Badge(c, "复数 Numerus (使用 --- 分隔)")
				}
			})

			// Quick Locale Switcher Pills
			locs := a.enabledNonRefLocales()
			if len(locs) > 0 {
				ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, "目标语种:").FontSize(12).TextColor(t.TextMuted)
					for _, l := range locs {
						lid := l.ID
						active := a.activeEditLocale == lid
						bg := t.Surface
						fg := t.Text
						border := t.Border
						if active {
							bg = t.Accent.Alpha(0.15)
							fg = t.Accent
							border = t.Accent
						}
						pill := ui.Button(c, lid).
							Padding(t.Space(0.5), t.Space(2)).
							Background(bg).
							TextColor(fg).
							Border(1, border)
						if pill.Clicked() {
							a.activeEditLocale = lid
							a.syncEditorFromSelection()
						}
					}
				})
			}

			// Main 2-column inspector: Left (Source + Editor), Right (Surgical XML Diff)
			ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Start).Children(func() {
				// Left column: Source, RefText, Live TextArea
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1.5)).Children(func() {
					ui.Row(c).Gap(t.Space(2)).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Padding(t.Space(1.5)).Background(t.Surface).Radius(t.Radius).Children(func() {
							ui.Text(c, "英文原文 (Source)").FontSize(11).FontWeight(600).TextColor(t.TextMuted)
							ui.Text(c, row.Key.Source).FontSize(12).Selectable()
						})
						ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Padding(t.Space(1.5)).Background(t.Surface).Radius(t.Radius).Children(func() {
							ui.Text(c, "参考译文").FontSize(11).FontWeight(600).TextColor(t.TextMuted)
							refTxt := row.RefText
							if refTxt == "" {
								refTxt = "(无)"
							}
							ui.Text(c, refTxt).FontSize(12).Selectable()
						})
					})

					cell := row.Cells[a.activeEditLocale]
					ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
						ui.Text(c, fmt.Sprintf("译文编辑 (%s)", a.activeEditLocale)).FontSize(12).FontWeight(600)
						badge, fg := formatCellStatusBadge(c, cell)
						ui.Text(c, badge).FontSize(11).TextColor(fg)
						ui.Spacer(c)
						if ui.Button(c, "重置该格").Padding(t.Space(0.5), t.Space(2)).Clicked() {
							a.resetActiveCell()
						}
					})

					if row.Numerus {
						ui.Text(c, "提示：此词条包含复数形式，请使用单独一行 --- 分隔各个复数译文。").FontSize(11).TextColor(t.Warning)
					}

					if ui.TextArea(c, &a.editBuf).
						Placeholder("输入译文（实时保存，O(1) 刷新单元格）...").
						Lines(3, 6).
						Changed() {
						a.onEditCell(a.editBuf)
					}

					if len(cell.Warnings) > 0 {
						ui.Row(c).Gap(t.Space(1)).Wrap().Children(func() {
							for _, w := range cell.Warnings {
								ui.Text(c, "⚠ "+w).
									FontSize(11).
									TextColor(t.Danger).
									Padding(t.Space(0.5), t.Space(1.5)).
									Background(t.Danger.Alpha(0.12)).
									Radius(t.Radius)
							}
						})
					}
				})

				// Right column: Surgical XML Diff
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1)).Children(func() {
					cell := row.Cells[a.activeEditLocale]
					oldStatus := cell.Status
					if oldStatus == "" {
						oldStatus = domain.StatusUnfinished
					}
					newText := cell.Effective
					if newText == "" {
						newText = cell.OldText
					}
					beforeXML, afterXML := tsxml.FormatSnippetDiff(
						row.Key.Context,
						row.Key.Source,
						cell.OldText,
						newText,
						oldStatus,
						row.Numerus,
					)

					ui.Text(c, "修改前 XML (Before)").FontSize(11).FontWeight(600).TextColor(t.TextMuted)
					ui.Box(c).Padding(t.Space(1.5)).Background(t.Surface).Border(1, t.Border).Radius(t.Radius).Children(func() {
						ui.Text(c, beforeXML).Font("SF Mono, Menlo, monospace").FontSize(11).TextColor(t.TextMuted).Selectable()
					})

					ui.Text(c, "回填后 XML (After)").FontSize(11).FontWeight(600).TextColor(t.Accent)
					ui.Box(c).Padding(t.Space(1.5)).Background(t.Surface).Border(1, t.Accent.Alpha(0.5)).Radius(t.Radius).Children(func() {
						ui.Text(c, afterXML).Font("SF Mono, Menlo, monospace").FontSize(11).Selectable()
					})
				})
			})
		})
	})
}

func (a *App) viewDiagnosticsLogs(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("共 %d 条运行日志", len(a.logs))).FontSize(12).TextColor(t.TextMuted).Grow(1)
		if ui.Button(c, "复制日志").Padding(t.Space(1), t.Space(2)).Clicked() {
			a.copyLogsToClipboard()
		}
		if ui.Button(c, "清空日志").Padding(t.Space(1), t.Space(2)).Clicked() {
			a.logs = nil
		}
	})

	ui.List(c, &a.logListState, len(a.logs), func(i int) {
		ev := a.logs[i]
		col := t.Text
		switch strings.ToLower(ev.Level) {
		case "error":
			col = t.Danger
		case "warn":
			col = t.Warning
		}
		prefix := ev.Time.Format("15:04:05") + " [" + strings.ToUpper(ev.Level) + "]"
		if ev.ProviderID != "" {
			prefix += " [" + ev.ProviderID + "]"
		}
		if ev.LocaleID != "" {
			prefix += " [" + ev.LocaleID + "]"
		}
		ui.Row(c).Padding(t.Space(0.5), t.Space(1.5)).Gap(t.Space(1.5)).Children(func() {
			ui.Text(c, prefix).Font("SF Mono, Menlo, monospace").FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(0)
			ui.Text(c, ev.Message).FontSize(12).TextColor(col).Selectable()
		})
	}).Grow(1).MinHeight(0).Border(1, t.Border).Radius(t.Radius).Padding(t.Space(1))
}
