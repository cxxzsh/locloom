package tsxml

import "locloom/internal/domain"

// MessageEntry holds one <message> parsed from a .ts file, along with exact byte offsets
// of its <translation> element so we can patch in-place without reformatting the XML.
type MessageEntry struct {
	Context      string
	Source       string
	Comment      string
	ExtraComment string
	LocationFile string
	LocationLine string
	Translation  string // For Numerus==true, domain.JoinNumerus(NumerusForms)
	Numerus      bool
	NumerusForms []string
	Status       domain.TransStatus
	TransStart   int // byte offset in UTF-8 Raw of '<' starting <translation...>
	TransEnd     int // byte offset in UTF-8 Raw right after '>' ending </translation> or <translation.../>
}

// Key returns the canonical domain.EntryKey for this message.
func (m MessageEntry) Key() domain.EntryKey {
	return domain.EntryKey{
		Context: m.Context,
		Source:  m.Source,
		Comment: m.Comment,
	}
}

// ContextBlock represents one <context> section in a .ts file.
type ContextBlock struct {
	Name          string
	Messages      []MessageEntry
	CloseTagStart int // byte offset in Raw of "</context>"
	Indent        string
}

// TSFile represents a parsed .ts file in memory with its UTF-8 raw bytes preserved.
type TSFile struct {
	FilePath      string
	RelPath       string // normalized '/' relative path within directory or zip
	FileName      string
	Language      string
	SourceLang    string
	LocaleID      string
	HasBOM        bool
	LineEnding    string // "\r\n" or "\n"
	Raw           []byte // UTF-8 bytes (without BOM) matching TransStart/TransEnd offsets
	CloseTagStart int    // byte offset in Raw of "</TS>"
	Contexts      []ContextBlock
}

// Bundle represents a loaded directory or .zip archive of .ts files.
type Bundle struct {
	SourcePath string
	IsZip      bool
	Files      []*TSFile
	ZipExtras  map[string][]byte // non-.ts zip entries preserved verbatim
}

// PatchItem defines one patch instruction for a .ts file.
type PatchItem struct {
	Key          domain.EntryKey
	NewText      string
	NumerusForms []string
	Action       domain.PatchActionType
	Numerus      bool
}

// PatchResult holds the outcome of applying patches to a TSFile.
type PatchResult struct {
	Applied   int
	Appended  int
	Skipped   int
	OutputRaw []byte // final bytes (including UTF-8 BOM if HasBOM and preserved line endings)
}

// SaveOptions controls how patched files are written to disk or zip.
type SaveOptions struct {
	CreateBackup bool
	OutputPath   string // empty = in-place for dir, or <stem>_updated.zip for zip
}

// SaveReport summarizes a SaveBundle operation.
type SaveReport struct {
	OutputPath   string
	FilesWritten int
	BackupPaths  []string
}
