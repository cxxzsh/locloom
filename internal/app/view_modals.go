package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"locloom/internal/provider"

	"locloom/internal/config"
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
				if ui.Button(c, tr.BtnClose).Padding(t.Space(1), t.Space(2.5)).Radius(pillRadius).Clicked() {
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
				if ui.Button(c, tr.BtnCancel).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Clicked() {
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
			ui.Checkbox(c, &a.exportRelevantOnly, tr.CheckRelevantLocales).Disabled(idx == 0).MinWidth(0)
			ui.Row(c).Justify(ui.End).Gap(t.Space(2)).Children(func() {
				if ui.Button(c, tr.BtnCancel).Padding(t.Space(1.5), t.Space(3)).Radius(pillRadius).Clicked() {
					a.showExportModal = false
				}
				if ui.PrimaryButton(c, tr.BtnExportConfirm).Padding(t.Space(1.5), t.Space(3)).Radius(pillRadius).Clicked() {
					scope := sheet.ExportScope(a.exportScope)
					relevantOnly := a.exportRelevantOnly
					a.showExportModal = false
					a.exportExcelAsync(scope, relevantOnly)
				}
			})
		})
	})
	a.viewSettingsModal(c)
	a.viewOnboardingModal(c)
}

func (a *App) localizedHelpMarkdown() string {
	if a.helpMarkdownZH != "" {
		md := a.helpMarkdownZH
		if a.UILang() == i18n.LangEN {
			md = a.helpMarkdown
		}
		return stripReadmeLangSwitcher(md)
	}
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

func stripReadmeLangSwitcher(md string) string {
	const switcher = "[English](README.md) | [简体中文](README.zh-CN.md)"
	if !strings.Contains(md, switcher) {
		return strings.TrimSpace(md)
	}
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	skippedRule := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == switcher {
			continue
		}
		if !skippedRule && trimmed == "---" {
			skippedRule = true
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func (a *App) viewSettingsModal(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	ui.Modal(c, &a.showSettings, func() {
		ui.Column(c).Width(560).MaxWidth(560).MaxHeight(560).Gap(t.Space(2.5)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr.SettingsModalTitle).FontSize(16).FontWeight(700).Grow(1)
				if ui.Button(c, tr.BtnClose).Padding(t.Space(1), t.Space(2.5)).Radius(pillRadius).Clicked() {
					a.showSettings = false
				}
			})

			ui.Tabs(c, &a.settingsTab, tr.SettingsTabGeneral, tr.SettingsTabNetwork, tr.SettingsTabUpdates).FillWidth()

			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(t.Space(2.5)).Padding(t.Space(1)).Children(func() {
					switch a.settingsTab {
					case 0: // General & Appearance
						ui.Column(c).Gap(t.Space(1)).Children(func() {
							ui.Text(c, tr.LabelUILang).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
							langIdx := 0
							if a.UILang() == i18n.LangEN {
								langIdx = 1
							}
							if ui.Segmented(c, &langIdx, "中文", "English").FillEqually().Changed() {
								if langIdx == 0 {
									a.SetUILang(i18n.LangZH)
								} else {
									a.SetUILang(i18n.LangEN)
								}
							}
						})

						ui.Column(c).Gap(t.Space(1)).Children(func() {
							ui.Text(c, tr.LabelThemeMode).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
							curTheme := a.effectiveThemeMode()
							if ui.Segmented(c, &curTheme, tr.ThemeSystem, tr.ThemeLight, tr.ThemeDark).FillEqually().Changed() {
								a.SetThemeMode(curTheme)
							}
						})

						ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, tr.LabelGlobalConcurrency).FontSize(12).FontWeight(600).Grow(1)
							if ui.Button(c, "-").Padding(t.Space(0.5), t.Space(2)).Radius(pillRadius).Disabled(a.cfg.Concurrency <= 1).Tooltip(tr.TipConcurrencyDec).Clicked() {
								a.cfg.Concurrency--
								a.scheduleConfigSave()
							}
							ui.Text(c, fmt.Sprintf("%d", a.cfg.Concurrency)).FontSize(13).FontWeight(600).Width(24).TextAlign(ui.Center)
							if ui.Button(c, "+").Padding(t.Space(0.5), t.Space(2)).Radius(pillRadius).Disabled(a.cfg.Concurrency >= 16).Tooltip(tr.TipConcurrencyInc).Clicked() {
								a.cfg.Concurrency++
								a.scheduleConfigSave()
							}
						})

					case 1: // Proxy & Cache
						ui.Column(c).Gap(t.Space(1)).Children(func() {
							ui.Text(c, tr.SectionNetworkProxy).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
							prevProxyEmpty := strings.TrimSpace(a.cfg.Proxy.ProxyURL) == ""
							if ui.TextInput(c, &a.cfg.Proxy.ProxyURL).
								Placeholder(tr.PlaceholderProxy).
								Tooltip(tr.TipProxyURL).
								Changed() {
								a.netPool.Update(a.cfg.Proxy)
								if prevProxyEmpty && strings.TrimSpace(a.cfg.Proxy.ProxyURL) != "" && !a.anyCloudProviderUsesProxy() {
									a.setCloudProvidersUseProxy(true)
								}
								a.scheduleConfigSave()
							}
							if ui.Checkbox(c, &a.cfg.Proxy.SkipTLSVerify, tr.CheckSkipTLS).AlignSelf(ui.Start).Tooltip(tr.TipSkipTLS).Changed() {
								a.netPool.Update(a.cfg.Proxy)
								a.scheduleConfigSave()
							}
							if strings.TrimSpace(a.cfg.Proxy.ProxyURL) != "" {
								useCloudProxy := a.allCloudProvidersUseProxy()
								if ui.Checkbox(c, &useCloudProxy, tr.CheckProxyAllCloud).AlignSelf(ui.Start).Tooltip(tr.TipProxyAllCloud).Changed() {
									a.setCloudProvidersUseProxy(useCloudProxy)
									a.scheduleConfigSave()
								}
							}
						})

						ui.Divider(c)

						ui.Column(c).Gap(t.Space(1)).Children(func() {
							ui.Text(c, tr.SectionConcurrencyCache).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
							ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
								if ui.Checkbox(c, &a.cfg.BypassCache, tr.CheckBypassCache).Tooltip(tr.TipBypassCache).Changed() {
									a.scheduleConfigSave()
								}
								ui.Spacer(c)
								cacheLen := 0
								if a.cache != nil {
									cacheLen = a.cache.Len()
								}
								if ui.Button(c, fmt.Sprintf(tr.BtnClearCacheFmt, cacheLen)).Padding(t.Space(0.5), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipClearCache).Clicked() {
									if a.cache != nil {
										_ = a.cache.Clear()
										a.setStatus(tr.StatusCacheCleared, false)
									}
								}
							})
							if a.cache != nil && a.cache.Len() > 0 {
								isEN := a.UILang() == "en"
								if curRev := a.cache.Revision(); !a.cacheViewLoaded || curRev != a.cacheViewRev {
									a.cacheViewLocCounts = a.cache.CountsByLocale()
									a.cacheViewProvCounts = a.cache.CountsByProvider()
									a.cacheViewEntries = a.cache.Entries()
									a.cacheViewRev = curRev
									a.cacheViewLoaded = true
								}
								locCounts := a.cacheViewLocCounts
								provCounts := a.cacheViewProvCounts

								allLocLbl := "全部语种"
								allProvLbl := "全部引擎"
								searchPlaceholder := "搜索源文 / 译文…"
								clearFilteredFmt := "清理筛选 (%d)"
								if isEN {
									allLocLbl = "All Locales"
									allProvLbl = "All Engines"
									searchPlaceholder = "Search source / translation…"
									clearFilteredFmt = "Clear Filtered (%d)"
								}

								locIDs := make([]string, 0, len(locCounts))
								for id := range locCounts {
									locIDs = append(locIDs, id)
								}
								slices.Sort(locIDs)
								locLabels := make([]string, 0, len(locIDs)+1)
								locLabels = append(locLabels, allLocLbl)
								for _, id := range locIDs {
									locLabels = append(locLabels, fmt.Sprintf("%s (%d)", a.localeSelectLabel(id), locCounts[id]))
								}
								curLocLbl := allLocLbl
								if idx := slices.Index(locIDs, a.cacheClearLocale); idx >= 0 {
									curLocLbl = locLabels[idx+1]
								} else {
									a.cacheClearLocale = ""
								}

								provIDs := make([]string, 0, len(provCounts))
								for id := range provCounts {
									provIDs = append(provIDs, id)
								}
								slices.Sort(provIDs)
								provLabels := make([]string, 0, len(provIDs)+1)
								provLabels = append(provLabels, allProvLbl)
								for _, id := range provIDs {
									provLabels = append(provLabels, fmt.Sprintf("%s (%d)", id, provCounts[id]))
								}
								curProvLbl := allProvLbl
								if idx := slices.Index(provIDs, a.cacheClearProvider); idx >= 0 {
									curProvLbl = provLabels[idx+1]
								} else {
									a.cacheClearProvider = ""
								}

								entries := a.cacheViewEntries
								q := strings.ToLower(strings.TrimSpace(a.cacheSearchQuery))
								filtered := make([]provider.CacheEntry, 0, len(entries))
								filteredKeys := make([]string, 0, len(entries))
								for _, e := range entries {
									if a.cacheClearLocale != "" && e.TargetLocale != a.cacheClearLocale {
										continue
									}
									if a.cacheClearProvider != "" && e.ProviderID != a.cacheClearProvider {
										continue
									}
									if q != "" {
										if !strings.Contains(strings.ToLower(e.Source), q) &&
											!strings.Contains(strings.ToLower(e.Translated), q) &&
											!strings.Contains(strings.ToLower(e.TargetLocale), q) &&
											!strings.Contains(strings.ToLower(e.ProviderID), q) {
											continue
										}
									}
									filtered = append(filtered, e)
									filteredKeys = append(filteredKeys, e.Key)
								}

								ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
									ui.TextInput(c, &a.cacheSearchQuery).Placeholder(searchPlaceholder).Grow(1)
									if ui.Select(c, &curLocLbl, locLabels).Width(150).Changed() {
										if curLocLbl == allLocLbl {
											a.cacheClearLocale = ""
										} else if idx := slices.Index(locLabels, curLocLbl); idx > 0 {
											a.cacheClearLocale = locIDs[idx-1]
										}
									}
									if ui.Select(c, &curProvLbl, provLabels).Width(120).Changed() {
										if curProvLbl == allProvLbl {
											a.cacheClearProvider = ""
										} else if idx := slices.Index(provLabels, curProvLbl); idx > 0 {
											a.cacheClearProvider = provIDs[idx-1]
										}
									}
									if len(filteredKeys) > 0 {
										if ui.Button(c, fmt.Sprintf(clearFilteredFmt, len(filteredKeys))).Padding(t.Space(0.5), t.Space(1.5)).Radius(pillRadius).Clicked() {
											n := a.cache.DeleteKeys(filteredKeys)
											a.setStatus(fmt.Sprintf("%s (%d)", tr.StatusCacheCleared, n), false)
										}
									}
								})

								ui.Scroll(c).MaxHeight(200).Background(insetBg(t)).Border(1, hairline(t)).Radius(t.Radius).Padding(t.Space(0.5)).Children(func() {
									ui.Column(c).Gap(2).Children(func() {
										limit := min(len(filtered), 100)
										for i := 0; i < limit; i++ {
											e := filtered[i]
											ui.Row(c).Gap(t.Space(1)).Padding(t.Space(0.4), t.Space(0.8)).AlignItems(ui.Center).Children(func() {
												ui.Text(c, e.ProviderID).FontSize(11).TextColor(t.TextMuted).Width(72).Shrink(0)
												ui.Text(c, e.TargetLocale).FontSize(11).TextColor(t.Accent).Width(48).Shrink(0)
												summary := strings.ReplaceAll(e.Source, "\n", " ") + " → " + strings.ReplaceAll(e.Translated, "\n", " ")
												ui.Text(c, summary).FontSize(11).Grow(1)
												if ui.Button(c, "×").Padding(0, t.Space(0.8)).Radius(pillRadius).Clicked() {
													if a.cache.DeleteKey(e.Key) {
														a.setStatus(fmt.Sprintf("%s (1)", tr.StatusCacheCleared), false)
													}
												}
											})
										}
										if len(filtered) > 100 {
											moreTxt := fmt.Sprintf("仅显示前 100 条（共 %d 条），请使用搜索或筛选缩小范围", len(filtered))
											if isEN {
												moreTxt = fmt.Sprintf("Showing first 100 of %d entries; filter to narrow down", len(filtered))
											}
											ui.Text(c, moreTxt).FontSize(11).TextColor(t.TextMuted).Padding(t.Space(0.5), t.Space(1))
										}
									})
								})
							}
						})

					case 2: // Updates & Onboarding
						ui.Column(c).Gap(t.Space(1.5)).Children(func() {
							ui.Text(c, fmt.Sprintf(tr.LabelCurrentVersionFmt, config.AppVersion)).FontSize(13).FontWeight(600)
							if ui.Checkbox(c, &a.cfg.CheckUpdatesOnStartup, tr.CheckAutoUpdate).AlignSelf(ui.Start).Changed() {
								a.scheduleConfigSave()
							}
							ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
								lbl := tr.BtnCheckUpdates
								if a.checkingUpdate {
									lbl = tr.BtnCheckingUpdates
								}
								if ui.PrimaryButton(c, lbl).Padding(t.Space(1), t.Space(2.5)).Radius(pillRadius).Disabled(a.checkingUpdate).Clicked() {
									a.checkUpdatesAsync(false)
								}
								if a.pendingUpdate != nil {
									lbl := tr.BtnInstallUpdate
									if a.installingUpd {
										lbl = tr.BtnInstalling
									}
									if ui.PrimaryButton(c, lbl).Padding(t.Space(1), t.Space(2.5)).Radius(pillRadius).Disabled(a.installingUpd).Clicked() {
										a.installUpdate()
									}
								}
								if a.updateInfo != nil && a.updateInfo.HasUpdate {
									if ui.Button(c, tr.BtnOpenReleasePage).Padding(t.Space(1), t.Space(2.5)).Radius(pillRadius).Clicked() {
										a.openReleasePage()
									}
								}
							})
							if a.installingUpd && a.updateTotal > 0 {
								ui.Progress(c, float64(a.updateDL)/float64(a.updateTotal))
							}
							if a.updateErr != "" {
								ui.Text(c, fmt.Sprintf(tr.StatusUpdateErrFmt, a.updateErr)).FontSize(12).TextColor(t.Danger)
							} else if a.pendingUpdate != nil {
								ui.Text(c, fmt.Sprintf(tr.StatusUpdateAvailFmt, a.pendingUpdate.Version, config.AppVersion)).
									FontSize(12).FontWeight(600).TextColor(t.Accent)
								if a.pendingUpdate.Notes != "" {
									ui.Scroll(c).MaxHeight(120).Background(insetBg(t)).Border(1, hairline(t)).Radius(t.Radius).Children(func() {
										markdown.View(c, a.pendingUpdate.Notes, &a.mdOpts)
									})
								}
							} else if a.updateInfo != nil {
								if a.updateInfo.HasUpdate {
									ui.Text(c, fmt.Sprintf(tr.StatusUpdateAvailFmt, a.updateInfo.LatestVersion, a.updateInfo.CurrentVersion)).
										FontSize(12).FontWeight(600).TextColor(t.Accent)
								} else {
									ui.Text(c, fmt.Sprintf(tr.StatusUpToDateFmt, a.updateInfo.CurrentVersion)).
										FontSize(12).TextColor(t.Success)
								}
							}
						})

						ui.Divider(c)

						ui.Row(c).AlignItems(ui.Center).Children(func() {
							if ui.Button(c, tr.BtnOpenOnboarding).Padding(t.Space(1), t.Space(2.5)).Radius(pillRadius).Clicked() {
								a.showSettings = false
								a.showOnboarding = true
							}
						})
					}
				})
			})
		})
	})
}

