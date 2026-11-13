package domain

import "strings"

// EntryKey uniquely identifies a translatable message across source-keyed (.ts, .po)
// and ID-keyed (.json, .arb, .xcstrings, strings.xml, .resx, .xliff) localization formats.
type EntryKey struct {
	ID      string `json:"id,omitempty"`
	Context string `json:"context"`
	Source  string `json:"source"`
	Comment string `json:"comment,omitempty"`
}

// Canonical returns a stable, collision-free composite string key.
func (k EntryKey) Canonical() string {
	if k.ID != "" {
		return "id:" + k.ID + "\x00" + k.Context + "\x00" + k.Source + "\x00" + k.Comment
	}
	return k.Context + "\x00" + k.Source + "\x00" + k.Comment
}

// IDContext returns a key scoped to (ID, Context) for ID-based formats.
func (k EntryKey) IDContext() EntryKey {
	return EntryKey{ID: k.ID, Context: k.Context}
}

// ContextSource returns a key scoped to (ID, Context, Source) with empty Comment.
func (k EntryKey) ContextSource() EntryKey {
	return EntryKey{ID: k.ID, Context: k.Context, Source: k.Source}
}

// SourceOnly returns a context-agnostic glossary key scoped only to Source.
func (k EntryKey) SourceOnly() EntryKey {
	return EntryKey{Source: k.Source}
}
// TransStatus describes the current state of a <translation> node in a .ts file.
type TransStatus string

const (
	StatusMissing    TransStatus = "missing"
	StatusEmpty      TransStatus = "empty"
	StatusUnfinished TransStatus = "unfinished"
	StatusFinished   TransStatus = "finished"
	StatusVanished   TransStatus = "vanished"
)

// WriteMode controls how candidate translations reconcile with existing finished .ts entries.
type WriteMode int

const (
	ModeFillUnfinished WriteMode = iota
	ModeOverwriteExisting
)

// PatchActionType indicates what write-back operation will occur for a cell.
type PatchActionType string

const (
	ActionNone         PatchActionType = "none"
	ActionSkipFinished PatchActionType = "skip_finished"
	ActionFillGap      PatchActionType = "fill_gap"
	ActionOverwrite    PatchActionType = "overwrite"
	ActionAppendNew    PatchActionType = "append_new"
)

// Mutates reports whether the patch action modifies the target .ts file.
func (a PatchActionType) Mutates() bool {
	return a == ActionFillGap || a == ActionOverwrite || a == ActionAppendNew
}

// Origin identifies where a workspace translation value came from.
const (
	OriginTS       = "ts"
	OriginXLSX     = "xlsx"
	OriginTM       = "ts_tm"
	OriginGoogle   = "google_web"
	OriginBing     = "bing_web"
	OriginDeepL    = "deepl"
	OriginMyMemory = "mymemory"
	OriginLibre    = "libretranslate"
	OriginBaidu    = "baidu_api"
	OriginNiutrans = "niutrans"
	OriginLLM      = "openai_compat"
	OriginManual   = "manual"
	OriginCache    = "cache"
)

// IsMachineOrigin reports whether an origin came from an automated MT/LLM engine or cache.
func IsMachineOrigin(origin string) bool {
	switch origin {
	case OriginGoogle, OriginBing, OriginDeepL, OriginMyMemory, OriginLibre, OriginBaidu, OriginNiutrans, OriginLLM, OriginCache:
		return true
	default:
		return false
	}
}

// PluralSeparator separates multiple <numerusform> values when viewed/edited as plain text.
const PluralSeparator = "\n---\n"

// JoinNumerus combines plural forms into a single editable representation.
func JoinNumerus(forms []string) string {
	if len(forms) == 0 {
		return ""
	}
	return strings.Join(forms, PluralSeparator)
}

// SplitNumerus splits a combined plural string back into individual <numerusform> values.
func SplitNumerus(text string) []string {
	if text == "" {
		return nil
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.Contains(normalized, PluralSeparator) {
		return []string{text}
	}
	return strings.Split(normalized, PluralSeparator)
}
