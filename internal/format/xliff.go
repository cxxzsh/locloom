package format

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

type xliffAdapter struct{}

func init() {
	RegisterAdapter(&xliffAdapter{})
}

func (a *xliffAdapter) ID() FormatID {
	return FormatXLIFF
}

func (a *xliffAdapter) Name(lang string) string {
	return "XLIFF (.xlf / .xliff)"
}

func (a *xliffAdapter) Topology() Topology {
	return TopologyPerLocale
}

func (a *xliffAdapter) IDAsKey() bool {
	return true
}

func (a *xliffAdapter) Extensions() []string {
	return []string{"xlf", "xliff"}
}

func (a *xliffAdapter) MatchPath(relPath string, raw []byte) int {
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == ".xlf" || ext == ".xliff" {
		return 95
	}
	return 0
}

type xliffItem struct {
	entry           NormalizedEntry
	isV2            bool
	sourceElemEnd   int
	sourceIndent    string
	hasTarget       bool
	targetElemStart int
	targetElemEnd   int
	segTagStart     int
	segTagEnd       int
	segRawTag       string
}

type xliffDoc struct {
	isV2         bool
	items        []xliffItem
	bodyClosePos int // </body> for 1.2
	fileClosePos int // </file> for 2.0
}

var (
	reXLIFFInlineTag     = regexp.MustCompile(`<(?:/)?(?:x|ph|g|bx|ex|bpt|ept|it|mrk|sc|ec|pc)\b[^>]*>`)
	reSegStateAttr       = regexp.MustCompile(`(\bstate\s*=\s*)(["'])[^"']*["']`)
	reXLIFFOpenTag       = regexp.MustCompile(`<xliff\b[^>]*>`)
	reFileOpenTag        = regexp.MustCompile(`<file\b[^>]*>`)
	reAttrTrgLang        = regexp.MustCompile(`(?i)\btrgLang\s*=\s*(["'])[^"']*["']`)
	reAttrTargetLanguage = regexp.MustCompile(`(?i)\btarget-language\s*=\s*(["'])[^"']*["']`)
)

func unescapeXLIFFInner(raw string) string {
	if !strings.ContainsRune(raw, '<') {
		return html.UnescapeString(raw)
	}
	locs := reXLIFFInlineTag.FindAllStringIndex(raw, -1)
	if len(locs) == 0 {
		return html.UnescapeString(raw)
	}
	var b strings.Builder
	b.Grow(len(raw))
	cursor := 0
	for _, loc := range locs {
		if loc[0] > cursor {
			b.WriteString(html.UnescapeString(raw[cursor:loc[0]]))
		}
		b.WriteString(raw[loc[0]:loc[1]])
		cursor = loc[1]
	}
	if cursor < len(raw) {
		b.WriteString(html.UnescapeString(raw[cursor:]))
	}
	return b.String()
}

func escapeXLIFFInner(s string) string {
	if !strings.ContainsRune(s, '<') {
		return escapeXMLText(s)
	}
	locs := reXLIFFInlineTag.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return escapeXMLText(s)
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	cursor := 0
	for _, loc := range locs {
		if loc[0] > cursor {
			b.WriteString(escapeXMLText(s[cursor:loc[0]]))
		}
		b.WriteString(s[loc[0]:loc[1]])
		cursor = loc[1]
	}
	if cursor < len(s) {
		b.WriteString(escapeXMLText(s[cursor:]))
	}
	return b.String()
}

