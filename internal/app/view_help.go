package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"locloom/internal/i18n"
)

type helpNavItem struct {
	icon  *ui.SVG
	label string
}

type helpStepItem struct {
	num   string
	title string
	desc  string
}

type helpBadgeItem struct {
	badge string
	title string
	desc  string
	tone  string // "green", "accent", "orange", "red"
}

type helpFormatItem struct {
	name string
	ext  string
	desc string
}

type helpShortcutItem struct {
	keys string
	desc string
}

type helpQAItem struct {
	q string
	a string
}

func (a *App) viewHelpModal(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()
	isEN := a.UILang() == i18n.LangEN

	ui.Modal(c, &a.showHelp, func() {
		// Preserve direct markdown rendering when a single custom help markdown is injected
		// (e.g. unit tests or external overrides via SetHelpMarkdown).
		if a.helpMarkdown != "" && a.helpMarkdownZH == "" {
			ui.Column(c).Width(760).MaxWidth(760).MaxHeight(620).Gap(t.Space(2)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, tr.HelpModalTitle).FontSize(18).FontWeight(700).Grow(1)
					if ui.Button(c, tr.BtnClose).Padding(t.Space(1.25), t.Space(3)).Radius(pillRadius).Clicked() {
						a.showHelp = false
					}
				})
				ui.Divider(c)
				ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
					markdown.View(c, a.localizedHelpMarkdown(), &a.mdOpts)
				})
			})
			return
		}

		subtitle := "跨平台无损本地化工作台 · 操作参考"
		onboardingLbl := "新手引导"
		if isEN {
			subtitle = "Lossless Localization Workbench · Quick Reference"
			onboardingLbl = "Quick Tour"
		}

		ui.Column(c).Width(980).MaxWidth(980).Height(690).MaxHeight(690).Gap(t.Space(2.5)).Children(func() {
			// Brand Header
			ui.Row(c).Gap(t.Space(2.5)).AlignItems(ui.Center).Children(func() {
				brandIcon := iconBrandDark
				if !t.Dark {
					brandIcon = iconBrandLight
				}
				ui.Image(c, brandIcon).Size(34, 34).Shrink(0)
				ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
					ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						ui.Text(c, tr.HelpModalTitle).FontSize(18).FontWeight(700).SingleLine()
						ui.Row(c).
							Padding(2, t.Space(2)).
							Radius(pillRadius).
							Background(accentTint(t)).
							Children(func() {
								ui.Text(c, "v"+currentAppVersion()).FontSize(12).FontWeight(600).TextColor(accentInk(t)).SingleLine()
							})
					})
					ui.Text(c, subtitle).FontSize(12.5).TextColor(t.TextMuted).SingleLine()
				})
				if ui.Button(c, onboardingLbl).Padding(t.Space(1.5), t.Space(3)).Radius(pillRadius).Clicked() {
					a.showHelp = false
					a.showOnboarding = true
				}
				if ui.Button(c, tr.BtnClose).Padding(t.Space(1.5), t.Space(3)).Radius(pillRadius).Clicked() {
					a.showHelp = false
				}
			})

			ui.Divider(c)

			navItems := helpNavItems(isEN)
			if a.helpTab < 0 || a.helpTab >= len(navItems) {
				a.helpTab = 0
			}

			// Two-column layout: left navigation rail + right scrollable guide content
			ui.Row(c).Grow(1).MinHeight(0).Gap(t.Space(3)).AlignItems(ui.Stretch).Children(func() {
				// Left Navigation Rail
				ui.Column(c).
					Width(206).
					Shrink(0).
					AlignSelf(ui.Stretch).
					Padding(t.Space(2)).
					Gap(t.Space(1.25)).
					Background(insetBg(t)).
					Border(1, hairline(t)).
					Radius(cardRadius).
					Children(func() {
						for idx, item := range navItems {
							active := a.helpTab == idx
							bg := ui.Transparent
							fg := t.Text
							iconCol := t.TextMuted
							fw := 500
							if active {
								bg = accentTint(t)
								fg = accentInk(t)
								iconCol = accentInk(t)
								fw = 600
							}
							row := ui.Row(c).
								Key(fmt.Sprintf("help-nav-%d", idx)).
								Padding(t.Space(2), t.Space(2.5)).
								Gap(t.Space(2)).
								AlignItems(ui.Center).
								Radius(t.Radius).
								Background(bg).
								Cursor(ui.CursorPointer)
							row.Children(func() {
								if item.icon != nil {
									ui.Icon(c, item.icon).Size(16, 16).TextColor(iconCol).Shrink(0)
								}
								ui.Text(c, item.label).FontSize(13.5).FontWeight(fw).TextColor(fg).SingleLine().Grow(1).MinWidth(0)
							})
							if row.Clicked() {
								a.helpTab = idx
							}
						}
						ui.Spacer(c)
						footerTip := "按 Esc 关闭弹窗"
						if isEN {
							footerTip = "Press Esc to close"
						}
						ui.Text(c, footerTip).FontSize(12).TextColor(ink3(t)).Padding(t.Space(1), t.Space(2))
					})

				// Right Content Canvas
				ui.Scroll(c).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
					ui.Column(c).Key(fmt.Sprintf("help-page-%d", a.helpTab)).Gap(t.Space(3)).Padding(0, t.Space(2), t.Space(2.5), 0).Children(func() {
						switch a.helpTab {
						case 0:
							a.viewHelpQuickStart(c, isEN)
						case 1:
							a.viewHelpPipelineLocales(c, isEN)
						case 2:
							a.viewHelpSaveFormats(c, isEN)
						case 3:
							a.viewHelpShortcutsTips(c, isEN)
						case 4:
							a.viewHelpFAQPaths(c, isEN)
						}
					})
				})
			})
		})
	})
}

