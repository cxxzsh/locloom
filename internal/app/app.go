package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"locloom/internal/config"
	"locloom/internal/domain"
	"locloom/internal/format"
	"locloom/internal/i18n"
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/planner"
	"locloom/internal/provider"
	_ "locloom/internal/providers"
	"locloom/internal/sheet"
	"locloom/internal/updater"
)

// App holds the workbench UI state and coordinates background I/O/translation tasks
// via lock-free UI-thread dispatch.
type App struct {
	win          *mygo.Window
	cfg          config.AppConfig
	netPool      *netpool.Pool
	cache        *provider.Cache
	catalog      *domain.Catalog
	bundle       *format.Bundle
	plan         *planner.MatrixPlan
	extraEntries []domain.EntryKey
	defaultCtx   string

	// Layout & navigation state
	sidebarTab      int
	bottomTab       int
	statusFilter    int
	contextFilter   string
	searchQuery     string
	searchHandle    ui.Handle
	matrixHeightPct float32

	// Selection & O(1) cell editing state
	focusedLocale    string
	selectedRow      int
	selectedKey      domain.EntryKey
	activeEditLocale string
	editBuf          string
	pluralEditBufs   []string
	editBoundRowKey  domain.EntryKey
	editBoundLocale  string

	filteredRows []int
	tableState   ui.ListState
	rowSel       ui.Selection[string]
	ctxListState ui.ListState
	logListState ui.ListState

	pendingTableOps []func()
	// Modals & popovers
	showHelp           bool
	helpTab            int
	showSettings       bool
	settingsTab        int
	showOnboarding     bool
	checkingUpdate     bool
	updateInfo         *updater.ReleaseInfo
	pendingUpdate      *mygo.Update // signed update offered by mygo.Updater
	installingUpd      bool
	updateDL           int64 // install download progress, bytes
	updateTotal        int64
	updateErr          string
	helpMarkdown       string
	helpMarkdownZH     string
	mdOpts             markdown.Options
	showExportModal    bool
	exportScope        int
	exportRelevantOnly bool
	showClearMenu      bool

	// Provider UI state
	expandedProvider  map[string]bool
	revealedSecrets   map[string]bool
	providerTestMsg   map[string]string
	providerTestErr   map[string]bool
	providerTesting   map[string]bool
	discoveredModels  map[string][]string
	discoveringModels map[string]bool
	dragHoverTarget   string
	dragHoverAfter    bool

	// Locale sidebar search
	localeSearchQuery string
	// Custom locale form & memoized cache inspector snapshot
	cacheClearLocale    string
	cacheClearProvider  string
	cacheSearchQuery    string
	cacheViewRev        uint64
	cacheViewLoaded     bool
	cacheViewLocCounts  map[string]int
	cacheViewProvCounts map[string]int
	cacheViewEntries    []provider.CacheEntry
	newLocID            string
	newLocName          string
	newLocCode          string

	// Concurrent translation & write-back state
	translating         bool
	writing             bool
	translatingRow      bool
	cancelTranslate     context.CancelFunc
	translateStatuses   map[string]provider.LocaleStatus
	translateDoneCount  int
	translateTotalCount int
	translateWave       int // 1-based wave of the running cascade; 0 when idle
	translateWaveTotal  int
	// Logs & status banner
	logs            []provider.LogEvent
	statusBanner    string
	statusBannerErr bool

	// Debounced config persistence
	saveGen   uint64
	saveTimer *time.Timer

	// Transient CLI overrides (never persisted to config)
	cliLangOverride  string
	cliThemeOverride int // -1 = unset, 0..2 = forced ThemeMode

	// Pending Finder/OS file-open paths queued before SetWindow
	pendingOpenMu    sync.Mutex
	pendingOpenPaths []string

	// Close confirmation guard & background task tracking for race-free tests
	forceClose      bool
	confirmingClose bool
	bgWG            sync.WaitGroup
}

// New loads persisted configuration and initializes the workbench.
func New() *App {
	cfg, _, _ := config.Load()
	return NewWithConfig(cfg)
}

