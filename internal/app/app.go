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
	"locloom/internal/locale"
	"locloom/internal/netpool"
	"locloom/internal/planner"
	"locloom/internal/provider"
	_ "locloom/internal/providers"
	"locloom/internal/sheet"
	"locloom/internal/tsxml"
)

// App holds the workbench UI state and coordinates background I/O/translation tasks
// via lock-free UI-thread dispatch.
type App struct {
	win     *mygo.Window
	cfg     config.AppConfig
	netPool *netpool.Pool
	cache   *provider.Cache
	catalog *domain.Catalog
	bundle  *tsxml.Bundle
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
	editBoundRowKey  domain.EntryKey
	editBoundLocale  string

	filteredRows  []int
	tableState    ui.ListState
	ctxListState  ui.ListState
	logListState  ui.ListState

	// Modals & popovers
	showHelp        bool
	helpMarkdown    string
	mdOpts          markdown.Options
	showExportModal bool
	exportScope     int
	showClearMenu   bool

	// Provider UI state
	providerTestMsg   map[string]string
	providerTestErr   map[string]bool
	providerTesting   map[string]bool
	discoveredModels  map[string][]string
	discoveringModels map[string]bool

	// Custom locale form
	newLocID   string
	newLocName string
	newLocCode string

	// Concurrent translation state
	translating         bool
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

	a := &App{
		cfg:               cfg,
		netPool:           netpool.New(cfg.Proxy),
		cache:             provider.GlobalCache(),
		catalog:           domain.NewCatalog(),
		selectedRow:       -1,
		matrixHeightPct:   64,
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

	a.logListState.FollowEnd = true

	a.rebuildPlan()
	a.appendLog("info", "", "", "工作台已就绪，可拖拽 .ts 目录 / .zip / .xlsx 到窗口，或通过顶部按钮加载项目。")
	return a
}

// SetWindow binds the native window for dialogs and background UI-thread updates.
func (a *App) SetWindow(w *mygo.Window) {
	a.win = w
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

// ApplyOSTheme synchronizes mygo's OS theme source with cfg.ThemeMode.
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

// LoadTSTarget loads a .ts directory or .zip archive into the workbench.
func (a *App) LoadTSTarget(dirOrZip string) error {
	bundle, err := tsxml.LoadBundle(dirOrZip, a.cfg.FilePattern, a.cfg.Locales)
	if err != nil {
		a.setStatus("加载 .ts 失败: "+err.Error(), true)
		a.appendLog("error", "", "", "加载 .ts 失败: "+err.Error())
		return err
	}
	a.applyLoadedBundle(bundle)
	return nil
}

func (a *App) applyLoadedBundle(bundle *tsxml.Bundle) {
	a.bundle = bundle
	a.cfg.LastTSPath = bundle.SourcePath

	discovered := make([]string, 0, len(bundle.Files))
	for _, tf := range bundle.Files {
		if tf.LocaleID != "" {
			discovered = append(discovered, tf.LocaleID)
		}
	}
	merged, added := locale.MergeDiscovered(a.cfg.Locales, discovered)
	a.cfg.Locales = merged

	a.contextFilter = ""
	a.selectedRow = 0
	a.rebuildPlan()

	msg := fmt.Sprintf("已加载 %s（%d 个 .ts 文件，%d 条词条）", filepath.Base(bundle.SourcePath), len(bundle.Files), a.plan.Summary.TotalRows)
	if len(added) > 0 {
		msg += fmt.Sprintf("，自动发现新语种: %v", added)
	}
	a.setStatus(msg, false)
	a.appendLog("info", "", "", msg)
	a.scheduleConfigSave()
}

// ImportFromExcelFile reads an .xlsx workbook and populates the workspace catalog.
func (a *App) ImportFromExcelFile(xlsxPath, sheetName string, skipRef bool) error {
	wb, err := sheet.LoadExcel(xlsxPath, sheetName, a.cfg.Locales, skipRef)
	if err != nil {
		a.setStatus("导入 Excel 失败: "+err.Error(), true)
		a.appendLog("error", "", "", "导入 Excel 失败: "+err.Error())
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
		if r.Source != "" {
			extra = append(extra, domain.EntryKey{
				Context: r.Context,
				Source:  r.Source,
				Comment: r.Comment,
			})
		}
	}
	a.extraEntries = extra
	a.rebuildPlan()

	msg := fmt.Sprintf("已导入 %s（工作表 %s，%d 条译文）", filepath.Base(wb.FilePath), wb.SheetName, count)
	a.setStatus(msg, false)
	a.appendLog("info", "", "", msg)
	a.scheduleConfigSave()
}

// ExportToExcelFile writes the current translation matrix to an .xlsx workbook.
func (a *App) ExportToExcelFile(xlsxPath string, scope sheet.ExportScope) error {
	if a.plan == nil {
		return fmt.Errorf("当前没有可导出的词条数据")
	}
	opts := planner.BuildExportRows(a.plan, scope, a.cfg.Locales)
	if err := sheet.ExportExcel(xlsxPath, opts); err != nil {
		a.setStatus("导出 Excel 失败: "+err.Error(), true)
		a.appendLog("error", "", "", "导出 Excel 失败: "+err.Error())
		return err
	}
	msg := fmt.Sprintf("已导出 %d 行到 %s", len(opts.Rows), filepath.Base(xlsxPath))
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
		a.openTSDirAsync()
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		a.writeTSAsync()
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) {
		a.showExportModal = true
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		a.searchHandle.Focus(c)
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
				if ui.Button(c, "关闭").Padding(t.Space(0.5), t.Space(2)).Clicked() {
					a.statusBanner = ""
				}
			})
			ui.Divider(c)
		}

		if dragOver {
			ui.Row(c).Padding(t.Space(2), t.Space(3)).Background(t.Accent.Alpha(0.15)).Justify(ui.Center).Children(func() {
				ui.Text(c, "松开即可加载 .ts 目录 / .zip 压缩包 / .xlsx 词表").FontSize(13).FontWeight(600).TextColor(t.Accent)
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