func helpNavItems(isEN bool) []helpNavItem {
	if isEN {
		return []helpNavItem{
			{icon: iconPlay, label: "Quick Start"},
			{icon: iconRefresh, label: "Pipeline & Locales"},
			{icon: iconSave, label: "Save & Formats"},
			{icon: iconLogs, label: "Shortcuts & Tips"},
			{icon: iconHelp, label: "FAQ & Paths"},
		}
	}
	return []helpNavItem{
		{icon: iconPlay, label: "快速上手"},
		{icon: iconRefresh, label: "翻译与语种"},
		{icon: iconSave, label: "保存与格式"},
		{icon: iconLogs, label: "快捷键与技巧"},
		{icon: iconHelp, label: "常见问题与路径"},
	}
}

func (a *App) viewHelpQuickStart(c *ui.Context, isEN bool) {
	t := c.Theme()

	heroTitle := "4 步完成多语种无损本地化"
	heroDesc := "LocLoom 在不破坏原文件格式、注释、BOM 与换行符的前提下，并发补齐多语种译文，并默认将完整结果输出到独立文件夹。"
	steps := []helpStepItem{
		{"1", "加载项目（目录 / 单文件 / ZIP）", "点击顶部「打开目录」「打开文件」，或直接把文件夹、单个文件、.zip 拖入窗口。LocLoom 会自动识别 8 大本地化格式并同步项目语种。"},
		{"2", "确认翻译引擎与目标语种", "在左侧「管线」拖动卡片调整瀑布流回退顺序；在「语种」勾选目标语言。若 zh_CN 等参考语种也有缺口，再次点击「★ 参考」即可取消参考锁定。"},
		{"3", "并发补缺与逐条精修", "点击「开始并发翻译」并发补齐空缺；在主表切换「聚焦单语种 / 全景矩阵」，配合底部精修区单行翻译、重译、检查 [⚠] 告警与差异预览。"},
		{"4", "无损写回保存", "点击「写回保存」(Cmd/Ctrl+S)。默认输出完整副本到同级 *_updated/ 文件夹，原目录零改动；若选择原位覆盖，旧文件自动集中备份进 .locloom_backup/。"},
	}
	badgeSecTitle := "界面状态徽章说明"
	badgeNote := "以下前缀仅用于表格预览提示，绝不会写入本地化文件或导出的 Excel。"
	badges := []helpBadgeItem{
		{"[补]", "补缺写入", "原词条未完成或为空，写回时填入译文并移除 unfinished 标记。", "green"},
		{"[改]", "覆盖已有", "手动编辑、清空、整行重译或覆盖模式触发，替换已有译文。", "accent"},
		{"[新]", "追加新词条", "勾选「允许从 Excel 追加新词条」后，插入原文件缺失的新键。", "orange"},
		{"[⚠]", "格式与占位符告警", "%1 / %s / {name} 占位符、尾随制表符或结尾标点与原文不一致。", "red"},
	}

	if isEN {
		heroTitle = "4-Step Lossless Localization Workflow"
		heroDesc = "LocLoom fills multi-language gaps concurrently while preserving formatting, comments, BOM, and line endings — saving a complete copy into a dedicated folder by default."
		steps = []helpStepItem{
			{"1", "Load Project (Folder / File / ZIP)", "Click Open Dir or Open File, or drag and drop a folder, file, or .zip archive. LocLoom auto-detects 8 formats and aligns target locales."},
			{"2", "Configure Pipeline & Target Locales", "Drag provider cards in Pipeline to set fallback order. In Locales, pick targets — click '★ Ref' again to clear reference lock if that locale also needs translation."},
			{"3", "Translate Concurrently & Review", "Click Start Translate to fill gaps via the waterfall. Switch between Focus Locale and Full Matrix, inspect diffs, jump gaps, and fix [⚠] warnings."},
			{"4", "Save Back Safely", "Click Save Back (Cmd/Ctrl+S). Writes a full standalone copy to *_updated/ without touching originals; in-place saves collect backups under .locloom_backup/."},
		}
		badgeSecTitle = "Status Badges"
		badgeNote = "Badges appear only in the UI table and are never written into localization files or Excel exports."
		badges = []helpBadgeItem{
			{"[Fill]", "Fill Gap", "Entry is unfinished or empty; writes translation and marks finished.", "green"},
			{"[Over]", "Overwrite", "Manual edit, clear, row re-translate, or Overwrite mode replacing text.", "accent"},
			{"[Add]", "Append New", "Inserts new keys from Excel when Append Missing is enabled.", "orange"},
			{"[⚠]", "Format Warning", "Placeholder (%1/%s/{name}), trailing tab, or punctuation mismatch.", "red"},
		}
	}

	// Hero callout
	ui.Column(c).
		Padding(t.Space(2.5), t.Space(3)).
		Gap(t.Space(1)).
		Background(accentTint(t)).
		Border(1, t.Accent.Alpha(0.28)).
		Radius(cardRadius).
		Children(func() {
			ui.Text(c, heroTitle).FontSize(15).FontWeight(700).TextColor(accentInk(t))
			ui.Text(c, heroDesc).FontSize(13).TextColor(t.Text)
		})

	// 4 Step Cards
	ui.Column(c).Gap(t.Space(2)).Children(func() {
		for _, st := range steps {
			helpCard(c, t, func() {
				ui.Row(c).Gap(t.Space(2.5)).AlignItems(ui.Start).Children(func() {
					ui.Row(c).
						Width(28).
						Height(28).
						AlignItems(ui.Center).
						Justify(ui.Center).
						Radius(pillRadius).
						Background(accentTint(t)).
						Shrink(0).
						Children(func() {
							ui.Text(c, st.num).FontSize(13).FontWeight(700).TextColor(accentInk(t))
						})
					ui.Column(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, st.title).FontSize(14).FontWeight(600)
						ui.Text(c, st.desc).FontSize(13).TextColor(t.TextMuted)
					})
				})
			})
		}
	})

	// Status Badges 2x2 Grid
	helpSectionTitle(c, t, badgeSecTitle)
	helpRenderBadgeRow(c, t, badges[0], badges[1])
	helpRenderBadgeRow(c, t, badges[2], badges[3])
	ui.Text(c, badgeNote).FontSize(12).TextColor(ink3(t))
}

