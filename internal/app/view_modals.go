package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"locloom/internal/i18n"
	"locloom/internal/sheet"
)

func (a *App) viewModals(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()
	// Help Markdown Modal
	ui.Modal(c, &a.showHelp, func() {
		ui.Column(c).Width(720).MaxWidth(720).MaxHeight(580).Gap(t.Space(2)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr.HelpModalTitle).FontSize(16).FontWeight(700).Grow(1)
				if ui.Button(c, tr.BtnClose).Padding(t.Space(1), t.Space(2.5)).Clicked() {
					a.showHelp = false
				}
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				md := a.localizedHelpMarkdown()
				if md == "" {
					md = tr.HelpFallbackMD
				}
				markdown.View(c, md, &a.mdOpts)
			})
		})
	})

	// Export Excel Modal
	ui.Modal(c, &a.showExportModal, func() {
		ui.Column(c).Width(480).MaxWidth(480).Gap(t.Space(2.5)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr.ExportModalTitle).FontSize(16).FontWeight(700).Grow(1)
				if ui.Button(c, tr.BtnCancel).Padding(t.Space(1), t.Space(2)).Clicked() {
					a.showExportModal = false
				}
			})
			ui.Divider(c)

			ui.Text(c, tr.LabelExportScope).FontSize(13).FontWeight(600)
			ui.Segmented(c, &a.exportScope, tr.ExportScopeAll, tr.ExportScopeGaps, tr.ExportScopeChanges, tr.ExportScopeWarnings).FillEqually()

			descriptions := []string{
				tr.ExportDescAll,
				tr.ExportDescGaps,
				tr.ExportDescChanges,
				tr.ExportDescWarnings,
			}
			idx := a.exportScope
			if idx < 0 || idx >= len(descriptions) {
				idx = 0
			}
			ui.Text(c, descriptions[idx]).FontSize(12).TextColor(t.TextMuted).MinWidth(0)
			ui.Row(c).Justify(ui.End).Gap(t.Space(2)).Children(func() {
				if ui.Button(c, tr.BtnCancel).Padding(t.Space(1.5), t.Space(3)).Clicked() {
					a.showExportModal = false
				}
				if ui.PrimaryButton(c, tr.BtnExportConfirm).Padding(t.Space(1.5), t.Space(3)).Clicked() {
					scope := sheet.ExportScope(a.exportScope)
					a.showExportModal = false
					a.exportExcelAsync(scope)
				}
			})
		})
	})
}

func (a *App) localizedHelpMarkdown() string {
	md := a.helpMarkdown
	if md == "" {
		return ""
	}
	const zhAnchor = `<a id="简体中文"></a>`
	idx := strings.Index(md, zhAnchor)
	if idx < 0 {
		return md
	}
	if a.UILang() == i18n.LangEN {
		enPart := md[:idx]
		enPart = strings.TrimSpace(enPart)
		enPart = strings.TrimSuffix(enPart, "---")
		enPart = strings.ReplaceAll(enPart, `<a id="english"></a>`, "")
		return strings.TrimSpace(enPart)
	}
	zhPart := md[idx+len(zhAnchor):]
	return strings.TrimSpace(zhPart)
}
