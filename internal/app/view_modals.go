package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"locloom/internal/sheet"
)

func (a *App) viewModals(c *ui.Context) {
	t := c.Theme()

	// Help Markdown Modal
	ui.Modal(c, &a.showHelp, func() {
		ui.Column(c).Width(720).MaxHeight(580).Gap(t.Space(2)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "LocLoom 使用指南").FontSize(16).FontWeight(700).Grow(1)
				if ui.Button(c, "关闭").Padding(t.Space(1), t.Space(2.5)).Clicked() {
					a.showHelp = false
				}
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				md := a.helpMarkdown
				if md == "" {
					md = "# LocLoom\n\n跨平台多语言并发翻译与无损回填工作台。"
				}
				markdown.View(c, md, &a.mdOpts)
			})
		})
	})

	// Export Excel Modal
	ui.Modal(c, &a.showExportModal, func() {
		ui.Column(c).Width(460).Gap(t.Space(2.5)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "导出 Excel 词条表 (.xlsx)").FontSize(16).FontWeight(700).Grow(1)
				if ui.Button(c, "取消").Padding(t.Space(1), t.Space(2)).Clicked() {
					a.showExportModal = false
				}
			})
			ui.Divider(c)

			ui.Text(c, "选择导出范围：").FontSize(13).FontWeight(600)
			ui.Segmented(c, &a.exportScope, "全部词条", "仅未翻译", "仅待写入变更", "仅含告警").FillEqually()

			descriptions := []string{
				"导出当前矩阵中的全部词条（含原文、参考译文与各语种译文）。",
				"仅导出至少有一个已启用语种仍为空缺的词条，方便分发外包翻译。",
				"仅导出本次会写回 .ts 文件的补齐、覆盖或新增词条。",
				"仅导出存在 Qt 占位符缺失、多余或结构告警的词条。",
			}
			idx := a.exportScope
			if idx < 0 || idx >= len(descriptions) {
				idx = 0
			}
			ui.Text(c, descriptions[idx]).FontSize(12).TextColor(t.TextMuted)

			ui.Row(c).Justify(ui.End).Gap(t.Space(2)).Children(func() {
				if ui.Button(c, "取消").Padding(t.Space(1.5), t.Space(3)).Clicked() {
					a.showExportModal = false
				}
				if ui.PrimaryButton(c, "选择路径并导出...").Padding(t.Space(1.5), t.Space(3)).Clicked() {
					scope := sheet.ExportScope(a.exportScope)
					a.showExportModal = false
					a.exportExcelAsync(scope)
				}
			})
		})
	})
}