func (a *App) viewHelpPipelineLocales(c *ui.Context, isEN bool) {
	t := c.Theme()

	if isEN {
		helpSectionTitle(c, t, "10-Source Waterfall Priority")
		helpCard(c, t, func() {
			helpTierRow(c, t, "Local First", "Local .xlsx Glossary → Local .ts Translation Memory (TM). Instant, deterministic, zero network cost.", t.Success)
			ui.Divider(c)
			helpTierRow(c, t, "Keyless Online", "Google Web → Microsoft Bing / Edge → Lingva / LibreTranslate → MyMemory. Recommended concurrency: 2–4 to avoid rate limits.", t.Accent)
			ui.Divider(c)
			helpTierRow(c, t, "API & LLM", "DeepL / DeepLX → OpenAI / Ollama Compatible LLMs → Baidu API → Niutrans API. Supports model discovery & custom domain prompts.", t.Warning)
		})

		helpSectionTitle(c, t, "Locale Selection & Cascade Translation")
		helpCard(c, t, func() {
			helpBullet(c, t, "Project-Scoped Presets", "When a multi-locale project is loaded, Tier-1 / CJK / Europe / SEA & MENA / All intersect with project locales so ghost languages are never injected.")
			helpBullet(c, t, "Toggleable Reference Locale (★ Ref)", "Reference locale appears in the Ref column for cross-checking and is excluded from batch translation. Click '★ Ref' again to clear reference mode and translate it like any other target.")
			helpBullet(c, t, "Translate-From Cascade Waves", "Each locale card has a 'From' picker (e.g. zh_TW from zh_CN). Dependent locales automatically execute in topological waves within one run.")
			helpBullet(c, t, "Add Missing Locales", "Search hidden built-in locales in the search box or use Add Custom Locale, then click 'Scaffold Missing Files' to create skeleton files.")
		})

		helpSectionTitle(c, t, "Proxy & Local Cache")
		helpCard(c, t, func() {
			helpBullet(c, t, "HTTP / SOCKS5 Proxy", "Configure once in Settings → Network & Cache, then toggle 'Route all cloud engines through this proxy' (automatically skips localhost).")
			helpBullet(c, t, "Cache Inspector", "Search cached entries by source, translation, locale, or engine, delete filtered entries selectively, or check 'Disable local cache' for fresh runs.")
		})
		return
	}

	helpSectionTitle(c, t, "十源并发瀑布流推荐层级")
	helpCard(c, t, func() {
		helpTierRow(c, t, "本地优先", "本地 .xlsx 词条表 → 本地 .ts 翻译记忆库（TM）。毫秒命中、零网络开销，适合复用既有术语。", t.Success)
		ui.Divider(c)
		helpTierRow(c, t, "免 Key 在线", "Google Web → Bing / Edge → Lingva / LibreTranslate → MyMemory。建议在设置里把并发设为 2–4 防限流。", t.Accent)
		ui.Divider(c)
		helpTierRow(c, t, "API 与大模型", "DeepL / DeepLX → OpenAI / Ollama 兼容大模型 → 百度翻译 → 小牛翻译。支持自动发现模型与领域提示词。", t.Warning)
	})

	helpSectionTitle(c, t, "语种选择、参考列与级联翻译")
	helpCard(c, t, func() {
		helpBullet(c, t, "预设组合自动限定项目范围", "加载多语种项目后，点「主流 / 东亚 / 欧洲 / 东南亚与中东 / 全部」会自动与项目已有语种取交集，不会塞入项目不存在的幽灵语种；单模板项目则可自由勾选新语种。")
		helpBullet(c, t, "「★ 参考」可随时取消", "参考语种仅用于主表参考列对照，不参与批量翻译。若参考语种自身（如 zh_CN.ts）也有未翻译词条，再次点击「★ 参考」即可取消锁定并纳入翻译。")
		helpBullet(c, t, "「译自」自动级联波次", "每个语种卡片都可设置「译自」（例如 zh_TW 译自 zh_CN、zh_HK 译自 zh_TW），点击开始翻译后自动按拓扑顺序分波次执行。")
		helpBullet(c, t, "新增项目外语种", "在语种搜索框输入语种代码可直接找到并启用内置语种，或在底部添加自定义语种，再点「生成缺失语种骨架」。")
	})

	helpSectionTitle(c, t, "代理与翻译缓存")
	helpCard(c, t, func() {
		helpBullet(c, t, "一键云端代理", "在「设置 → 代理与缓存」填入 HTTP / SOCKS5 代理后，可一键为全部云端引擎开启代理（自动跳过 127.0.0.1 / localhost）。")
		helpBullet(c, t, "缓存精准管理", "缓存查看器支持按语种、引擎、原文/译文筛选并批量删除；需要强制重新请求引擎时可勾选「完全禁用本地翻译缓存」。")
	})
}

