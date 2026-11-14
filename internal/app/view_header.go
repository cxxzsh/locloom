package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"locloom/internal/format"
	"locloom/internal/i18n"
)

func (a *App) viewHeader(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()
	ui.Row(c).
		Padding(t.Space(2), t.Space(3)).
		Gap(t.Space(2)).
		AlignItems(ui.Center).
		Background(t.Surface).
		Children(func() {
			// Brand, format badge & loaded source summary (shrinkable so right utility buttons never clip at 1280px)
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
				brandIcon := iconBrandDark
				if !t.Dark {
					brandIcon = iconBrandLight
				}
				ui.Image(c, brandIcon).Size(22, 22).Shrink(0)
				ui.Text(c, "LocLoom").FontSize(15).FontWeight(700).TextColor(t.Text).SingleLine().Shrink(0)
				if a.bundle != nil {
					ui.Badge(c, format.FormatLabel(a.bundle.Format, a.UILang())).Shrink(0)
				}

				sourceLabel := tr.SourceUnloaded
				if a.bundle != nil {
					sourceLabel = filepath.Base(a.bundle.SourcePath)
				}
				rows := 0
				pct := 0.0
				if a.plan != nil {
					rows = a.plan.Summary.TotalRows
					if len(a.plan.Summary.LocaleStats) > 0 {
						var sum float64
						var cnt int
						for id, st := range a.plan.Summary.LocaleStats {
							if id == a.plan.RefLocaleID {
								continue
							}
							sum += st.Completion
							cnt++
						}
						if cnt > 0 {
							pct = sum / float64(cnt)
						}
					}
				}
				badgeText := fmt.Sprintf(tr.HeaderSummaryFmt, sourceLabel, rows, pct*100)
				ui.Badge(c, badgeText).Shrink(1).MinWidth(0).Tooltip(badgeText)
			})

			// File & Excel Action Group
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Shrink(0).Children(func() {
				if headerBtn(c, iconFolder, tr.BtnOpenDir, tr.TipOpenDir).Clicked() {
					a.openDirAsync()
				}
				if headerBtn(c, iconZip, tr.BtnOpenFile, tr.TipOpenFile).Clicked() {
					a.openFileAsync()
				}
				if headerBtn(c, iconImport, tr.BtnImportExcel, tr.TipImportExcel).Clicked() {
					a.importExcelAsync()
				}
				if headerBtn(c, iconExport, tr.BtnExportExcel, tr.TipExportExcel).Clicked() {
					a.showExportModal = true
				}
			})
			ui.Spacer(c)

			// Primary CTA Group
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Shrink(0).Children(func() {
				if a.translating {
					ui.Spinner(c).Label(tr.StatusTranslating)
					frac := 0.0
					if a.translateTotalCount > 0 {
						frac = float64(a.translateDoneCount) / float64(a.translateTotalCount)
					}
					ui.Column(c).Width(110).Gap(2).Children(func() {
						ui.Text(c, fmt.Sprintf("%d/%d", a.translateDoneCount, a.translateTotalCount)).
							FontSize(11).TextColor(t.TextMuted).SingleLine()
						ui.Progress(c, frac)
					})
					if ui.Button(c, tr.BtnStopTranslate).
						Background(t.Danger).
						TextColor(t.AccentText).
						Padding(t.Space(1.5), t.Space(3)).
						Clicked() {
						a.stopTranslation()
					}
				} else {
					btn := ui.PrimaryButton(c, "").Padding(t.Space(1.5), t.Space(3))
					btn.Children(func() {
						ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, iconPlay).Size(14, 14)
							ui.Text(c, tr.BtnStartTranslate).FontSize(13).FontWeight(600).SingleLine()
						})
					})
					if btn.Clicked() {
						a.startTranslation()
					}
				}

				pendingCount := 0
				if a.plan != nil {
					pendingCount = a.plan.Summary.TotalChanges + a.plan.Summary.TotalAppends
				}
				writeLabel := tr.BtnWriteTS
				if pendingCount > 0 {
					writeLabel = fmt.Sprintf(tr.BtnWriteTSCountFmt, pendingCount)
				}
				writeBtn := ui.Button(c, "").
					Padding(t.Space(1.5), t.Space(3)).
					Disabled(a.translating || a.bundle == nil).
					Tooltip(tr.TipWriteTS)
				writeBtn.Children(func() {
					ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconSave).Size(14, 14)
						ui.Text(c, writeLabel).FontSize(13).SingleLine()
					})
				})
				if writeBtn.Clicked() {
					a.writeBackAsync()
				}
			})

			// Utility Buttons
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Shrink(0).Children(func() {
				themeLabels := []string{tr.ThemeSystem, tr.ThemeLight, tr.ThemeDark}
				lbl := themeLabels[a.cfg.ThemeMode%3]
				if headerBtn(c, iconTheme, lbl, tr.TipTheme).Clicked() {
					a.SetThemeMode((a.cfg.ThemeMode + 1) % 3)
				}

				if ui.Button(c, tr.BtnLangToggle).
					Padding(t.Space(1.5), t.Space(2)).
					Tooltip(tr.TipLangToggle).
					Clicked() {
					if a.UILang() == i18n.LangEN {
						a.SetUILang(i18n.LangZH)
					} else {
						a.SetUILang(i18n.LangEN)
					}
				}

				logLabel := fmt.Sprintf(tr.HeaderLogsFmt, len(a.logs))
				if headerBtn(c, iconLogs, logLabel, tr.TipLogs).Clicked() {
					a.bottomTab = 1
					if a.matrixHeightPct > 72 {
						a.matrixHeightPct = 72
					}
				}
				helpBtn := ui.Button(c, "").
					Padding(t.Space(1.5), t.Space(2)).
					Label(tr.BtnHelp).
					Tooltip(tr.TipHelp)
				helpBtn.Children(func() {
					ui.Icon(c, iconHelp).Size(15, 15)
				})
				if helpBtn.Clicked() {
					a.showHelp = true
				}
			})
		})
}

func headerBtn(c *ui.Context, ic *ui.SVG, label, tip string) ui.Element {
	t := c.Theme()
	btn := ui.Button(c, "").
		Padding(t.Space(1.5), t.Space(2.5)).
		Label(label).
		Tooltip(tip)
	btn.Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			if ic != nil {
				ui.Icon(c, ic).Size(14, 14)
			}
			ui.Text(c, label).FontSize(12).SingleLine()
		})
	})
	return btn
}
