package format

import (
	"sync"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/tsxml"
)

// FormatID identifies a supported localization file format.
type FormatID string

const (
	FormatQtTS       FormatID = "qt-ts"
	FormatGettextPO  FormatID = "gettext-po"
	FormatJSON       FormatID = "json-i18n"
	FormatARB        FormatID = "flutter-arb"
	FormatXCStrings  FormatID = "apple-xcstrings"
	FormatAndroidXML FormatID = "android-xml"
	FormatResX       FormatID = "dotnet-resx"
	FormatXLIFF      FormatID = "xliff"
)

// Topology describes how locales map to files on disk.
type Topology int

const (
	TopologyPerLocale Topology = iota
	TopologyMultiLocale
)

// NormalizedEntry represents a single parsed localization entry from any format.
type NormalizedEntry struct {
	Key          domain.EntryKey
	LocaleID     string
	Text         string
	Numerus      bool
	PluralForms  []string
	PluralKeys   []string
	Status       domain.TransStatus
	Location     string
	ExtraComment string
}

// DocRecord represents one parsed localization file (on disk or inside a .zip archive).
type DocRecord struct {
	FormatID   FormatID
	FilePath   string
	RelPath    string
	FileName   string
	LocaleID   string
	Locales    []string
	SourceLang string
	IsTemplate bool
	HasBOM     bool
	LineEnding string
	Raw        []byte
	Entries    []NormalizedEntry
	Opaque     any
}

// Bundle represents a loaded localization project (single file, directory, or .zip archive).
type Bundle struct {
	Format       FormatID
	Topology     Topology
	SourcePath   string
	IsZip        bool
	IsSingleFile bool
	SourceLocale string
	Locales      []string
	Docs         []*DocRecord
	ZipExtras    map[string][]byte
	TSBundle     *tsxml.Bundle
}

// PatchOp describes a surgical translation update or append operation on a target locale.
type PatchOp struct {
	Key         domain.EntryKey
	LocaleID    string
	NewText     string
	PluralForms []string
	Action      domain.PatchActionType
	Numerus     bool
}

// SaveOptions controls backup creation and output path behavior.
type SaveOptions struct {
	CreateBackup bool
	OutputPath   string
}

// SaveReport summarizes written files and backup paths after Save.
type SaveReport struct {
	OutputPath   string
	FilesWritten int
	BackupDir    string
	BackupPaths  []string
}

// Adapter is the contract implemented by each pluggable localization format driver.
type Adapter interface {
	ID() FormatID
	Name(lang string) string
	Topology() Topology
	IDAsKey() bool
	Extensions() []string
	// MatchPath returns a positive specificity score (1..100) if relPath/raw belongs to this format, or 0 otherwise.
	MatchPath(relPath string, raw []byte) int
	ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error)
	ApplyPatches(doc *DocRecord, patches []PatchOp) (outputRaw []byte, applied int, appended int, err error)
	NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error)
	PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (beforeSnippet, afterSnippet string)
}

var (
	adaptersMu sync.RWMutex
	adapters   []Adapter
)

// RegisterAdapter registers a FormatAdapter into the global format registry.
func RegisterAdapter(a Adapter) {
	if a == nil {
		return
	}
	adaptersMu.Lock()
	defer adaptersMu.Unlock()
	for i, existing := range adapters {
		if existing.ID() == a.ID() {
			adapters[i] = a
			return
		}
	}
	adapters = append(adapters, a)
}

// Adapters returns a snapshot of all registered format adapters.
func Adapters() []Adapter {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	out := make([]Adapter, len(adapters))
	copy(out, adapters)
	return out
}

// GetAdapter looks up a registered adapter by FormatID.
func GetAdapter(id FormatID) (Adapter, bool) {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	for _, a := range adapters {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}