func (a *App) viewHelpSaveFormats(c *ui.Context, isEN bool) {
	t := c.Theme()

	saveTitle := "两种写回保存位置（左侧「项目」面板可切换）"
	folderTitle := "输出到新文件夹（默认推荐）"
	folderDesc := "在原目录同级生成独立的 *_updated/ 完整文件夹（未修改的语种文件也会原样复制），原目录零改动。后续继续编辑并写回时，会稳定更新同一个 *_updated/ 目录，不会叠出 _updated_updated。"
	inplaceTitle := "原位覆盖 + 集中备份"
	inplaceDesc := "直接修改当前打开的文件或目录（ZIP 仍输出 *_updated.zip）。勾选「写回前备份到文件夹」时，覆盖前的旧文件会按原目录结构集中归档到 .locloom_backup/<时间戳>/，不会在目录里散落 .bak 文件。"
	formatsTitle := "原生支持的 8 大无损格式"
	formats := []helpFormatItem{
		{"Qt Linguist", ".ts", "字节偏移无损补丁，保留 BOM、CRLF、注释、location 与复数"},
		{"GNU Gettext", ".po / .pot", "保留文件头、msgctxt、fuzzy 标记、注释与 msgstr[n] 复数"},
		{"JSON i18n", ".json", "支持嵌套/扁平结构与多命名空间隔离，保留原键序与缩进"},
		{"Flutter ARB", ".arb", "完整保留 @@locale、@metadata 描述及 ICU 占位符"},
		{"Apple Catalog", ".xcstrings", "单文件多语种目录，保留 extractionState、state 与复数变体"},
		{"Android XML", "strings.xml", "支持 values-<lang>、translatable=\"false\"、数组与 plurals"},
		{".NET ResX", ".resx", "保留 xsd:schema、resheader、注释与非字符串二进制节点"},
		{"XLIFF", ".xlf / .xliff", "同时支持 XLIFF 1.2（trans-unit）与 2.0（unit / segment）"},
	}
	excelTitle := "Excel (.xlsx) 审校协作"
	excelDesc := "支持导出「全部词条」「仅未翻译」「仅待写回」「仅含告警」4 种范围，并可勾选「仅导出相关语种列」剔除无关列；所有格式/占位符告警会自动写入单元格批注，审校修改后拖回窗口即可导入。"

	if isEN {
		saveTitle = "Save Location Modes (Sidebar → Project)"
		folderTitle = "New Folder (Default & Recommended)"
		folderDesc = "Writes a complete, self-contained copy into a sibling *_updated/ folder (untouched files are copied verbatim) leaving originals untouched. Repeated saves reuse the same *_updated/ folder."
		inplaceTitle = "In Place + Centralized Backups"
		inplaceDesc = "Updates opened files directly (ZIP still writes *_updated.zip). When backups are enabled, overwritten files are archived cleanly under .locloom_backup/<timestamp>/ instead of scattering .bak files."
		formatsTitle = "8 Native Lossless Formats"
		formats = []helpFormatItem{
			{"Qt Linguist", ".ts", "Byte-offset XML patching preserving BOM, CRLF, comments & plurals"},
			{"GNU Gettext", ".po / .pot", "Preserves headers, msgctxt, fuzzy flags, comments & msgstr[n]"},
			{"JSON i18n", ".json", "Nested/flat JSON with namespace isolation, key order & indent kept"},
			{"Flutter ARB", ".arb", "Preserves @@locale, @metadata blocks & ICU placeholders"},
			{"Apple Catalog", ".xcstrings", "Single-file multi-locale catalog keeping variations & metadata"},
			{"Android XML", "strings.xml", "Supports values-<lang>, translatable=\"false\" & plurals"},
			{".NET ResX", ".resx", "Preserves xsd:schema, resheader, comments & binary resources"},
			{"XLIFF", ".xlf / .xliff", "Supports both XLIFF 1.2 (trans-unit) and 2.0 (unit/segment)"},
		}
		excelTitle = "Excel (.xlsx) Review Workflow"
		excelDesc = "Export All, Gaps Only, Pending Changes, or Warnings Only — optionally omitting irrelevant locale columns. Validation warnings are attached as Excel cell comments; drag the reviewed .xlsx back to import."
	}

	helpSectionTitle(c, t, saveTitle)
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Padding(t.Space(2.5)).Gap(t.Space(1.25)).Background(greenTint(t)).Border(1, t.Success.Alpha(0.30)).Radius(cardRadius).Children(func() {
			ui.Text(c, folderTitle).FontSize(13.5).FontWeight(700).TextColor(t.Success)
			ui.Text(c, folderDesc).FontSize(12.5).TextColor(t.Text)
		})
		ui.Column(c).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Padding(t.Space(2.5)).Gap(t.Space(1.25)).Background(insetBg(t)).Border(1, hairline(t)).Radius(cardRadius).Children(func() {
			ui.Text(c, inplaceTitle).FontSize(13.5).FontWeight(700).TextColor(t.Text)
			ui.Text(c, inplaceDesc).FontSize(12.5).TextColor(t.TextMuted)
		})
	})

	helpSectionTitle(c, t, formatsTitle)
	for i := 0; i < len(formats); i += 2 {
		left := formats[i]
		right := formats[i+1]
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Stretch).Children(func() {
			helpFormatCard(c, t, left)
			helpFormatCard(c, t, right)
		})
	}

	helpSectionTitle(c, t, excelTitle)
	helpCard(c, t, func() {
		ui.Text(c, excelDesc).FontSize(13).TextColor(t.TextMuted)
	})
}

