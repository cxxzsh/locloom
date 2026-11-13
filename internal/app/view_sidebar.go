package app

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/provider"
)

func (a *App) viewSidebar(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(2)).Children(func() {
		ui.Tabs(c, &a.sidebarTab, "项目", "管线", "语种").FillWidth()

		switch a.sidebarTab {
		case 0:
			a.viewSidebarWorkspace(c)
		case 1:
			a.viewSidebarPipeline(c)
		case 2:
			a.viewSidebarLocales(c)
		}
	})
}

func (a *App) viewSidebarWorkspace(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2.5)).Padding(t.Space(1)).Children(func() {
			// Write mode & options
			ui.Text(c, "写回策略").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			wm := int(a.cfg.WriteMode)
			if ui.Segmented(c, &wm, "安全补齐空缺", "覆盖已有译文").FillEqually().Changed() {
				a.cfg.WriteMode = domain.WriteMode(wm)
				a.rebuildPlan()
				a.scheduleConfigSave()
			}

			if ui.Checkbox(c, &a.cfg.AppendMissing, "追加词表新增词条").Changed() {
				a.rebuildPlan()
				a.scheduleConfigSave()
			}
			if a.cfg.AppendMissing {
				if ui.TextInput(c, &a.cfg.AppendContext).
					Placeholder("默认上下文 (留空使用 Common)").
					Changed() {
					a.defaultCtx = a.cfg.AppendContext
					a.rebuildPlan()
					a.scheduleConfigSave()
				}
			}
			if ui.Checkbox(c, &a.cfg.CreateBackup, "写回前备份 .bak").Changed() {
				a.scheduleConfigSave()
			}

			ui.Divider(c)

			// Context Navigator (virtualized ui.List)
			totalRows := 0
			var ctxNames []string
			var ctxCounts map[string]int
			if a.plan != nil {
				totalRows = a.plan.Summary.TotalRows
				ctxNames = a.plan.Summary.ContextNames
				ctxCounts = a.plan.Summary.ContextCounts
			}
			ui.Text(c, fmt.Sprintf("上下文导航 (%d)", len(ctxNames))).FontSize(12).FontWeight(600).TextColor(t.TextMuted)

			itemCount := 1 + len(ctxNames)
			ui.List(c, &a.ctxListState, itemCount, func(i int) {
				name := ""
				label := fmt.Sprintf("全部上下文 (%d)", totalRows)
				if i > 0 {
					name = ctxNames[i-1]
					cnt := ctxCounts[name]
					disp := name
					if disp == "" {
						disp = "(无上下文)"
					}
					label = fmt.Sprintf("%s (%d)", disp, cnt)
				}
				active := a.contextFilter == name
				bg := ui.Transparent
				fg := t.Text
				if active {
					bg = t.Accent.Alpha(0.14)
					fg = t.Accent
				}
				row := ui.Row(c).
					Padding(t.Space(1), t.Space(2)).
					Radius(t.Radius).
					Background(bg).
					Cursor(ui.CursorPointer)
				row.Children(func() {
					ui.Text(c, label).FontSize(12).TextColor(fg).SingleLine().Grow(1)
				})
				if row.Clicked() {
					a.contextFilter = name
					a.refilterRows()
				}
			}).Height(165).Border(1, t.Border).Radius(t.Radius).Padding(t.Space(1))

			ui.Divider(c)

			// Per-Locale Progress Summary
			ui.Text(c, "语种进度总览").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			for _, loc := range a.enabledNonRefLocales() {
				locID := loc.ID
				locName := loc.Name
				var pct float64
				var gaps, warns int
				if a.plan != nil {
					if st, ok := a.plan.Summary.LocaleStats[locID]; ok {
						pct = st.Completion
						gaps = st.GapsAfter
						warns = st.Warnings
					}
				}
				focused := a.focusedLocale == locID
				cardBg := t.Surface
				borderCol := t.Border
				if focused {
					cardBg = t.Accent.Alpha(0.1)
					borderCol = t.Accent
				}
				card := ui.Column(c).
					Padding(t.Space(1.5), t.Space(2)).
					Gap(t.Space(1)).
					Background(cardBg).
					Border(1, borderCol).
					Radius(t.Radius).
					Cursor(ui.CursorPointer)
				card.Children(func() {
					ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
						ui.Text(c, fmt.Sprintf("%s · %s", locID, locName)).FontSize(12).FontWeight(600).SingleLine().Grow(1)
						ui.Text(c, fmt.Sprintf("%.0f%%", pct*100)).FontSize(11).TextColor(t.TextMuted).SingleLine()
					})
					ui.Progress(c, pct)
					if gaps > 0 || warns > 0 {
						ui.Row(c).Gap(t.Space(1.5)).Children(func() {
							if gaps > 0 {
								ui.Text(c, fmt.Sprintf("缺 %d", gaps)).FontSize(11).TextColor(t.Warning)
							}
							if warns > 0 {
								ui.Text(c, fmt.Sprintf("告警 %d", warns)).FontSize(11).TextColor(t.Danger)
							}
						})
					}
				})
				if card.Clicked() {
					a.focusedLocale = locID
					a.activeEditLocale = locID
					a.syncEditorFromSelection()
				}
			}
		})
	})
}