func (a *xliffAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	normRel := strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "./")
	utf8Bytes, hasBOM := decodeUTF8WithBOM(raw)
	le := detectLineEnding(utf8Bytes)

	xdoc := &xliffDoc{bodyClosePos: -1, fileClosePos: -1}
	var entries []NormalizedEntry

	dec := xml.NewDecoder(bytes.NewReader(utf8Bytes))
	dec.Strict = false
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}

	var (
		srcLang      string
		trgLang      string
		fileOrig     string
		inUnit       bool
		inSegment    bool
		inSource     bool
		sourceDepth  int
		inTarget     bool
		targetDepth  int
		inNote       bool
		unitID       string
		sourceStart  int
		sourceEnd    int
		sourceElemSt int
		sourceElemEn int
		hasSource    bool
		targetStart  int
		targetEnd    int
		targetElemSt int
		targetElemEn int
		hasTarget    bool
		targetState  string
		segTagSt     int
		segTagEn     int
		segState     string
		noteBuf      strings.Builder
	)

	resetUnit := func() {
		unitID = ""
		hasSource = false
		hasTarget = false
		targetState = ""
		segTagSt = 0
		segTagEn = 0
		segState = ""
		noteBuf.Reset()
	}

	flushUnit := func() {
		if unitID == "" && !hasSource {
			return
		}
		srcText := ""
		if hasSource && sourceEnd >= sourceStart {
			srcText = unescapeXLIFFInner(string(utf8Bytes[sourceStart:sourceEnd]))
		}
		trgText := ""
		if hasTarget && targetEnd >= targetStart {
			trgText = unescapeXLIFFInner(string(utf8Bytes[targetStart:targetEnd]))
		}
		state := targetState
		if xdoc.isV2 && state == "" {
			state = segState
		}
		status := resolveXLIFFStatus(hasTarget, trgText, state)

		entry := NormalizedEntry{
			Key: domain.EntryKey{
				ID:      unitID,
				Context: fileOrig,
				Source:  srcText,
				Comment: strings.TrimSpace(noteBuf.String()),
			},
			Text:   trgText,
			Status: status,
		}
		item := xliffItem{
			entry:           entry,
			isV2:            xdoc.isV2,
			sourceElemEnd:   sourceElemEn,
			sourceIndent:    leadingIndentAt(utf8Bytes, sourceElemSt),
			hasTarget:       hasTarget,
			targetElemStart: targetElemSt,
			targetElemEnd:   targetElemEn,
			segTagStart:     segTagSt,
			segTagEnd:       segTagEn,
		}
		if segTagEn > segTagSt {
			item.segRawTag = string(utf8Bytes[segTagSt:segTagEn])
		}
		xdoc.items = append(xdoc.items, item)
		entries = append(entries, entry)
	}

	for {
		prevOffset := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse xliff %q: %w", normRel, err)
		}
		currOffset := int(dec.InputOffset())

		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if inSource {
				sourceDepth++
				continue
			}
			if inTarget {
				targetDepth++
				continue
			}
			switch name {
			case "xliff":
				for _, attr := range t.Attr {
					switch strings.ToLower(attr.Name.Local) {
					case "version":
						if strings.HasPrefix(strings.TrimSpace(attr.Value), "2") {
							xdoc.isV2 = true
						}
					case "srclang":
						srcLang = strings.TrimSpace(attr.Value)
					case "trglang":
						trgLang = strings.TrimSpace(attr.Value)
					}
				}
			case "file":
				for _, attr := range t.Attr {
					switch strings.ToLower(attr.Name.Local) {
					case "source-language":
						if srcLang == "" {
							srcLang = strings.TrimSpace(attr.Value)
						}
					case "target-language":
						if trgLang == "" {
							trgLang = strings.TrimSpace(attr.Value)
						}
					case "original":
						fileOrig = attr.Value
					}
				}
			case "trans-unit":
				inUnit = true
				resetUnit()
				for _, attr := range t.Attr {
					if strings.EqualFold(attr.Name.Local, "id") {
						unitID = attr.Value
					}
				}
			case "unit":
				xdoc.isV2 = true
				inUnit = true
				resetUnit()
				for _, attr := range t.Attr {
					if strings.EqualFold(attr.Name.Local, "id") {
						unitID = attr.Value
					}
				}
			case "segment":
				if inUnit {
					inSegment = true
					segTagSt = prevOffset
					segTagEn = currOffset
					segState = ""
					for _, attr := range t.Attr {
						if strings.EqualFold(attr.Name.Local, "state") {
							segState = strings.TrimSpace(attr.Value)
						}
					}
				}
			case "source":
				if inUnit {
					inSource = true
					sourceDepth = 1
					sourceElemSt = prevOffset
					sourceStart = currOffset
					hasSource = true
				}
			case "target":
				if inUnit {
					inTarget = true
					targetDepth = 1
					targetElemSt = prevOffset
					targetStart = currOffset
					hasTarget = true
					targetState = ""
					for _, attr := range t.Attr {
						if strings.EqualFold(attr.Name.Local, "state") {
							targetState = strings.TrimSpace(attr.Value)
						}
					}
				}
			case "note":
				if inUnit {
					inNote = true
				}
			}

		case xml.CharData:
			if inNote {
				noteBuf.Write(t)
			}

		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			if inSource {
				sourceDepth--
				if sourceDepth == 0 && name == "source" {
					inSource = false
					sourceEnd = prevOffset
					sourceElemEn = currOffset
					if currOffset >= 2 && utf8Bytes[currOffset-2] == '/' {
						sourceEnd = sourceStart
					}
				}
				continue
			}
			if inTarget {
				targetDepth--
				if targetDepth == 0 && name == "target" {
					inTarget = false
					targetEnd = prevOffset
					targetElemEn = currOffset
					if currOffset >= 2 && utf8Bytes[currOffset-2] == '/' {
						targetEnd = targetStart
					}
				}
				continue
			}
			switch name {
			case "note":
				inNote = false
			case "segment":
				if inSegment {
					inSegment = false
				}
			case "trans-unit", "unit":
				if inUnit {
					inUnit = false
					flushUnit()
				}
			case "body":
				xdoc.bodyClosePos = prevOffset
			case "file":
				xdoc.fileClosePos = prevOffset
			}
		}
	}

	locID := resolveXLIFFLocale(trgLang, normRel, filePattern, known)
	for i := range entries {
		entries[i].LocaleID = locID
		xdoc.items[i].entry.LocaleID = locID
	}

	return &DocRecord{
		FormatID:   FormatXLIFF,
		FilePath:   normRel,
		RelPath:    normRel,
		FileName:   filepath.Base(normRel),
		LocaleID:   locID,
		SourceLang: srcLang,
		HasBOM:     hasBOM,
		LineEnding: le,
		Raw:        utf8Bytes,
		Entries:    entries,
		Opaque:     xdoc,
	}, nil
}

