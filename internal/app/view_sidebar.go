package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/provider"
)

func (a *App) viewSidebar(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	ui.Column(c).Fill().Background(t.Background).Padding(t.Space(2)).Gap(t.Space(2)).Children(func() {
		ui.Tabs(c, &a.sidebarTab, tr.TabProject, tr.TabPipeline, tr.TabLocales).FillWidth()
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
	tr := a.tr()

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2.5)).Padding(t.Space(1)).Children(func() {
			// Write mode & options
			ui.Text(c, tr.SectionWriteStrategy).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			wm := int(a.cfg.WriteMode)
			if ui.Segmented(c, &wm, tr.WriteModeFill, tr.WriteModeOverwrite).FillEqually().Changed() {
				a.cfg.WriteMode = domain.WriteMode(wm)
				a.rebuildPlan()
				a.scheduleConfigSave()
			}

			if ui.Checkbox(c, &a.cfg.AppendMissing, tr.CheckAppendMissing).Changed() {
				a.rebuildPlan()
				a.scheduleConfigSave()
			}
			if a.cfg.AppendMissing {
				if ui.TextInput(c, &a.cfg.AppendContext).
					Placeholder(tr.PlaceholderDefaultCtx).
					Changed() {
					a.defaultCtx = a.cfg.AppendContext
					a.rebuildPlan()
					a.scheduleConfigSave()
				}
			}
			if ui.Checkbox(c, &a.cfg.CreateBackup, tr.CheckCreateBackup).Changed() {
				a.scheduleConfigSave()
			}
			if ui.Checkbox(c, &a.cfg.EnablePluralForms, tr.CheckPluralForms).Changed() {
				a.editBoundLocale = ""
				a.syncEditorFromSelection()
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
			ui.Text(c, fmt.Sprintf(tr.SectionContextNavFmt, len(ctxNames))).FontSize(12).FontWeight(600).TextColor(t.TextMuted)

			itemCount := 1 + len(ctxNames)
			ui.List(c, &a.ctxListState, itemCount, func(i int) {
				name := ""
				label := fmt.Sprintf(tr.AllContextsFmt, totalRows)
				if i > 0 {
					name = ctxNames[i-1]
					cnt := ctxCounts[name]
					disp := name
					if disp == "" {
						disp = tr.NoContextLabel
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
			ui.Text(c, tr.SectionLocaleProgress).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			lang := a.UILang()
			for _, loc := range a.enabledNonRefLocales() {
				locID := loc.ID
				locName := loc.DisplayName(lang)
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
								ui.Text(c, fmt.Sprintf(tr.LocaleGapFmt, gaps)).FontSize(11).TextColor(t.Warning)
							}
							if warns > 0 {
								ui.Text(c, fmt.Sprintf(tr.LocaleWarnFmt, warns)).FontSize(11).TextColor(t.Danger)
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
	tr := a.tr()

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2.5)).Padding(t.Space(1)).Children(func() {
			// Concurrency & Cache
			ui.Text(c, tr.SectionConcurrencyCache).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr.LabelGlobalConcurrency).FontSize(12).Grow(1)
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
				if ui.Checkbox(c, &a.cfg.BypassCache, tr.CheckBypassCache).Grow(1).Changed() {
					a.scheduleConfigSave()
				}
				cacheLen := 0
				if a.cache != nil {
					cacheLen = a.cache.Len()
				}
				if ui.Button(c, fmt.Sprintf(tr.BtnClearCacheFmt, cacheLen)).Padding(t.Space(0.5), t.Space(2)).Clicked() {
					if a.cache != nil {
						_ = a.cache.Clear()
						a.setStatus(tr.StatusCacheCleared, false)
					}
				}
			})

			ui.Divider(c)

			// Network & Proxy
			ui.Text(c, tr.SectionNetworkProxy).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			prevProxyEmpty := strings.TrimSpace(a.cfg.Proxy.ProxyURL) == ""
			if ui.TextInput(c, &a.cfg.Proxy.ProxyURL).
				Placeholder(tr.PlaceholderProxy).
				Changed() {
				a.netPool.Update(a.cfg.Proxy)
				if prevProxyEmpty && strings.TrimSpace(a.cfg.Proxy.ProxyURL) != "" && !a.anyCloudProviderUsesProxy() {
					a.setCloudProvidersUseProxy(true)
				}
				a.scheduleConfigSave()
			}
			if ui.Checkbox(c, &a.cfg.Proxy.SkipTLSVerify, tr.CheckSkipTLS).Changed() {
				a.netPool.Update(a.cfg.Proxy)
				a.scheduleConfigSave()
			}
			if strings.TrimSpace(a.cfg.Proxy.ProxyURL) != "" {
				useCloudProxy := a.allCloudProvidersUseProxy()
				if ui.Checkbox(c, &useCloudProxy, tr.CheckProxyAllCloud).Changed() {
					a.setCloudProvidersUseProxy(useCloudProxy)
					a.scheduleConfigSave()
				}
			}

			ui.Divider(c)

			// Ordered Waterfall Provider Cards
			ui.Row(c).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
				ui.Text(c, tr.SectionWaterfall).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if !a.isDefaultProviderOrder() {
					if ui.Button(c, tr.BtnResetOrder).Padding(t.Space(0.5), t.Space(1.5)).Tooltip(tr.TipResetOrder).Clicked() {
						a.resetProviderOrder()
					}
				}
			})
			ui.Text(c, tr.TipDragReorder).FontSize(11).TextColor(t.TextMuted)
			provs := provider.Ordered(a.cfg.ProviderOrder)
			var pendingDrop *pendingProviderDrop
			ui.Column(c).Gap(0).Children(func() {
				for idx, p := range provs {
					if drop := a.viewProviderCard(c, provs, idx, p); drop != nil {
						pendingDrop = drop
					}
				}
			})
			if pendingDrop != nil {
				a.reorderProvider(pendingDrop.srcID, pendingDrop.targetID, pendingDrop.insertAfter)
			}
		})
	})
}