func (a *App) viewSidebarPipeline(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2.5)).Padding(t.Space(1)).Children(func() {
			// Concurrency & Cache
			ui.Text(c, "并发与缓存").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "全局并发数").FontSize(12).Grow(1)
				if ui.Button(c, "-").Padding(t.Space(0.5), t.Space(2)).Disabled(a.cfg.Concurrency <= 1).Clicked() {
					a.cfg.Concurrency--
					a.scheduleConfigSave()
				}
				ui.Text(c, fmt.Sprintf("%d", a.cfg.Concurrency)).FontSize(13).FontWeight(600).Width(24).TextAlign(ui.Center)
				if ui.Button(c, "+").Padding(t.Space(0.5), t.Space(2)).Disabled(a.cfg.Concurrency >= 16).Clicked() {
					a.cfg.Concurrency++
					a.scheduleConfigSave()
				}
			})

			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				if ui.Checkbox(c, &a.cfg.BypassCache, "跳过本地翻译缓存").Grow(1).Changed() {
					a.scheduleConfigSave()
				}
				cacheLen := 0
				if a.cache != nil {
					cacheLen = a.cache.Len()
				}
				if ui.Button(c, fmt.Sprintf("清空(%d)", cacheLen)).Padding(t.Space(0.5), t.Space(2)).Clicked() {
					if a.cache != nil {
						_ = a.cache.Clear()
						a.setStatus("已清空本地翻译缓存", false)
					}
				}
			})

			ui.Divider(c)

			// Network & Proxy
			ui.Text(c, "网络与代理").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			if ui.TextInput(c, &a.cfg.Proxy.ProxyURL).
				Placeholder("http://127.0.0.1:8080 或 socks5://127.0.0.1:1080").
				Changed() {
				a.netPool.Update(a.cfg.Proxy)
				a.scheduleConfigSave()
			}
			if ui.Checkbox(c, &a.cfg.Proxy.SkipTLSVerify, "跳过 TLS 证书校验").Changed() {
				a.netPool.Update(a.cfg.Proxy)
				a.scheduleConfigSave()
			}

			ui.Divider(c)

			// Ordered Waterfall Provider Cards
			ui.Text(c, "翻译引擎瀑布流（按顺序回退）").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			provs := provider.Ordered(a.cfg.ProviderOrder)
			for idx, p := range provs {
				a.viewProviderCard(c, p, idx == 0, idx == len(provs)-1)
			}
		})
	})
}