func (a *App) viewHelpShortcutsTips(c *ui.Context, isEN bool) {
	t := c.Theme()

	scTitle := "键盘快捷键"
	shortcuts := []helpShortcutItem{
		{"Cmd / Ctrl + O", "打开本地化项目目录"},
		{"Cmd / Ctrl + S", "写回保存当前所有待写回变更"},
		{"Cmd / Ctrl + E", "打开导出 Excel (.xlsx) 对话框"},
		{"Cmd / Ctrl + F", "聚焦主表搜索框（支持按原文、译文、键名搜索）"},
		{"F3  /  Alt + ↓", "跳转到下一个待办缺口"},
		{"Shift + F3  /  Alt + ↑", "跳转到上一个待办缺口"},
		{"Esc", "关闭当前打开的弹窗"},
	}
	tipsTitle := "高频效率技巧"
	if isEN {
		scTitle = "Keyboard Shortcuts"
		shortcuts = []helpShortcutItem{
			{"Cmd / Ctrl + O", "Open localization project folder"},
			{"Cmd / Ctrl + S", "Save Back pending changes"},
			{"Cmd / Ctrl + E", "Open Export Excel (.xlsx) dialog"},
			{"Cmd / Ctrl + F", "Focus search box (matches source, translation, key)"},
			{"F3  /  Alt + ↓", "Jump to next untranslated gap"},
			{"Shift + F3  /  Alt + ↑", "Jump to previous untranslated gap"},
			{"Esc", "Close active modal dialog"},
		}
		tipsTitle = "Power Tips"
	}

	helpSectionTitle(c, t, scTitle)
	helpCard(c, t, func() {
		for i, sc := range shortcuts {
			if i > 0 {
				ui.Divider(c)
			}
			ui.Row(c).Gap(t.Space(2.5)).AlignItems(ui.Center).Padding(t.Space(0.75), 0).Children(func() {
				ui.Row(c).
					Width(182).
					Shrink(0).
					Children(func() {
						ui.Row(c).
							Padding(3, t.Space(2)).
							Radius(t.Radius).
							Background(fieldBg(t)).
							Border(1, t.Border).
							Children(func() {
								ui.Text(c, sc.keys).FontSize(12.5).FontWeight(600).SingleLine()
							})
					})
				ui.Text(c, sc.desc).FontSize(13).TextColor(t.Text).Grow(1).MinWidth(0)
			})
		}
	})

	helpSectionTitle(c, t, tipsTitle)
	helpCard(c, t, func() {
		if isEN {
			helpBullet(c, t, "Row Context Menu", "Right-click any row to Copy Source, Copy Key/Context, Translate Row (fill gaps), or Re-translate Row (overwrite all enabled targets).")
			helpBullet(c, t, "One-Click Warning Filter", "Click the orange '⚠ N' pill in the top header to immediately filter to rows with placeholder or punctuation warnings.")
			helpBullet(c, t, "Drag & Drop Anything", "Drop a folder, single/multiple localization files, a .zip archive, or an .xlsx workbook straight onto the window.")
			helpBullet(c, t, "Plural Forms Editor", "Enable 'Enable Plural Forms Editing' in Project sidebar to edit One / Few / Many / Other plural forms in separate fields.")
		} else {
			helpBullet(c, t, "右键菜单快捷操作", "在主表任意行右键，可直接复制原文、复制键名/上下文、单行翻译（补缺）或整行重译（覆盖全部已启用语种）。")
			helpBullet(c, t, "顶部 ⚠ 徽章一键筛告警", "顶部标题栏出现橙色「⚠ N」按钮时，点击即可立刻筛出全部占位符/标点异常词条逐一修复。")
			helpBullet(c, t, "任意拖放加载", "直接将文件夹、单/多个本地化文件、.zip 压缩包或 .xlsx 词表拖入窗口即可自动识别加载。")
			helpBullet(c, t, "复数多词形分栏编辑", "在左侧「项目」勾选「启用复数多词形编辑」后，含复数规则的词条会按单数/复数自动分框编辑，避免手拼分隔符。")
		}
	})
}