// NewWithConfig initializes the workbench with an explicit configuration.
func NewWithConfig(cfg config.AppConfig) *App {
	if cfg.LeftPaneWidth <= 0 {
		cfg.LeftPaneWidth = 310
	}
	if cfg.BottomPaneHeight <= 0 {
		cfg.BottomPaneHeight = 250
	}
	if cfg.FilePattern == "" {
		cfg.FilePattern = "{id}.ts"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if len(cfg.Locales) == 0 {
		cfg.Locales = locale.DefaultLocales()
	}
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = locale.DefaultProfiles()
	}
	rawLang := strings.ToLower(strings.TrimSpace(cfg.UILang))
	if rawLang == i18n.LangAuto {
		cfg.UILang = i18n.LangAuto
	} else {
		cfg.UILang = i18n.Resolve(rawLang)
	}
	a := &App{
		cfg:               cfg,
		cliThemeOverride:  -1,
		netPool:           netpool.New(cfg.Proxy),
		cache:             provider.GlobalCache(),
		catalog:           domain.NewCatalog(),
		defaultCtx:        cfg.AppendContext,
		selectedRow:       -1,
		matrixHeightPct:   64,
		expandedProvider:  make(map[string]bool),
		revealedSecrets:   make(map[string]bool),
		providerTestMsg:   make(map[string]string),
		providerTestErr:   make(map[string]bool),
		providerTesting:   make(map[string]bool),
		discoveredModels:  make(map[string][]string),
		discoveringModels: make(map[string]bool),
		translateStatuses: make(map[string]provider.LocaleStatus),
	}
	a.tableState.Selected = &a.selectedRow
	a.tableState.Key = func(i int) any {
		if a.plan == nil || i < 0 || i >= len(a.filteredRows) {
			return i
		}
		return a.plan.Rows[a.filteredRows[i]].Key.Canonical()
	}
	a.tableState.Label = func(i int) string {
		if a.plan == nil || i < 0 || i >= len(a.filteredRows) {
			return ""
		}
		return a.plan.Rows[a.filteredRows[i]].Key.Source
	}
	a.tableState.CellSelect = true
	a.tableState.Selection = &a.rowSel
	a.tableState.CellMenu = a.buildMatrixCellMenu
	a.tableState.HeaderMenu = a.buildMatrixHeaderMenu

	a.logListState.FollowEnd = true

	a.rebuildPlan()
	a.exportRelevantOnly = true
	a.appendLog("info", "", "", a.tr().LogWorkbenchReady)
	return a
}

// SetWindow binds the native window for dialogs and background UI-thread updates.
func (a *App) SetWindow(w *mygo.Window) {
	a.win = w
	a.ApplyOSTheme()
	a.syncWindowEdited()
	if w != nil {
		w.OnClose(func(e *mygo.CloseEvent) {
			if a.forceClose {
				a.FlushConfigSave()
				return
			}
			pending := a.PendingChangeCount()
			if !a.translating && pending == 0 {
				a.FlushConfigSave()
				return
			}
			e.PreventDefault()
			if a.confirmingClose {
				return
			}
			a.confirmingClose = true
			tr := a.tr()
			msg := fmt.Sprintf(tr.ConfirmCloseMsgFmt, pending)
			if a.translating {
				if pending > 0 {
					msg = tr.ConfirmCloseTranslatingMsg + "\n\n" + msg
				} else {
					msg = tr.ConfirmCloseTranslatingMsg
				}
			}
			targetWin := w
			go func() {
				res, err := mygo.Dialog.Message(mygo.MessageOptions{
					Parent:             targetWin,
					Type:               mygo.MessageWarning,
					Title:              tr.ConfirmCloseTitle,
					Message:            msg,
					Buttons:            []string{tr.BtnDiscardAndClose, tr.BtnCancel},
					DefaultButton:      1,
					CancelButton:       1,
					DestructiveButtons: []int{0},
				})
				a.dispatchUI(func() {
					a.confirmingClose = false
					if err != nil || res.Button != 0 {
						return
					}
					if a.translating {
						a.stopTranslation()
					}
					a.FlushConfigSave()
					a.forceClose = true
					targetWin.Close()
				})
			}()
		})
	}
	a.drainPendingOpenPaths()
}