func (a *App) viewProviderCard(c *ui.Context, p provider.Provider, isFirst, isLast bool) {
	t := c.Theme()
	pid := p.ID()
	enabled := a.isProviderActive(pid)

	if a.cfg.ProviderConfigs == nil {
		a.cfg.ProviderConfigs = make(map[string]map[string]string)
	}
	if a.cfg.ProviderConfigs[pid] == nil {
		a.cfg.ProviderConfigs[pid] = make(map[string]string)
	}
	vals := a.cfg.ProviderConfigs[pid]

	border := t.Border
	if enabled {
		border = t.Accent.Alpha(0.6)
	}

	ui.Column(c).
		Padding(t.Space(2)).
		Gap(t.Space(1.5)).
		Background(t.Surface).
		Border(1, border).
		Radius(t.Radius).
		Children(func() {
			// Card Header
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.Name()).FontSize(13).FontWeight(600).SingleLine().Grow(1)
				kindLabel := "引擎"
				if p.Kind() == provider.KindLocal {
					kindLabel = "本地"
				}
				ui.Badge(c, kindLabel)
				if ui.Button(c, "↑").Padding(t.Space(0.5), t.Space(1.5)).Disabled(isFirst).Tooltip("上移优先级").Clicked() {
					a.moveProvider(pid, -1)
				}
				if ui.Button(c, "↓").Padding(t.Space(0.5), t.Space(1.5)).Disabled(isLast).Tooltip("下移优先级").Clicked() {
					a.moveProvider(pid, 1)
				}
				if ui.Switch(c, &enabled).Changed() {
					a.toggleProvider(pid, enabled)
				}
			})

			ui.Text(c, p.Description()).FontSize(11).TextColor(t.TextMuted)

			if enabled {
				// Dynamic ConfigSchema fields
				for _, field := range p.ConfigSchema() {
					fKey := field.Key
					curVal := vals[fKey]
					if curVal == "" && field.Default != "" {
						curVal = field.Default
					}

					ui.Column(c).Gap(t.Space(0.5)).Children(func() {
						ui.Text(c, field.Label).FontSize(11).FontWeight(500).TextColor(t.TextMuted)
						switch field.Type {
						case provider.FieldBool:
							checked := curVal == "true" || curVal == "1"
							if ui.Checkbox(c, &checked, field.Help).Changed() {
								if checked {
									vals[fKey] = "true"
								} else {
									vals[fKey] = "false"
								}
								a.scheduleConfigSave()
							}
						case provider.FieldSelect:
							opts := field.Options
							if fKey == "model" && len(a.discoveredModels[pid]) > 0 {
								opts = a.discoveredModels[pid]
							}
							if len(opts) > 0 {
								if ui.Combobox(c, &curVal, opts).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							} else {
								if ui.TextInput(c, &curVal).Placeholder(field.Placeholder).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							}
						case provider.FieldSecret:
							if ui.TextInput(c, &curVal).Password().Placeholder(field.Placeholder).Changed() {
								vals[fKey] = curVal
								a.scheduleConfigSave()
							}
						case provider.FieldFile, provider.FieldDir:
							isDir := field.Type == provider.FieldDir
							exts := slices.Clone(field.Extensions)
							fLabel := field.Label
							ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
								if ui.TextInput(c, &curVal).Placeholder(field.Placeholder).Grow(1).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
								if ui.Button(c, "浏览").Padding(t.Space(1), t.Space(2)).Clicked() {
									win := a.win
									go func() {
										opts := mygo.OpenDialogOptions{
											Parent:    win,
											Title:     fLabel,
											Directory: isDir,
										}
										if !isDir && len(exts) > 0 {
											opts.Filters = []mygo.FileFilter{{Name: fLabel, Extensions: exts}}
										}
										paths, err := mygo.Dialog.Open(opts)
										if err == nil && len(paths) > 0 {
											picked := paths[0]
											a.dispatchUI(func() {
												a.cfg.ProviderConfigs[pid][fKey] = picked
												a.scheduleConfigSave()
											})
										}
									}()
								}
							})
						default:
							if fKey == "model" && len(a.discoveredModels[pid]) > 0 {
								if ui.Combobox(c, &curVal, a.discoveredModels[pid]).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							} else {
								if ui.TextInput(c, &curVal).Placeholder(field.Placeholder).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							}
						}
					})
				}

				// Actions row: Test Connectivity + Discover Models (if supported)
				ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
					testing := a.providerTesting[pid]
					if ui.Button(c, "连通测试").Padding(t.Space(1), t.Space(2)).Disabled(testing).Clicked() {
						a.testProviderAsync(pid)
					}
					if _, ok := p.(provider.ModelDiscoverer); ok {
						disc := a.discoveringModels[pid]
						if ui.Button(c, "发现模型").Padding(t.Space(1), t.Space(2)).Disabled(disc).Clicked() {
							a.discoverModelsAsync(pid)
						}
					}
				})
				if msg := a.providerTestMsg[pid]; msg != "" {
					col := t.Success
					if a.providerTestErr[pid] {
						col = t.Danger
					}
					ui.Text(c, msg).FontSize(11).TextColor(col)
				}
			}
		})
}

