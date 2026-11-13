package format

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/tsxml"
)

type qtTSAdapter struct{}

func init() {
	RegisterAdapter(&qtTSAdapter{})
}

func (a *qtTSAdapter) ID() FormatID {
	return FormatQtTS
}

func (a *qtTSAdapter) Name(lang string) string {
	return "Qt Linguist (.ts)"
}

func (a *qtTSAdapter) Topology() Topology {
	return TopologyPerLocale
}

func (a *qtTSAdapter) IDAsKey() bool {
	return false
}

func (a *qtTSAdapter) Extensions() []string {
	return []string{"ts"}
}

func (a *qtTSAdapter) MatchPath(relPath string, raw []byte) int {
	if !strings.EqualFold(filepath.Ext(relPath), ".ts") {
		return 0
	}
	if len(raw) == 0 {
		return 50
	}
	if bytes.Contains(raw, []byte("<TS")) || bytes.Contains(raw, []byte("<!DOCTYPE TS")) {
		return 95
	}
	if bytes.Contains(raw, []byte("<?xml")) {
		return 80
	}
	// Avoid hijacking TypeScript source files that happen to share the .ts extension.
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("import ")) ||
		bytes.HasPrefix(trimmed, []byte("export ")) ||
		bytes.HasPrefix(trimmed, []byte("const ")) ||
		bytes.HasPrefix(trimmed, []byte("interface ")) ||
		bytes.HasPrefix(trimmed, []byte("type ")) {
		return 0
	}
	return 40
}

func (a *qtTSAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	tf, err := tsxml.ParseBytes(relPath, raw)
	if err != nil {
		return nil, err
	}
	tf.LocaleID = resolveTSLocaleID(tf, filePattern, known)
	return docFromTSFile(tf), nil
}

func resolveTSLocaleID(tf *tsxml.TSFile, filePattern string, known []locale.LocaleSpec) string {
	stemmed := locale.StemLocaleID(tf.RelPath, filePattern, known)
	if stemmed != "" {
		return stemmed
	}
	if tf.Language != "" {
		if id, _, ok := locale.ParseHeaderLocale(tf.Language, known); ok {
			return id
		}
		return tf.Language
	}
	return tf.FileName
}

func docFromTSFile(tf *tsxml.TSFile) *DocRecord {
	var entries []NormalizedEntry
	for _, ctx := range tf.Contexts {
		for _, m := range ctx.Messages {
			loc := m.LocationFile
			if loc != "" && m.LocationLine != "" {
				loc = loc + ":" + m.LocationLine
			}
			entries = append(entries, NormalizedEntry{
				Key:          m.Key(),
				LocaleID:     tf.LocaleID,
				Text:         m.Translation,
				Numerus:      m.Numerus,
				PluralForms:  append([]string(nil), m.NumerusForms...),
				Status:       m.Status,
				Location:     loc,
				ExtraComment: m.ExtraComment,
			})
		}
	}
	return &DocRecord{
		FormatID:   FormatQtTS,
		FilePath:   tf.FilePath,
		RelPath:    tf.RelPath,
		FileName:   tf.FileName,
		LocaleID:   tf.LocaleID,
		SourceLang: tf.SourceLang,
		HasBOM:     tf.HasBOM,
		LineEnding: tf.LineEnding,
		Raw:        tf.Raw,
		Entries:    entries,
		Opaque:     tf,
	}
}

func (a *qtTSAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	if doc == nil {
		return nil, 0, 0, fmt.Errorf("nil DocRecord")
	}
	tf, ok := doc.Opaque.(*tsxml.TSFile)
	if !ok || tf == nil {
		var err error
		tf, err = tsxml.ParseBytes(doc.RelPath, doc.Raw)
		if err != nil {
			return nil, 0, 0, err
		}
		tf.LocaleID = doc.LocaleID
	}

	items := make([]tsxml.PatchItem, 0, len(patches))
	for _, p := range patches {
		items = append(items, tsxml.PatchItem{
			Key:          p.Key,
			NewText:      p.NewText,
			NumerusForms: append([]string(nil), p.PluralForms...),
			Action:       p.Action,
			Numerus:      p.Numerus,
		})
	}
	res, err := tsxml.ApplyPatches(tf, items)
	if err != nil {
		return nil, 0, 0, err
	}
	return res.OutputRaw, res.Applied, res.Appended, nil
}

func (a *qtTSAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	var tmplTF *tsxml.TSFile
	if template != nil {
		if tf, ok := template.Opaque.(*tsxml.TSFile); ok {
			tmplTF = tf
		}
	}
	if tmplTF != nil {
		cloned, err := tsxml.CloneSkeleton(tmplTF, target, newRelPath)
		if err != nil {
			return nil, err
		}
		return docFromTSFile(cloned), nil
	}
	le := "\n"
	if template != nil && template.LineEnding != "" {
		le = template.LineEnding
	}
	emptyTF := tsxml.NewEmptyTSFile(target, newRelPath, le)
	return docFromTSFile(emptyTF), nil
}

func (a *qtTSAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	return tsxml.FormatSnippetDiff(key.Context, key.Source, oldText, newText, oldStatus, numerus)
}