// UILang returns the resolved active UI language ("zh" or "en").
func (a *App) UILang() string {
	if a.cliLangOverride != "" {
		return i18n.Resolve(a.cliLangOverride)
	}
	return i18n.Resolve(a.cfg.UILang)
}

// ApplyCLILang sets a transient CLI language override without modifying or saving config.json.
func (a *App) ApplyCLILang(lang string) {
	trimmed := strings.ToLower(strings.TrimSpace(lang))
	if trimmed == i18n.LangAuto || trimmed == i18n.LangZH || trimmed == i18n.LangEN {
		a.cliLangOverride = trimmed
	} else {
		a.cliLangOverride = i18n.Resolve(trimmed)
	}
	if a.win != nil {
		a.win.SetTitle(a.tr().WindowTitle)
	}
	if a.plan != nil {
		a.rebuildPlan()
	}
}

// SetUILang switches the active UI language, updates the window title, rebuilds the plan
// so validation messages reflect the new language, and schedules config persistence.
func (a *App) SetUILang(lang string) {
	a.cliLangOverride = ""
	trimmed := strings.ToLower(strings.TrimSpace(lang))
	if trimmed == i18n.LangAuto {
		a.cfg.UILang = i18n.LangAuto
	} else {
		a.cfg.UILang = i18n.Resolve(trimmed)
	}
	if a.win != nil {
		a.win.SetTitle(a.tr().WindowTitle)
	}
	a.rebuildPlan()
	a.scheduleConfigSave()
}

// RefreshSystemLocale refreshes window title and validation plan when OS locale changes under "auto" mode.
func (a *App) RefreshSystemLocale() {
	if a.win != nil {
		a.win.SetTitle(a.tr().WindowTitle)
	}
	if a.plan != nil && a.plan.UILang != a.UILang() {
		a.rebuildPlan()
	}
	a.invalidate()
}

// DispatchRefreshLocale posts RefreshSystemLocale onto the UI thread when a window is attached.
func (a *App) DispatchRefreshLocale() {
	a.dispatchUI(func() {
		a.RefreshSystemLocale()
	})
}

func (a *App) tr() *i18n.Catalog {
	return i18n.Of(a.UILang())
}

// WindowTitle returns the localized window title for the active UI language.
func (a *App) WindowTitle() string {
	return a.tr().WindowTitle
}

// SetHelpMarkdown sets the Markdown content shown in the Help modal.
func (a *App) SetHelpMarkdown(md string) {
	a.helpMarkdown = md
	a.helpMarkdownZH = ""
}

// SetHelpMarkdowns sets separate English and Simplified Chinese Markdown documents for the Help modal.
func (a *App) SetHelpMarkdowns(enMD, zhMD string) {
	a.helpMarkdown = enMD
	a.helpMarkdownZH = zhMD
}

// SetShowHelp opens or closes the Help modal.
func (a *App) SetShowHelp(open bool) {
	a.showHelp = open
}

func (a *App) effectiveThemeMode() int {
	if a.cliThemeOverride >= 0 && a.cliThemeOverride <= 2 {
		return a.cliThemeOverride
	}
	return a.cfg.ThemeMode
}

// ApplyCLITheme sets a transient CLI theme override (0=system, 1=light, 2=dark) without saving to config.json.
func (a *App) ApplyCLITheme(mode int) {
	if mode < 0 || mode > 2 {
		mode = 0
	}
	a.cliThemeOverride = mode
	a.ApplyOSTheme()
}

// SetThemeMode sets the theme mode (0=system, 1=light, 2=dark).
func (a *App) SetThemeMode(mode int) {
	if mode < 0 || mode > 2 {
		mode = 0
	}
	a.cliThemeOverride = -1
	a.cfg.ThemeMode = mode
	a.ApplyOSTheme()
	a.scheduleConfigSave()
}

