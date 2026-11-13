package app

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

func (a *App) viewHeader(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).
		Padding(t.Space(2), t.Space(3)).
		Gap(t.Space(2)).
		AlignItems(ui.Center).
		Background(t.Surface).
		Children(func() {
			// Brand & loaded source badge
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, "LocLoom").FontSize(15).FontWeight(700).TextColor(t.Text).SingleLine()

				sourceLabel := "未加载项目"
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
				badgeText := fmt.Sprintf("%s · %d 条 · %.0f%%", sourceLabel, rows, pct*100)
				ui.Badge(c, badgeText)
			})

			// File & Excel Action Group
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Shrink(0).Children(func() {
				if headerBtn(c, iconFolder, "打开目录", "打开 Qt .ts 目录 (Cmd/Ctrl+O)").Clicked() {
					a.openTSDirAsync()
				}
				if headerBtn(c, iconZip, "打开 ZIP", "打开包含 .ts 的 ZIP 压缩包").Clicked() {
					a.openZipAsync()
				}
				if headerBtn(c, iconImport, "导入 Excel", "导入 .xlsx 翻译词表").Clicked() {
					a.importExcelAsync()
				}
				if headerBtn(c, iconExport, "导出 Excel", "导出 .xlsx 词表 (Cmd/Ctrl+E)").Clicked() {
					a.showExportModal = true
				}
			})

			ui.Spacer(c)

			// Primary CTA Group
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Shrink(0).Children(func() {
				if a.translating {
					ui.Spinner(c).Label("正在并发翻译")
					frac := 0.0
					if a.translateTotalCount > 0 {
						frac = float64(a.translateDoneCount) / float64(a.translateTotalCount)
					}
					ui.Column(c).Width(110).Gap(2).Children(func() {
						ui.Text(c, fmt.Sprintf("%d/%d", a.translateDoneCount, a.translateTotalCount)).
							FontSize(11).TextColor(t.TextMuted).SingleLine()
						ui.Progress(c, frac)
					})
					if ui.Button(c, "停止翻译").
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
							ui.Text(c, "开始并发翻译").FontSize(13).FontWeight(600).SingleLine()
						})
					})
					if btn.Clicked() {
						a.startTranslation()
					}
				}

				writeBtn := ui.Button(c, "").
					Padding(t.Space(1.5), t.Space(3)).
					Disabled(a.translating || a.bundle == nil).
					Tooltip("无损回填写回 .ts 文件 (Cmd/Ctrl+S)")
				writeBtn.Children(func() {
					ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconSave).Size(14, 14)
						ui.Text(c, "写回 .ts").FontSize(13).SingleLine()
					})
				})
				if writeBtn.Clicked() {
					a.writeTSAsync()
				}
			})

			// Utility Buttons
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Shrink(0).Children(func() {
				themeLabels := []string{"系统", "浅色", "深色"}
				lbl := themeLabels[a.cfg.ThemeMode%3]
				if headerBtn(c, iconTheme, lbl, "切换外观主题 (跟随系统 / 浅色 / 深色)").Clicked() {
					a.SetThemeMode((a.cfg.ThemeMode + 1) % 3)
				}

				logLabel := fmt.Sprintf("日志(%d)", len(a.logs))
				if headerBtn(c, iconLogs, logLabel, "查看运行日志与诊断").Clicked() {
					a.bottomTab = 1
				}

				helpBtn := ui.Button(c, "").
					Padding(t.Space(1.5), t.Space(2)).
					Label("帮助").
					Tooltip("打开使用指南")
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