func (a *App) viewOnboardingModal(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	ui.Modal(c, &a.showOnboarding, func() {
		ui.Column(c).Width(560).MaxWidth(560).Gap(t.Space(2.5)).Children(func() {
			ui.Column(c).Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, tr.OnboardingTitle).FontSize(18).FontWeight(700)
				ui.Text(c, tr.OnboardingSubtitle).FontSize(12).TextColor(t.TextMuted)
			})
			ui.Divider(c)

			steps := []struct {
				title string
				desc  string
			}{
				{tr.OnboardingStep1Title, tr.OnboardingStep1Desc},
				{tr.OnboardingStep2Title, tr.OnboardingStep2Desc},
				{tr.OnboardingStep3Title, tr.OnboardingStep3Desc},
			}
			for _, st := range steps {
				ui.Column(c).
					Padding(t.Space(1.5), t.Space(2)).
					Gap(t.Space(0.5)).
					Background(insetBg(t)).
					Border(1, hairline(t)).
					Radius(cardRadius).
					Children(func() {
						ui.Text(c, st.title).FontSize(13).FontWeight(600).TextColor(t.Accent)
						ui.Text(c, st.desc).FontSize(12).TextColor(t.TextMuted)
					})
			}

			ui.Row(c).Justify(ui.End).Gap(t.Space(2)).Children(func() {
				if ui.PrimaryButton(c, tr.BtnOnboardingStart).Padding(t.Space(1.5), t.Space(3.5)).Radius(pillRadius).Clicked() {
					a.showOnboarding = false
					if !a.cfg.HasSeenOnboarding {
						a.cfg.HasSeenOnboarding = true
						a.scheduleConfigSave()
					}
				}
			})
		})
	})
}