// IsDarkTheme reports whether the resolved active theme is dark.
func (a *App) IsDarkTheme() bool {
	switch a.effectiveThemeMode() {
	case 1:
		return false
	case 2:
		return true
	default:
		return a.win != nil && mygo.Theme.IsDark()
	}
}

// SyncAppIcon updates the OS Dock and window icon to match the active Dark/Light theme.
func (a *App) SyncAppIcon() {
	if a.win == nil {
		return
	}
	if iconPNG, err := AppIconPNGForTheme(a.IsDarkTheme()); err == nil && len(iconPNG) > 0 {
		_ = mygo.App.Dock.SetIcon(iconPNG)
		_ = a.win.SetIcon(iconPNG)
	}
}

// ApplyOSTheme synchronizes mygo's OS theme source and Dock/window icon with the effective theme mode.
func (a *App) ApplyOSTheme() {
	if a.win == nil {
		return
	}
	switch a.effectiveThemeMode() {
	case 1:
		mygo.Theme.SetSource(mygo.ThemeLight)
	case 2:
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
	a.SyncAppIcon()
}

// PendingChangeCount returns the number of unsaved write-back changes/appends in the active plan.
func (a *App) PendingChangeCount() int {
	if a.plan == nil {
		return 0
	}
	return a.plan.Summary.TotalChanges + a.plan.Summary.TotalAppends
}

// HasUnsavedChanges reports whether there are pending write-back changes in the active plan.
func (a *App) HasUnsavedChanges() bool {
	return a.PendingChangeCount() > 0
}

func (a *App) syncWindowEdited() {
	if a.win != nil {
		a.win.SetDocumentEdited(a.HasUnsavedChanges())
	}
}

// OpenExternalPath handles a file or directory opened from OS shell / macOS Finder.
func (a *App) OpenExternalPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	a.pendingOpenMu.Lock()
	win := a.win
	if win == nil {
		a.pendingOpenPaths = append(a.pendingOpenPaths, path)
		a.pendingOpenMu.Unlock()
		return
	}
	a.pendingOpenMu.Unlock()
	a.dispatchUI(func() {
		a.handleDroppedPaths([]string{path})
	})
}

func (a *App) drainPendingOpenPaths() {
	a.pendingOpenMu.Lock()
	paths := a.pendingOpenPaths
	a.pendingOpenPaths = nil
	a.pendingOpenMu.Unlock()
	if len(paths) > 0 {
		a.handleDroppedPaths(paths)
	}
}

// dispatchUI executes fn on the mygo main UI thread when a window is attached,
// or runs directly in headless/test mode.
func (a *App) dispatchUI(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
	} else {
		fn()
	}
}

// runOnUI runs fn on the mygo main UI thread and waits for it, so a
// background goroutine can read or mutate plan state safely between
// translation waves. With no window (headless/tests) it runs inline.
func (a *App) runOnUI(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	done := make(chan struct{})
	a.win.Update(func() {
		fn()
		close(done)
	})
	<-done
}

func (a *App) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

func (a *App) scheduleConfigSave() {
	if a.win == nil {
		return
	}
	a.saveGen++
	gen := a.saveGen
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(450*time.Millisecond, func() {
		a.dispatchUI(func() {
			if gen != a.saveGen {
				return
			}
			cloned := a.cfg.Clone()
			go func() {
				_ = config.Save(cloned)
			}()
		})
	})
}

// FlushConfigSave cancels any pending debounced config timer and writes config synchronously.
func (a *App) FlushConfigSave() {
	a.saveGen++
	if a.saveTimer != nil {
		a.saveTimer.Stop()
		a.saveTimer = nil
	}
	_ = config.Save(a.cfg.Clone())
}

func (a *App) setStatus(msg string, isErr bool) {
	a.statusBanner = msg
	a.statusBannerErr = isErr
}

// LoadTSTarget loads any supported localization file, directory, or .zip archive into the workbench.
// Kept as a compatibility alias for CLI flags and existing tests.
func (a *App) LoadTSTarget(path string) error {
	return a.LoadProject(path)
}