func (a *App) viewSidebarLocales(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(1)).Children(func() {
			// Preset Profiles in 2-column Grid (no horizontal overflow)
			ui.Text(c, "语种预设组合").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.Grid(c).Columns(2).Gap(t.Space(1.5)).Children(func() {
				for _, prof := range a.cfg.Profiles {
					p := prof
					if ui.Button(c, p.Name).Padding(t.Space(1), t.Space(2)).Clicked() {
						a.applyLocaleProfile(p)
					}
				}
			})

			ui.Row(c).Gap(t.Space(1.5)).Children(func() {
				if ui.Button(c, "全选").Padding(t.Space(1), t.Space(2)).Grow(1).Clicked() {
					a.selectAllLocales(true)
				}
				if ui.Button(c, "全不选").Padding(t.Space(1), t.Space(2)).Grow(1).Clicked() {
					a.selectAllLocales(false)
				}
			})

			// FilePattern & Skeleton scaffold
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Text(c, "文件命名模式").FontSize(11).TextColor(t.TextMuted)
				if ui.TextInput(c, &a.cfg.FilePattern).Placeholder("{id}.ts").Changed() {
					a.scheduleConfigSave()
				}
				if ui.Button(c, "补全缺失语种骨架 .ts").Padding(t.Space(1), t.Space(2)).Disabled(a.bundle == nil).Clicked() {
					a.scaffoldMissingTSFiles()
				}
			})

			ui.Divider(c)

			// Locale list
			ui.Text(c, fmt.Sprintf("语种列表 (%d)", len(a.cfg.Locales))).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			for i := range a.cfg.Locales {
				loc := &a.cfg.Locales[i]
				ui.Row(c).
					Padding(t.Space(1), t.Space(1.5)).
					Gap(t.Space(1)).
					AlignItems(ui.Center).
					Background(t.Surface).
					Radius(t.Radius).
					Children(func() {
						if ui.Checkbox(c, &loc.Enabled, "").Disabled(loc.IsRef).Changed() {
							a.rebuildPlan()
							a.scheduleConfigSave()
						}
						ui.Column(c).Grow(1).MinWidth(0).Children(func() {
							ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
								ui.Text(c, loc.ID).FontSize(12).FontWeight(600).SingleLine()
								if loc.EngineCode != "" {
									ui.Badge(c, loc.EngineCode)
								}
							})
							ui.Text(c, loc.Name).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
						refLabel := "设为参考"
						if loc.IsRef {
							refLabel = "★ 参考"
						}
						if ui.Button(c, refLabel).Padding(t.Space(0.5), t.Space(1.5)).Disabled(loc.IsRef).Clicked() {
							a.setRefLocale(loc.ID)
						}
						if !loc.IsRef {
							if ui.Button(c, "×").Padding(t.Space(0.5), t.Space(1.5)).Tooltip("移除语种").Clicked() {
								a.removeLocale(loc.ID)
								return
							}
						}
					})
			}

			ui.Divider(c)

			// Compact Add Custom Locale Form
			ui.Text(c, "添加自定义语种").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.TextInput(c, &a.newLocID).Placeholder("语种 ID (如 nl_NL)")
				ui.Row(c).Gap(t.Space(1)).Children(func() {
					ui.TextInput(c, &a.newLocName).Placeholder("名称 (如 荷兰语)").Grow(1)
					ui.TextInput(c, &a.newLocCode).Placeholder("引擎码 (nl)").Width(90)
				})
				if ui.Button(c, "添加语种").Padding(t.Space(1), t.Space(2)).Clicked() {
					a.addCustomLocale()
				}
			})
		})
	})
}
