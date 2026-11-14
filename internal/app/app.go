package app

import (
	"context"
	"fmt"
	"path/filepath"
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
)

// App holds the workbench UI state and coordinates background I/O/translation tasks
// via lock-free UI-thread dispatch.
type App struct {
	win     *mygo.Window
	cfg     config.AppConfig
	netPool *netpool.Pool
	cache   *provider.Cache
	catalog *domain.Catalog
	bundle  *format.Bundle
	plan    *planner.MatrixPlan
	extraEntries []domain.EntryKey
	defaultCtx   string

	// Layout & navigation state
	sidebarTab    int
	bottomTab     int
	statusFilter  int
	contextFilter string
	searchQuery   string
	searchHandle  ui.Handle
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

	filteredRows  []int
	tableState    ui.ListState
	ctxListState  ui.ListState
	logListState  ui.ListState

	pendingTableOps []func()
	// Modals & popovers
	showHelp        bool
	helpMarkdown    string
	mdOpts          markdown.Options
	showExportModal bool
	exportScope     int
	showClearMenu   bool

	// Provider UI state
	expandedProvider  map[string]bool
	revealedSecrets   map[string]bool
	providerTestMsg   map[string]string
	providerTestErr   map[string]bool
	providerTesting   map[string]bool
	discoveredModels  map[string][]string
	discoveringModels map[string]bool

	// Locale sidebar search
	localeSearchQuery string
	// Custom locale form
	newLocID   string
	newLocName string
	newLocCode string

	// Concurrent translation state
	translating         bool
	translatingRow      bool
	cancelTranslate     context.CancelFunc
	translateStatuses   map[string]provider.LocaleStatus
	translateDoneCount  int
	translateTotalCount int
	// Logs & status banner
	logs            []provider.LogEvent
	statusBanner    string
	statusBannerErr bool

	// Debounced config persistence
	saveGen   uint64
	saveTimer *time.Timer
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
	cfg.UILang = i18n.Resolve(cfg.UILang)

	a := &App{
		cfg:               cfg,
		netPool:           netpool.New(cfg.Proxy),
		cache:             provider.GlobalCache(),
		catalog:           domain.NewCatalog(),
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
	a.tableState.CellMenu = a.buildMatrixCellMenu
	a.tableState.HeaderMenu = a.buildMatrixHeaderMenu

	a.logListState.FollowEnd = true

	a.rebuildPlan()
	a.appendLog("info", "", "", a.tr().LogWorkbenchReady)
	return a
}

// SetWindow binds the native window for dialogs and background UI-thread updates.
func (a *App) SetWindow(w *mygo.Window) {
	a.win = w
	a.ApplyOSTheme()
}
// UILang returns the resolved active UI language ("zh" or "en").
func (a *App) UILang() string {
	return i18n.Resolve(a.cfg.UILang)
}

// SetUILang switches the active UI language, updates the window title, rebuilds the plan
// so validation messages reflect the new language, and schedules config persistence.
func (a *App) SetUILang(lang string) {
	a.cfg.UILang = i18n.Resolve(lang)
	if a.win != nil {
		a.win.SetTitle(a.tr().WindowTitle)
	}
	a.rebuildPlan()
	a.scheduleConfigSave()
}

func (a *App) tr() *i18n.Catalog {
	return i18n.Of(a.cfg.UILang)
}

// WindowTitle returns the localized window title for the active UI language.
func (a *App) WindowTitle() string {
	return a.tr().WindowTitle
}

// SetHelpMarkdown sets the Markdown content shown in the Help modal.
func (a *App) SetHelpMarkdown(md string) {
	a.helpMarkdown = md
}

// SetShowHelp opens or closes the Help modal.
func (a *App) SetShowHelp(open bool) {
	a.showHelp = open
}

// SetThemeMode sets the theme mode (0=system, 1=light, 2=dark).
func (a *App) SetThemeMode(mode int) {
	if mode < 0 || mode > 2 {
		mode = 0
	}
	a.cfg.ThemeMode = mode
	a.ApplyOSTheme()
	a.scheduleConfigSave()
}

// IsDarkTheme reports whether the resolved active theme is dark.
func (a *App) IsDarkTheme() bool {
	switch a.cfg.ThemeMode {
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

// ApplyOSTheme synchronizes mygo's OS theme source and Dock/window icon with cfg.ThemeMode.
func (a *App) ApplyOSTheme() {
	if a.win == nil {
		return
	}
	switch a.cfg.ThemeMode {
	case 1:
		mygo.Theme.SetSource(mygo.ThemeLight)
	case 2:
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
	a.SyncAppIcon()
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
	opts := planner.BuildExportRows(a.plan, scope, a.cfg.Locales)
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
	switch a.cfg.ThemeMode {
	case 1:
		c.SetTheme(ui.LightTheme())
	case 2:
		c.SetTheme(ui.DarkTheme())
	default:
		if a.win != nil && mygo.Theme.IsDark() {
			c.SetTheme(ui.DarkTheme())
		} else {
			c.SetTheme(ui.LightTheme())
		}
	}
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
			}
		}
		if c.Shortcut(0, ui.KeyF3) || c.Shortcut(ui.Alt, ui.KeyDown) {
			a.jumpGap(1)
		}
		if c.Shortcut(ui.Shift, ui.KeyF3) || c.Shortcut(ui.Alt, ui.KeyUp) {
			a.jumpGap(-1)
		}
	}
	root := ui.Column(c).Fill().Background(t.Background)
	if files := root.DroppedFiles(); len(files) > 0 {
		a.handleDroppedPaths(files)
	}
	dragOver := root.FileDragOver()

	root.Children(func() {
		a.viewHeader(c)
		ui.Divider(c)

		if a.statusBanner != "" {
			bg := t.Surface
			fg := t.Text
			if a.statusBannerErr {
				bg = t.Danger.Alpha(0.14)
				fg = t.Danger
			}
			ui.Row(c).Padding(t.Space(1), t.Space(3)).Background(bg).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.statusBanner).FontSize(12).TextColor(fg).SingleLine().Grow(1)
				if ui.Button(c, a.tr().BtnClose).Padding(t.Space(0.5), t.Space(2)).Clicked() {
					a.statusBanner = ""
				}
			})
			ui.Divider(c)
		}

		if dragOver {
			ui.Row(c).Padding(t.Space(2), t.Space(3)).Background(t.Accent.Alpha(0.15)).Justify(ui.Center).Children(func() {
				ui.Text(c, a.tr().DropHint).FontSize(13).FontWeight(600).TextColor(t.Accent)
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
		}).Grow(1).MinHeight(0).SplitReset(310)
		if split.Changed() {
			a.scheduleConfigSave()
		}
	})

	a.viewModals(c)
}
