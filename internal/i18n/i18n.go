package i18n

import (
	"os"
	"strings"
	"sync"
)

const (
	// LangZH represents Simplified Chinese.
	LangZH = "zh"
	// LangEN represents English.
	LangEN = "en"
	// LangAuto resolves the UI language from OS environment variables or platform locale.
	LangAuto = "auto"
)

var (
	sysLocaleMu  sync.RWMutex
	systemLocale string
)

// SetSystemLocale sets the OS-reported preferred locale tag used when LangAuto cannot resolve via env vars.
func SetSystemLocale(tag string) {
	sysLocaleMu.Lock()
	systemLocale = strings.TrimSpace(tag)
	sysLocaleMu.Unlock()
}

// SystemLocale returns the OS-reported preferred locale tag, if set.
func SystemLocale() string {
	sysLocaleMu.RLock()
	tag := systemLocale
	sysLocaleMu.RUnlock()
	return tag
}

// Resolve normalizes a language tag ("en", "zh", "auto", etc.) to LangEN or LangZH.
func Resolve(lang string) string {
	norm := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case norm == LangEN || strings.HasPrefix(norm, "en-") || strings.HasPrefix(norm, "en_"):
		return LangEN
	case norm == LangZH || strings.HasPrefix(norm, "zh-") || strings.HasPrefix(norm, "zh_"):
		return LangZH
	case norm == LangAuto:
		return resolveFromEnv()
	default:
		return LangZH
	}
}

func resolveFromEnv() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if val == "" || val == "c" || val == "posix" {
			continue
		}
		if strings.HasPrefix(val, "zh") {
			return LangZH
		}
		if strings.HasPrefix(val, "en") {
			return LangEN
		}
		// Any other explicit non-Chinese locale falls back to English.
		return LangEN
	}
	if sys := strings.ToLower(strings.TrimSpace(SystemLocale())); sys != "" && sys != "c" && sys != "posix" {
		if strings.HasPrefix(sys, "zh") {
			return LangZH
		}
		return LangEN
	}
	return LangZH
}

// Of returns the static, zero-allocation UI string catalog for the given language.
func Of(lang string) *Catalog {
	if Resolve(lang) == LangEN {
		return &enCatalog
	}
	return &zhCatalog
}