type providerDragItem struct {
	ID string
}

type pendingProviderDrop struct {
	srcID       string
	targetID    string
	insertAfter bool
}

func providerIndex(provs []provider.Provider, id string) int {
	for i, p := range provs {
		if p.ID() == id {
			return i
		}
	}
	return -1
}

func isProviderMoveNoOp(provs []provider.Provider, srcID string, dstIdx int, insertAfter bool) bool {
	srcIdx := providerIndex(provs, srcID)
	if srcIdx < 0 || dstIdx < 0 || srcIdx == dstIdx {
		return true
	}
	if insertAfter && dstIdx == srcIdx-1 {
		return true
	}
	if !insertAfter && dstIdx == srcIdx+1 {
		return true
	}
	return false
}

func (a *App) viewProviderCard(c *ui.Context, provs []provider.Provider, idx int, p provider.Provider) *pendingProviderDrop {
	t := c.Theme()
	tr := a.tr()
	lang := a.UILang()
	pid := p.ID()
	isFirst := idx == 0
	isLast := idx == len(provs)-1
	enabled := a.isProviderActive(pid)
	expanded := a.expandedProvider[pid]

	if a.cfg.ProviderConfigs == nil {
		a.cfg.ProviderConfigs = make(map[string]map[string]string)
	}
	if a.cfg.ProviderConfigs[pid] == nil {
		a.cfg.ProviderConfigs[pid] = make(map[string]string)
	}
	vals := a.cfg.ProviderConfigs[pid]

	halfGap := t.Space(1.25)
	var topPad, botPad float32
	if !isFirst {
		topPad = halfGap
	}
	if !isLast {
		botPad = halfGap
	}

	slot := ui.Column(c).Key("slot:"+pid).Padding(topPad, 0, botPad, 0)
	overItem, isDragOver := ui.DragOver[providerDragItem](slot)
	dropItem, didDrop := ui.Drop[providerDragItem](slot)

	insertAfter := a.dragHoverTarget == pid && a.dragHoverAfter
	if isDragOver || didDrop {
		_, relY, _ := slot.PointerPosition()
		if bounds := slot.LayoutBounds(); bounds.H > 0 {
			insertAfter = relY > bounds.H*0.5
		} else if isDragOver && a.dragHoverTarget != pid {
			srcIdx := providerIndex(provs, overItem.ID)
			insertAfter = srcIdx >= 0 && srcIdx < idx
		}
	}

	if isDragOver {
		a.dragHoverTarget = pid
		a.dragHoverAfter = insertAfter
		if !isProviderMoveNoOp(provs, overItem.ID, idx, insertAfter) {
			after := insertAfter
			slot.DrawOver(func(pt *ui.Painter, r ui.Rect) {
				lineY := r.Y
				if isFirst && !after {
					lineY = r.Y + 2
				}
				if after {
					lineY = r.Y + r.H
					if isLast {
						lineY = r.Y + r.H - 2
					}
				}
				pt.Fill(ui.Rect{X: r.X + 4, Y: lineY - 1, W: max(0, r.W-4), H: 2}, t.Accent, 1)
				pt.Fill(ui.Rect{X: r.X, Y: lineY - 3, W: 6, H: 6}, t.Accent, 3)
			})
		}
	}

	border := t.Border
	if enabled {
		border = t.Accent.Alpha(0.6)
	}

	slot.Children(func() {
		card := ui.Column(c).
			Key(pid).
			Padding(t.Space(2)).
			Gap(t.Space(1.5)).
			Background(t.Surface).
			Border(1, border).
			Radius(t.Radius).
			Drag(providerDragItem{ID: pid})
		if card.Dragging() {
			card = card.Opacity(0.45).Cursor(ui.CursorGrabbing)
		}
		card.Children(func() {
			// Collapsible & Reorderable Card Header
			hdrRow := ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center)
			hdrRow.ContextMenu(func(m *ui.Menu) {
				if m.Item(tr.MenuMoveTop).Disabled(isFirst).Chosen() {
					a.moveProviderToEdge(pid, true)
				}
				if m.Item(tr.TipMoveUp).Disabled(isFirst).Chosen() {
					a.moveProvider(pid, -1)
				}
				if m.Item(tr.TipMoveDown).Disabled(isLast).Chosen() {
					a.moveProvider(pid, 1)
				}
				if m.Item(tr.MenuMoveBottom).Disabled(isLast).Chosen() {
					a.moveProviderToEdge(pid, false)
				}
			})
			hdrRow.Children(func() {
				ui.Row(c).
					AlignItems(ui.Center).
					Padding(t.Space(0.5)).
					Cursor(ui.CursorGrab).
					Tooltip(tr.TipDragReorder).
					Children(func() {
						ui.Icon(c, iconGrip).Size(14, 14).TextColor(t.TextMuted)
					})

				arrow := "▸"
				if expanded {
					arrow = "▾"
				}
				hdr := ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Grow(1).MinWidth(0).Cursor(ui.CursorPointer)
				hdr.Children(func() {
					ui.Text(c, arrow).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(0)
					ui.Text(c, provider.ProviderName(p, lang)).FontSize(13).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
				})
				if hdr.Clicked() {
					a.expandedProvider[pid] = !expanded
				}

				kindLabel := tr.BadgeEngine
				if p.Kind() == provider.KindLocal {
					kindLabel = tr.BadgeLocal
				}
				ui.Badge(c, kindLabel).Shrink(0)
				if ui.Switch(c, &enabled).Changed() {
					a.toggleProvider(pid, enabled)
				}
			})

			if expanded {
				ui.Text(c, provider.ProviderDescription(p, lang)).FontSize(11).TextColor(t.TextMuted).MinWidth(0)

				// Dynamic ConfigSchema fields
				for _, field := range provider.ProviderSchema(p, lang) {
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
								if ui.Combobox(c, &curVal, opts).FillWidth().Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							} else {
								if ui.TextInput(c, &curVal).Placeholder(field.Placeholder).MinWidth(0).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							}
						case provider.FieldSecret:
							secretKey := pid + "." + fKey
							revealed := a.revealedSecrets[secretKey]
							ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
								inp := ui.TextInput(c, &curVal).Placeholder(field.Placeholder).Grow(1).MinWidth(0)
								if !revealed {
									inp = inp.Password()
								}
								if inp.Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
								toggleLbl := tr.BtnShowSecret
								if revealed {
									toggleLbl = tr.BtnHideSecret
								}
								if ui.Button(c, toggleLbl).Padding(t.Space(1), t.Space(2)).Clicked() {
									a.revealedSecrets[secretKey] = !revealed
								}
							})
						case provider.FieldFile, provider.FieldDir:
							isDir := field.Type == provider.FieldDir
							exts := slices.Clone(field.Extensions)
							fLabel := field.Label
							ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
								if ui.TextInput(c, &curVal).Placeholder(field.Placeholder).Grow(1).MinWidth(0).Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
								if ui.Button(c, tr.BtnBrowse).Padding(t.Space(1), t.Space(2)).Clicked() {
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
								if ui.Combobox(c, &curVal, a.discoveredModels[pid]).FillWidth().Changed() {
									vals[fKey] = curVal
									a.scheduleConfigSave()
								}
							} else {
								if ui.TextInput(c, &curVal).Placeholder(field.Placeholder).MinWidth(0).Changed() {
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
					if ui.Button(c, tr.BtnTestConn).Padding(t.Space(1), t.Space(2)).Disabled(testing).Clicked() {
						a.testProviderAsync(pid)
					}
					if _, ok := p.(provider.ModelDiscoverer); ok {
						disc := a.discoveringModels[pid]
						if ui.Button(c, tr.BtnDiscoverModels).Padding(t.Space(1), t.Space(2)).Disabled(disc).Clicked() {
							a.discoverModelsAsync(pid)
						}
					}
				})
				if msg := a.providerTestMsg[pid]; msg != "" {
					col := t.Success
					if a.providerTestErr[pid] {
						col = t.Danger
					}
					ui.Text(c, msg).FontSize(11).TextColor(col).MinWidth(0)
				}
			}
		})
	})

	if didDrop {
		a.dragHoverTarget = ""
		return &pendingProviderDrop{
			srcID:       dropItem.ID,
			targetID:    pid,
			insertAfter: insertAfter,
		}
	}
	if !isDragOver && a.dragHoverTarget == pid {
		a.dragHoverTarget = ""
	}
	return nil
}

