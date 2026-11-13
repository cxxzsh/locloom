package format

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

type resxAdapter struct{}

func init() {
	RegisterAdapter(&resxAdapter{})
}

func (a *resxAdapter) ID() FormatID {
	return FormatResX
}

func (a *resxAdapter) Name(lang string) string {
	return ".NET Resources (.resx)"
}

func (a *resxAdapter) Topology() Topology {
	return TopologyPerLocale
}

func (a *resxAdapter) IDAsKey() bool {
	return true
}

func (a *resxAdapter) Extensions() []string {
	return []string{"resx"}
}

func (a *resxAdapter) MatchPath(relPath string, raw []byte) int {
	if strings.EqualFold(filepath.Ext(relPath), ".resx") {
		return 95
	}
	return 0
}

type resxItem struct {
	entry         NormalizedEntry
	valElemStart  int
	valElemEnd    int
	hasValueElem  bool
	dataClosePos  int
}

type resxDoc struct {
	items        []resxItem
	rootClosePos int
}

func resolveResXLocale(relPath string, known []locale.LocaleSpec) (localeID string, isTemplate bool) {
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if dot := strings.LastIndexByte(stem, '.'); dot > 0 {
		suffix := stem[dot+1:]
		if id, _, ok := locale.ParseHeaderLocale(suffix, known); ok {
			return id, false
		}
		if len(suffix) >= 2 && len(suffix) <= 10 {
			return strings.ReplaceAll(suffix, "-", "_"), false
		}
	}
	return "en_US", true
}

func (a *resxAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	normRel := strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "./")
	utf8Bytes, hasBOM := decodeUTF8WithBOM(raw)
	le := detectLineEnding(utf8Bytes)
	locID, isTemplate := resolveResXLocale(normRel, known)

	rdoc := &resxDoc{rootClosePos: -1}
	var entries []NormalizedEntry

	dec := xml.NewDecoder(bytes.NewReader(utf8Bytes))
	dec.Strict = false
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}

	var (
		inData       bool
		skipData     bool
		inValue      bool
		inComment    bool
		dataName     string
		valElemStart int
		valElemEnd   int
		hasVal       bool
		valBuf       strings.Builder
		cmtBuf       strings.Builder
	)

	for {
		prevOffset := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse resx %q: %w", normRel, err)
		}
		currOffset := int(dec.InputOffset())

		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "data":
				inData = true
				skipData = false
				hasVal = false
				dataName = ""
				valElemStart = 0
				valElemEnd = 0
				valBuf.Reset()
				cmtBuf.Reset()
				for _, attr := range t.Attr {
					an := strings.ToLower(attr.Name.Local)
					switch an {
					case "name":
						dataName = attr.Value
					case "type", "mimetype":
						if strings.TrimSpace(attr.Value) != "" {
							skipData = true
						}
					}
				}
			case "value":
				if inData && !skipData {
					inValue = true
					valElemStart = prevOffset
					valBuf.Reset()
				}
			case "comment":
				if inData && !skipData {
					inComment = true
					cmtBuf.Reset()
				}
			}
		case xml.CharData:
			if inValue {
				valBuf.Write(t)
			} else if inComment {
				cmtBuf.Write(t)
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "value":
				if inValue {
					inValue = false
					valElemEnd = currOffset
					hasVal = true
				}
			case "comment":
				inComment = false
			case "data":
				if inData {
					inData = false
					if !skipData && dataName != "" {
						txt := valBuf.String()
						status := domain.StatusFinished
						if txt == "" {
							status = domain.StatusEmpty
						}
						entry := NormalizedEntry{
							Key: domain.EntryKey{
								ID:      dataName,
								Comment: strings.TrimSpace(cmtBuf.String()),
							},
							LocaleID: locID,
							Text:     txt,
							Status:   status,
						}
						if isTemplate {
							entry.Key.Source = txt
						}
						rdoc.items = append(rdoc.items, resxItem{
							entry:        entry,
							valElemStart: valElemStart,
							valElemEnd:   valElemEnd,
							hasValueElem: hasVal,
							dataClosePos: prevOffset,
						})
						entries = append(entries, entry)
					}
				}
			case "root":
				rdoc.rootClosePos = prevOffset
			}
		}
	}

	return &DocRecord{
		FormatID:   FormatResX,
		FilePath:   normRel,
		RelPath:    normRel,
		FileName:   filepath.Base(normRel),
		LocaleID:   locID,
		IsTemplate: isTemplate,
		HasBOM:     hasBOM,
		LineEnding: le,
		Raw:        utf8Bytes,
		Entries:    entries,
		Opaque:     rdoc,
	}, nil
}