// Catalog holds all localized UI strings and format templates for a single language.
type Catalog struct {
	// Window & general
	WindowTitle                string
	BtnClose                   string
	BtnCancel                  string
	DropHint                   string
	LogWorkbenchReady          string
	ConfirmCloseTitle          string
	ConfirmCloseMsgFmt         string
	ConfirmCloseTranslatingMsg string
	BtnDiscardAndClose         string

	// App-level status & errors
	ErrLoadTSFmt               string
	StatusLoadedBundleFmt      string
	StatusDiscoveredLocalesFmt string
	ErrImportExcelFmt          string
	StatusImportedExcelFmt     string
	ErrNoExportData            string
	ErrExportExcelFmt          string
	StatusExportedExcelFmt     string

	// Header
	SourceUnloaded             string
	HeaderSummaryFmt           string
	BtnOpenDir                 string
	TipOpenDir                 string
	BtnOpenFile                string
	TipOpenFile                string
	BtnImportExcel             string
	TipImportExcel             string
	BtnExportExcel             string
	TipExportExcel             string
	StatusTranslating          string
	BtnStopTranslate           string
	BtnStartTranslate          string
	TipStopTranslate           string
	TipStartTranslate          string
	StatusTranslatingBannerFmt string
	StatusTranslatingWaveFmt   string
	StatusTranslatingRowFmt    string
	BtnTranslatingRow          string
	BtnWriteTS                 string
	BtnWriteTSCountFmt         string
	TipWriteTS                 string
	ThemeSystem                string
	ThemeLight                 string
	ThemeDark                  string
	TipTheme                   string
	BtnLangToggle              string
	TipLangToggle              string
	HeaderLogsFmt              string
	TipLogs                    string
	BtnHelp                    string
	TipHelp                    string

	// Sidebar tabs
	TabProject  string
	TabPipeline string
	TabLocales  string

	// Sidebar: Project tab
	SectionWriteStrategy   string
	WriteModeFill          string
	WriteModeOverwrite     string
	CheckAppendMissing     string
	DescWriteModeFill      string
	DescWriteModeOverwrite string
	LabelAppendContext     string
	PlaceholderDefaultCtx  string
	CheckCreateBackup      string
	CheckPluralForms       string
	TipWriteMode           string
	TipAppendMissing       string
	TipCreateBackup        string
	TipPluralForms         string
	TipLocaleProgressCard  string
	SectionContextNavFmt   string
	AllContextsFmt         string
	NoContextLabel         string
	SectionLocaleProgress  string
	LocaleGapFmt           string
	LocaleWarnFmt          string

	// Sidebar: Pipeline tab
	SectionConcurrencyCache string
	LabelGlobalConcurrency  string
	CheckBypassCache        string
	BtnClearCacheFmt        string
	StatusCacheCleared      string
	SectionNetworkProxy     string
	PlaceholderProxy        string
	CheckSkipTLS            string
	CheckProxyAllCloud      string
	TipConcurrencyDec       string
	TipConcurrencyInc       string
	TipBypassCache          string
	TipClearCache           string
	TipProxyURL             string
	TipSkipTLS              string
	TipProxyAllCloud        string
	TipProviderToggle       string
	SectionWaterfall        string
	BadgeLocal              string
	BadgeEngine             string
	TipMoveUp               string
	TipMoveDown             string
	BtnResetOrder           string
	TipResetOrder           string
	TipDragReorder          string
	MenuMoveTop             string
	MenuMoveBottom          string
	BtnBrowse               string
	BtnTestConn             string
	BtnDiscoverModels       string
	BtnShowSecret           string
	BtnHideSecret           string
	TipBrowseFile           string
	TipTestConn             string
	TipDiscoverModels       string
	TipToggleSecret         string

	// Sidebar: Locales tab
	SectionPresetProfiles   string
	BtnSelectAll            string
	BtnDeselectAll          string
	BtnSyncBundleLocales    string
	TipSyncBundleLocales    string
	PlaceholderLocaleSearch string
	BadgeInBundle           string
	LabelFilePattern        string
	BtnScaffoldTS           string
	TipSelectAllLocales     string
	TipDeselectAllLocales   string
	TipFilePattern          string
	TipScaffoldTS           string
	TipLocaleEnable         string
	TipSetRef               string
	SectionLocaleListFmt    string
	BtnSetRef               string
	BtnIsRef                string
	TipRemoveLocale         string
	LabelTranslateFrom      string
	TipTranslateFrom        string
	OptSourceDefault        string
	SectionAddLocale        string
	PlaceholderLocaleID     string
	PlaceholderLocaleName   string
	PlaceholderEngineCode   string
	BtnAddLocale            string
	TipAddLocale            string
	// Matrix toolbar & empty states
	FilterAll         string
	FilterGaps        string
	FilterPending     string
	FilterWarnings    string
	ViewModeFocus     string
	ViewModeFull      string
	PlaceholderSearch string
	MenuClear         string
	MenuClearMachine  string
	MenuClearAll      string
	TipStatusFilter   string
	TipMatrixViewMode string
	TipFocusedLocale  string
	TipSearchField    string
	TipMenuClear      string
	EmptyNoDataTitle  string
	EmptyNoDataHint   string
	EmptyNoMatch      string

	// Matrix columns, badges, and origins
	ColContext      string
	ColKeyID        string
	ColSource       string
	ColSourceShort  string
	ColRef          string
	ColRefFmt       string
	ColRefShort     string
	ColTarget       string
	ColTargetFmt    string
	ColStatus       string
	EmptyCell       string
	BadgeWarnFmt    string
	BadgeFillPrefix string
	BadgeOverPrefix string
	BadgeAddPrefix  string
	BadgeSkipped    string
	BadgePending    string
	BadgeReady      string
	TagWarn         string
	TagFill         string
	TagOver         string
	TagAdd          string
	OriginTS        string
	OriginXLSX      string
	OriginTM        string
	OriginGoogle    string
	OriginLLM       string
	OriginManual    string
	OriginCache     string

	// Bottom inspector & logs
	TabInspector        string
	TabLogsFmt          string
	InspectorEmptyHint  string
	LabelKeyIDPrefix    string
	LabelContextPrefix  string
	LabelLocationPrefix string
	LabelCommentPrefix  string
	BadgeNumerus        string
	LabelTargetLocale   string
	LabelSource         string
	LabelRef            string
	LabelNone           string
	LabelEditTargetFmt  string
	BtnTranslateRow     string
	TipTranslateRow     string
	BtnPrevGap          string
	TipPrevGap          string
	BtnNextGap          string
	TipNextGap          string
	BtnCopySrc          string
	BtnUseSrc           string
	BtnResetCell        string
	TipLocalePill       string
	TipCopySrc          string
	TipUseSrc           string
	TipResetCell        string
	HintNumerus         string
	PlaceholderEditor   string
	LabelBeforeXML      string
	LabelAfterXML       string
	LogCountFmt         string
	BtnCopyLogs         string
	BtnClearLogs        string
	TipCopyLogs         string
	TipClearLogs        string

	// Modals
	HelpModalTitle         string
	HelpFallbackMD         string
	ExportModalTitle       string
	LabelExportScope       string
	ExportScopeAll         string
	ExportScopeGaps        string
	ExportScopeChanges     string
	BtnSettings            string
	TipSettings            string
	SettingsModalTitle     string
	SettingsTabGeneral     string
	SettingsTabNetwork     string
	SettingsTabUpdates     string
	LabelUILang            string
	LabelThemeMode         string
	LabelCurrentVersionFmt string
	CheckAutoUpdate        string
	BtnCheckUpdates        string
	BtnCheckingUpdates     string
	StatusUpToDateFmt      string
	StatusUpdateAvailFmt   string
	StatusUpdateErrFmt     string
	BtnInstallUpdate       string
	BtnInstalling          string
	StatusUpdateInstalled  string
	BtnOpenReleasePage     string
	BtnOpenOnboarding      string
	PipelineSummaryFmt     string
	OnboardingTitle        string
	OnboardingSubtitle     string
	OnboardingStep1Title   string
	OnboardingStep1Desc    string
	OnboardingStep2Title   string
	OnboardingStep2Desc    string
	OnboardingStep3Title   string
	OnboardingStep3Desc    string
	BtnOnboardingStart     string
	ExportScopeWarnings    string
	ExportDescAll          string
	ExportDescGaps         string
	ExportDescChanges      string
	ExportDescWarnings     string
	CheckRelevantLocales   string
	BtnExportConfirm       string

	// Actions, native dialogs & background tasks
	DialogOpenTSDir           string
	ErrLoadTSDirFmt           string
	DialogOpenFile            string
	FilterLocFilesName        string
	ErrLoadFilesFmt           string
	DialogImportExcel         string
	FilterExcelName           string
	DialogExportExcel         string
	StatusTestingConn         string
	StatusConnFailedFmt       string
	LogConnFailedFmt          string
	StatusConnOK              string
	LogConnOK                 string
	StatusDiscoverFailedFmt   string
	LogDiscoverFailedFmt      string
	StatusDiscoveredModelsFmt string
	LogDiscoveredModelsFmt    string
	LogConfigSkippedFmt       string
	ErrNoActiveProvider       string
	ErrNoTargetLocale         string
	StatusNoGapsToTranslate   string
	LogStartTranslateFmt      string
	StatusTranslateDoneFmt    string
	LogStoppingTranslate      string
	ErrLoadTSFirst            string
	ErrBuildPatchFmt          string
	LogBuildPatchErrFmt       string
	StatusNoTSChanges         string
	ErrWriteTSFmt             string
	StatusWriteTSDoneFmt      string
	StatusClearedMachineFmt   string
	StatusClearedAll          string
	ErrScaffoldLoadFirst      string
	LogScaffoldErrFmt         string
	StatusAllLocalesHaveTS    string
	StatusScaffoldDoneFmt     string
	ErrEnterLocaleID          string
	ErrLocaleExistsFmt        string
	StatusCopiedLogs          string
	StatusCopiedSrc           string
	StatusTranslateRowDoneFmt string
	StatusNoGapsFound         string
	StatusRowNoGaps           string
	CtxTranslateRow           string
	CtxRetranslateCellFmt     string
	CtxClearCellFmt           string
	CtxClearRow               string
	CtxRevertCellFmt          string
	CtxRevertRow              string
	CtxUseSrcFmt              string
	CtxCopySource             string
	CtxCopyTargetFmt          string
	CtxCopyRef                string
	CtxCopyKeyCtx             string
	CtxFilterContextFmt       string
	CtxClearContextFilter     string
	CtxHeaderFocusFmt         string
	CtxHeaderRevertColFmt     string
	StatusClearedCellFmt      string
	StatusClearedRow          string
	StatusRevertedCellFmt     string
	StatusRevertedRow         string
	StatusRevertedColFmt      string
	StatusCopiedText          string
}