// LoadProject auto-detects and loads a localization file, directory, or .zip archive.
func (a *App) LoadProject(path string) error {
	bundle, err := format.Load(path, a.cfg.FilePattern, a.cfg.Locales)
	if err != nil {
		msg := fmt.Sprintf(a.tr().ErrLoadTSFmt, err)
		a.setStatus(msg, true)
		a.appendLog("error", "", "", msg)
		return err
	}
	a.applyLoadedBundle(bundle)
	return nil
}

func (a *App) applyLoadedBundle(bundle *format.Bundle) {
	// Clear staged catalog overrides when switching from one project to a different project,
	// while keeping staged edits on Refresh (same SourcePath) and pre-loaded Excel imports (bundle == nil).
	if a.bundle != nil && bundle != nil && a.bundle.SourcePath != bundle.SourcePath {
		a.catalog.ClearAll()
		a.extraEntries = nil
		a.editBoundLocale = ""
	}
	a.bundle = bundle
	a.cfg.LastTSPath = bundle.SourcePath

	discovered := make([]string, 0, len(bundle.Locales)+len(bundle.Docs))
	discovered = append(discovered, bundle.Locales...)
	for _, doc := range bundle.Docs {
		if doc.LocaleID != "" {
			discovered = append(discovered, doc.LocaleID)
		}
		discovered = append(discovered, doc.Locales...)
	}
	merged, added := locale.MergeDiscovered(a.cfg.Locales, discovered)
	a.cfg.Locales = merged

	// Smart Project Sync: align reference and enabled target locales with the loaded bundle.
	if len(bundle.Locales) > 0 {
		inBundle := make(map[string]bool, len(bundle.Locales))
		for _, id := range bundle.Locales {
			inBundle[id] = true
		}
		targetInBundle := 0
		for _, loc := range a.cfg.Locales {
			if !loc.IsRef && inBundle[loc.ID] && loc.ID != bundle.SourceLocale {
				targetInBundle++
			}
		}
		if targetInBundle > 0 {
			firstInBundle := ""
			for i := range a.cfg.Locales {
				id := a.cfg.Locales[i].ID
				if a.cfg.Locales[i].IsRef {
					a.cfg.Locales[i].Enabled = inBundle[id]
					continue
				}
				present := inBundle[id] && id != bundle.SourceLocale
				a.cfg.Locales[i].Enabled = present
				if present && firstInBundle == "" {
					firstInBundle = id
				}
			}
			if firstInBundle != "" {
				a.focusedLocale = firstInBundle
				a.activeEditLocale = firstInBundle
			}
		} else {
			// Single-template project (e.g. en.json or .pot): ensure at least one target locale is enabled.
			anyEnabled := false
			for _, loc := range a.cfg.Locales {
				if !loc.IsRef && loc.Enabled {
					anyEnabled = true
					break
				}
			}
			if !anyEnabled {
				for i := range a.cfg.Locales {
					if !a.cfg.Locales[i].IsRef {
						a.cfg.Locales[i].Enabled = true
						break
					}
				}
			}
		}
	}

	a.contextFilter = ""
	a.selectedRow = 0
	a.rebuildPlan()

	tr := a.tr()
	msg := fmt.Sprintf(tr.StatusLoadedBundleFmt, filepath.Base(bundle.SourcePath), len(bundle.Docs), a.plan.Summary.TotalRows)
	if len(added) > 0 {
		msg += fmt.Sprintf(tr.StatusDiscoveredLocalesFmt, added)
	}
	a.setStatus(msg, false)
	a.appendLog("info", "", "", msg)
	a.scheduleConfigSave()
}

// ImportFromExcelFile reads an .xlsx workbook and populates the workspace catalog.
func (a *App) ImportFromExcelFile(xlsxPath, sheetName string, skipRef bool) error {
	wb, err := sheet.LoadExcel(xlsxPath, sheetName, a.cfg.Locales, skipRef)
	if err != nil {
		msg := fmt.Sprintf(a.tr().ErrImportExcelFmt, err)
		a.setStatus(msg, true)
		a.appendLog("error", "", "", msg)
		return err
	}
	a.applyLoadedWorkbook(wb, skipRef)
	return nil
}