func (a *resxAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	if doc == nil {
		return nil, 0, 0, fmt.Errorf("nil DocRecord")
	}
	rdoc, ok := doc.Opaque.(*resxDoc)
	if !ok || rdoc == nil {
		parsed, err := a.ParseDoc(doc.RelPath, doc.Raw, "", nil)
		if err != nil {
			return nil, 0, 0, err
		}
		rdoc = parsed.Opaque.(*resxDoc)
	}

	le := doc.LineEnding
	if le == "" {
		le = "\n"
	}

	byID := make(map[string]*resxItem, len(rdoc.items))
	for i := range rdoc.items {
		it := &rdoc.items[i]
		if it.entry.Key.ID != "" {
			byID[it.entry.Key.ID] = it
		}
	}

	var edits []byteSpanEdit
	var appends []PatchOp
	applied := 0
	appended := 0

	for idx, p := range patches {
		if !p.Action.Mutates() {
			continue
		}
		if p.Action == domain.ActionAppendNew {
			appends = append(appends, p)
			continue
		}
		it := byID[p.Key.ID]
		if it == nil {
			continue
		}
		escaped := applyLineEnding(escapeXMLText(p.NewText), le)
		if it.hasValueElem && it.valElemEnd > it.valElemStart {
			edits = append(edits, byteSpanEdit{
				start: it.valElemStart,
				end:   it.valElemEnd,
				order: idx,
				data:  []byte("<value>" + escaped + "</value>"),
			})
			applied++
		} else if it.dataClosePos > 0 {
			edits = append(edits, byteSpanEdit{
				start: it.dataClosePos,
				end:   it.dataClosePos,
				order: idx,
				data:  []byte("  <value>" + escaped + "</value>" + le + "  "),
			})
			applied++
		}
	}

	if len(appends) > 0 && rdoc.rootClosePos >= 0 {
		var buf bytes.Buffer
		for _, p := range appends {
			id := p.Key.ID
			if id == "" {
				id = p.Key.Source
			}
			buf.WriteString(fmt.Sprintf(`  <data name="%s" xml:space="preserve">`, escapeXMLAttr(id)) + le)
			buf.WriteString("    <value>" + applyLineEnding(escapeXMLText(p.NewText), le) + "</value>" + le)
			if p.Key.Comment != "" {
				buf.WriteString("    <comment>" + applyLineEnding(escapeXMLText(p.Key.Comment), le) + "</comment>" + le)
			}
			buf.WriteString("  </data>" + le)
			appended++
		}
		edits = append(edits, byteSpanEdit{
			start: rdoc.rootClosePos,
			end:   rdoc.rootClosePos,
			order: len(patches) * 100,
			data:  buf.Bytes(),
		})
	}

	out := applyByteSpanEdits(doc.Raw, edits, doc.HasBOM)
	return out, applied, appended, nil
}

func (a *resxAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	le := "\n"
	if template != nil && template.LineEnding != "" {
		le = template.LineEnding
	}
	if template == nil || len(template.Raw) == 0 {
		raw := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>%s<root>%s</root>%s`, le, le, le)
		doc, err := a.ParseDoc(newRelPath, []byte(raw), "", []locale.LocaleSpec{target})
		if err != nil {
			return nil, err
		}
		doc.LocaleID = target.ID
		doc.IsTemplate = false
		return doc, nil
	}

	rdoc, ok := template.Opaque.(*resxDoc)
	if !ok || rdoc == nil {
		parsed, err := a.ParseDoc(template.RelPath, template.Raw, "", nil)
		if err != nil {
			return nil, err
		}
		rdoc = parsed.Opaque.(*resxDoc)
	}

	var edits []byteSpanEdit
	for idx, it := range rdoc.items {
		if it.hasValueElem && it.valElemEnd > it.valElemStart {
			edits = append(edits, byteSpanEdit{
				start: it.valElemStart,
				end:   it.valElemEnd,
				order: idx,
				data:  []byte("<value></value>"),
			})
		}
	}
	out := applyByteSpanEdits(template.Raw, edits, template.HasBOM)
	doc, err := a.ParseDoc(newRelPath, out, "", []locale.LocaleSpec{target})
	if err != nil {
		return nil, err
	}
	doc.LocaleID = target.ID
	doc.IsTemplate = false
	return doc, nil
}

func (a *resxAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	id := key.ID
	if id == "" {
		id = key.Source
	}
	return fmt.Sprintf("<data name=\"%s\" xml:space=\"preserve\">\n    <value>%s</value>\n</data>", escapeXMLAttr(id), escapeXMLText(oldText)),
		fmt.Sprintf("<data name=\"%s\" xml:space=\"preserve\">\n    <value>%s</value>\n</data>", escapeXMLAttr(id), escapeXMLText(newText))
}