func resolveXLIFFLocale(trgLang, relPath, filePattern string, known []locale.LocaleSpec) string {
	if trgLang != "" {
		if id, _, ok := locale.ParseHeaderLocale(trgLang, known); ok {
			return id
		}
		return strings.ReplaceAll(trgLang, "-", "_")
	}
	return stemLocaleWithExt(relPath, filePattern, known)
}

func resolveXLIFFStatus(hasTarget bool, text, state string) domain.TransStatus {
	if !hasTarget {
		return domain.StatusMissing
	}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "needs-translation", "new", "needs-review-translation", "needs-l10n", "needs-adaptation":
		return domain.StatusUnfinished
	case "initial":
		if text == "" {
			return domain.StatusEmpty
		}
		return domain.StatusUnfinished
	}
	if text == "" {
		return domain.StatusEmpty
	}
	return domain.StatusFinished
}

func (a *xliffAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	if doc == nil {
		return nil, 0, 0, fmt.Errorf("nil DocRecord")
	}
	xdoc, ok := doc.Opaque.(*xliffDoc)
	if !ok || xdoc == nil {
		parsed, err := a.ParseDoc(doc.RelPath, doc.Raw, "", nil)
		if err != nil {
			return nil, 0, 0, err
		}
		xdoc = parsed.Opaque.(*xliffDoc)
	}

	le := doc.LineEnding
	if le == "" {
		le = "\n"
	}

	byID := make(map[string]*xliffItem, len(xdoc.items))
	bySrc := make(map[string]*xliffItem, len(xdoc.items))
	for i := range xdoc.items {
		it := &xdoc.items[i]
		if it.entry.Key.ID != "" {
			byID[it.entry.Key.ID] = it
		}
		if it.entry.Key.Source != "" {
			if _, exists := bySrc[it.entry.Key.Source]; !exists {
				bySrc[it.entry.Key.Source] = it
			}
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
		var it *xliffItem
		if p.Key.ID != "" {
			it = byID[p.Key.ID]
		}
		if it == nil && p.Key.Source != "" {
			it = bySrc[p.Key.Source]
		}
		if it == nil {
			continue
		}

		inner := applyLineEnding(escapeXLIFFInner(p.NewText), le)
		if it.isV2 {
			if it.segTagEnd > it.segTagStart && it.segRawTag != "" {
				var updatedSeg string
				if reSegStateAttr.MatchString(it.segRawTag) {
					updatedSeg = reSegStateAttr.ReplaceAllString(it.segRawTag, `${1}${2}translated${2}`)
				} else if strings.HasSuffix(it.segRawTag, ">") {
					updatedSeg = it.segRawTag[:len(it.segRawTag)-1] + ` state="translated">`
				}
				if updatedSeg != "" && updatedSeg != it.segRawTag {
					edits = append(edits, byteSpanEdit{
						start: it.segTagStart,
						end:   it.segTagEnd,
						order: idx*10 + 1,
						data:  []byte(updatedSeg),
					})
				}
			}
			if it.hasTarget && it.targetElemEnd > it.targetElemStart {
				edits = append(edits, byteSpanEdit{
					start: it.targetElemStart,
					end:   it.targetElemEnd,
					order: idx*10 + 2,
					data:  []byte("<target>" + inner + "</target>"),
				})
				applied++
			} else if it.sourceElemEnd > 0 {
				indent := it.sourceIndent
				if indent == "" {
					indent = "        "
				}
				edits = append(edits, byteSpanEdit{
					start: it.sourceElemEnd,
					end:   it.sourceElemEnd,
					order: idx*10 + 2,
					data:  []byte(le + indent + "<target>" + inner + "</target>"),
				})
				applied++
			}
		} else {
			if it.hasTarget && it.targetElemEnd > it.targetElemStart {
				edits = append(edits, byteSpanEdit{
					start: it.targetElemStart,
					end:   it.targetElemEnd,
					order: idx*10 + 2,
					data:  []byte(`<target state="translated">` + inner + `</target>`),
				})
				applied++
			} else if it.sourceElemEnd > 0 {
				indent := it.sourceIndent
				if indent == "" {
					indent = "        "
				}
				edits = append(edits, byteSpanEdit{
					start: it.sourceElemEnd,
					end:   it.sourceElemEnd,
					order: idx*10 + 2,
					data:  []byte(le + indent + `<target state="translated">` + inner + `</target>`),
				})
				applied++
			}
		}
	}

	if len(appends) > 0 {
		insertPos := xdoc.bodyClosePos
		if xdoc.isV2 || insertPos < 0 {
			insertPos = xdoc.fileClosePos
		}
		if insertPos >= 0 {
			var buf bytes.Buffer
			for _, p := range appends {
				id := p.Key.ID
				if id == "" {
					id = p.Key.Source
				}
				src := applyLineEnding(escapeXLIFFInner(p.Key.Source), le)
				trg := applyLineEnding(escapeXLIFFInner(p.NewText), le)
				if xdoc.isV2 {
					buf.WriteString(fmt.Sprintf(`    <unit id="%s">`, escapeXMLAttr(id)) + le)
					buf.WriteString(`      <segment state="translated">` + le)
					buf.WriteString(`        <source>` + src + `</source>` + le)
					buf.WriteString(`        <target>` + trg + `</target>` + le)
					buf.WriteString(`      </segment>` + le)
					buf.WriteString(`    </unit>` + le)
				} else {
					buf.WriteString(fmt.Sprintf(`      <trans-unit id="%s">`, escapeXMLAttr(id)) + le)
					buf.WriteString(`        <source>` + src + `</source>` + le)
					buf.WriteString(`        <target state="translated">` + trg + `</target>` + le)
					buf.WriteString(`      </trans-unit>` + le)
				}
				appended++
			}
			edits = append(edits, byteSpanEdit{
				start: insertPos,
				end:   insertPos,
				order: len(patches) * 100,
				data:  buf.Bytes(),
			})
		}
	}

	out := applyByteSpanEdits(doc.Raw, edits, doc.HasBOM)
	return out, applied, appended, nil
}

func (a *xliffAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	le := "\n"
	if template != nil && template.LineEnding != "" {
		le = template.LineEnding
	}
	bcp := strings.ReplaceAll(target.ID, "_", "-")
	if template == nil || len(template.Raw) == 0 {
		raw := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>%s<xliff version="1.2" xmlns="urn:oasis:names:tc:xliff:document:1.2">%s  <file original="messages" source-language="en" target-language="%s" datatype="plaintext">%s    <body>%s    </body>%s  </file>%s</xliff>%s`,
			le, le, escapeXMLAttr(bcp), le, le, le, le, le)
		doc, err := a.ParseDoc(newRelPath, []byte(raw), "", []locale.LocaleSpec{target})
		if err != nil {
			return nil, err
		}
		doc.LocaleID = target.ID
		return doc, nil
	}

	xdoc, ok := template.Opaque.(*xliffDoc)
	if !ok || xdoc == nil {
		parsed, err := a.ParseDoc(template.RelPath, template.Raw, "", nil)
		if err != nil {
			return nil, err
		}
		xdoc = parsed.Opaque.(*xliffDoc)
	}

	var edits []byteSpanEdit
	for idx, it := range xdoc.items {
		if it.isV2 && it.segTagEnd > it.segTagStart && reSegStateAttr.MatchString(it.segRawTag) {
			updatedSeg := reSegStateAttr.ReplaceAllStringFunc(it.segRawTag, func(s string) string {
				loc := reSegStateAttr.FindStringSubmatchIndex(s)
				if len(loc) >= 4 {
					return s[loc[2]:loc[3]] + `"initial"`
				}
				return s
			})
			edits = append(edits, byteSpanEdit{
				start: it.segTagStart,
				end:   it.segTagEnd,
				order: idx * 2,
				data:  []byte(updatedSeg),
			})
		}
		if it.hasTarget && it.targetElemEnd > it.targetElemStart {
			repl := `<target state="needs-translation"></target>`
			if it.isV2 {
				repl = `<target></target>`
			}
			edits = append(edits, byteSpanEdit{
				start: it.targetElemStart,
				end:   it.targetElemEnd,
				order: idx*2 + 1,
				data:  []byte(repl),
			})
		}
	}
	out := applyByteSpanEdits(template.Raw, edits, false)
	out = updateXLIFFTargetLang(out, bcp, xdoc.isV2)
	if template.HasBOM {
		out = append([]byte{0xEF, 0xBB, 0xBF}, out...)
	}
	doc, err := a.ParseDoc(newRelPath, out, "", []locale.LocaleSpec{target})
	if err != nil {
		return nil, err
	}
	doc.LocaleID = target.ID
	return doc, nil
}

func updateXLIFFTargetLang(raw []byte, bcp string, isV2 bool) []byte {
	escaped := escapeXMLAttr(bcp)
	if isV2 {
		loc := reXLIFFOpenTag.FindIndex(raw)
		if loc == nil {
			return raw
		}
		tag := string(raw[loc[0]:loc[1]])
		var updated string
		if reAttrTrgLang.MatchString(tag) {
			updated = reAttrTrgLang.ReplaceAllStringFunc(tag, func(string) string {
				return `trgLang="` + escaped + `"`
			})
		} else if strings.HasSuffix(tag, "/>") {
			updated = tag[:len(tag)-2] + ` trgLang="` + escaped + `"/>`
		} else {
			updated = tag[:len(tag)-1] + ` trgLang="` + escaped + `">`
		}
		var buf bytes.Buffer
		buf.Grow(len(raw) + len(updated))
		buf.Write(raw[:loc[0]])
		buf.WriteString(updated)
		buf.Write(raw[loc[1]:])
		return buf.Bytes()
	}

	locs := reFileOpenTag.FindAllIndex(raw, -1)
	if len(locs) == 0 {
		return raw
	}
	var buf bytes.Buffer
	buf.Grow(len(raw) + len(locs)*32)
	cursor := 0
	for _, loc := range locs {
		buf.Write(raw[cursor:loc[0]])
		tag := string(raw[loc[0]:loc[1]])
		var updated string
		if reAttrTargetLanguage.MatchString(tag) {
			updated = reAttrTargetLanguage.ReplaceAllStringFunc(tag, func(string) string {
				return `target-language="` + escaped + `"`
			})
		} else if strings.HasSuffix(tag, "/>") {
			updated = tag[:len(tag)-2] + ` target-language="` + escaped + `"/>`
		} else {
			updated = tag[:len(tag)-1] + ` target-language="` + escaped + `">`
		}
		buf.WriteString(updated)
		cursor = loc[1]
	}
	buf.Write(raw[cursor:])
	return buf.Bytes()
}

func (a *xliffAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	id := key.ID
	if id == "" {
		id = key.Source
	}
	return fmt.Sprintf("<trans-unit id=\"%s\">\n    <source>%s</source>\n    <target>%s</target>\n</trans-unit>",
			escapeXMLAttr(id), escapeXLIFFInner(key.Source), escapeXLIFFInner(oldText)),
		fmt.Sprintf("<trans-unit id=\"%s\">\n    <source>%s</source>\n    <target state=\"translated\">%s</target>\n</trans-unit>",
			escapeXMLAttr(id), escapeXLIFFInner(key.Source), escapeXLIFFInner(newText))
}
