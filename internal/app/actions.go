package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/planner"
	"locloom/internal/provider"
	"locloom/internal/sheet"
	"locloom/internal/tsxml"
)

func (a *App) enabledNonRefLocales() []locale.LocaleSpec {
	var out []locale.LocaleSpec
	for _, loc := range a.cfg.Locales {
		if loc.Enabled && !loc.IsRef {
			out = append(out, loc)
		}
	}
	return out
}

func (a *App) enabledNonRefIDs() []string {
	locs := a.enabledNonRefLocales()
	out := make([]string, len(locs))
	for i, l := range locs {
		out[i] = l.ID
	}
	return out
}

func (a *App) rebuildPlan() {
	in := planner.PlanInput{
		Bundle:        a.bundle,
		ExtraEntries:  a.extraEntries,
		DefaultCtx:    a.defaultCtx,
		Catalog:       a.catalog,
		Locales:       a.cfg.Locales,
		WriteMode:     a.cfg.WriteMode,
		AppendMissing: a.cfg.AppendMissing,
	}
	a.plan = planner.BuildPlan(in)

	ids := a.enabledNonRefIDs()
	if len(ids) > 0 {
		if a.focusedLocale == "" || !slices.Contains(ids, a.focusedLocale) {
			a.focusedLocale = ids[0]
		}
		if a.activeEditLocale == "" || !slices.Contains(ids, a.activeEditLocale) {
			a.activeEditLocale = a.focusedLocale
		}
	} else {
		a.focusedLocale = ""
		a.activeEditLocale = ""
	}

	a.refilterRows()
}

func (a *App) refilterRows() {
	a.filteredRows = a.filteredRows[:0]
	if a.plan == nil {
		a.selectedRow = -1
		a.syncEditorFromSelection()
		return
	}

	q := strings.ToLower(strings.TrimSpace(a.searchQuery))
	for i := range a.plan.Rows {
		row := &a.plan.Rows[i]
		if a.contextFilter != "" && row.Key.Context != a.contextFilter {
			continue
		}
		switch a.statusFilter {
		case 1: // Gaps
			if !row.HasGap {
				continue
			}
		case 2: // Pending changes
			if !row.HasChange {
				continue
			}
		case 3: // Warnings
			if !row.HasWarning {
				continue
			}
		}
		if q != "" && !rowMatchesQuery(row, q, a.focusedLocale) {
			continue
		}
		a.filteredRows = append(a.filteredRows, i)
	}

	// Preserve selected row by EntryKey when possible.
	newSel := -1
	if len(a.filteredRows) > 0 {
		for idx, rowIdx := range a.filteredRows {
			if a.plan.Rows[rowIdx].Key == a.selectedKey {
				newSel = idx
				break
			}
		}
		if newSel < 0 {
			if a.selectedRow >= 0 && a.selectedRow < len(a.filteredRows) {
				newSel = a.selectedRow
			} else {
				newSel = 0
			}
		}
	}
	a.selectedRow = newSel
	a.syncEditorFromSelection()
}

func rowMatchesQuery(row *planner.MatrixRow, q, focusedLocale string) bool {
	if strings.Contains(strings.ToLower(row.Key.Context), q) ||
		strings.Contains(strings.ToLower(row.Key.Source), q) ||
		strings.Contains(strings.ToLower(row.Key.Comment), q) ||
		strings.Contains(strings.ToLower(row.RefText), q) {
		return true
	}
	if focusedLocale != "" {
		if cell, ok := row.Cells[focusedLocale]; ok {
			if strings.Contains(strings.ToLower(cell.Effective), q) ||
				strings.Contains(strings.ToLower(cell.OldText), q) ||
				strings.Contains(strings.ToLower(cell.NewText), q) {
				return true
			}
		}
	}
	for _, cell := range row.Cells {
		if strings.Contains(strings.ToLower(cell.Effective), q) {
			return true
		}
	}
	return false
}

func (a *App) currentMatrixRow() (int, *planner.MatrixRow) {
	if a.plan == nil || a.selectedRow < 0 || a.selectedRow >= len(a.filteredRows) {
		return -1, nil
	}
	rowIdx := a.filteredRows[a.selectedRow]
	if rowIdx < 0 || rowIdx >= len(a.plan.Rows) {
		return -1, nil
	}
	return rowIdx, &a.plan.Rows[rowIdx]
}