var zhCatalog = Catalog{
	WindowTitle:                "LocLoom",
	BtnClose:                   "关闭",
	BtnCancel:                  "取消",
	DropHint:                   "松开即可加载本地化文件 / 目录 / .zip / .xlsx 词表",
	LogWorkbenchReady:          "已就绪，可拖拽本地化文件 / 目录 / .zip / .xlsx 到窗口，或通过顶部按钮加载项目。",
	ConfirmCloseTitle:          "尚有未保存的本地化变更",
	ConfirmCloseMsgFmt:         "当前有 %d 处待写回变更尚未保存，确定直接关闭吗？",
	ConfirmCloseTranslatingMsg: "并发翻译任务正在运行中，关闭窗口将中止任务并丢失未写回的译文。",
	BtnDiscardAndClose:         "放弃并关闭",

	ErrLoadTSFmt:               "加载本地化项目失败: %v",
	StatusLoadedBundleFmt:      "已加载 %s（%d 个文件，%d 条词条）",
	StatusDiscoveredLocalesFmt: "，自动发现新语种: %v",
	ErrImportExcelFmt:          "导入 Excel 失败: %v",
	StatusImportedExcelFmt:     "已导入 %s（工作表 %s，%d 条译文）",
	ErrNoExportData:            "当前没有可导出的词条数据",
	ErrExportExcelFmt:          "导出 Excel 失败: %v",
	StatusExportedExcelFmt:     "已导出 %d 行到 %s",

	SourceUnloaded:             "未加载项目",
	HeaderSummaryFmt:           "%s · %d 条 · %.0f%%",
	BtnOpenDir:                 "打开目录",
	TipOpenDir:                 "打开本地化目录 (Cmd/Ctrl+O)",
	BtnOpenFile:                "打开文件",
	TipOpenFile:                "打开本地化文件（.ts/.po/.json/.arb/.xcstrings/strings.xml/.resx/.xliff/.zip）",
	BtnImportExcel:             "导入 Excel",
	TipImportExcel:             "导入 .xlsx 翻译词表",
	BtnExportExcel:             "导出 Excel",
	TipExportExcel:             "导出 .xlsx 词表 (Cmd/Ctrl+E)",
	StatusTranslating:          "正在并发翻译",
	BtnStopTranslate:           "停止翻译",
	BtnStartTranslate:          "开始并发翻译",
	TipStopTranslate:           "立即中止当前并发翻译任务（已完成的条目会保留在暂存区）",
	TipStartTranslate:          "按「管线」中的引擎顺序并发翻译待处理词条（受写回策略控制）",
	StatusTranslatingWaveFmt:   "第 %d/%d 轮（级联）",
	StatusTranslatingBannerFmt: "正在并发翻译：%d / %d (%.0f%%)",
	StatusTranslatingRowFmt:    "正在翻译当前词条（%d 个目标语种，按瀑布流引擎顺序执行）...",
	BtnTranslatingRow:          "⚡ 翻译中...",
	BtnWriteTS:                 "写回保存",
	BtnWriteTSCountFmt:         "写回保存 (%d)",
	TipWriteTS:                 "将待写回变更原位无损保存到文件或 ZIP (Cmd/Ctrl+S)",
	ThemeSystem:                "跟随系统",
	ThemeLight:                 "浅色",
	ThemeDark:                  "深色",
	TipTheme:                   "切换外观主题 (跟随系统 / 浅色 / 深色)",
	BtnLangToggle:              "EN",
	TipLangToggle:              "Switch UI to English",
	HeaderLogsFmt:              "日志(%d)",
	TipLogs:                    "查看运行日志与诊断",
	BtnHelp:                    "帮助",
	TipHelp:                    "打开使用指南",

	TabProject:  "项目",
	TabPipeline: "管线",
	TabLocales:  "语种",

	SectionWriteStrategy:   "写回策略",
	DescWriteModeFill:      "仅翻译空缺词条，保留已有译文",
	DescWriteModeOverwrite: "重译全部词条并覆盖已有译文",
	LabelAppendContext:     "新词条默认分组",
	WriteModeFill:          "安全补齐空缺",
	WriteModeOverwrite:     "覆盖已有译文",
	CheckAppendMissing:     "允许从 Excel 追加新词条",
	PlaceholderDefaultCtx:  "留空则归入 Common",
	CheckCreateBackup:      "写回前备份 .bak",
	CheckPluralForms:       "启用复数多词形编辑",
	TipWriteMode:           "安全补齐空缺：仅翻译未完成词条，保留已有译文；\n覆盖已有译文：按当前引擎瀑布流重新翻译所有非消失词条",
	TipAppendMissing:       "勾选后，从 .xlsx 词表导入但原文件中不存在的新词条也会追加写回（默认隔离外部幽灵词条）",
	TipCreateBackup:        "写回修改前在同目录自动生成 .bak 备份文件，防止误操作丢失原文件",
	TipPluralForms:         "对包含复数规则的词条按 One / Other 等分栏单独编辑，避免手动拼接分隔符",
	TipLocaleProgressCard:  "点击切换到「聚焦单语种」并定位该语种",
	SectionContextNavFmt:   "上下文导航 (%d)",
	AllContextsFmt:         "全部上下文 (%d)",
	NoContextLabel:         "(无上下文)",
	SectionLocaleProgress:  "语种进度总览",
	LocaleGapFmt:           "缺 %d",
	LocaleWarnFmt:          "告警 %d",

	SectionConcurrencyCache: "并发与缓存",
	LabelGlobalConcurrency:  "全局并发数",
	CheckBypassCache:        "跳过本地翻译缓存",
	BtnClearCacheFmt:        "清空(%d)",
	StatusCacheCleared:      "已清空本地翻译缓存",
	SectionNetworkProxy:     "网络与代理",
	PlaceholderProxy:        "http://127.0.0.1:8080 或 socks5://127.0.0.1:1080",
	CheckSkipTLS:            "跳过 TLS 证书校验",
	CheckProxyAllCloud:      "对所有云端引擎启用此代理",
	TipConcurrencyDec:       "减少全局并发请求数（1–16）",
	TipConcurrencyInc:       "增加全局并发请求数（1–16）；免费在线引擎建议 2–4 以避免限流",
	TipBypassCache:          "勾选后忽略本地 translation-cache.json 缓存，强制重新请求翻译引擎",
	TipClearCache:           "清空全部本地持久化的翻译缓存记录（不影响当前已加载项目）",
	TipProxyURL:             "支持 http://、https://、socks5:// 代理地址；填写后可一键为云端引擎启用",
	TipSkipTLS:              "仅在公司自签名证书或本地抓包调试时勾选；正常网络请保持关闭",
	TipProxyAllCloud:        "一键开启或关闭所有云端翻译引擎的代理开关（自动跳过 127.0.0.1 / localhost）",
	TipProviderToggle:       "启用或停用该翻译引擎；仅启用的引擎会参与瀑布流",
	SectionWaterfall:        "翻译引擎瀑布流（按顺序回退）",
	BadgeLocal:              "本地",
	BadgeEngine:             "引擎",
	TipMoveUp:               "上移优先级",
	TipMoveDown:             "下移优先级",
	BtnResetOrder:           "重置默认",
	TipResetOrder:           "恢复推荐的默认瀑布流回退顺序",
	TipDragReorder:          "按住卡片拖动可调整回退顺序（右键更多操作）",
	MenuMoveTop:             "移至最前",
	MenuMoveBottom:          "移至最后",
	BtnBrowse:               "浏览",
	BtnTestConn:             "连通测试",
	BtnDiscoverModels:       "发现模型",
	BtnShowSecret:           "显示",
	BtnHideSecret:           "隐藏",
	TipBrowseFile:           "打开系统文件或目录选择器",
	TipTestConn:             "发送一条短测试请求，验证当前引擎连通性与鉴权配置",
	TipDiscoverModels:       "从 API 端点自动拉取可用模型列表",
	TipToggleSecret:         "显示或隐藏密钥明文",

	SectionPresetProfiles:   "语种预设组合",
	BtnSelectAll:            "全选",
	BtnDeselectAll:          "全不选",
	BtnSyncBundleLocales:    "仅选项目语种",
	TipSyncBundleLocales:    "仅启用当前已加载项目中实际包含的目标语种",
	PlaceholderLocaleSearch: "搜索语种 ID 或名称...",
	BadgeInBundle:           "项目内",
	LabelFilePattern:        "文件命名模式",
	BtnScaffoldTS:           "补全缺失语种骨架文件",
	SectionLocaleListFmt:    "语种列表 (%d)",
	BtnSetRef:               "设为参考",
	TipSelectAllLocales:     "启用下方全部目标语种",
	TipDeselectAllLocales:   "停用下方全部目标语种（参考语种除外）",
	TipFilePattern:          "生成缺失语种文件时的命名模板，{id} 会替换为语种 ID（如 zh_CN.ts）",
	TipScaffoldTS:           "为已启用但磁盘尚不存在的语种自动生成骨架文件（.xcstrings 多语种单文件除外）",
	TipLocaleEnable:         "勾选后纳入当前项目的翻译与写回范围",
	TipSetRef:               "设为主参考语种：其译文会作为参考列显示，协助其他语种对照翻译",
	BtnIsRef:                "★ 参考",
	TipRemoveLocale:         "移除语种",
	LabelTranslateFrom:      "译自",
	TipTranslateFrom:        "指定该语种的翻译源：默认用文件原文；改为其他语种时，用该语种已有译文作为源文翻译（如 zh_TW 选 zh_CN 作简繁转换）",
	OptSourceDefault:        "默认（文件原文）",
	SectionAddLocale:        "添加自定义语种",
	PlaceholderLocaleID:     "语种 ID (如 nl_NL)",
	PlaceholderLocaleName:   "名称 (如 荷兰语)",
	PlaceholderEngineCode:   "引擎码 (nl)",
	BtnAddLocale:            "添加语种",
	TipAddLocale:            "添加一条自定义目标语种到配置列表",

	FilterAll:         "全部",
	FilterGaps:        "待补齐",
	FilterPending:     "待写入",
	FilterWarnings:    "有告警",
	ViewModeFocus:     "聚焦单语种",
	ViewModeFull:      "全景矩阵",
	PlaceholderSearch: "搜索原文、上下文、译文 (Cmd/Ctrl+F)",
	MenuClear:         "清空...",
	MenuClearMachine:  "清空机翻结果",
	MenuClearAll:      "清空全部暂存译文",
	TipStatusFilter:   "按状态筛选词条：全部 / 仅待补齐 / 仅待写入 / 仅含占位符或空白告警",
	TipMatrixViewMode: "聚焦单语种：只看当前目标语种与状态列；全景矩阵：横向对比全部已启用语种",
	TipFocusedLocale:  "选择「聚焦单语种」模式下要查看与编辑的目标语种",
	TipSearchField:    "按上下文、Key ID、原文、参考译文或目标译文实时过滤 (Cmd/Ctrl+F)",
	TipMenuClear:      "批量清空当前工作区的机翻或全部暂存译文（磁盘文件不受影响，直到点击写回保存）",
	EmptyNoDataTitle:  "尚未加载词条数据",
	EmptyNoDataHint:   "请点击顶部「打开目录」/「打开文件」/「导入 Excel」，或直接将文件拖拽到窗口",
	EmptyNoMatch:      "当前筛选条件下没有匹配词条",

	ColContext:      "上下文",
	ColKeyID:        "键名 / 上下文",
	ColSource:       "英文原文 (Source)",
	ColSourceShort:  "英文原文",
	ColRef:          "参考译文",
	ColRefFmt:       "参考 (%s)",
	ColRefShort:     "参考",
	ColTarget:       "目标译文",
	ColTargetFmt:    "目标译文 (%s)",
	ColStatus:       "状态/来源",
	EmptyCell:       "(空)",
	BadgeWarnFmt:    "⚠ 告警(%d)",
	BadgeFillPrefix: "补齐 · ",
	BadgeOverPrefix: "覆盖 · ",
	BadgeAddPrefix:  "新增 · ",
	BadgeSkipped:    "已完成 (跳过)",
	BadgePending:    "待补齐",
	BadgeReady:      "已就绪",
	TagWarn:         "[⚠]",
	TagFill:         "[补]",
	TagOver:         "[改]",
	TagAdd:          "[新]",
	OriginTS:        "源文件",
	OriginXLSX:      "Excel",
	OriginTM:        "语料库",
	OriginGoogle:    "Google",
	OriginLLM:       "LLM",
	OriginManual:    "手工",
	OriginCache:     "缓存",

	TabInspector:           "词条精修与格式对比",
	TabLogsFmt:             "运行日志与诊断 (%d)",
	InspectorEmptyHint:     "在上方表格中点击任意词条行，即可在此处精修译文并预览格式回填差异",
	LabelKeyIDPrefix:       "键名: ",
	LabelContextPrefix:     "上下文: ",
	LabelLocationPrefix:    "位置: ",
	LabelCommentPrefix:     "注释: ",
	BadgeNumerus:           "复数 Numerus (使用 --- 分隔)",
	LabelTargetLocale:      "目标语种:",
	LabelSource:            "英文原文 (Source)",
	LabelRef:               "参考译文",
	LabelNone:              "(无)",
	LabelEditTargetFmt:     "译文编辑 (%s)",
	BtnTranslateRow:        "⚡ 翻译此条",
	TipTranslateRow:        "使用当前激活的翻译引擎即时翻译此行",
	BtnPrevGap:             "◀ 上一待办",
	TipPrevGap:             "跳转到上一个缺口或告警行 (Alt+Up)",
	BtnNextGap:             "下一待办 ▶",
	TipNextGap:             "跳转到下一个缺口或告警行 (Alt+Down / F3)",
	BtnCopySrc:             "复制原文",
	BtnUseSrc:              "填入原文",
	BtnResetCell:           "重置该格",
	TipLocalePill:          "点击切换下方编辑器当前编辑的目标语种",
	TipCopySrc:             "将英文原文复制到系统剪贴板",
	TipUseSrc:              "直接将英文原文填入当前译文框（适合品牌名、代码或无需翻译的词条）",
	TipResetCell:           "撤销当前单元格的未保存修改，还原回文件原始值",
	HintNumerus:            "提示：此词条包含复数形式，请使用单独一行 --- 分隔各个复数译文。",
	PlaceholderEditor:      "输入译文（实时保存，O(1) 刷新单元格）...",
	LabelBeforeXML:         "修改前片段 (Before)",
	LabelAfterXML:          "回填后片段 (After)",
	LogCountFmt:            "共 %d 条运行日志",
	BtnCopyLogs:            "复制日志",
	BtnClearLogs:           "清空日志",
	BtnSettings:            "设置",
	TipSettings:            "打开全局设置（外观、语言、网络代理、缓存与自动更新）",
	SettingsModalTitle:     "LocLoom 设置",
	SettingsTabGeneral:     "通用与外观",
	SettingsTabNetwork:     "代理与缓存",
	SettingsTabUpdates:     "更新与引导",
	LabelUILang:            "界面语言",
	LabelThemeMode:         "外观主题",
	LabelCurrentVersionFmt: "当前版本：v%s",
	CheckAutoUpdate:        "启动时自动检查更新",
	BtnCheckUpdates:        "立即检查更新",
	BtnCheckingUpdates:     "正在检查...",
	StatusUpToDateFmt:      "已是最新版本 (v%s)",
	StatusUpdateAvailFmt:   "发现新版本 v%s（当前 v%s）",
	StatusUpdateErrFmt:     "检查更新失败：%v",
	BtnInstallUpdate:       "安装并重启",
	BtnInstalling:          "正在安装更新...",
	StatusUpdateInstalled:  "更新已安装，正在重启...",
	BtnOpenReleasePage:     "前往下载新版本",
	BtnOpenOnboarding:      "重新查看新手引导",
	PipelineSummaryFmt:     "并发 %d · 缓存 %d 条 · %s",
	OnboardingTitle:        "欢迎使用 LocLoom",
	OnboardingSubtitle:     "原位无损本地化工作台 · 3 步快速上手",
	OnboardingStep1Title:   "1. 加载项目文件",
	OnboardingStep1Desc:    "点击顶部「打开目录 / 打开文件」，或直接将文件夹 / .zip / .xlsx 拖入窗口。支持 .ts、.po、.json、.arb、.xcstrings、strings.xml、.resx、.xliff。",
	OnboardingStep2Title:   "2. 配置引擎并并发翻译",
	OnboardingStep2Desc:    "在左侧「管线」启用免费在线源（Google / Bing / LibreTranslate / MyMemory）或自有 API，点击顶部「开始并发翻译」。",
	OnboardingStep3Title:   "3. 精修校对并写回保存",
	OnboardingStep3Desc:    "在表格或底部精修区检查译文与 [⚠] 占位符告警，确认无误后点击「写回保存」原位无损写入文件。",
	BtnOnboardingStart:     "开始使用",
	TipCopyLogs:            "复制全部运行日志到剪贴板",
	TipClearLogs:           "清空当前面板日志",

	HelpModalTitle:      "LocLoom 使用指南",
	HelpFallbackMD:      "# LocLoom\n\n跨平台多语言并发翻译与原位无损回填工具。",
	ExportModalTitle:    "导出 Excel 词条表 (.xlsx)",
	LabelExportScope:    "选择导出范围：",
	ExportScopeAll:      "全部词条",
	ExportScopeGaps:     "仅未翻译",
	ExportScopeChanges:  "仅待写入变更",
	ExportScopeWarnings: "仅含告警",
	ExportDescAll:       "导出当前矩阵中的全部词条（含原文、参考译文与各语种译文）。",
	ExportDescGaps:      "仅导出至少有一个已启用语种仍为空缺的词条，方便分发外包翻译。",
	ExportDescChanges:   "仅导出本次会写回 .ts 文件的补齐、覆盖或新增词条。",
	ExportDescWarnings:  "仅导出存在 Qt 占位符缺失、多余或结构告警的词条，告警内容以批注附在对应单元格上（悬停可见）。",
	CheckRelevantLocales: "仅导出相关语种的列（去掉无匹配词条的语种，参考语保留）",
	BtnExportConfirm:    "选择路径并导出...",

	DialogOpenTSDir:           "选择本地化项目目录",
	ErrLoadTSDirFmt:           "加载目录失败: %v",
	DialogOpenFile:            "选择本地化文件或 ZIP 压缩包",
	FilterLocFilesName:        "本地化文件与 ZIP",
	ErrLoadFilesFmt:           "加载文件失败: %v",
	DialogImportExcel:         "导入 Excel 翻译词表 (.xlsx)",
	FilterExcelName:           "Excel 工作簿",
	DialogExportExcel:         "导出 Excel 词条表",
	StatusTestingConn:         "正在测试连通性...",
	StatusConnFailedFmt:       "失败: %v",
	LogConnFailedFmt:          "连通测试失败: %v",
	StatusConnOK:              "连通正常",
	LogConnOK:                 "连通测试通过",
	StatusDiscoverFailedFmt:   "发现模型失败: %v",
	LogDiscoverFailedFmt:      "发现模型失败: %v",
	StatusDiscoveredModelsFmt: "已发现 %d 个模型",
	LogDiscoveredModelsFmt:    "发现 %d 个可用模型",
	LogConfigSkippedFmt:       "配置跳过: %v",
	ErrNoActiveProvider:       "请先在「管线」选项卡中启用至少一个翻译提供者",
	ErrNoTargetLocale:         "请先在「语种」选项卡中启用至少一个目标语种",
	StatusNoGapsToTranslate:   "当前没有需要翻译的空缺词条",
	LogStartTranslateFmt:      "开始并发翻译：%d 个语种，共 %d 条待译词条（并发度 %d）",
	StatusTranslateDoneFmt:    "并发翻译完成：新增 %d 条译文，耗时 %s",
	LogStoppingTranslate:      "正在取消并发翻译任务...",
	ErrLoadTSFirst:            "请先加载本地化文件、目录或 .zip 压缩包",
	ErrBuildPatchFmt:          "生成补丁失败 (%s): %v",
	LogBuildPatchErrFmt:       "生成补丁失败: %v",
	StatusNoTSChanges:         "没有需要写回的变更",
	ErrWriteTSFmt:             "写回保存失败: %v",
	StatusWriteTSDoneFmt:      "写回完成：%d 个文件（更新 %d 条，追加 %d 条）-> %s",
	StatusClearedMachineFmt:   "已清空 %d 条机翻暂存译文",
	StatusClearedAll:          "已清空全部工作区暂存译文",
	ErrScaffoldLoadFirst:      "请先加载项目再补全骨架文件",
	LogScaffoldErrFmt:         "生成骨架失败: %v",
	StatusAllLocalesHaveTS:    "所有已启用语种均已有对应文件",
	StatusScaffoldDoneFmt:     "已为 %d 个缺失语种生成骨架文件",
	ErrEnterLocaleID:          "请输入语种 ID（例如 nl_NL）",
	ErrLocaleExistsFmt:        "语种 ID 已存在: %s",
	StatusCopiedLogs:          "已复制运行日志到剪贴板",
	StatusCopiedSrc:           "已复制原文到剪贴板",
	StatusTranslateRowDoneFmt: "单行翻译完成：更新 %d 个语种",
	StatusNoGapsFound:         "未找到待办或告警行",
	StatusRowNoGaps:           "当前行所有已启用语种均已有译文",
	CtxTranslateRow:           "⚡ 翻译此行（所有待补语种）",
	CtxRetranslateCellFmt:     "⚡ 重新翻译此格 (%s)",
	CtxClearCellFmt:           "清空此格译文 (%s)",
	CtxClearRow:               "清空整行所有语种译文",
	CtxRevertCellFmt:          "还原此格至文件原值 (%s)",
	CtxRevertRow:              "还原整行至文件原值",
	CtxUseSrcFmt:              "将原文填入此格 (%s)",
	CtxCopySource:             "复制英文原文",
	CtxCopyTargetFmt:          "复制此格译文 (%s)",
	CtxCopyRef:                "复制参考译文",
	CtxCopyKeyCtx:             "复制键名 / 上下文",
	CtxFilterContextFmt:       "仅看此上下文 (%s)",
	CtxClearContextFilter:     "取消上下文筛选（显示全部）",
	CtxHeaderFocusFmt:         "聚焦此语种 (%s)",
	CtxHeaderRevertColFmt:     "还原此列全部暂存变更 (%s)",
	StatusClearedCellFmt:      "已清空 %s 单元格译文",
	StatusClearedRow:          "已清空当前行所有目标语种译文",
	StatusRevertedCellFmt:     "已还原 %s 单元格至文件原值",
	StatusRevertedRow:         "已还原当前行至文件原值",
	StatusRevertedColFmt:      "已还原 %s 列全部暂存变更",
	StatusCopiedText:          "已复制到剪贴板",
}