func (a *App) viewHelpFAQPaths(c *ui.Context, isEN bool) {
	t := c.Theme()

	faqTitle := "常见问题"
	faqs := []helpQAItem{
		{
			"为什么 zh_CN.ts 里有未翻译词条，但点「开始并发翻译」没翻它？",
			"默认配置将 zh_CN 设为「★ 参考」语种（显示在参考列，不参与批量翻译）。在左侧「语种」页再次点击「★ 参考」即可取消参考状态，随后 zh_CN 就会像其他目标语种一样参与翻译。",
		},
		{
			"写回保存后的文件在哪里？原来的文件被覆盖了吗？",
			"默认使用「输出到新文件夹」模式，会在原目录同级生成 *_updated/（包含全部文件），原目录完全不动；如需直接覆盖原文件，可在左侧「项目 → 保存位置」切换为「原位覆盖」。",
		},
		{
			"免费引擎（Google / Bing / Lingva）偶发超时或翻不出来怎么办？",
			"建议在「设置」中把并发数设为 2–4、配置可用代理，并在「管线」开启多个引擎组成瀑布流自动回退；单批部分失败时 LocLoom 会保留已成功条目，仅对缺失条目自动回退。",
		},
	}
	pathsTitle := "配置、缓存与启动参数"

	if isEN {
		faqTitle = "Frequently Asked Questions"
		faqs = []helpQAItem{
			{
				"Why are unfinished entries in zh_CN.ts skipped during batch translation?",
				"By default zh_CN is marked as '★ Ref' (shown in the Reference column and excluded from batch translation). Click '★ Ref' again in the Locales tab to clear reference mode and make it translatable.",
			},
			{
				"Where are saved files written? Were my original files overwritten?",
				"By default Save Location is set to New Folder, writing a complete copy to *_updated/ next to your project while leaving originals untouched. Switch to In Place in Sidebar → Project if you want direct overwrite.",
			},
			{
				"What if a free online engine rate-limits or times out?",
				"Set concurrency to 2–4 in Settings, configure a proxy if needed, and enable multiple providers in Pipeline for automatic waterfall fallback. Partial batch successes are always preserved.",
			},
		}
		pathsTitle = "Config, Cache & CLI Flags"
	}

	helpSectionTitle(c, t, faqTitle)
	for _, qa := range faqs {
		helpCard(c, t, func() {
			ui.Text(c, "Q: "+qa.q).FontSize(13.5).FontWeight(600).TextColor(t.Text)
			ui.Text(c, qa.a).FontSize(13).TextColor(t.TextMuted)
		})
	}

	helpSectionTitle(c, t, pathsTitle)
	helpCard(c, t, func() {
		if isEN {
			helpBullet(c, t, "macOS", "Config: ~/Library/Application Support/locloom/config.json · Cache: ~/Library/Caches/locloom/translation-cache.json")
			helpBullet(c, t, "Windows", "Config: %AppData%\\locloom\\config.json · Cache: %LocalAppData%\\locloom\\translation-cache.json")
			helpBullet(c, t, "Linux", "Config: ~/.config/locloom/config.json · Cache: ~/.cache/locloom/translation-cache.json")
			helpBullet(c, t, "Portable Mode", "Place a config.json file next to the LocLoom executable to store settings alongside the binary.")
			ui.Divider(c)
			helpBullet(c, t, "CLI Launch Flags", "-src <path>  -xlsx <path>  -theme <system|light|dark>  -lang <auto|zh|en>  -open-help")
		} else {
			helpBullet(c, t, "macOS", "配置：~/Library/Application Support/locloom/config.json · 缓存：~/Library/Caches/locloom/translation-cache.json")
			helpBullet(c, t, "Windows", "配置：%AppData%\\locloom\\config.json · 缓存：%LocalAppData%\\locloom\\translation-cache.json")
			helpBullet(c, t, "Linux", "配置：~/.config/locloom/config.json · 缓存：~/.cache/locloom/translation-cache.json")
			helpBullet(c, t, "便携模式", "在可执行文件同级目录放置 config.json 即可自动切换为便携读写。")
			ui.Divider(c)
			helpBullet(c, t, "常用启动参数", "-src <目录/文件/zip>  -xlsx <词表>  -theme <system|light|dark>  -lang <auto|zh|en>  -open-help")
		}
	})
}