func (a *App) syncEditorFromSelection() {
	_, row := a.currentMatrixRow()
	if row == nil {
		a.selectedKey = domain.EntryKey{}
		a.editBoundRowKey = domain.EntryKey{}
		a.editBoundLocale = ""
		a.editBuf = ""
		return
	}
	a.selectedKey = row.Key
	if a.activeEditLocale == "" {
		a.activeEditLocale = a.focusedLocale
	}
	if row.Key == a.editBoundRowKey && a.activeEditLocale == a.editBoundLocale {
		return
	}
	a.editBoundRowKey = row.Key
	a.editBoundLocale = a.activeEditLocale
	if cell, ok := row.Cells[a.activeEditLocale]; ok {
		a.editBuf = cell.Effective
	} else {
		a.editBuf = ""
	}
}

// onEditCell performs an O(1) cell update without rebuilding the entire MatrixPlan.
func (a *App) onEditCell(newText string) {
	rowIdx, row := a.currentMatrixRow()
	if row == nil || a.activeEditLocale == "" {
		return
	}
	a.catalog.Set(a.activeEditLocale, row.Key, newText, domain.OriginManual)
	planner.UpdateCell(a.plan, rowIdx, a.activeEditLocale, a.catalog)
}

func (a *App) resetActiveCell() {
	rowIdx, row := a.currentMatrixRow()
	if row == nil || a.activeEditLocale == "" {
		return
	}
	a.catalog.Delete(a.activeEditLocale, row.Key)
	planner.UpdateCell(a.plan, rowIdx, a.activeEditLocale, a.catalog)
	if cell, ok := row.Cells[a.activeEditLocale]; ok {
		a.editBuf = cell.Effective
	} else {
		a.editBuf = ""
	}
}

func (a *App) isProviderActive(id string) bool {
	return slices.Contains(a.cfg.ActiveProviders, id)
}

func (a *App) toggleProvider(id string, on bool) {
	if on {
		if !slices.Contains(a.cfg.ActiveProviders, id) {
			a.cfg.ActiveProviders = append(a.cfg.ActiveProviders, id)
		}
	} else {
		a.cfg.ActiveProviders = slices.DeleteFunc(a.cfg.ActiveProviders, func(s string) bool { return s == id })
	}
	a.scheduleConfigSave()
}

func (a *App) moveProvider(id string, delta int) {
	provs := provider.Ordered(a.cfg.ProviderOrder)
	order := make([]string, len(provs))
	idx := -1
	for i, p := range provs {
		order[i] = p.ID()
		if p.ID() == id {
			idx = i
		}
	}
	if idx < 0 {
		return
	}
	target := idx + delta
	if target < 0 || target >= len(order) {
		return
	}
	order[idx], order[target] = order[target], order[idx]
	a.cfg.ProviderOrder = order
	a.scheduleConfigSave()
}

func (a *App) openTSDirAsync() {
	win := a.win
	defaultPath := a.cfg.LastTSPath
	filePattern := a.cfg.FilePattern
	locales := slices.Clone(a.cfg.Locales)

	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "选择包含 Qt .ts 文件的目录",
			DefaultPath: defaultPath,
			Directory:   true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		bundle, err := tsxml.LoadBundle(paths[0], filePattern, locales)
		a.dispatchUI(func() {
			if err != nil {
				a.setStatus("加载 .ts 目录失败: "+err.Error(), true)
				a.appendLog("error", "", "", "加载 .ts 目录失败: "+err.Error())
				return
			}
			a.applyLoadedBundle(bundle)
		})
	}()
}

