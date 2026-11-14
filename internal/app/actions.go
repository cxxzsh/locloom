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
	"locloom/internal/format"
	"locloom/internal/locale"
	"locloom/internal/planner"
	"locloom/internal/provider"
	"locloom/internal/sheet"
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
		FormatBundle:  a.bundle,
		ExtraEntries:  a.extraEntries,
		DefaultCtx:    a.defaultCtx,
		Catalog:       a.catalog,
		Locales:       a.cfg.Locales,
		WriteMode:     a.cfg.WriteMode,
		AppendMissing: a.cfg.AppendMissing,
		UILang:        a.UILang(),
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
	if a.plan == nil {
		a.filteredRows = nil
		a.selectedRow = -1
		a.syncEditorFromSelection()
		return
	}

	filtered := make([]int, 0, len(a.plan.Rows))
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
		filtered = append(filtered, i)
	}
	a.filteredRows = filtered

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
	if strings.Contains(strings.ToLower(row.Key.ID), q) ||
		strings.Contains(strings.ToLower(row.Key.Context), q) ||
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
		a.pluralEditBufs = nil
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
	cell := row.Cells[a.activeEditLocale]
	if row.Numerus && !a.cfg.EnablePluralForms && strings.Contains(cell.Effective, domain.PluralSeparator) {
		a.editBuf = domain.SplitNumerus(cell.Effective)[0]
	} else {
		a.editBuf = cell.Effective
	}
	a.syncPluralBufs(row, cell)
}

func (a *App) syncPluralBufs(row *planner.MatrixRow, cell planner.CellPlan) {
	if row == nil || !row.Numerus {
		a.pluralEditBufs = nil
		return
	}
	labels := locale.PluralFormLabels(a.activeEditLocale, a.UILang(), cell.PluralCount)
	parts := domain.SplitNumerus(cell.Effective)
	n := max(len(labels), len(parts))
	bufs := make([]string, n)
	copy(bufs, parts)
	a.pluralEditBufs = bufs
}

func (a *App) onEditPluralForm(idx int, val string) {
	if idx < 0 || idx >= len(a.pluralEditBufs) {
		return
	}
	a.pluralEditBufs[idx] = val
	joined := domain.JoinNumerus(a.pluralEditBufs)
	a.editBuf = joined
	a.onEditCell(joined)
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
	cell := row.Cells[a.activeEditLocale]
	a.editBuf = cell.Effective
	a.syncPluralBufs(row, cell)
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

func (a *App) openDirAsync() {
	win := a.win
	defaultPath := a.cfg.LastTSPath
	filePattern := a.cfg.FilePattern
	locales := slices.Clone(a.cfg.Locales)
	tr := a.tr()

	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       tr.DialogOpenTSDir,
			DefaultPath: defaultPath,
			Directory:   true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		bundle, err := format.Load(paths[0], filePattern, locales)
		a.dispatchUI(func() {
			if err != nil {
				msg := fmt.Sprintf(a.tr().ErrLoadTSDirFmt, err)
				a.setStatus(msg, true)
				a.appendLog("error", "", "", msg)
				return
			}
			a.applyLoadedBundle(bundle)
		})
	}()
}