var enCatalog = Catalog{
	WindowTitle:                "LocLoom",
	BtnClose:                   "Close",
	BtnCancel:                  "Cancel",
	DropHint:                   "Drop to load localization file(s) / folder / .zip / .xlsx",
	LogWorkbenchReady:          "Ready. Drag & drop localization files, folder, .zip, or .xlsx onto the window, or use the top toolbar buttons.",
	ConfirmCloseTitle:          "Unsaved Localization Changes",
	ConfirmCloseMsgFmt:         "You have %d unsaved change(s). Close without saving?",
	ConfirmCloseTranslatingMsg: "A translation job is still running. Closing now will abort it and discard unsaved translations.",
	BtnDiscardAndClose:         "Discard & Close",

	ErrLoadTSFmt:               "Failed to load localization project: %v",
	StatusLoadedBundleFmt:      "Loaded %s (%d files, %d entries)",
	StatusDiscoveredLocalesFmt: ", auto-discovered locales: %v",
	ErrImportExcelFmt:          "Failed to import Excel: %v",
	StatusImportedExcelFmt:     "Imported %s (sheet %s, %d translations)",
	ErrNoExportData:            "No entries available to export",
	ErrExportExcelFmt:          "Failed to export Excel: %v",
	StatusExportedExcelFmt:     "Exported %d rows to %s",

	SourceUnloaded:             "Unloaded",
	HeaderSummaryFmt:           "%s · %d items · %.0f%%",
	BtnOpenDir:                 "Open Dir",
	TipOpenDir:                 "Open localization folder (Cmd/Ctrl+O)",
	BtnOpenFile:                "Open File",
	TipOpenFile:                "Open localization file(s) (.ts/.po/.json/.arb/.xcstrings/strings.xml/.resx/.xliff/.zip)",
	BtnImportExcel:             "Import Excel",
	TipImportExcel:             "Import .xlsx translation workbook",
	BtnExportExcel:             "Export Excel",
	TipExportExcel:             "Export .xlsx workbook (Cmd/Ctrl+E)",
	StatusTranslating:          "Translating",
	BtnStopTranslate:           "Stop",
	BtnStartTranslate:          "Start Translate",
	TipStopTranslate:           "Abort the running translation job (completed items stay staged)",
	TipStartTranslate:          "Run concurrent translation across enabled providers in waterfall order",
	StatusTranslatingWaveFmt:   "Cascade wave %d/%d",
	StatusTranslatingBannerFmt: "Translating: %d / %d (%.0f%%)",
	StatusTranslatingRowFmt:    "Translating current row (%d target locale(s) via waterfall)...",
	BtnTranslatingRow:          "⚡ Translating...",
	BtnWriteTS:                 "Save Back",
	BtnWriteTSCountFmt:         "Save Back (%d)",
	TipWriteTS:                 "Save pending changes losslessly to files or ZIP (Cmd/Ctrl+S)",
	ThemeSystem:                "System",
	ThemeLight:                 "Light",
	ThemeDark:                  "Dark",
	TipTheme:                   "Cycle theme (System / Light / Dark)",
	BtnLangToggle:              "中文",
	TipLangToggle:              "切换界面为中文",
	HeaderLogsFmt:              "Logs(%d)",
	TipLogs:                    "View logs & diagnostics",
	BtnHelp:                    "Help",
	TipHelp:                    "Open usage guide",

	TabProject:  "Project",
	TabPipeline: "Pipeline",
	TabLocales:  "Locales",

	SectionWriteStrategy:   "Write Strategy",
	WriteModeFill:          "Fill Gaps Only",
	WriteModeOverwrite:     "Overwrite Existing",
	DescWriteModeFill:      "Fills empty entries only; keeps existing translations",
	DescWriteModeOverwrite: "Re-translates all entries and overwrites existing ones",
	LabelAppendContext:     "Default group for new keys",
	CheckAppendMissing:     "Allow Adding New Keys from Excel",
	PlaceholderDefaultCtx:  "Blank = Common",
	CheckCreateBackup:      "Backup .bak before write",
	CheckPluralForms:       "Enable Plural Forms Editing",
	TipWriteMode:           "Fill Gaps Only: keeps existing translations and fills gaps;\nOverwrite Existing: re-translates all non-vanished entries",
	TipAppendMissing:       "When enabled, new keys from .xlsx absent from source files are appended on Save Back",
	TipCreateBackup:        "Create a .bak backup alongside each modified file before writing back",
	TipPluralForms:         "Edit plural forms (One / Other / etc.) in separate labeled fields",
	TipLocaleProgressCard:  "Click to focus and edit this locale in the matrix",
	SectionContextNavFmt:   "Contexts (%d)",
	AllContextsFmt:         "All Contexts (%d)",
	NoContextLabel:         "(No Context)",
	SectionLocaleProgress:  "Locale Progress",
	LocaleGapFmt:           "Gap %d",
	LocaleWarnFmt:          "Warn %d",

	SectionConcurrencyCache: "Concurrency & Cache",
	LabelGlobalConcurrency:  "Concurrency",
	CheckBypassCache:        "Bypass local cache",
	BtnClearCacheFmt:        "Clear(%d)",
	StatusCacheCleared:      "Local translation cache cleared",
	SectionNetworkProxy:     "Network & Proxy",
	PlaceholderProxy:        "http://127.0.0.1:8080 or socks5://127.0.0.1:1080",
	CheckSkipTLS:            "Skip TLS verification",
	CheckProxyAllCloud:      "Route all cloud engines through this proxy",
	TipConcurrencyDec:       "Decrease concurrent translation workers (1–16)",
	TipConcurrencyInc:       "Increase concurrent translation workers (1–16); 2–4 recommended for free web engines",
	TipBypassCache:          "Ignore local translation-cache.json and force fresh engine requests",
	TipClearCache:           "Clear all locally persisted translation cache entries",
	TipProxyURL:             "HTTP / HTTPS / SOCKS5 proxy URL; enables one-click proxy toggle for cloud engines",
	TipSkipTLS:              "Skip TLS verification (only for self-signed corporate proxies or debugging)",
	TipProxyAllCloud:        "Toggle proxy usage for all cloud engines (skips localhost)",
	TipProviderToggle:       "Enable or disable this provider in the translation waterfall",
	SectionWaterfall:        "Waterfall Providers (Priority Order)",
	BadgeLocal:              "Local",
	BadgeEngine:             "Cloud",
	TipMoveUp:               "Move up",
	TipMoveDown:             "Move down",
	BtnResetOrder:           "Reset",
	TipResetOrder:           "Restore recommended default waterfall order",
	TipDragReorder:          "Drag cards to reorder fallback priority (right-click for more)",
	MenuMoveTop:             "Move to Top",
	MenuMoveBottom:          "Move to Bottom",
	BtnBrowse:               "Browse",
	BtnTestConn:             "Test",
	BtnDiscoverModels:       "Models",
	BtnShowSecret:           "Show",
	BtnHideSecret:           "Hide",
	TipBrowseFile:           "Open system file or folder picker",
	TipTestConn:             "Send a quick probe request to verify connectivity and credentials",
	TipDiscoverModels:       "Fetch available model IDs from the API endpoint",
	TipToggleSecret:         "Show or hide secret value",

	SectionPresetProfiles:   "Preset Profiles",
	BtnSelectAll:            "Select All",
	BtnDeselectAll:          "Deselect All",
	BtnSyncBundleLocales:    "Sync Bundle",
	TipSyncBundleLocales:    "Enable only target locales present in the loaded project",
	PlaceholderLocaleSearch: "Filter locales...",
	BadgeInBundle:           "In Bundle",
	LabelFilePattern:        "Filename Pattern",
	BtnScaffoldTS:           "Scaffold Missing Locale Files",
	SectionLocaleListFmt:    "Locales (%d)",
	BtnSetRef:               "Set Ref",
	BtnIsRef:                "★ Ref",
	TipSelectAllLocales:     "Enable all target locales below",
	TipDeselectAllLocales:   "Disable all target locales below (except reference)",
	TipFilePattern:          "Filename template for scaffolding missing locales ({id} expands to locale ID)",
	TipScaffoldTS:           "Generate skeleton files for enabled locales missing on disk (N/A for single-file multi-locale formats)",
	TipLocaleEnable:         "Include this locale in translation and write-back scope",
	TipSetRef:               "Set as reference locale shown alongside source text for cross-checking",
	TipRemoveLocale:         "Remove locale",
	LabelTranslateFrom:      "From",
	TipTranslateFrom:        "Pick the source text for this locale: the bundle's source by default, or another enabled locale's existing translations (e.g. zh_TW from zh_CN for Simplified→Traditional)",
	OptSourceDefault:        "Default (file source)",
	SectionAddLocale:        "Add Custom Locale",
	PlaceholderLocaleID:     "Locale ID (e.g. nl_NL)",
	PlaceholderLocaleName:   "Name (e.g. Dutch)",
	PlaceholderEngineCode:   "Code (nl)",
	BtnAddLocale:            "Add Locale",
	TipAddLocale:            "Add a custom target locale to your configuration",

	FilterAll:         "All",
	FilterGaps:        "Gaps",
	FilterPending:     "Pending",
	FilterWarnings:    "Warnings",
	ViewModeFocus:     "Focus Locale",
	ViewModeFull:      "Full Matrix",
	PlaceholderSearch: "Search source, context, translation (Cmd/Ctrl+F)",
	MenuClear:         "Clear...",
	MenuClearMachine:  "Clear Machine Translations",
	MenuClearAll:      "Clear All Staged Translations",
	TipStatusFilter:   "Filter rows by status: All / Gaps / Pending Save / Warnings",
	TipMatrixViewMode: "Focus: single target locale + status; Full Matrix: all enabled locales side-by-side",
	TipFocusedLocale:  "Select target locale for Focus view",
	TipSearchField:    "Filter rows by context, key, source, reference, or target text (Cmd/Ctrl+F)",
	TipMenuClear:      "Clear staged machine or all translations in memory (files stay untouched until Save Back)",
	EmptyNoDataTitle:  "No entries loaded",
	EmptyNoDataHint:   "Click Open Dir / Open File / Import Excel above, or drag & drop files here",
	EmptyNoMatch:      "No entries match the current filter",

	ColContext:      "Context",
	ColKeyID:        "Key / Context",
	ColSource:       "Source",
	ColSourceShort:  "Source",
	ColRef:          "Ref",
	ColRefFmt:       "Ref (%s)",
	ColRefShort:     "Ref",
	ColTarget:       "Target",
	ColTargetFmt:    "Target (%s)",
	ColStatus:       "Status",
	EmptyCell:       "(Empty)",
	BadgeWarnFmt:    "⚠ Warn(%d)",
	BadgeFillPrefix: "Fill · ",
	BadgeOverPrefix: "Over · ",
	BadgeAddPrefix:  "Add · ",
	BadgeSkipped:    "Done (Skip)",
	BadgePending:    "Pending",
	BadgeReady:      "Ready",
	TagWarn:         "[⚠]",
	TagFill:         "[Fill]",
	TagOver:         "[Over]",
	TagAdd:          "[Add]",
	OriginTS:        "Source",
	OriginXLSX:      "Excel",
	OriginTM:        "TM",
	OriginGoogle:    "Google",
	OriginLLM:       "LLM",
	OriginManual:    "Manual",
	OriginCache:     "Cache",

	TabInspector:           "Inspector & Format Diff",
	TabLogsFmt:             "Logs & Diagnostics (%d)",
	InspectorEmptyHint:     "Click any entry row above to edit its translation and preview the format diff",
	LabelKeyIDPrefix:       "Key: ",
	LabelContextPrefix:     "Context: ",
	LabelLocationPrefix:    "Location: ",
	LabelCommentPrefix:     "Comment: ",
	BadgeNumerus:           "Plural Numerus (separate with ---)",
	LabelTargetLocale:      "Target Locale:",
	LabelSource:            "Source",
	LabelRef:               "Reference",
	LabelNone:              "(None)",
	LabelEditTargetFmt:     "Translation (%s)",
	BtnTranslateRow:        "⚡ Translate Row",
	TipTranslateRow:        "Translate this row immediately using active providers",
	BtnPrevGap:             "◀ Prev Gap",
	TipPrevGap:             "Jump to previous gap or warning row (Alt+Up)",
	BtnNextGap:             "Next Gap ▶",
	TipNextGap:             "Jump to next gap or warning row (Alt+Down / F3)",
	BtnCopySrc:             "Copy Src",
	BtnUseSrc:              "Use Src",
	BtnResetCell:           "Reset Cell",
	TipLocalePill:          "Switch active target locale in the editor below",
	TipCopySrc:             "Copy source text to clipboard",
	TipUseSrc:              "Fill editor with source text (useful for brand names, identifiers, or untranslatable strings)",
	TipResetCell:           "Discard unsaved edits on this cell and revert to file original",
	HintNumerus:            "Tip: This entry has plural forms. Separate each form with a line containing only ---.",
	PlaceholderEditor:      "Enter translation (auto-saved, O(1) cell refresh)...",
	LabelBeforeXML:         "Before",
	LabelAfterXML:          "After",
	LogCountFmt:            "%d log entries",
	BtnCopyLogs:            "Copy Logs",
	BtnClearLogs:           "Clear Logs",
	TipCopyLogs:            "Copy all logs to clipboard",
	TipClearLogs:           "Clear log panel",
	BtnSettings:            "Settings",
	TipSettings:            "Open global settings (appearance, language, proxy, cache, and updates)",
	SettingsModalTitle:     "LocLoom Settings",
	SettingsTabGeneral:     "General",
	SettingsTabNetwork:     "Proxy & Cache",
	SettingsTabUpdates:     "Updates & Guide",
	LabelUILang:            "Interface Language",
	LabelThemeMode:         "Theme",
	LabelCurrentVersionFmt: "Current version: v%s",
	CheckAutoUpdate:        "Check for updates on startup",
	BtnCheckUpdates:        "Check for Updates",
	BtnCheckingUpdates:     "Checking...",
	StatusUpToDateFmt:      "You're up to date (v%s)",
	StatusUpdateAvailFmt:   "Update available: v%s (current v%s)",
	StatusUpdateErrFmt:     "Update check failed: %v",
	BtnInstallUpdate:       "Install & Relaunch",
	BtnInstalling:          "Installing update...",
	StatusUpdateInstalled:  "Update installed, relaunching...",
	BtnOpenReleasePage:     "Download Latest Release",
	BtnOpenOnboarding:      "Show Onboarding Guide",
	PipelineSummaryFmt:     "Workers %d · Cache %d · %s",
	OnboardingTitle:        "Welcome to LocLoom",
	OnboardingSubtitle:     "Lossless Localization Workbench · 3-Step Quick Start",
	OnboardingStep1Title:   "1. Load Your Project",
	OnboardingStep1Desc:    "Click Open Dir / Open File above, or drop a folder / .zip / .xlsx onto the window. Supports .ts, .po, .json, .arb, .xcstrings, strings.xml, .resx, .xliff.",
	OnboardingStep2Title:   "2. Configure Engines & Translate",
	OnboardingStep2Desc:    "Enable free web engines (Google / Bing / LibreTranslate / MyMemory) or your own API in Pipeline, then click Start Translate.",
	OnboardingStep3Title:   "3. Review & Save Back",
	OnboardingStep3Desc:    "Review translations and [⚠] placeholder warnings in the matrix or inspector, then click Save Back to patch files losslessly.",
	BtnOnboardingStart:     "Get Started",

	HelpModalTitle:      "LocLoom User Guide",
	HelpFallbackMD:      "# LocLoom\n\nCross-platform concurrent localization and lossless patching workbench.",
	ExportModalTitle:    "Export Excel Workbook (.xlsx)",
	LabelExportScope:    "Export Scope:",
	ExportScopeAll:      "All Entries",
	ExportScopeGaps:     "Gaps Only",
	ExportScopeChanges:  "Pending Changes",
	ExportScopeWarnings: "Warnings Only",
	ExportDescAll:       "Export all entries in the matrix (source, reference, and all target locales).",
	ExportDescGaps:      "Export only entries with at least one missing enabled locale for external translation.",
	ExportDescChanges:   "Export only entries with staged fill, overwrite, or append changes.",
	ExportDescWarnings:  "Export only entries with Qt placeholder or structural validation warnings; each warning is attached as a cell comment (hover to read).",
	CheckRelevantLocales: "Relevant locales only (drop columns with no matching entries; reference stays)",
	BtnExportConfirm:    "Choose Path & Export...",

	DialogOpenTSDir:           "Select Localization Folder",
	ErrLoadTSDirFmt:           "Failed to load folder: %v",
	DialogOpenFile:            "Select Localization File(s) or ZIP",
	FilterLocFilesName:        "Localization Files & ZIP",
	ErrLoadFilesFmt:           "Failed to load file(s): %v",
	DialogImportExcel:         "Import Excel Workbook (.xlsx)",
	FilterExcelName:           "Excel Workbook",
	DialogExportExcel:         "Export Excel Workbook",
	StatusTestingConn:         "Testing connection...",
	StatusConnFailedFmt:       "Failed: %v",
	LogConnFailedFmt:          "Connection test failed: %v",
	StatusConnOK:              "Connected",
	LogConnOK:                 "Connection test passed",
	StatusDiscoverFailedFmt:   "Model discovery failed: %v",
	LogDiscoverFailedFmt:      "Model discovery failed: %v",
	StatusDiscoveredModelsFmt: "Found %d models",
	LogDiscoveredModelsFmt:    "Discovered %d available models",
	LogConfigSkippedFmt:       "Config skipped: %v",
	ErrNoActiveProvider:       "Enable at least one provider in the Pipeline tab first",
	ErrNoTargetLocale:         "Enable at least one target locale in the Locales tab first",
	StatusNoGapsToTranslate:   "No missing entries to translate",
	LogStartTranslateFmt:      "Starting translation: %d locales, %d entries (concurrency %d)",
	StatusTranslateDoneFmt:    "Translation complete: %d entries added in %s",
	LogStoppingTranslate:      "Cancelling translation tasks...",
	ErrLoadTSFirst:            "Load a localization file, folder, or .zip archive first",
	ErrBuildPatchFmt:          "Failed to build patch (%s): %v",
	LogBuildPatchErrFmt:       "Failed to build patch: %v",
	StatusNoTSChanges:         "No changes to save back",
	ErrWriteTSFmt:             "Save back failed: %v",
	StatusWriteTSDoneFmt:      "Write complete: %d files (%d updated, %d appended) -> %s",
	StatusClearedMachineFmt:   "Cleared %d machine translations",
	StatusClearedAll:          "Cleared all staged translations",
	ErrScaffoldLoadFirst:      "Load a project before scaffolding",
	LogScaffoldErrFmt:         "Scaffold failed: %v",
	StatusAllLocalesHaveTS:    "All enabled locales already have files",
	StatusScaffoldDoneFmt:     "Scaffolded %d missing locale file(s)",
	ErrEnterLocaleID:          "Enter a locale ID (e.g. nl_NL)",
	ErrLocaleExistsFmt:        "Locale ID already exists: %s",
	StatusCopiedLogs:          "Logs copied to clipboard",
	StatusCopiedSrc:           "Copied source text to clipboard",
	StatusTranslateRowDoneFmt: "Row translated: updated %d locale(s)",
	StatusNoGapsFound:         "No gap or warning rows found",
	StatusRowNoGaps:           "Current row has no gaps for enabled locales",
	CtxTranslateRow:           "⚡ Translate Row (All Gaps)",
	CtxRetranslateCellFmt:     "⚡ Retranslate Cell (%s)",
	CtxClearCellFmt:           "Clear Cell Translation (%s)",
	CtxClearRow:               "Clear Entire Row (All Locales)",
	CtxRevertCellFmt:          "Revert Cell to File (%s)",
	CtxRevertRow:              "Revert Entire Row to File",
	CtxUseSrcFmt:              "Fill Source into Cell (%s)",
	CtxCopySource:             "Copy Source Text",
	CtxCopyTargetFmt:          "Copy Cell Translation (%s)",
	CtxCopyRef:                "Copy Reference Text",
	CtxCopyKeyCtx:             "Copy Key / Context",
	CtxFilterContextFmt:       "Filter by Context (%s)",
	CtxClearContextFilter:     "Show All Contexts",
	CtxHeaderFocusFmt:         "Focus Locale (%s)",
	CtxHeaderRevertColFmt:     "Revert All Staged in Column (%s)",
	StatusClearedCellFmt:      "Cleared translation for %s",
	StatusClearedRow:          "Cleared translations across all target locales for row",
	StatusRevertedCellFmt:     "Reverted %s cell to file original",
	StatusRevertedRow:         "Reverted row to file original",
	StatusRevertedColFmt:      "Reverted all staged changes in %s",
	StatusCopiedText:          "Copied to clipboard",
}