func (a *App) applyLoadedWorkbook(wb *sheet.WorkbookData, skipRef bool) {
	merged, _ := locale.MergeDiscovered(a.cfg.Locales, wb.DiscoveredLocales)
	a.cfg.Locales = merged

	count := sheet.PopulateCatalog(wb, a.catalog, skipRef)
	a.cfg.LastXLSXPath = wb.FilePath

	// Collect extra entries from Excel rows in case AppendMissing is enabled.
	extra := make([]domain.EntryKey, 0, len(wb.Rows))
	for _, r := range wb.Rows {
		if r.Source != "" || r.KeyID != "" {
			extra = append(extra, domain.EntryKey{
				ID:      r.KeyID,
				Context: r.Context,
				Source:  r.Source,
				Comment: r.Comment,
			})
		}
	}
	a.extraEntries = extra
	a.editBoundLocale = ""
	a.rebuildPlan()

	msg := fmt.Sprintf(a.tr().StatusImportedExcelFmt, filepath.Base(wb.FilePath), wb.SheetName, count)
	a.setStatus(msg, false)
	a.appendLog("info", "", "", msg)
	a.scheduleConfigSave()
}

// ExportToExcelFile writes the current translation matrix to an .xlsx workbook.
func (a *App) ExportToExcelFile(xlsxPath string, scope sheet.ExportScope) error {
	if a.plan == nil {
		return fmt.Errorf("%s", a.tr().ErrNoExportData)
	}
	opts := planner.BuildExportRows(a.plan, scope, a.cfg.Locales, a.exportRelevantOnly)
	if err := sheet.ExportExcel(xlsxPath, opts); err != nil {
		msg := fmt.Sprintf(a.tr().ErrExportExcelFmt, err)
		a.setStatus(msg, true)
		a.appendLog("error", "", "", msg)
		return err
	}
	msg := fmt.Sprintf(a.tr().StatusExportedExcelFmt, len(opts.Rows), filepath.Base(xlsxPath))
	a.setStatus(msg, false)
	a.appendLog("info", "", "", msg)
	return nil
}

