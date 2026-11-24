package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"locloom/internal/config"
	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/locale"
	"locloom/internal/provider"
)

func (a *App) viewSidebar(c *ui.Context) {
	t := c.Theme()
	tr := a.tr()

	card(c, t).Fill().MarginEnd(t.Space(1.5)).Padding(t.Space(2)).Gap(t.Space(2)).Children(func() {
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
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Text(c, tr.SectionWriteStrategy).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				wm := int(a.cfg.WriteMode)
				if ui.Segmented(c, &wm, tr.WriteModeFill, tr.WriteModeOverwrite).FillEqually().Cursor(ui.CursorPointer).Tooltip(tr.TipWriteMode).Changed() {
					a.cfg.WriteMode = domain.WriteMode(wm)
					a.rebuildPlan()
					a.scheduleConfigSave()
				}
				wmDesc := tr.DescWriteModeFill
				if a.cfg.WriteMode == domain.ModeOverwriteExisting {
					wmDesc = tr.DescWriteModeOverwrite
				}
				ui.Text(c, wmDesc).FontSize(11).TextColor(t.TextMuted)
			})
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Text(c, tr.SectionSaveTarget).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				stm := a.cfg.SaveTargetMode
				if ui.Segmented(c, &stm, tr.SaveTargetFolder, tr.SaveTargetInPlace).FillEqually().Cursor(ui.CursorPointer).Tooltip(tr.TipSaveTarget).Changed() {
					a.cfg.SaveTargetMode = stm
					a.scheduleConfigSave()
				}
				stDesc := tr.DescSaveTargetInPlace
				if a.cfg.SaveTargetMode == config.SaveTargetNewFolder {
					targetHint := "*_updated/"
					if a.bundle != nil {
						out := format.DefaultOutputPath(a.bundle)
						if a.bundle.IsSingleFile {
							targetHint = filepath.Base(filepath.Dir(out)) + "/"
						} else if a.bundle.IsZip {
							targetHint = filepath.Base(out)
						} else if out != "" {
							targetHint = filepath.Base(out) + "/"
						}
					}
					stDesc = fmt.Sprintf(tr.DescSaveTargetFolder, targetHint)
				}
				ui.Text(c, stDesc).FontSize(11).TextColor(t.TextMuted)
			})
			ui.Divider(c)

			if ui.Checkbox(c, &a.cfg.CreateBackup, tr.CheckCreateBackup).AlignSelf(ui.Start).Tooltip(tr.TipCreateBackup).Changed() {
				a.scheduleConfigSave()
			}
			if ui.Checkbox(c, &a.cfg.EnablePluralForms, tr.CheckPluralForms).AlignSelf(ui.Start).Tooltip(tr.TipPluralForms).Changed() {
				a.editBoundLocale = ""
				a.syncEditorFromSelection()
				a.scheduleConfigSave()
			}
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				if ui.Checkbox(c, &a.cfg.AppendMissing, tr.CheckAppendMissing).AlignSelf(ui.Start).Tooltip(tr.TipAppendMissing).Changed() {
					a.rebuildPlan()
					a.scheduleConfigSave()
				}
				if a.cfg.AppendMissing {
					ui.Column(c).Gap(t.Space(0.5)).Children(func() {
						ui.Text(c, tr.LabelAppendContext).FontSize(11).TextColor(t.TextMuted)
						var ctxChoices []string
						if a.plan != nil {
							for _, cn := range a.plan.Summary.ContextNames {
								if strings.TrimSpace(cn) != "" {
									ctxChoices = append(ctxChoices, cn)
								}
							}
						}
						if !slices.Contains(ctxChoices, "MainWindow") {
							ctxChoices = append(ctxChoices, "MainWindow")
						}
						if !slices.Contains(ctxChoices, "Common") {
							ctxChoices = append(ctxChoices, "Common")
						}
						if ui.Autocomplete(c, &a.cfg.AppendContext, ctxChoices).
							Placeholder(tr.PlaceholderDefaultCtx).
							Label(tr.LabelAppendContext).
							Tooltip(tr.TipAppendMissing).
							Changed() {
							a.defaultCtx = a.cfg.AppendContext
							a.rebuildPlan()
							a.scheduleConfigSave()
						}
					})
				}
			})

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
					bg = accentTint(t)
					fg = accentInk(t)
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
			}).Height(165).Background(insetBg(t)).Border(1, hairline(t)).Radius(t.Radius).Padding(t.Space(1))

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
				cardBg := insetBg(t)
				borderCol := hairline(t)
				if focused {
					cardBg = accentTint(t)
					borderCol = t.Accent.Alpha(0.55)
				}
				card := ui.Column(c).
					Padding(t.Space(1.5), t.Space(2)).
					Gap(t.Space(1)).
					Background(cardBg).
					Border(1, borderCol).
					Radius(cardRadius).
					Cursor(ui.CursorPointer).
					Tooltip(tr.TipLocaleProgressCard)
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
			// Compact summary of Concurrency / Cache / Proxy (full controls live in Settings modal)
			cacheLen := 0
			if a.cache != nil {
				cacheLen = a.cache.Len()
			}
			proxyLabel := tr.LabelNone
			if p := strings.TrimSpace(a.cfg.Proxy.ProxyURL); p != "" {
				proxyLabel = p
			}
			ui.Row(c).
				Padding(t.Space(1.5), t.Space(2)).
				Gap(t.Space(1.5)).
				AlignItems(ui.Center).
				Background(insetBg(t)).
				Border(1, hairline(t)).
				Radius(cardRadius).
				Children(func() {
					ui.Text(c, fmt.Sprintf(tr.PipelineSummaryFmt, a.cfg.Concurrency, cacheLen, proxyLabel)).
						FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
					if ui.Button(c, tr.BtnSettings).Padding(t.Space(0.5), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipSettings).Clicked() {
						a.settingsTab = 1
						a.showSettings = true
					}
				})

			// Ordered Waterfall Provider Cards
			ui.Row(c).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
				ui.Text(c, tr.SectionWaterfall).FontSize(12).FontWeight(600).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if !a.isDefaultProviderOrder() {
					if ui.Button(c, tr.BtnResetOrder).Padding(t.Space(0.5), t.Space(1.5)).Radius(pillRadius).Tooltip(tr.TipResetOrder).Clicked() {
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

	border := hairline(t)
	if enabled {
		border = t.Accent.Alpha(0.55)
	}

	slot.Children(func() {
		card := cardStyles(t, ui.Column(c).
			Key(pid).
			Padding(t.Space(2)).
			Gap(t.Space(1.5))).
			Border(1, border).
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
				if ui.Switch(c, &enabled).Tooltip(tr.TipProviderToggle).Changed() {
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
							if ui.Checkbox(c, &checked, field.Help).AlignSelf(ui.Start).Changed() {
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
								if ui.Button(c, toggleLbl).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipToggleSecret).Clicked() {
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
								if ui.Button(c, tr.BtnBrowse).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipBrowseFile).Clicked() {
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
					if ui.Button(c, tr.BtnTestConn).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Disabled(testing).Tooltip(tr.TipTestConn).Clicked() {
						a.testProviderAsync(pid)
					}
					if _, ok := p.(provider.ModelDiscoverer); ok {
						disc := a.discoveringModels[pid]
						if ui.Button(c, tr.BtnDiscoverModels).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Disabled(disc).Tooltip(tr.TipDiscoverModels).Clicked() {
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
					btn := ui.Button(c, p.DisplayName(lang)).Padding(t.Space(1), t.Space(2)).Radius(pillRadius)
					if a.isProfileActive(p) {
						btn = btn.Background(accentTint(t)).TextColor(accentInk(t))
					}
					if btn.Clicked() {
						a.applyLocaleProfile(p)
					}
				}
			})

			ui.Row(c).Gap(t.Space(1)).Wrap().Children(func() {
				if ui.Button(c, tr.BtnSelectAll).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipSelectAllLocales).Clicked() {
					a.selectAllLocales(true)
				}
				if ui.Button(c, tr.BtnDeselectAll).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipDeselectAllLocales).Clicked() {
					a.selectAllLocales(false)
				}
				if ui.Button(c, tr.BtnSyncBundleLocales).
					Padding(t.Space(1), t.Space(2)).
					Radius(pillRadius).
					Disabled(a.bundle == nil).
					Tooltip(tr.TipSyncBundleLocales).
					Clicked() {
					a.syncBundleLocales()
				}
			})

			// FilePattern & Skeleton scaffold
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Text(c, tr.LabelFilePattern).FontSize(11).TextColor(t.TextMuted)
				if ui.TextInput(c, &a.cfg.FilePattern).Placeholder("{id}.ts").MinWidth(0).Tooltip(tr.TipFilePattern).Changed() {
					a.scheduleConfigSave()
				}
				if ui.Button(c, tr.BtnScaffoldTS).
					Padding(t.Space(1), t.Space(2)).
					Radius(pillRadius).
					Disabled(a.bundle == nil || a.bundle.Topology == format.TopologyMultiLocale).
					Tooltip(tr.TipScaffoldTS).
					Clicked() {
					a.scaffoldMissingLocaleFiles()
				}
			})

			ui.Divider(c)

			// Locale search & list
			// Locale search & list: with a bundle loaded the list is scoped to
			// project locales (bundle ∪ enabled); searching bypasses the scope so
			// hidden presets can still be found and enabled as a manual "add".
			scope := a.sidebarLocaleScope()
			visibleCount := 0
			for _, l := range a.cfg.Locales {
				if scope == nil || scope[l.ID] {
					visibleCount++
				}
			}
			ui.Text(c, fmt.Sprintf(tr.SectionLocaleListFmt, visibleCount)).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			ui.SearchField(c, &a.localeSearchQuery).Placeholder(tr.PlaceholderLocaleSearch).MinWidth(0)

			locQuery := strings.ToLower(strings.TrimSpace(a.localeSearchQuery))
			pendingRemoveID := ""
			for i := range a.cfg.Locales {
				loc := &a.cfg.Locales[i]
				if locQuery == "" && scope != nil && !scope[loc.ID] {
					continue
				}
				if locQuery != "" {
					disp := strings.ToLower(loc.DisplayName(lang))
					if !strings.Contains(strings.ToLower(loc.ID), locQuery) &&
						!strings.Contains(disp, locQuery) &&
						!strings.Contains(strings.ToLower(loc.EngineCode), locQuery) {
						continue
					}
				}
				ui.Column(c).
					Key("loc:"+loc.ID).
					Padding(t.Space(1), t.Space(1.5)).
					Gap(t.Space(1)).
					Background(insetBg(t)).
					Border(1, hairline(t)).
					Radius(t.Radius).
					Children(func() {
						ui.Row(c).
							Gap(t.Space(1)).
							AlignItems(ui.Center).
							Children(func() {
								if ui.Checkbox(c, &loc.Enabled, "").Disabled(loc.IsRef).Tooltip(tr.TipLocaleEnable).Changed() {
									a.ensureActiveLocaleValid()
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
								var refBtn ui.Element
								if loc.IsRef {
									refBtn = ui.Button(c, tr.BtnIsRef).
										Padding(t.Space(0.5), t.Space(1.5)).
										Radius(pillRadius).
										Background(accentTint(t)).
										TextColor(accentInk(t)).
										Tooltip(tr.TipSetRef)
								} else {
									refBtn = ui.Button(c, tr.BtnSetRef).
										Padding(t.Space(0.5), t.Space(1.5)).
										Radius(pillRadius).
										Tooltip(tr.TipSetRef)
								}
								if refBtn.Clicked() {
									a.setRefLocale(loc.ID)
								}
								if !loc.IsRef {
									if ui.Button(c, "×").Padding(t.Space(0.5), t.Space(1.5)).Radius(pillRadius).Tooltip(tr.TipRemoveLocale).Clicked() {
										pendingRemoveID = loc.ID
									}
								}
							})
						if !loc.IsRef {
							a.viewLocaleSourcePicker(c, loc)
						}
					})
			}
			if pendingRemoveID != "" {
				a.removeLocale(pendingRemoveID)
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
				if ui.Button(c, tr.BtnAddLocale).Padding(t.Space(1), t.Space(2)).Radius(pillRadius).Tooltip(tr.TipAddLocale).Clicked() {
					a.addCustomLocale()
				}
			})
		})
	})
}