func (a *App) openFileAsync() {
	win := a.win
	defaultPath := a.cfg.LastTSPath
	filePattern := a.cfg.FilePattern
	locales := slices.Clone(a.cfg.Locales)
	tr := a.tr()

	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       tr.DialogOpenFile,
			DefaultPath: defaultPath,
			Multiple:    true,
			Filters:     []mygo.FileFilter{{Name: tr.FilterLocFilesName, Extensions: format.SupportedExtensions()}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		var bundle *format.Bundle
		if len(paths) == 1 {
			bundle, err = format.Load(paths[0], filePattern, locales)
		} else {
			bundle, err = format.LoadFiles(paths, filePattern, locales)
		}
		a.dispatchUI(func() {
			if err != nil {
				msg := fmt.Sprintf(a.tr().ErrLoadFilesFmt, err)
				a.setStatus(msg, true)
				a.appendLog("error", "", "", msg)
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
	tr := a.tr()

	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       tr.DialogImportExcel,
			DefaultPath: defaultPath,
			Filters:     []mygo.FileFilter{{Name: tr.FilterExcelName, Extensions: []string{"xlsx"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		wb, err := sheet.LoadExcel(paths[0], "", locales, true)
		a.dispatchUI(func() {
			if err != nil {
				msg := fmt.Sprintf(a.tr().ErrImportExcelFmt, err)
				a.setStatus(msg, true)
				a.appendLog("error", "", "", msg)
				return
			}
			a.applyLoadedWorkbook(wb, true)
		})
	}()
}

func (a *App) exportExcelAsync(scope sheet.ExportScope) {
	if a.plan == nil {
		a.setStatus(a.tr().ErrNoExportData, true)
		return
	}
	opts := planner.BuildExportRows(a.plan, scope, a.cfg.Locales)
	win := a.win
	tr := a.tr()
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
			Title:       tr.DialogExportExcel,
			DefaultPath: defaultName,
			Filters:     []mygo.FileFilter{{Name: tr.FilterExcelName, Extensions: []string{"xlsx"}}},
		})
		if err != nil || p == "" {
			return
		}
		err = sheet.ExportExcel(p, opts)
		a.dispatchUI(func() {
			if err != nil {
				msg := fmt.Sprintf(a.tr().ErrExportExcelFmt, err)
				a.setStatus(msg, true)
				a.appendLog("error", "", "", msg)
				return
			}
			msg := fmt.Sprintf(a.tr().StatusExportedExcelFmt, len(opts.Rows), filepath.Base(p))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
		})
	}()
}

func (a *App) handleDroppedPaths(paths []string) {
	var excelPaths []string
	var locPaths []string
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".xlsx" || ext == ".xls" {
			excelPaths = append(excelPaths, p)
		} else {
			locPaths = append(locPaths, p)
		}
	}

	for _, ep := range excelPaths {
		_ = a.ImportFromExcelFile(ep, "", true)
	}

	if len(locPaths) == 1 {
		_ = a.LoadProject(locPaths[0])
	} else if len(locPaths) > 1 {
		bundle, err := format.LoadFiles(locPaths, a.cfg.FilePattern, a.cfg.Locales)
		if err != nil {
			msg := fmt.Sprintf(a.tr().ErrLoadFilesFmt, err)
			a.setStatus(msg, true)
			a.appendLog("error", "", "", msg)
			return
		}
		a.applyLoadedBundle(bundle)
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
	a.providerTestMsg[provID] = a.tr().StatusTestingConn
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
				a.providerTestMsg[provID] = fmt.Sprintf(a.tr().StatusConnFailedFmt, err)
				a.providerTestErr[provID] = true
				a.appendLog("error", provID, "", fmt.Sprintf(a.tr().LogConnFailedFmt, err))
			} else {
				a.providerTestMsg[provID] = a.tr().StatusConnOK
				a.providerTestErr[provID] = false
				a.appendLog("info", provID, "", a.tr().LogConnOK)
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
				a.providerTestMsg[provID] = fmt.Sprintf(a.tr().StatusDiscoverFailedFmt, err)
				a.providerTestErr[provID] = true
				a.appendLog("error", provID, "", fmt.Sprintf(a.tr().LogDiscoverFailedFmt, err))
				return
			}
			a.discoveredModels[provID] = models
			a.providerTestMsg[provID] = fmt.Sprintf(a.tr().StatusDiscoveredModelsFmt, len(models))
			a.providerTestErr[provID] = false
			a.appendLog("info", provID, "", fmt.Sprintf(a.tr().LogDiscoveredModelsFmt, len(models)))
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
				a.appendLog("warn", p.ID(), "", fmt.Sprintf(a.tr().LogConfigSkippedFmt, err))
				continue
			}
			activeIDs = append(activeIDs, p.ID())
		}
	}
	if len(activeIDs) == 0 {
		a.setStatus(a.tr().ErrNoActiveProvider, true)
		return
	}

	targetLocales := a.enabledNonRefLocales()
	if len(targetLocales) == 0 {
		a.setStatus(a.tr().ErrNoTargetLocale, true)
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
		a.setStatus(a.tr().StatusNoGapsToTranslate, false)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.translating = true
	a.cancelTranslate = cancel
	a.translateDoneCount = 0
	a.translateTotalCount = totalItems
	a.translateStatuses = make(map[string]provider.LocaleStatus)
	a.appendLog("info", "", "", fmt.Sprintf(a.tr().LogStartTranslateFmt, len(tasks), totalItems, cfg.Concurrency))

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
			msg := fmt.Sprintf(a.tr().StatusTranslateDoneFmt, len(res.Cells), res.Duration.Round(time.Millisecond))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
			a.rebuildPlan()
		})
	}()
}