// View renders the entire workbench on the mygo main UI thread without locks.
func (a *App) View(c *ui.Context) {
	c.SetTheme(beautifulTheme(a.IsDarkTheme()))
	t := c.Theme()

	// Cross-platform keyboard shortcuts
	if c.Shortcut(ui.Cmd, ui.KeyO) {
		a.openDirAsync()
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		a.writeBackAsync()
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) {
		a.showExportModal = true
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		a.searchHandle.Focus(c)
	}
	if !c.FocusTakesText() {
		if c.Shortcut(0, ui.KeyEscape) {
			if a.showHelp {
				a.showHelp = false
			} else if a.showExportModal {
				a.showExportModal = false
			} else if a.showSettings {
				a.showSettings = false
			} else if a.showOnboarding {
				a.showOnboarding = false
				if !a.cfg.HasSeenOnboarding {
					a.cfg.HasSeenOnboarding = true
					a.scheduleConfigSave()
				}
			}
		}
		if c.Shortcut(0, ui.KeyF3) || c.Shortcut(ui.Alt, ui.KeyDown) {
			a.jumpGap(1)
		}
		if c.Shortcut(ui.Shift, ui.KeyF3) || c.Shortcut(ui.Alt, ui.KeyUp) {
			a.jumpGap(-1)
		}
	}
	// Matrix rows select as a tinted wash, the RecordsTable way:
	// chosen rows at accent 7%, the cursor column's cells at accent 12%
	// with --accent-ink text (a transparent CellText falls back to white).
	a.tableState.CellsStyle = ui.CellsStyle{
		Cell:     t.Accent.Alpha(0.14),
		CellText: accentInk(t),
		Row:      t.Accent.Alpha(0.09),
	}
	root := ui.Column(c).Fill().Background(canvasBg(t))
	if files := root.DroppedFiles(); len(files) > 0 {
		a.handleDroppedPaths(files)
	}
	dragOver := root.FileDragOver()

	root.Children(func() {
		a.viewHeader(c)
		ui.Divider(c)

		if a.translating {
			tr := a.tr()
			frac := 0.0
			if a.translateTotalCount > 0 {
				frac = float64(a.translateDoneCount) / float64(a.translateTotalCount)
			}
			var activeParts []string
			for locID, st := range a.translateStatuses {
				if st.ActiveProvider != "" && st.Completed < st.Total {
					activeParts = append(activeParts, fmt.Sprintf("%s (%s %d/%d)", locID, st.ActiveProvider, st.Completed, st.Total))
				}
			}
			slices.Sort(activeParts)
			detail := strings.Join(activeParts, " · ")
			if len(activeParts) > 3 {
				detail = strings.Join(activeParts[:3], " · ") + fmt.Sprintf(" (+%d)", len(activeParts)-3)
			}
			ui.Row(c).Padding(t.Space(1.5), t.Space(3)).Background(accentTint(t)).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, a.translateBannerText(tr, frac)).
					FontSize(12).FontWeight(600).TextColor(t.Accent).SingleLine()
				if detail != "" {
					ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
				} else {
					ui.Spacer(c)
				}
				ui.Column(c).Width(160).Children(func() {
					ui.Progress(c, frac)
				})
				if ui.Button(c, tr.BtnStopTranslate).Padding(t.Space(0.5), t.Space(2)).Radius(pillRadius).Background(t.Danger).TextColor(t.AccentText).Tooltip(tr.TipStopTranslate).Clicked() {
					a.stopTranslation()
				}
			})
			ui.Divider(c)
		} else if a.translatingRow {
			tr := a.tr()
			ui.Row(c).Padding(t.Space(1.5), t.Space(3)).Background(accentTint(t)).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, a.statusBanner).FontSize(12).FontWeight(600).TextColor(accentInk(t)).SingleLine().Grow(1)
				_ = tr
			})
			ui.Divider(c)
		} else if a.statusBanner != "" {
			// Static status banner shown when no translation is active
			bg := t.Surface
			fg := t.Text
			if a.statusBannerErr {
				bg = redTint(t)
				fg = t.Danger
			}
			ui.Row(c).Padding(t.Space(1), t.Space(3)).Background(bg).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.statusBanner).FontSize(12).TextColor(fg).SingleLine().Grow(1)
				if ui.Button(c, a.tr().BtnClose).Padding(t.Space(0.5), t.Space(2)).Radius(pillRadius).Clicked() {
					a.statusBanner = ""
				}
			})
			ui.Divider(c)
		}

		if dragOver {
			ui.Row(c).Padding(t.Space(2), t.Space(3)).Background(accentTint(t)).Justify(ui.Center).Children(func() {
				ui.Text(c, a.tr().DropHint).FontSize(13).FontWeight(600).TextColor(accentInk(t))
			})
		}

		// Main 3-Pane Workbench: Left Sidebar | (Center Matrix / Bottom Inspector)
		split := ui.Split(c, &a.cfg.LeftPaneWidth, func() {
			a.viewSidebar(c)
		}, func() {
			ui.SplitVerticalPercent(c, &a.matrixHeightPct, func() {
				a.viewMatrix(c)
			}, func() {
				a.viewInspector(c)
			}).Fill().SplitReset(64)
		}).Grow(1).MinHeight(0).SplitReset(310).Padding(t.Space(1.5))
		if split.Changed() {
			a.scheduleConfigSave()
		}
	})

	a.viewModals(c)
}

// translateBannerText renders the in-flight translation status: plain
// "x/y" for a single wave, or "wave i/n · x/y" while a dependent
// cascade runs.
func (a *App) translateBannerText(tr *i18n.Catalog, frac float64) string {
	base := fmt.Sprintf(tr.StatusTranslatingBannerFmt, a.translateDoneCount, a.translateTotalCount, frac*100)
	if a.translateWaveTotal > 1 && a.translateWave > 0 {
		return fmt.Sprintf(tr.StatusTranslatingWaveFmt, a.translateWave, a.translateWaveTotal) + " · " + base
	}
	return base
}