func helpSectionTitle(c *ui.Context, t *ui.Theme, title string) {
	ui.Text(c, title).FontSize(13.5).FontWeight(700).TextColor(t.TextMuted)
}

func helpCard(c *ui.Context, t *ui.Theme, body func()) {
	ui.Column(c).
		Padding(t.Space(2.5)).
		Gap(t.Space(1.75)).
		Background(insetBg(t)).
		Border(1, hairline(t)).
		Radius(cardRadius).
		Children(body)
}

func helpRenderBadgeRow(c *ui.Context, t *ui.Theme, left, right helpBadgeItem) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Stretch).Children(func() {
		helpBadgeCard(c, t, left)
		helpBadgeCard(c, t, right)
	})
}

func helpBadgeCard(c *ui.Context, t *ui.Theme, item helpBadgeItem) {
	var bg, fg ui.Color
	switch item.tone {
	case "green":
		bg, fg = greenTint(t), t.Success
	case "orange":
		bg, fg = orangeTint(t), t.Warning
	case "red":
		bg, fg = redTint(t), t.Danger
	default:
		bg, fg = accentTint(t), accentInk(t)
	}
	ui.Column(c).
		Grow(1).
		MinWidth(0).
		AlignSelf(ui.Stretch).
		Padding(t.Space(2), t.Space(2.5)).
		Gap(t.Space(1)).
		Background(insetBg(t)).
		Border(1, hairline(t)).
		Radius(cardRadius).
		Children(func() {
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Padding(2, t.Space(1.75)).Radius(pillRadius).Background(bg).Shrink(0).Children(func() {
					ui.Text(c, item.badge).FontSize(12).FontWeight(700).TextColor(fg).SingleLine()
				})
				ui.Text(c, item.title).FontSize(13.5).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			})
			ui.Text(c, item.desc).FontSize(12.5).TextColor(t.TextMuted)
		})
}