// viewLocaleSourcePicker renders the "translate from" selector on a locale
// card: empty means the bundle's reference text, otherwise the pinned
// locale's own entries feed the engine.
func (a *App) viewLocaleSourcePicker(c *ui.Context, loc *locale.LocaleSpec) {
	t := c.Theme()
	tr := a.tr()
	options := []string{tr.OptSourceDefault}
	ids := []string{""}
	seenID := map[string]bool{"": true}
	for _, l := range a.cfg.Locales {
		if l.ID == loc.ID || !l.Enabled {
			continue
		}
		options = append(options, l.ID)
		ids = append(ids, l.ID)
		seenID[l.ID] = true
	}
	if loc.SourceLocale != "" && !seenID[loc.SourceLocale] {
		options = append(options, loc.SourceLocale)
		ids = append(ids, loc.SourceLocale)
	}
	selLabel := options[0]
	for i, id := range ids {
		if id == loc.SourceLocale {
			selLabel = options[i]
			break
		}
	}
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr.LabelTranslateFrom).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		if ui.Select(c, &selLabel, options).Width(150).Shrink(1).Tooltip(tr.TipTranslateFrom).Changed() {
			if idx := slices.Index(options, selLabel); idx >= 0 && idx < len(ids) {
				loc.SourceLocale = ids[idx]
				a.scheduleConfigSave()
			}
		}
	})
}