func (a *App) openZipAsync() {
	win := a.win
	defaultPath := a.cfg.LastTSPath
	filePattern := a.cfg.FilePattern
	locales := slices.Clone(a.cfg.Locales)

	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "选择包含 .ts 的 ZIP 压缩包",
			DefaultPath: defaultPath,
			Filters:     []mygo.FileFilter{{Name: "ZIP 压缩包", Extensions: []string{"zip"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		bundle, err := tsxml.LoadBundle(paths[0], filePattern, locales)
		a.dispatchUI(func() {
			if err != nil {
				a.setStatus("加载 ZIP 失败: "+err.Error(), true)
				a.appendLog("error", "", "", "加载 ZIP 失败: "+err.Error())
				return
			}
			a.applyLoadedBundle(bundle)
		})
	}()
}

func (a *App) importExcelAsync() {
	win := a.win
	defaultPath := a.cfg.LastXLSXPath
	locales := slices.Clone(a.cfg.Locales)

	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "导入 Excel 翻译词表 (.xlsx)",
			DefaultPath: defaultPath,
			Filters:     []mygo.FileFilter{{Name: "Excel 工作簿", Extensions: []string{"xlsx"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		wb, err := sheet.LoadExcel(paths[0], "", locales, true)
		a.dispatchUI(func() {
			if err != nil {
				a.setStatus("导入 Excel 失败: "+err.Error(), true)
				a.appendLog("error", "", "", "导入 Excel 失败: "+err.Error())
				return
			}
			a.applyLoadedWorkbook(wb, true)
		})
	}()
}

func (a *App) exportExcelAsync(scope sheet.ExportScope) {
	if a.plan == nil {
		a.setStatus("没有可导出的词条数据", true)
		return
	}
	opts := planner.BuildExportRows(a.plan, scope, a.cfg.Locales)
	win := a.win
	defaultName := "translations_export.xlsx"
	if a.bundle != nil {
		base := filepath.Base(a.bundle.SourcePath)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		if base != "" && base != "." {
			defaultName = base + "_export.xlsx"
		}
	}

	go func() {
		p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       "导出 Excel 词条表",
			DefaultPath: defaultName,
			Filters:     []mygo.FileFilter{{Name: "Excel 工作簿", Extensions: []string{"xlsx"}}},
		})
		if err != nil || p == "" {
			return
		}
		err = sheet.ExportExcel(p, opts)
		a.dispatchUI(func() {
			if err != nil {
				a.setStatus("导出 Excel 失败: "+err.Error(), true)
				a.appendLog("error", "", "", "导出 Excel 失败: "+err.Error())
				return
			}
			msg := fmt.Sprintf("已导出 %d 行到 %s", len(opts.Rows), filepath.Base(p))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
		})
	}()
}

func (a *App) handleDroppedPaths(paths []string) {
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".xlsx":
			_ = a.ImportFromExcelFile(p, "", true)
		case ".zip":
			_ = a.LoadTSTarget(p)
		case ".ts":
			_ = a.LoadTSTarget(filepath.Dir(p))
		default:
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				_ = a.LoadTSTarget(p)
			}
		}
	}
}

func (a *App) testProviderAsync(provID string) {
	p, ok := provider.Get(provID)
	if !ok {
		return
	}
	cfg := a.cfg.Clone()
	a.netPool.Update(cfg.Proxy)
	values := cfg.ProviderConfigs[provID]

	a.providerTesting[provID] = true
	a.providerTestMsg[provID] = "正在测试连通性..."
	a.providerTestErr[provID] = false

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		err := p.Configure(values, a.netPool)
		if err == nil {
			err = p.Check(ctx)
		}
		a.dispatchUI(func() {
			a.providerTesting[provID] = false
			if err != nil {
				a.providerTestMsg[provID] = "失败: " + err.Error()
				a.providerTestErr[provID] = true
				a.appendLog("error", provID, "", "连通测试失败: "+err.Error())
			} else {
				a.providerTestMsg[provID] = "连通正常"
				a.providerTestErr[provID] = false
				a.appendLog("info", provID, "", "连通测试通过")
			}
		})
	}()
}

func (a *App) discoverModelsAsync(provID string) {
	p, ok := provider.Get(provID)
	if !ok {
		return
	}
	disc, ok := p.(provider.ModelDiscoverer)
	if !ok {
		return
	}
	cfg := a.cfg.Clone()
	a.netPool.Update(cfg.Proxy)
	values := cfg.ProviderConfigs[provID]

	a.discoveringModels[provID] = true

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		var models []string
		err := p.Configure(values, a.netPool)
		if err == nil {
			models, err = disc.DiscoverModels(ctx)
		}
		a.dispatchUI(func() {
			a.discoveringModels[provID] = false
			if err != nil {
				a.providerTestMsg[provID] = "发现模型失败: " + err.Error()
				a.providerTestErr[provID] = true
				a.appendLog("error", provID, "", "发现模型失败: "+err.Error())
				return
			}
			a.discoveredModels[provID] = models
			a.providerTestMsg[provID] = fmt.Sprintf("已发现 %d 个模型", len(models))
			a.providerTestErr[provID] = false
			a.appendLog("info", provID, "", fmt.Sprintf("发现 %d 个可用模型", len(models)))
		})
	}()
}