func (a *App) stopTranslation() {
	if a.cancelTranslate != nil {
		a.cancelTranslate()
		a.appendLog("warn", "", "", a.tr().LogStoppingTranslate)
	}
}

func (a *App) writeBackAsync() {
	if a.bundle == nil || a.plan == nil {
		a.setStatus(a.tr().ErrLoadTSFirst, true)
		return
	}
	bundle := a.bundle
	plan := a.plan
	locales := slices.Clone(a.cfg.Locales)
	filePattern := a.cfg.FilePattern
	opts := format.SaveOptions{
		CreateBackup: a.cfg.CreateBackup,
	}

	go func() {
		patchesByLocale := planner.BuildFormatPatches(plan)
		totalApplied := 0
		totalAppended := 0
		for _, ops := range patchesByLocale {
			for _, op := range ops {
				if op.Action == domain.ActionAppendNew {
					totalAppended++
				} else if op.Action.Mutates() {
					totalApplied++
				}
			}
		}
		if totalApplied == 0 && totalAppended == 0 {
			a.dispatchUI(func() {
				a.setStatus(a.tr().StatusNoTSChanges, false)
			})
			return
		}

		report, err := format.Save(bundle, patchesByLocale, locales, filePattern, opts)
		if err != nil {
			a.dispatchUI(func() {
				msg := fmt.Sprintf(a.tr().ErrWriteTSFmt, err)
				a.setStatus(msg, true)
				a.appendLog("error", "", "", msg)
			})
			return
		}

		var reloaded *format.Bundle
		var reloadErr error
		if !bundle.IsZip && report.OutputPath != "" {
			if info, statErr := os.Stat(report.OutputPath); statErr == nil && (info.IsDir() || bundle.IsSingleFile) {
				reloaded, reloadErr = format.Load(report.OutputPath, filePattern, locales)
			}
		}

		a.dispatchUI(func() {
			msg := fmt.Sprintf(a.tr().StatusWriteTSDoneFmt,
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
	a.setStatus(fmt.Sprintf(a.tr().StatusClearedMachineFmt, n), false)
}

func (a *App) clearAllTranslations() {
	a.catalog.ClearAll()
	a.rebuildPlan()
	a.setStatus(a.tr().StatusClearedAll, false)
}

func (a *App) scaffoldMissingLocaleFiles() {
	if a.bundle == nil {
		a.setStatus(a.tr().ErrScaffoldLoadFirst, true)
		return
	}
	ad, ok := format.GetAdapter(a.bundle.Format)
	if !ok {
		return
	}
	existing := make(map[string]bool)
	var template *format.DocRecord
	for _, doc := range a.bundle.Docs {
		if doc.LocaleID != "" {
			existing[doc.LocaleID] = true
		}
		for _, l := range doc.Locales {
			existing[l] = true
		}
		if template == nil {
			template = doc
		}
	}

	created := 0
	for _, loc := range a.cfg.Locales {
		if !loc.Enabled || existing[loc.ID] {
			continue
		}
		relPath := loc.FileName(a.cfg.FilePattern)
		newDoc, err := ad.NewLocaleDoc(template, loc, relPath)
		if err != nil {
			a.appendLog("error", "", loc.ID, fmt.Sprintf(a.tr().LogScaffoldErrFmt, err))
			continue
		}
		if !a.bundle.IsZip && !a.bundle.IsSingleFile {
			newDoc.FilePath = filepath.Join(a.bundle.SourcePath, filepath.FromSlash(relPath))
		}
		a.bundle.Docs = append(a.bundle.Docs, newDoc)
		if !slices.Contains(a.bundle.Locales, loc.ID) {
			a.bundle.Locales = append(a.bundle.Locales, loc.ID)
		}
		created++
	}

	if created == 0 {
		a.setStatus(a.tr().StatusAllLocalesHaveTS, false)
		return
	}
	a.rebuildPlan()
	msg := fmt.Sprintf(a.tr().StatusScaffoldDoneFmt, created)
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

func (a *App) syncBundleLocales() {
	if a.bundle == nil || len(a.bundle.Locales) == 0 {
		return
	}
	inBundle := make(map[string]bool, len(a.bundle.Locales))
	for _, id := range a.bundle.Locales {
		inBundle[id] = true
	}
	for i := range a.cfg.Locales {
		if a.cfg.Locales[i].IsRef {
			a.cfg.Locales[i].Enabled = true
			continue
		}
		a.cfg.Locales[i].Enabled = inBundle[a.cfg.Locales[i].ID]
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
		a.setStatus(a.tr().ErrEnterLocaleID, true)
		return
	}
	for _, loc := range a.cfg.Locales {
		if loc.ID == id {
			a.setStatus(fmt.Sprintf(a.tr().ErrLocaleExistsFmt, id), true)
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
	a.setStatus(a.tr().StatusCopiedLogs, false)
}

func (a *App) jumpGap(delta int) {
	if a.plan == nil || len(a.filteredRows) == 0 {
		a.setStatus(a.tr().StatusNoGapsFound, false)
		return
	}
	n := len(a.filteredRows)
	start := a.selectedRow
	if start < 0 || start >= n {
		if delta > 0 {
			start = -1
		} else {
			start = n
		}
	}
	for step := 1; step <= n; step++ {
		idx := ((start + delta*step) % n + n) % n
		row := &a.plan.Rows[a.filteredRows[idx]]
		if row.HasGap || row.HasWarning {
			a.selectedRow = idx
			a.syncEditorFromSelection()
			return
		}
	}
	a.setStatus(a.tr().StatusNoGapsFound, false)
}

func (a *App) copySourceToClipboard() {
	_, row := a.currentMatrixRow()
	if row == nil {
		return
	}
	_ = mygo.Clipboard.Write(transfer.TextData(row.Key.Source))
	a.setStatus(a.tr().StatusCopiedSrc, false)
}

func (a *App) useSourceAsTranslation() {
	_, row := a.currentMatrixRow()
	if row == nil || a.activeEditLocale == "" {
		return
	}
	if row.Numerus && len(a.pluralEditBufs) > 0 {
		for i := range a.pluralEditBufs {
			a.pluralEditBufs[i] = row.Key.Source
		}
		a.editBuf = domain.JoinNumerus(a.pluralEditBufs)
	} else {
		a.editBuf = row.Key.Source
	}
	a.onEditCell(a.editBuf)
}

func (a *App) translateCurrentRowAsync() {
	rowIdx, _ := a.currentMatrixRow()
	a.translateRowAtAsync(rowIdx, "")
}

func (a *App) translateRowAtAsync(planRowIdx int, forceLocaleID string) {
	if a.translating || a.translatingRow || a.plan == nil {
		return
	}
	if planRowIdx < 0 || planRowIdx >= len(a.plan.Rows) {
		return
	}
	row := &a.plan.Rows[planRowIdx]
	if strings.TrimSpace(row.Key.Source) == "" && strings.TrimSpace(row.RefText) == "" {
		return
	}
	cfg := a.cfg.Clone()
	a.netPool.Update(cfg.Proxy)

	ordered := provider.Ordered(cfg.ProviderOrder)
	var activeIDs []string
	for _, p := range ordered {
		if slices.Contains(cfg.ActiveProviders, p.ID()) {
			if err := p.Configure(cfg.ProviderConfigs[p.ID()], a.netPool); err != nil {
				a.appendLog("warn", p.ID(), "", fmt.Sprintf(a.tr().LogConfigSkippedFmt, err))
				continue
			}
			activeIDs = append(activeIDs, p.ID())
		}
	}
	if len(activeIDs) == 0 {
		a.setStatus(a.tr().ErrNoActiveProvider, true)
		return
	}

	targetLocales := a.enabledNonRefLocales()
	if forceLocaleID != "" {
		var single []locale.LocaleSpec
		for _, l := range a.cfg.Locales {
			if l.ID == forceLocaleID {
				single = append(single, l)
				break
			}
		}
		targetLocales = single
	}
	if len(targetLocales) == 0 {
		a.setStatus(a.tr().ErrNoTargetLocale, true)
		return
	}

	tasks := make(map[string][]provider.SourceItem)
	refID := a.plan.RefLocaleID
	for _, loc := range targetLocales {
		if forceLocaleID == "" {
			cell, ok := row.Cells[loc.ID]
			if ok && cell.Effective != "" && cfg.WriteMode == domain.ModeFillUnfinished {
				continue
			}
			if ok && cfg.WriteMode == domain.ModeFillUnfinished && cell.Status == domain.StatusFinished {
				continue
			}
		}
		tasks[loc.ID] = []provider.SourceItem{{
			ID:        row.Key.Canonical(),
			Key:       row.Key,
			RefLocale: refID,
			RefText:   row.RefText,
			Numerus:   row.Numerus,
		}}
	}
	if len(tasks) == 0 {
		a.setStatus(a.tr().StatusRowNoGaps, false)
		return
	}

	a.translatingRow = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		opts := provider.DispatchOptions{
			Registry:    provider.DefaultRegistry(),
			Cache:       a.cache,
			ProviderIDs: activeIDs,
			Locales:     targetLocales,
			Tasks:       tasks,
			Concurrency: cfg.Concurrency,
			BypassCache: cfg.BypassCache || forceLocaleID != "",
		}
		res := provider.RunConcurrent(ctx, opts)
		a.dispatchUI(func() {
			a.translatingRow = false
			for _, cell := range res.Cells {
				origin := cell.ProviderID
				if forceLocaleID != "" {
					origin = domain.OriginManual
				}
				a.catalog.Set(cell.LocaleID, cell.Key, cell.Text, origin)
			}
			for _, ev := range res.Events {
				a.logs = append(a.logs, ev)
			}
			a.trimLogs()
			if len(res.Cells) > 0 {
				a.editBoundLocale = ""
				a.rebuildPlan()
				a.syncEditorFromSelection()
			}
			a.setStatus(fmt.Sprintf(a.tr().StatusTranslateRowDoneFmt, len(res.Cells)), false)
		})
	}()
}

func (a *App) clearCellAt(planRowIdx int, locID string) {
	if a.plan == nil || planRowIdx < 0 || planRowIdx >= len(a.plan.Rows) || locID == "" {
		return
	}
	row := &a.plan.Rows[planRowIdx]
	a.catalog.SetCleared(locID, row.Key, domain.OriginManual)
	planner.UpdateCell(a.plan, planRowIdx, locID, a.catalog)
	a.editBoundLocale = ""
	a.syncEditorFromSelection()
	a.setStatus(fmt.Sprintf(a.tr().StatusClearedCellFmt, locID), false)
}

func (a *App) clearRowAt(planRowIdx int) {
	if a.plan == nil || planRowIdx < 0 || planRowIdx >= len(a.plan.Rows) {
		return
	}
	row := &a.plan.Rows[planRowIdx]
	for _, loc := range a.enabledNonRefLocales() {
		a.catalog.SetCleared(loc.ID, row.Key, domain.OriginManual)
		planner.UpdateCell(a.plan, planRowIdx, loc.ID, a.catalog)
	}
	a.editBoundLocale = ""
	a.syncEditorFromSelection()
	a.setStatus(a.tr().StatusClearedRow, false)
}

func (a *App) revertCellAt(planRowIdx int, locID string) {
	if a.plan == nil || planRowIdx < 0 || planRowIdx >= len(a.plan.Rows) || locID == "" {
		return
	}
	row := &a.plan.Rows[planRowIdx]
	a.catalog.DeleteKeyVariants(locID, row.Key)
	planner.UpdateCell(a.plan, planRowIdx, locID, a.catalog)
	a.editBoundLocale = ""
	a.syncEditorFromSelection()
	a.setStatus(fmt.Sprintf(a.tr().StatusRevertedCellFmt, locID), false)
}

func (a *App) revertRowAt(planRowIdx int) {
	if a.plan == nil || planRowIdx < 0 || planRowIdx >= len(a.plan.Rows) {
		return
	}
	row := &a.plan.Rows[planRowIdx]
	for _, loc := range a.enabledNonRefLocales() {
		a.catalog.DeleteKeyVariants(loc.ID, row.Key)
		planner.UpdateCell(a.plan, planRowIdx, loc.ID, a.catalog)
	}
	a.editBoundLocale = ""
	a.syncEditorFromSelection()
	a.setStatus(a.tr().StatusRevertedRow, false)
}

func (a *App) copyText(s string) {
	_ = mygo.Clipboard.Write(transfer.TextData(s))
	a.setStatus(a.tr().StatusCopiedText, false)
}