func (a *App) viewSidebarLocales(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()
	lang := a.UILang()

	inBundle := make(map[string]bool)
	if a.bundle != nil {
		for _, id := range a.bundle.Locales {
			inBundle[id] = true
		}
	}

	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(1)).Children(func() {
			// Preset Profiles as wrapped chips
			ui.Text(c, tr.SectionPresetProfiles).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.Row(c).Gap(t.Space(1)).Wrap().Children(func() {
				for _, prof := range a.cfg.Profiles {
					p := prof
					if ui.Button(c, p.DisplayName(lang)).Padding(t.Space(1), t.Space(2)).Clicked() {
						a.applyLocaleProfile(p)
					}
				}
			})

			ui.Row(c).Gap(t.Space(1)).Wrap().Children(func() {
				if ui.Button(c, tr.BtnSelectAll).Padding(t.Space(1), t.Space(2)).Clicked() {
					a.selectAllLocales(true)
				}
				if ui.Button(c, tr.BtnDeselectAll).Padding(t.Space(1), t.Space(2)).Clicked() {
					a.selectAllLocales(false)
				}
				if ui.Button(c, tr.BtnSyncBundleLocales).
					Padding(t.Space(1), t.Space(2)).
					Disabled(a.bundle == nil).
					Tooltip(tr.TipSyncBundleLocales).
					Clicked() {
					a.syncBundleLocales()
				}
			})

			// FilePattern & Skeleton scaffold
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Text(c, tr.LabelFilePattern).FontSize(11).TextColor(t.TextMuted)
				if ui.TextInput(c, &a.cfg.FilePattern).Placeholder("{id}.ts").MinWidth(0).Changed() {
					a.scheduleConfigSave()
				}
				if ui.Button(c, tr.BtnScaffoldTS).
					Padding(t.Space(1), t.Space(2)).
					Disabled(a.bundle == nil || a.bundle.Topology == format.TopologyMultiLocale).
					Clicked() {
					a.scaffoldMissingLocaleFiles()
				}
			})

			ui.Divider(c)

			// Locale search & list
			ui.Text(c, fmt.Sprintf(tr.SectionLocaleListFmt, len(a.cfg.Locales))).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.SearchField(c, &a.localeSearchQuery).Placeholder(tr.PlaceholderLocaleSearch).MinWidth(0)

			locQuery := strings.ToLower(strings.TrimSpace(a.localeSearchQuery))
			for i := range a.cfg.Locales {
				loc := &a.cfg.Locales[i]
				if locQuery != "" {
					disp := strings.ToLower(loc.DisplayName(lang))
					if !strings.Contains(strings.ToLower(loc.ID), locQuery) &&
						!strings.Contains(disp, locQuery) &&
						!strings.Contains(strings.ToLower(loc.EngineCode), locQuery) {
						continue
					}
				}
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
								ui.Text(c, loc.ID).FontSize(12).FontWeight(600).SingleLine().MinWidth(0)
								if loc.EngineCode != "" {
									ui.Badge(c, loc.EngineCode).Shrink(0)
								}
								if inBundle[loc.ID] {
									ui.Badge(c, tr.BadgeInBundle).Shrink(0)
								}
							})
							ui.Text(c, loc.DisplayName(lang)).FontSize(11).TextColor(t.TextMuted).SingleLine().MinWidth(0)
						})
						refLabel := tr.BtnSetRef
						if loc.IsRef {
							refLabel = tr.BtnIsRef
						}
						if ui.Button(c, refLabel).Padding(t.Space(0.5), t.Space(1.5)).Disabled(loc.IsRef).Clicked() {
							a.setRefLocale(loc.ID)
						}
						if !loc.IsRef {
							if ui.Button(c, "×").Padding(t.Space(0.5), t.Space(1.5)).Tooltip(tr.TipRemoveLocale).Clicked() {
								a.removeLocale(loc.ID)
								return
							}
						}
					})
			}

			ui.Divider(c)

			// Compact Add Custom Locale Form
			ui.Text(c, tr.SectionAddLocale).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.TextInput(c, &a.newLocID).Placeholder(tr.PlaceholderLocaleID).MinWidth(0)
				ui.Row(c).Gap(t.Space(1)).Children(func() {
					ui.TextInput(c, &a.newLocName).Placeholder(tr.PlaceholderLocaleName).Grow(1).MinWidth(0)
					ui.TextInput(c, &a.newLocCode).Placeholder(tr.PlaceholderEngineCode).Width(90).MinWidth(0)
				})
				if ui.Button(c, tr.BtnAddLocale).Padding(t.Space(1), t.Space(2)).Clicked() {
					a.addCustomLocale()
				}
			})
		})
	})
}
