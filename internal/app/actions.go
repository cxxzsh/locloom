package app

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"locloom/internal/config"
	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/locale"
	"locloom/internal/planner"
	"locloom/internal/platform"
	"locloom/internal/provider"
	"locloom/internal/sheet"
	"locloom/internal/updater"
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
	a.syncWindowEdited()
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
	// The selected row shows as a full-row block, the RecordsTable way:
	// every cell at accent 14% (Cells.All), the editing locale's column
	// keeps the cursor for the border a focused table draws on it.
	if id := a.tableCursorColumnID(); a.tableState.Cells.Cursor != id || !a.tableState.Cells.All {
		a.tableState.Cells = ui.ColumnRange{Cursor: id, Anchor: id, All: true}
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
	a.syncWindowEdited()
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
	a.syncWindowEdited()
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

func (a *App) reorderProvider(srcID, targetID string, insertAfter bool) {
	if srcID == "" || targetID == "" || srcID == targetID {
		return
	}
	provs := provider.Ordered(a.cfg.ProviderOrder)
	prev := make([]string, len(provs))
	withoutSrc := make([]string, 0, len(provs))
	srcFound := false
	dstIdx := -1
	for i, p := range provs {
		pid := p.ID()
		prev[i] = pid
		if pid == srcID {
			srcFound = true
			continue
		}
		if pid == targetID {
			dstIdx = len(withoutSrc)
		}
		withoutSrc = append(withoutSrc, pid)
	}
	if !srcFound || dstIdx < 0 {
		return
	}
	insertAt := dstIdx
	if insertAfter {
		insertAt = dstIdx + 1
	}
	next := slices.Insert(withoutSrc, insertAt, srcID)
	if slices.Equal(next, prev) {
		return
	}
	a.cfg.ProviderOrder = next
	a.scheduleConfigSave()
}

func (a *App) moveProviderToEdge(id string, toTop bool) {
	provs := provider.Ordered(a.cfg.ProviderOrder)
	if len(provs) < 2 {
		return
	}
	if toTop {
		a.reorderProvider(id, provs[0].ID(), false)
		return
	}
	a.reorderProvider(id, provs[len(provs)-1].ID(), true)
}

func (a *App) isDefaultProviderOrder() bool {
	defOrder := config.Default().ProviderOrder
	provs := provider.Ordered(a.cfg.ProviderOrder)
	if len(provs) != len(defOrder) {
		return false
	}
	for i, p := range provs {
		if p.ID() != defOrder[i] {
			return false
		}
	}
	return true
}

func (a *App) resetProviderOrder() {
	if a.isDefaultProviderOrder() {
		return
	}
	a.cfg.ProviderOrder = slices.Clone(config.Default().ProviderOrder)
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

func (a *App) exportExcelAsync(scope sheet.ExportScope, relevantOnly bool) {
	if a.plan == nil {
		a.setStatus(a.tr().ErrNoExportData, true)
		return
	}
	opts := planner.BuildExportRows(a.plan, scope, a.cfg.Locales, relevantOnly)
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

	bundleSourceLocale := ""
	if a.bundle != nil {
		bundleSourceLocale = a.bundle.SourceLocale
	}

	// Dependent locales translate in waves: a locale pinned to another
	// target's output (e.g. zh_TW from zh_CN) runs after its source's wave
	// so it picks up the translations that wave just produced.
	waves := translationWaves(targetLocales)

	ctx, cancel := context.WithCancel(context.Background())
	a.translating = true
	a.cancelTranslate = cancel
	a.translateDoneCount = 0
	a.translateTotalCount = 0
	a.translateStatuses = make(map[string]provider.LocaleStatus)
	a.statusBanner = ""
	a.statusBannerErr = false

	go func() {
		totalTranslated := 0
		start := time.Now()
		for w, waveLocales := range waves {
			// Plan cells mutate on the UI thread as results land; build
			// each wave's task list there so it reads the latest texts.
			var tasks map[string][]provider.SourceItem
			var waveItems int
			a.runOnUI(func() {
				tasks, waveItems = buildTranslationTasks(a.plan, waveLocales, cfg.WriteMode, bundleSourceLocale)
				a.translateWave = w + 1
				a.translateWaveTotal = len(waves)
				a.translateTotalCount += waveItems
				if waveItems > 0 {
					a.appendLog("info", "", "", fmt.Sprintf(a.tr().LogStartTranslateFmt, len(tasks), waveItems, cfg.Concurrency))
				}
			})
			if waveItems == 0 {
				continue
			}
			res := provider.RunConcurrent(ctx, provider.DispatchOptions{
				Registry:    provider.DefaultRegistry(),
				Cache:       a.cache,
				ProviderIDs: activeIDs,
				Locales:     waveLocales,
				Tasks:       tasks,
				Concurrency: cfg.Concurrency,
				BypassCache: cfg.BypassCache,
				OnUpdate:    a.translationOnUpdate(),
			})
			totalTranslated += len(res.Cells)
			if ctx.Err() != nil {
				break
			}
		}
		dur := time.Since(start)
		a.dispatchUI(func() {
			a.translating = false
			a.cancelTranslate = nil
			a.translateWave = 0
			a.translateWaveTotal = 0
			msg := fmt.Sprintf(a.tr().StatusTranslateDoneFmt, totalTranslated, dur.Round(time.Millisecond))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
			a.rebuildPlan()
		})
	}()
}

// translationOnUpdate returns the shared OnUpdate callback wiring cell
// results and log events back onto the UI thread for one wave.
func (a *App) translationOnUpdate() func(map[string]provider.LocaleStatus, []provider.TranslatedCell, []provider.LogEvent) {
	return func(statuses map[string]provider.LocaleStatus, newCells []provider.TranslatedCell, newEvents []provider.LogEvent) {
		a.dispatchUI(func() {
			for id, st := range statuses {
				a.translateStatuses[id] = st
			}
			done := 0
			for _, st := range a.translateStatuses {
				done += st.Completed
			}
			a.translateDoneCount = done

			for _, cell := range newCells {
				a.catalog.Set(cell.LocaleID, cell.Key, cell.Text, cell.ProviderID)
			}
			if len(newCells) > 0 {
				updatedAny := false
				for _, cell := range newCells {
					if planner.UpdateCellByKey(a.plan, cell.Key, cell.LocaleID, a.catalog) {
						updatedAny = true
					}
					if cell.Key == a.editBoundRowKey && cell.LocaleID == a.activeEditLocale {
						a.editBoundLocale = ""
					}
				}
				if updatedAny {
					if a.statusFilter != 0 || strings.TrimSpace(a.searchQuery) != "" {
						a.refilterRows()
					} else {
						a.syncEditorFromSelection()
					}
					a.syncWindowEdited()
				}
			}
			for _, ev := range newEvents {
				a.logs = append(a.logs, ev)
			}
			a.trimLogs()
		})
	}
}

// translationWaves sorts target locales into dependency waves by
// SourceLocale: wave 0 translates from the bundle's reference, wave N
// from a locale of an earlier wave. A cycle falls back to config order
// so nothing is dropped.
func translationWaves(locs []locale.LocaleSpec) [][]locale.LocaleSpec {
	byID := make(map[string]locale.LocaleSpec, len(locs))
	for _, l := range locs {
		byID[l.ID] = l
	}
	depth := make(map[string]int, len(locs))
	var level func(l locale.LocaleSpec, seen map[string]bool) int
	level = func(l locale.LocaleSpec, seen map[string]bool) int {
		if d, ok := depth[l.ID]; ok {
			return d
		}
		d := 0
		if l.SourceLocale != "" {
			if src, ok := byID[l.SourceLocale]; ok && !seen[l.ID] {
				seen[l.ID] = true
				d = level(src, seen) + 1
			}
		}
		depth[l.ID] = d
		return d
	}
	maxD := 0
	for _, l := range locs {
		if d := level(l, map[string]bool{}); d > maxD {
			maxD = d
		}
	}
	waves := make([][]locale.LocaleSpec, maxD+1)
	for _, l := range locs {
		waves[depth[l.ID]] = append(waves[depth[l.ID]], l)
	}
	return waves
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

	a.bgWG.Add(1)
	go func() {
		defer a.bgWG.Done()
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
		if report.OutputPath != "" {
			if info, statErr := os.Stat(report.OutputPath); statErr == nil && (bundle.IsZip || info.IsDir() || bundle.IsSingleFile) {
				reloaded, reloadErr = format.Load(report.OutputPath, filePattern, locales)
				if reloadErr == nil && reloaded != nil && bundle.IsZip {
					reloaded.SourcePath = bundle.SourcePath
				}
			}
		}

		a.dispatchUI(func() {
			msg := fmt.Sprintf(a.tr().StatusWriteTSDoneFmt,
				report.FilesWritten, totalApplied, totalAppended, filepath.Base(report.OutputPath))
			a.setStatus(msg, false)
			a.appendLog("info", "", "", msg)
			if reloadErr == nil && reloaded != nil {
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
	if a.bundle.Topology == format.TopologyMultiLocale {
		a.setStatus(a.tr().StatusAllLocalesHaveTS, false)
		return
	}
	ad, ok := format.GetAdapter(a.bundle.Format)
	if !ok {
		return
	}
	existing := make(map[string]bool)
	markExisting := func(id string) {
		if id == "" {
			return
		}
		existing[id] = true
		existing[strings.ToLower(strings.ReplaceAll(id, "-", "_"))] = true
	}
	markExisting(a.bundle.SourceLocale)
	for _, doc := range a.bundle.Docs {
		markExisting(doc.LocaleID)
		for _, l := range doc.Locales {
			markExisting(l)
		}
	}
	template := format.PickTemplateForScaffold(a.bundle)

	baseDir := a.bundle.SourcePath
	if !a.bundle.IsZip && a.bundle.IsSingleFile {
		baseDir = filepath.Dir(a.bundle.SourcePath)
	}

	created := 0
	for _, loc := range a.cfg.Locales {
		normID := strings.ToLower(strings.ReplaceAll(loc.ID, "-", "_"))
		if !loc.Enabled || existing[loc.ID] || existing[normID] {
			continue
		}
		relPath := format.ScaffoldRelPath(a.bundle.Format, template, loc, a.cfg.FilePattern)
		newDoc, err := ad.NewLocaleDoc(template, loc, relPath)
		if err != nil {
			a.appendLog("error", "", loc.ID, fmt.Sprintf(a.tr().LogScaffoldErrFmt, err))
			continue
		}
		if !a.bundle.IsZip {
			fullPath, pathErr := safeScaffoldTargetPath(baseDir, relPath)
			if pathErr != nil {
				a.appendLog("error", "", loc.ID, fmt.Sprintf(a.tr().LogScaffoldErrFmt, pathErr))
				continue
			}
			outBytes := newDoc.Raw
			if newDoc.HasBOM && !bytes.HasPrefix(outBytes, []byte{0xEF, 0xBB, 0xBF}) {
				outBytes = append([]byte{0xEF, 0xBB, 0xBF}, outBytes...)
			}
			if writeErr := platform.AtomicWriteFile(fullPath, outBytes, 0o644); writeErr != nil {
				a.appendLog("error", "", loc.ID, fmt.Sprintf(a.tr().LogScaffoldErrFmt, writeErr))
				continue
			}
			newDoc.FilePath = fullPath
			if a.bundle.IsSingleFile {
				a.bundle.SourcePath = baseDir
				a.bundle.IsSingleFile = false
				a.cfg.LastTSPath = baseDir
				a.scheduleConfigSave()
			}
		}
		a.bundle.Docs = append(a.bundle.Docs, newDoc)
		if !slices.Contains(a.bundle.Locales, loc.ID) {
			a.bundle.Locales = append(a.bundle.Locales, loc.ID)
		}
		markExisting(loc.ID)
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

func safeScaffoldTargetPath(baseDir, relSlash string) (string, error) {
	s := strings.ReplaceAll(strings.TrimSpace(relSlash), "\\", "/")
	if strings.ContainsRune(s, 0) || strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("unsafe relative path %q", relSlash)
	}
	if len(s) >= 2 && s[1] == ':' && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) {
		return "", fmt.Errorf("unsafe drive-qualified path %q", relSlash)
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe path traversal in %q", relSlash)
		}
	}
	cleanBase := filepath.Clean(baseDir)
	full := filepath.Clean(filepath.Join(cleanBase, filepath.FromSlash(s)))
	relCheck, err := filepath.Rel(cleanBase, full)
	if err != nil || relCheck == "." || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("scaffold path %q escapes directory %q", relSlash, cleanBase)
	}
	return full, nil
}

func providerHasFieldKey(p provider.Provider, key string) bool {
	for _, f := range p.ConfigSchema() {
		if f.Key == key {
			return true
		}
	}
	return false
}

func isCloudProviderLoopback(p provider.Provider, values map[string]string) bool {
	if p.ID() == domain.OriginDeepL {
		mode := strings.ToLower(strings.TrimSpace(values["mode"]))
		if mode == "free" || mode == "pro" {
			return false
		}
	}
	effectiveURL := ""
	for _, f := range p.ConfigSchema() {
		if f.Key == "base_url" || f.Key == "endpoint_url" || f.Key == "host" {
			v := strings.TrimSpace(values[f.Key])
			if v == "" {
				v = strings.TrimSpace(f.Default)
			}
			if v != "" {
				effectiveURL = v
				break
			}
		}
	}
	if effectiveURL == "" {
		return false
	}
	if !strings.Contains(effectiveURL, "://") {
		effectiveURL = "http://" + effectiveURL
	}
	u, err := url.Parse(effectiveURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" || strings.HasSuffix(host, ".local")
}

func (a *App) cloudProxyEligibleProviders() []provider.Provider {
	var out []provider.Provider
	for _, p := range provider.Ordered(a.cfg.ProviderOrder) {
		if p.Kind() != provider.KindEngine || !providerHasFieldKey(p, "use_proxy") {
			continue
		}
		if isCloudProviderLoopback(p, a.cfg.ProviderConfigs[p.ID()]) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (a *App) anyCloudProviderUsesProxy() bool {
	for _, p := range a.cloudProxyEligibleProviders() {
		if a.cfg.ProviderConfigs[p.ID()]["use_proxy"] == "true" {
			return true
		}
	}
	return false
}

func (a *App) allCloudProvidersUseProxy() bool {
	provs := a.cloudProxyEligibleProviders()
	if len(provs) == 0 {
		return false
	}
	for _, p := range provs {
		if a.cfg.ProviderConfigs[p.ID()]["use_proxy"] != "true" {
			return false
		}
	}
	return true
}

func (a *App) setCloudProvidersUseProxy(on bool) {
	if a.cfg.ProviderConfigs == nil {
		a.cfg.ProviderConfigs = make(map[string]map[string]string)
	}
	val := "false"
	if on {
		val = "true"
	}
	for _, p := range a.cloudProxyEligibleProviders() {
		m := a.cfg.ProviderConfigs[p.ID()]
		if m == nil {
			m = make(map[string]string)
			a.cfg.ProviderConfigs[p.ID()] = m
		}
		m["use_proxy"] = val
	}
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

// sidebarLocaleScope returns the set of locale IDs shown in the sidebar list:
// locales present in the loaded bundle plus any still enabled (manually added
// but not yet scaffolded). It returns nil when the filter would hide nothing —
// no bundle loaded or every configured locale already matches — so the full
// list shows for single-template projects where targets must be picked freely.
func (a *App) sidebarLocaleScope() map[string]bool {
	if a.bundle == nil || len(a.bundle.Locales) == 0 {
		return nil
	}
	inBundle := make(map[string]bool, len(a.bundle.Locales))
	for _, id := range a.bundle.Locales {
		inBundle[id] = true
	}
	scope := make(map[string]bool, len(a.cfg.Locales))
	hidden := 0
	for _, loc := range a.cfg.Locales {
		if inBundle[loc.ID] || loc.Enabled {
			scope[loc.ID] = true
		} else {
			hidden++
		}
	}
	if hidden == 0 {
		return nil
	}
	return scope
}

func (a *App) selectAllLocales(on bool) {
	scope := a.sidebarLocaleScope()
	for i := range a.cfg.Locales {
		if a.cfg.Locales[i].IsRef {
			a.cfg.Locales[i].Enabled = true
			continue
		}
		if scope != nil && !scope[a.cfg.Locales[i].ID] {
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
		idx := ((start+delta*step)%n + n) % n
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

	refID := a.plan.RefLocaleID

	rowTasks := func(locs []locale.LocaleSpec) map[string][]provider.SourceItem {
		tasks := make(map[string][]provider.SourceItem)
		for _, loc := range locs {
			if forceLocaleID == "" {
				cell, ok := row.Cells[loc.ID]
				if ok && cell.Status == domain.StatusVanished {
					continue
				}
				if ok && cell.Effective != "" && cfg.WriteMode == domain.ModeFillUnfinished {
					continue
				}
				if ok && cfg.WriteMode == domain.ModeFillUnfinished && cell.Status == domain.StatusFinished {
					continue
				}
			}
			srcText, srcLoc := localeSource(a.plan, row, loc)
			if strings.TrimSpace(srcText) == "" {
				continue
			}
			if srcLoc == "" && a.bundle != nil {
				srcLoc = a.bundle.SourceLocale
			}
			tasks[loc.ID] = []provider.SourceItem{{
				ID:           row.Key.Canonical(),
				Key:          row.Key,
				SourceLocale: srcLoc,
				SrcText:      srcText,
				RefLocale:    refID,
				RefText:      row.RefText,
				Numerus:      row.Numerus,
			}}
		}
		return tasks
	}
	if n := len(rowTasks(targetLocales)); n == 0 {
		a.setStatus(a.tr().StatusRowNoGaps, false)
		return
	} else {
		a.translatingRow = true
		a.setStatus(fmt.Sprintf(a.tr().StatusTranslatingRowFmt, n), false)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		translated := 0
		// Dependent locales go in waves so e.g. zh_TW reads zh_CN's just
		// translated text for this row.
		for _, waveLocs := range translationWaves(targetLocales) {
			var tasks map[string][]provider.SourceItem
			a.runOnUI(func() {
				tasks = rowTasks(waveLocs)
			})
			if len(tasks) == 0 {
				continue
			}
			res := provider.RunConcurrent(ctx, provider.DispatchOptions{
				Registry:    provider.DefaultRegistry(),
				Cache:       a.cache,
				ProviderIDs: activeIDs,
				Locales:     waveLocs,
				Tasks:       tasks,
				Concurrency: cfg.Concurrency,
				BypassCache: cfg.BypassCache || forceLocaleID != "",
			})
			a.runOnUI(func() {
				for _, cell := range res.Cells {
					origin := cell.ProviderID
					if forceLocaleID != "" {
						origin = domain.OriginManual
					}
					a.catalog.Set(cell.LocaleID, cell.Key, cell.Text, origin)
					planner.UpdateCellByKey(a.plan, cell.Key, cell.LocaleID, a.catalog)
				}
				for _, ev := range res.Events {
					a.logs = append(a.logs, ev)
				}
			})
			translated += len(res.Cells)
			if ctx.Err() != nil {
				break
			}
		}
		a.dispatchUI(func() {
			a.translatingRow = false
			a.trimLogs()
			if translated > 0 {
				a.editBoundLocale = ""
				a.rebuildPlan()
				a.syncEditorFromSelection()
			}
			a.setStatus(fmt.Sprintf(a.tr().StatusTranslateRowDoneFmt, translated), false)
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
	a.syncWindowEdited()
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
	a.syncWindowEdited()
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
	a.syncWindowEdited()
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
	a.syncWindowEdited()
	a.setStatus(a.tr().StatusRevertedRow, false)
}

func (a *App) copyText(s string) {
	_ = mygo.Clipboard.Write(transfer.TextData(s))
	a.setStatus(a.tr().StatusCopiedText, false)
}

// localeSource returns the source text and locale ID a target locale
// translates from: its pinned SourceLocale's own text when that entry
// has content, else the reference text of the bundle.
func localeSource(plan *planner.MatrixPlan, row *planner.MatrixRow, loc locale.LocaleSpec) (srcText, srcLoc string) {
	if loc.SourceLocale != "" {
		if cell, ok := row.Cells[loc.SourceLocale]; ok && strings.TrimSpace(cell.Effective) != "" {
			return cell.Effective, loc.SourceLocale
		}
	}
	return row.RefText, ""
}

func buildTranslationTasks(plan *planner.MatrixPlan, targetLocales []locale.LocaleSpec, mode domain.WriteMode, bundleSourceLocale string) (map[string][]provider.SourceItem, int) {
	tasks := make(map[string][]provider.SourceItem)
	if plan == nil {
		return tasks, 0
	}
	totalItems := 0
	refID := plan.RefLocaleID
	for _, loc := range targetLocales {
		var items []provider.SourceItem
		for i := range plan.Rows {
			row := &plan.Rows[i]
			srcText, srcLoc := localeSource(plan, row, loc)
			if strings.TrimSpace(srcText) == "" {
				continue
			}
			if srcLoc == "" {
				srcLoc = bundleSourceLocale
			}
			cell, ok := row.Cells[loc.ID]
			if ok && cell.Status == domain.StatusVanished {
				continue
			}
			if ok && cell.Effective != "" && mode == domain.ModeFillUnfinished {
				continue
			}

			items = append(items, provider.SourceItem{
				ID:           row.Key.Canonical(),
				Key:          row.Key,
				SourceLocale: srcLoc,
				SrcText:      srcText,
				RefLocale:    refID,
				RefText:      row.RefText,
				Numerus:      row.Numerus,
			})
		}
		if len(items) > 0 {
			tasks[loc.ID] = items
			totalItems += len(items)
		}
	}
	return tasks, totalItems
}

func (a *App) checkUpdatesAsync(silent bool) {
	if a.checkingUpdate {
		return
	}
	a.checkingUpdate = true
	a.updateErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if mygo.Updater.Enabled() {
			up, err := mygo.Updater.Check(ctx)
			a.dispatchUI(func() {
				a.checkingUpdate = false
				if err != nil {
					a.updateErr = err.Error()
					if !silent {
						a.setStatus(fmt.Sprintf(a.tr().StatusUpdateErrFmt, err), true)
					}
					return
				}
				a.pendingUpdate = up
				a.updateInfo = nil
				if up != nil {
					a.setStatus(fmt.Sprintf(a.tr().StatusUpdateAvailFmt, up.Version, config.AppVersion), false)
				} else if !silent {
					a.setStatus(fmt.Sprintf(a.tr().StatusUpToDateFmt, config.AppVersion), false)
				}
			})
			return
		}
		client, _ := a.netPool.Client(true)
		info, err := updater.CheckLatest(ctx, client, updater.DefaultLatestReleaseURL, config.AppVersion)
		a.dispatchUI(func() {
			a.checkingUpdate = false
			if err != nil {
				a.updateErr = err.Error()
				if !silent {
					a.setStatus(fmt.Sprintf(a.tr().StatusUpdateErrFmt, err), true)
				}
				return
			}
			a.updateInfo = &info
			a.pendingUpdate = nil
			if info.HasUpdate {
				a.setStatus(fmt.Sprintf(a.tr().StatusUpdateAvailFmt, info.LatestVersion, info.CurrentVersion), false)
			} else if !silent {
				a.setStatus(fmt.Sprintf(a.tr().StatusUpToDateFmt, info.CurrentVersion), false)
			}
		})
	}()
}

// installUpdate downloads, verifies and swaps in the signed update
// offered by mygo.Updater, then relaunches. Progress is relayed onto
// the UI thread for the settings modal's progress bar.
func (a *App) installUpdate() {
	if a.installingUpd || a.pendingUpdate == nil {
		return
	}
	up := a.pendingUpdate
	a.installingUpd = true
	a.updateErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := up.Install(ctx, func(dl, total int64) {
			a.dispatchUI(func() {
				a.updateDL, a.updateTotal = dl, total
			})
		})
		a.dispatchUI(func() {
			a.installingUpd = false
			if err != nil {
				a.updateErr = err.Error()
				a.setStatus(fmt.Sprintf(a.tr().StatusUpdateErrFmt, err), true)
				return
			}
			a.pendingUpdate = nil
			a.setStatus(a.tr().StatusUpdateInstalled, false)
		})
		if err == nil {
			mygo.App.Relaunch()
		}
	}()
}

func (a *App) openReleasePage() {
	target := "https://github.com/cxxzsh/locloom/releases"
	if a.updateInfo != nil && strings.TrimSpace(a.updateInfo.HTMLURL) != "" {
		target = a.updateInfo.HTMLURL
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

// OnInteractiveReady runs startup tasks for interactive desktop sessions (first-run onboarding & update check).
func (a *App) OnInteractiveReady() {
	if !a.cfg.HasSeenOnboarding && !a.showHelp {
		a.showOnboarding = true
	}
	if a.cfg.CheckUpdatesOnStartup {
		a.checkUpdatesAsync(true)
	}
}
