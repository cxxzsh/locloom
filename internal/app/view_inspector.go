package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/format"
)

func (a *App) viewInspector(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(1.5)).Children(func() {
		logTabLabel := fmt.Sprintf(tr.TabLogsFmt, len(a.logs))
		ui.Tabs(c, &a.bottomTab, tr.TabInspector, logTabLabel).Shrink(0)

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
	tr := a.tr()
	_, row := a.currentMatrixRow()
	if row == nil {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr.InspectorEmptyHint).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}

	curFmt := format.FormatQtTS
	if a.bundle != nil && a.bundle.Format != "" {
		curFmt = a.bundle.Format
	}

	// 2-column horizontal layout so Left (Editor + Power Toolbar) and Right (Scrollable Format Diff) sit side-by-side cleanly within 240px.
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Gap(t.Space(2.5)).Children(func() {
		// Left Column: Metadata, Locale Pills, Source/Ref, Power Actions, Editor
		ui.Scroll(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Column(c).Gap(t.Space(1.5)).Padding(t.Space(0.5)).Children(func() {
				// Metadata strip
				ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Wrap().Children(func() {
					if row.Key.ID != "" {
						ui.Badge(c, tr.LabelKeyIDPrefix+row.Key.ID)
					}
					ctxName := row.Key.Context
					if ctxName == "" {
						ctxName = tr.NoContextLabel
					}
					ui.Badge(c, tr.LabelContextPrefix+ctxName)
					if row.Location != "" {
						ui.Text(c, tr.LabelLocationPrefix+row.Location).FontSize(11).TextColor(t.TextMuted).SingleLine()
					}
					if row.Key.Comment != "" {
						ui.Text(c, tr.LabelCommentPrefix+row.Key.Comment).FontSize(11).TextColor(t.TextMuted).SingleLine()
					}
					if row.Numerus {
						ui.Badge(c, tr.BadgeNumerus)
					}
				})

				// Quick Locale Switcher Pills
				locs := a.enabledNonRefLocales()
				if len(locs) > 0 {
					ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Wrap().Children(func() {
						ui.Text(c, tr.LabelTargetLocale).FontSize(11).TextColor(t.TextMuted)
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

				// Source & Reference cards
				ui.Row(c).Gap(t.Space(1.5)).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Padding(t.Space(1)).Background(t.Surface).Radius(t.Radius).Children(func() {
						ui.Text(c, tr.LabelSource).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
						ui.Text(c, row.Key.Source).FontSize(12).MinWidth(0).Selectable()
					})
					ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Padding(t.Space(1)).Background(t.Surface).Radius(t.Radius).Children(func() {
						ui.Text(c, tr.LabelRef).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
						refTxt := row.RefText
						if refTxt == "" {
							refTxt = tr.LabelNone
						}
						ui.Text(c, refTxt).FontSize(12).MinWidth(0).Selectable()
					})
				})

				// Power Action Toolbar + Target Status
				cell := row.Cells[a.activeEditLocale]
				ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, fmt.Sprintf(tr.LabelEditTargetFmt, a.activeEditLocale)).FontSize(12).FontWeight(600)
					badge, fg := formatCellStatusBadge(c, cell, tr)
					ui.Text(c, badge).FontSize(11).TextColor(fg)
					ui.Spacer(c)
					if ui.Button(c, tr.BtnTranslateRow).
						Padding(t.Space(0.5), t.Space(1.5)).
						Disabled(a.translating || a.translatingRow).
						Tooltip(tr.TipTranslateRow).
						Clicked() {
						a.translateCurrentRowAsync()
					}
					if ui.Button(c, tr.BtnPrevGap).
						Padding(t.Space(0.5), t.Space(1.5)).
						Tooltip(tr.TipPrevGap).
						Clicked() {
						a.jumpGap(-1)
					}
					if ui.Button(c, tr.BtnNextGap).
						Padding(t.Space(0.5), t.Space(1.5)).
						Tooltip(tr.TipNextGap).
						Clicked() {
						a.jumpGap(1)
					}
					if ui.Button(c, tr.BtnCopySrc).Padding(t.Space(0.5), t.Space(1.5)).Clicked() {
						a.copySourceToClipboard()
					}
					if ui.Button(c, tr.BtnUseSrc).Padding(t.Space(0.5), t.Space(1.5)).Clicked() {
						a.useSourceAsTranslation()
					}
					if ui.Button(c, tr.BtnResetCell).Padding(t.Space(0.5), t.Space(1.5)).Clicked() {
						a.resetActiveCell()
					}
				})

				if row.Numerus {
					ui.Text(c, tr.HintNumerus).FontSize(11).TextColor(t.Warning)
				}

				if ui.TextArea(c, &a.editBuf).
					Placeholder(tr.PlaceholderEditor).
					Lines(2, 5).
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
		})

		// Right Column: Scrollable Format-Aware Diff Preview
		ui.Scroll(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Column(c).Gap(t.Space(1)).Padding(t.Space(0.5)).Children(func() {
				cell := row.Cells[a.activeEditLocale]
				oldStatus := cell.Status
				if oldStatus == "" {
					oldStatus = domain.StatusUnfinished
				}
				newText := cell.Effective
				if newText == "" {
					newText = cell.OldText
				}
				beforeSnippet, afterSnippet := format.PreviewDiff(
					curFmt,
					row.Key,
					a.activeEditLocale,
					cell.OldText,
					newText,
					oldStatus,
					row.Numerus,
				)

				ui.Text(c, tr.LabelBeforeXML).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
				ui.Box(c).FillWidth().Padding(t.Space(1.5)).Background(t.Surface).Border(1, t.Border).Radius(t.Radius).Children(func() {
					ui.Text(c, beforeSnippet).Font("SF Mono, Menlo, monospace").FontSize(11).TextColor(t.TextMuted).MinWidth(0).Selectable()
				})

				ui.Text(c, tr.LabelAfterXML).FontSize(11).FontWeight(600).TextColor(t.Accent)
				ui.Box(c).FillWidth().Padding(t.Space(1.5)).Background(t.Surface).Border(1, t.Accent.Alpha(0.5)).Radius(t.Radius).Children(func() {
					ui.Text(c, afterSnippet).Font("SF Mono, Menlo, monospace").FontSize(11).MinWidth(0).Selectable()
				})
			})
		})
	})
}

func (a *App) viewDiagnosticsLogs(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf(tr.LogCountFmt, len(a.logs))).FontSize(12).TextColor(t.TextMuted).Grow(1)
		if ui.Button(c, tr.BtnCopyLogs).Padding(t.Space(1), t.Space(2)).Clicked() {
			a.copyLogsToClipboard()
		}
		if ui.Button(c, tr.BtnClearLogs).Padding(t.Space(1), t.Space(2)).Clicked() {
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
			ui.Text(c, ev.Message).FontSize(12).TextColor(col).Grow(1).MinWidth(0).Selectable()
		})
	}).Grow(1).MinHeight(0).Border(1, t.Border).Radius(t.Radius).Padding(t.Space(1))
}