func helpTierRow(c *ui.Context, t *ui.Theme, label, desc string, col ui.Color) {
	ui.Row(c).Gap(t.Space(2.5)).AlignItems(ui.Start).Children(func() {
		ui.Row(c).
			Width(112).
			Shrink(0).
			Children(func() {
				ui.Row(c).Padding(3, t.Space(2)).Radius(pillRadius).Background(statusTint(t, col)).Children(func() {
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(col).SingleLine()
				})
			})
		ui.Text(c, desc).FontSize(13).TextColor(t.Text).Grow(1).MinWidth(0)
	})
}

func helpBullet(c *ui.Context, t *ui.Theme, title, desc string) {
	ui.Column(c).Gap(3).Children(func() {
		ui.Text(c, "• "+title).FontSize(13.5).FontWeight(600).TextColor(t.Text)
		ui.Text(c, desc).FontSize(12.5).TextColor(t.TextMuted).Padding(0, 0, 0, t.Space(3))
	})
}

func helpFormatCard(c *ui.Context, t *ui.Theme, item helpFormatItem) {
	ui.Column(c).
		Grow(1).
		MinWidth(0).
		AlignSelf(ui.Stretch).
		Padding(t.Space(2), t.Space(2.5)).
		Gap(t.Space(0.75)).
		Background(insetBg(t)).
		Border(1, hairline(t)).
		Radius(cardRadius).
		Children(func() {
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.name).FontSize(13.5).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
				ui.Row(c).Padding(2, t.Space(1.5)).Radius(pillRadius).Background(fieldBg(t)).Shrink(0).Children(func() {
					ui.Text(c, item.ext).FontSize(11.5).FontWeight(600).TextColor(t.TextMuted).SingleLine()
				})
			})
			ui.Text(c, item.desc).FontSize(12.5).TextColor(t.TextMuted)
		})
}