func (a *App) startTranslation() {
	if a.translating || a.plan == nil {
		return
	}
	cfg := a.cfg.Clone()
	a.netPool.Update(cfg.Proxy)

	// Build ordered active provider IDs.
	ordered := provider.Ordered(cfg.ProviderOrder)
	var activeIDs []string
	for _, p := range ordered {
		if slices.Contains(cfg.ActiveProviders, p.ID()) {
			if err := p.Configure(cfg.ProviderConfigs[p.ID()], a.netPool); err != nil {
				a.appendLog("warn", p.ID(), "", "配置跳过: "+err.Error())
				continue
			}
			activeIDs = append(activeIDs, p.ID())
		}
	}
	if len(activeIDs) == 0 {
		a.setStatus("请先在「管线」选项卡中启用至少一个翻译提供者", true)
		return
	}

	targetLocales := a.enabledNonRefLocales()
	if len(targetLocales) == 0 {
		a.setStatus("请先在「语种」选项卡中启用至少一个目标语种", true)
		return
	}

	tasks := make(map[string][]provider.SourceItem)
	totalItems := 0
	refID := a.plan.RefLocaleID

	for _, loc := range targetLocales {
		var items []provider.SourceItem
		for i := range a.plan.Rows {
			row := &a.plan.Rows[i]
			if strings.TrimSpace(row.RefText) == "" {
				continue
			}
			cell, ok := row.Cells[loc.ID]
			if ok && cell.Effective != "" {
				continue
			}
			if ok && cfg.WriteMode == domain.ModeFillUnfinished && cell.Status == domain.StatusFinished {
				continue
			}
			items = append(items, provider.SourceItem{
				ID:        row.Key.Canonical(),
				Key:       row.Key,
				RefLocale: refID,
				RefText:   row.RefText,
				Numerus:   row.Numerus,
			})
		}
		if len(items) > 0 {
			tasks[loc.ID] = items
			totalItems += len(items)
		}
	}

	if totalItems == 0 {
		a.setStatus("当前没有需要翻译的空缺词条", false)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.translating = true
	a.cancelTranslate = cancel
	a.translateDoneCount = 0
	a.translateTotalCount = totalItems
	a.translateStatuses = make(map[string]provider.LocaleStatus)
	a.appendLog("info", "", "", fmt.Sprintf("开始并发翻译：%d 个语种，共 %d 条待译词条（并发度 %d）", len(tasks), totalItems, cfg.Concurrency))

	go func() {
		opts := provider.DispatchOptions{
			Registry:    provider.DefaultRegistry(),
			Cache:       a.cache,
			ProviderIDs: activeIDs,
			Locales:     targetLocales,
			Tasks:       tasks,
			Concurrency: cfg.Concurrency,
			BypassCache: cfg.BypassCache,
			OnUpdate: func(statuses map[string]provider.LocaleStatus, newCells []provider.TranslatedCell, newEvents []provider.LogEvent) {
				a.dispatchUI(func() {
					a.translateStatuses = statuses
					done := 0
					for _, st := range statuses {
						done += st.Completed
					}
					a.translateDoneCount = done

					for _, cell := range newCells {
						a.catalog.Set(cell.LocaleID, cell.Key, cell.Text, cell.ProviderID)
					}
					if len(newCells) > 0 {
						a.rebuildPlan()
					}
					for _, ev := range newEvents {
						a.logs = append(a.logs, ev)
					}
					a.trimLogs()
				})
			},
		}
		res := provider.RunConcurrent(ctx, opts)
		a.dispatchUI(func() {
			a.translating = false
			a.cancelTranslate = nil
			a.translateStatuses = res.Statuses
			msg := fmt.Sprintf("并发翻译完成：新增 %d 条译文，耗时 %s", len(res.Cells), res.Duration.Round(time.Millisecond))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
			a.rebuildPlan()
		})
	}()
}

func (a *App) stopTranslation() {
	if a.cancelTranslate != nil {
		a.cancelTranslate()
		a.appendLog("warn", "", "", "正在取消并发翻译任务...")
	}
}

func (a *App) writeTSAsync() {
	if a.bundle == nil || a.plan == nil {
		a.setStatus("请先加载 Qt .ts 目录或 .zip 压缩包", true)
		return
	}
	bundle := a.bundle
	plan := a.plan
	opts := tsxml.SaveOptions{
		CreateBackup: a.cfg.CreateBackup,
	}

	go func() {
		patched := make(map[string][]byte)
		totalApplied := 0
		totalAppended := 0

		for _, tf := range bundle.Files {
			if tf.LocaleID == "" {
				continue
			}
			patches := planner.BuildPatchesForFile(plan, tf, tf.LocaleID)
			if len(patches) == 0 {
				continue
			}
			res, err := tsxml.ApplyPatches(tf, patches)
			if err != nil {
				a.dispatchUI(func() {
					a.setStatus("生成补丁失败 ("+tf.RelPath+"): "+err.Error(), true)
					a.appendLog("error", "", tf.LocaleID, "生成补丁失败: "+err.Error())
				})
				return
			}
			if res.Applied > 0 || res.Appended > 0 {
				patched[tf.RelPath] = res.OutputRaw
				totalApplied += res.Applied
				totalAppended += res.Appended
			}
		}

		if len(patched) == 0 {
			a.dispatchUI(func() {
				a.setStatus("没有需要写回 .ts 文件的变更", false)
			})
			return
		}

		report, err := tsxml.SaveBundle(bundle, patched, opts)
		if err != nil {
			a.dispatchUI(func() {
				a.setStatus("写回 .ts 失败: "+err.Error(), true)
				a.appendLog("error", "", "", "写回 .ts 失败: "+err.Error())
			})
			return
		}

		// Reload the bundle cleanly from disk/output zip so offsets reflect the newly written XML.
		reloaded, reloadErr := tsxml.LoadBundle(report.OutputPath, a.cfg.FilePattern, a.cfg.Locales)

		a.dispatchUI(func() {
			msg := fmt.Sprintf("写回完成：%d 个文件（更新 %d 条，追加 %d 条）-> %s",
				report.FilesWritten, totalApplied, totalAppended, filepath.Base(report.OutputPath))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
			if reloadErr == nil && reloaded != nil && !bundle.IsZip {
				a.bundle = reloaded
				a.rebuildPlan()
			}
		})
	}()
}

func (a *App) clearMachineTranslations() {
	n := a.catalog.ClearByPredicate(func(_ string, _ domain.EntryKey, entry domain.TranslationEntry) bool {
		return domain.IsMachineOrigin(entry.Origin)
	})
	a.rebuildPlan()
	a.setStatus(fmt.Sprintf("已清空 %d 条机翻暂存译文", n), false)
}

func (a *App) clearAllTranslations() {
	a.catalog.ClearAll()
	a.rebuildPlan()
	a.setStatus("已清空全部工作区暂存译文", false)
}

func (a *App) scaffoldMissingTSFiles() {
	if a.bundle == nil {
		a.setStatus("请先加载 .ts 目录或 .zip 再补全骨架文件", true)
		return
	}
	existing := make(map[string]bool)
	var template *tsxml.TSFile
	for _, tf := range a.bundle.Files {
		if tf.LocaleID != "" {
			existing[tf.LocaleID] = true
		}
		if template == nil {
			template = tf
		}
	}

	created := 0
	for _, loc := range a.cfg.Locales {
		if !loc.Enabled || existing[loc.ID] {
			continue
		}
		relPath := loc.FileName(a.cfg.FilePattern)
		var newTF *tsxml.TSFile
		var err error
		if template != nil {
			newTF, err = tsxml.CloneSkeleton(template, loc, relPath)
		} else {
			newTF = tsxml.NewEmptyTSFile(loc, relPath, "\n")
		}
		if err != nil {
			a.appendLog("error", "", loc.ID, "生成骨架失败: "+err.Error())
			continue
		}
		if !a.bundle.IsZip {
			newTF.FilePath = filepath.Join(a.bundle.SourcePath, filepath.FromSlash(relPath))
		}
		a.bundle.Files = append(a.bundle.Files, newTF)
		created++
	}

	if created == 0 {
		a.setStatus("所有已启用语种均已有对应的 .ts 文件", false)
		return
	}
	a.rebuildPlan()
	msg := fmt.Sprintf("已为 %d 个缺失语种生成空白 .ts 骨架（写回 .ts 时将一并保存）", created)
	a.setStatus(msg, false)
	a.appendLog("info", "", "", msg)
}

func (a *App) applyLocaleProfile(p locale.Profile) {
	set := make(map[string]bool, len(p.LocaleIDs))
	for _, id := range p.LocaleIDs {
		set[id] = true
	}
	for i := range a.cfg.Locales {
		if a.cfg.Locales[i].IsRef {
			a.cfg.Locales[i].Enabled = true
			continue
		}
		a.cfg.Locales[i].Enabled = set[a.cfg.Locales[i].ID]
	}
	a.rebuildPlan()
	a.scheduleConfigSave()
}

func (a *App) selectAllLocales(on bool) {
	for i := range a.cfg.Locales {
		if a.cfg.Locales[i].IsRef {
			a.cfg.Locales[i].Enabled = true
			continue
		}
		a.cfg.Locales[i].Enabled = on
	}
	a.rebuildPlan()
	a.scheduleConfigSave()
}

func (a *App) setRefLocale(id string) {
	for i := range a.cfg.Locales {
		a.cfg.Locales[i].IsRef = a.cfg.Locales[i].ID == id
		if a.cfg.Locales[i].IsRef {
			a.cfg.Locales[i].Enabled = true
		}
	}
	a.rebuildPlan()
	a.scheduleConfigSave()
}

func (a *App) addCustomLocale() {
	id := strings.TrimSpace(a.newLocID)
	name := strings.TrimSpace(a.newLocName)
	code := strings.TrimSpace(a.newLocCode)
	if id == "" {
		a.setStatus("请输入语种 ID（例如 nl_NL）", true)
		return
	}
	for _, loc := range a.cfg.Locales {
		if loc.ID == id {
			a.setStatus("语种 ID 已存在: "+id, true)
			return
		}
	}
	if name == "" {
		name = id
	}
	if code == "" {
		code = id
	}
	a.cfg.Locales = append(a.cfg.Locales, locale.LocaleSpec{
		ID:         id,
		Name:       name,
		Enabled:    true,
		EngineCode: code,
	})
	a.newLocID = ""
	a.newLocName = ""
	a.newLocCode = ""
	a.rebuildPlan()
	a.scheduleConfigSave()
}

func (a *App) removeLocale(id string) {
	a.cfg.Locales = slices.DeleteFunc(a.cfg.Locales, func(l locale.LocaleSpec) bool {
		return l.ID == id && !l.IsRef
	})
	a.rebuildPlan()
	a.scheduleConfigSave()
}

func (a *App) appendLog(level, provID, locID, msg string) {
	a.logs = append(a.logs, provider.LogEvent{
		Time:       time.Now(),
		Level:      level,
		ProviderID: provID,
		LocaleID:   locID,
		Message:    msg,
	})
	a.trimLogs()
}

func (a *App) trimLogs() {
	const maxLogs = 500
	if len(a.logs) > maxLogs {
		a.logs = a.logs[len(a.logs)-maxLogs:]
	}
}

func (a *App) copyLogsToClipboard() {
	var sb strings.Builder
	for _, ev := range a.logs {
		sb.WriteString(ev.Time.Format("15:04:05"))
		sb.WriteString(" [")
		sb.WriteString(strings.ToUpper(ev.Level))
		sb.WriteString("]")
		if ev.ProviderID != "" {
			sb.WriteString(" [" + ev.ProviderID + "]")
		}
		if ev.LocaleID != "" {
			sb.WriteString(" [" + ev.LocaleID + "]")
		}
		sb.WriteString(" ")
		sb.WriteString(ev.Message)
		sb.WriteByte('\n')
	}
	_ = mygo.Clipboard.Write(transfer.TextData(sb.String()))
	a.setStatus("已复制运行日志到剪贴板", false)
}
