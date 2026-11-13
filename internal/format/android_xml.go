package format

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

type androidXMLAdapter struct{}

func init() {
	RegisterAdapter(&androidXMLAdapter{})
}

func (a *androidXMLAdapter) ID() FormatID {
	return FormatAndroidXML
}

func (a *androidXMLAdapter) Name(lang string) string {
	return "Android strings.xml"
}

func (a *androidXMLAdapter) Topology() Topology {
	return TopologyPerLocale
}

func (a *androidXMLAdapter) IDAsKey() bool {
	return true
}

func (a *androidXMLAdapter) Extensions() []string {
	return []string{"xml"}
}

func (a *androidXMLAdapter) MatchPath(relPath string, raw []byte) int {
	if !strings.EqualFold(filepath.Ext(relPath), ".xml") {
		return 0
	}
	base := strings.ToLower(filepath.Base(relPath))
	if base == "strings.xml" {
		if len(raw) == 0 || bytes.Contains(raw, []byte("<resources")) {
			return 95
		}
		return 70
	}
	if bytes.Contains(raw, []byte("<resources")) &&
		(bytes.Contains(raw, []byte("<string ")) || bytes.Contains(raw, []byte("<plurals "))) {
		return 85
	}
	return 0
}

type androidSpan struct {
	elemStart   int
	innerStart  int
	innerEnd    int
	elemEnd     int
	selfClosing bool
	tagName     string
	attrName    string
	quantity    string
}

type androidItem struct {
	entry     NormalizedEntry
	span      androidSpan
	itemSpans []androidSpan
}

type androidDoc struct {
	items             []androidItem
	resourcesClosePos int
}

func resolveAndroidLocale(relPath string, known []locale.LocaleSpec) (localeID string, isTemplate bool) {
	slash := strings.ReplaceAll(relPath, "\\", "/")
	dir := filepath.Base(filepath.Dir(slash))
	lowerDir := strings.ToLower(dir)
	if lowerDir == "values" {
		return "en_US", true
	}
	if strings.HasPrefix(lowerDir, "values-") {
		qual := dir[len("values-"):]
		// Handle BCP-47 "values-b+zh+Hans+CN"
		if strings.HasPrefix(qual, "b+") || strings.HasPrefix(qual, "B+") {
			parts := strings.Split(qual[2:], "+")
			var lang, region, script string
			for _, p := range parts {
				if len(p) == 2 && lang == "" {
					lang = strings.ToLower(p)
				} else if len(p) == 4 && script == "" {
					script = p
				} else if len(p) == 2 && region == "" {
					region = strings.ToUpper(p)
				}
			}
			if lang != "" {
				candidate := lang
				if region != "" {
					candidate = lang + "_" + region
				} else if strings.EqualFold(script, "Hans") && lang == "zh" {
					candidate = "zh_CN"
				} else if strings.EqualFold(script, "Hant") && lang == "zh" {
					candidate = "zh_TW"
				}
				if id, _, ok := locale.ParseHeaderLocale(candidate, known); ok {
					return id, false
				}
				return candidate, false
			}
		}
		// Handle "values-zh-rCN" -> "zh_CN"
		if strings.Contains(qual, "-r") {
			parts := strings.SplitN(qual, "-r", 2)
			if len(parts) == 2 && len(parts[1]) >= 2 {
				candidate := strings.ToLower(parts[0]) + "_" + strings.ToUpper(parts[1])
				if id, _, ok := locale.ParseHeaderLocale(candidate, known); ok {
					return id, false
				}
				return candidate, false
			}
		}
		// Handle "values-ja", "values-fr", etc.
		if id, _, ok := locale.ParseHeaderLocale(qual, known); ok {
			return id, false
		}
		return strings.ReplaceAll(qual, "-", "_"), false
	}

	stemmed := stemLocaleWithExt(slash, "", known)
	return stemmed, false
}

func (a *androidXMLAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	normRel := strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "./")
	utf8Bytes, hasBOM := decodeUTF8WithBOM(raw)
	le := detectLineEnding(utf8Bytes)
	locID, isTemplate := resolveAndroidLocale(normRel, known)

	adoc := &androidDoc{resourcesClosePos: -1}
	var entries []NormalizedEntry

	dec := xml.NewDecoder(bytes.NewReader(utf8Bytes))
	dec.Strict = false
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}

	var (
		inString      bool
		inPlurals     bool
		inItem        bool
		skipCurrent   bool
		currentSpan   androidSpan
		pluralsName   string
		pluralItems   []androidSpan
		pluralForms   []string
		pluralKeys    []string
		itemSpan      androidSpan
		lastComment   string
	)

	for {
		prevOffset := int(dec.InputOffset())
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse android xml %q: %w", normRel, err)
		}
		currOffset := int(dec.InputOffset())

		switch t := tok.(type) {
		case xml.Comment:
			if !inString && !inPlurals {
				lastComment = strings.TrimSpace(string(t))
			}
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "string":
				if !inPlurals {
					inString = true
					skipCurrent = false
					var keyName string
					for _, attr := range t.Attr {
						switch strings.ToLower(attr.Name.Local) {
						case "name":
							keyName = attr.Value
						case "translatable":
							if strings.EqualFold(strings.TrimSpace(attr.Value), "false") {
								skipCurrent = true
							}
						}
					}
					selfClose := currOffset >= 2 && currOffset <= len(utf8Bytes) && utf8Bytes[currOffset-2] == '/'
					currentSpan = androidSpan{
						elemStart:   prevOffset,
						innerStart:  currOffset,
						selfClosing: selfClose,
						tagName:     "string",
						attrName:    keyName,
					}
				}
			case "plurals":
				inPlurals = true
				skipCurrent = false
				pluralsName = ""
				pluralItems = nil
				pluralForms = nil
				pluralKeys = nil
				for _, attr := range t.Attr {
					switch strings.ToLower(attr.Name.Local) {
					case "name":
						pluralsName = attr.Value
					case "translatable":
						if strings.EqualFold(strings.TrimSpace(attr.Value), "false") {
							skipCurrent = true
						}
					}
				}
			case "item":
				if inPlurals && !skipCurrent {
					inItem = true
					var qty string
					for _, attr := range t.Attr {
						if strings.EqualFold(attr.Name.Local, "quantity") {
							qty = attr.Value
						}
					}
					selfClose := currOffset >= 2 && currOffset <= len(utf8Bytes) && utf8Bytes[currOffset-2] == '/'
					itemSpan = androidSpan{
						elemStart:   prevOffset,
						innerStart:  currOffset,
						selfClosing: selfClose,
						tagName:     "item",
						quantity:    qty,
					}
				}
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			switch name {
			case "string":
				if inString {
					inString = false
					if !skipCurrent && currentSpan.attrName != "" {
						currentSpan.innerEnd = prevOffset
						currentSpan.elemEnd = currOffset
						var rawInner []byte
						if !currentSpan.selfClosing && currentSpan.innerEnd >= currentSpan.innerStart {
							rawInner = utf8Bytes[currentSpan.innerStart:currentSpan.innerEnd]
						}
						txt := unescapeAndroidXML(string(rawInner))
						status := domain.StatusFinished
						if txt == "" {
							status = domain.StatusEmpty
						}
						entry := NormalizedEntry{
							Key: domain.EntryKey{
								ID:      currentSpan.attrName,
								Comment: lastComment,
							},
							LocaleID: locID,
							Text:     txt,
							Status:   status,
						}
						if isTemplate {
							entry.Key.Source = txt
						}
						adoc.items = append(adoc.items, androidItem{
							entry: entry,
							span:  currentSpan,
						})
						entries = append(entries, entry)
					}
					lastComment = ""
				}
			case "item":
				if inItem && inPlurals {
					inItem = false
					itemSpan.innerEnd = prevOffset
					itemSpan.elemEnd = currOffset
					var rawInner []byte
					if !itemSpan.selfClosing && itemSpan.innerEnd >= itemSpan.innerStart {
						rawInner = utf8Bytes[itemSpan.innerStart:itemSpan.innerEnd]
					}
					txt := unescapeAndroidXML(string(rawInner))
					pluralItems = append(pluralItems, itemSpan)
					pluralKeys = append(pluralKeys, itemSpan.quantity)
					pluralForms = append(pluralForms, txt)
				}
			case "plurals":
				if inPlurals {
					inPlurals = false
					if !skipCurrent && pluralsName != "" && len(pluralItems) > 0 {
						joined := domain.JoinNumerus(pluralForms)
						allEmpty := true
						for _, f := range pluralForms {
							if strings.TrimSpace(f) != "" {
								allEmpty = false
								break
							}
						}
						status := domain.StatusFinished
						if allEmpty {
							status = domain.StatusEmpty
						}
						entry := NormalizedEntry{
							Key: domain.EntryKey{
								ID:      pluralsName,
								Comment: lastComment,
							},
							LocaleID:    locID,
							Text:        joined,
							Numerus:     true,
							PluralForms: pluralForms,
							PluralKeys:  pluralKeys,
							Status:      status,
						}
						if isTemplate {
							entry.Key.Source = joined
						}
						adoc.items = append(adoc.items, androidItem{
							entry:     entry,
							itemSpans: pluralItems,
						})
						entries = append(entries, entry)
					}
					lastComment = ""
				}
			case "resources":
				adoc.resourcesClosePos = prevOffset
			}
		}
	}

	return &DocRecord{
		FormatID:   FormatAndroidXML,
		FilePath:   normRel,
		RelPath:    normRel,
		FileName:   filepath.Base(normRel),
		LocaleID:   locID,
		IsTemplate: isTemplate,
		HasBOM:     hasBOM,
		LineEnding: le,
		Raw:        utf8Bytes,
		Entries:    entries,
		Opaque:     adoc,
	}, nil
}

func unescapeAndroidXML(raw string) string {
	// Handle CDATA wrappers if present.
	if strings.Contains(raw, "<![CDATA[") {
		raw = strings.ReplaceAll(raw, "<![CDATA[", "")
		raw = strings.ReplaceAll(raw, "]]>", "")
	} else {
		raw = html.UnescapeString(raw)
	}
	if !strings.ContainsRune(raw, '\\') {
		return raw
	}
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			b.WriteByte(raw[i])
			continue
		}
		i++
		switch raw[i] {
		case '\'':
			b.WriteByte('\'')
		case '"':
			b.WriteByte('"')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(raw[i])
		}
	}
	return b.String()
}

func escapeAndroidXML(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '\'':
			b.WriteString(`\'`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (a *androidXMLAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	if doc == nil {
		return nil, 0, 0, fmt.Errorf("nil DocRecord")
	}
	adoc, ok := doc.Opaque.(*androidDoc)
	if !ok || adoc == nil {
		parsed, err := a.ParseDoc(doc.RelPath, doc.Raw, "", nil)
		if err != nil {
			return nil, 0, 0, err
		}
		adoc = parsed.Opaque.(*androidDoc)
	}

	le := doc.LineEnding
	if le == "" {
		le = "\n"
	}

	byID := make(map[string]*androidItem, len(adoc.items))
	for i := range adoc.items {
		it := &adoc.items[i]
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
		if it.entry.Numerus && len(it.itemSpans) > 0 {
			forms := p.PluralForms
			if len(forms) == 0 {
				forms = domain.SplitNumerus(p.NewText)
			}
			for fi, sp := range it.itemSpans {
				val := ""
				if fi < len(forms) {
					val = forms[fi]
				} else if len(forms) > 0 {
					val = forms[len(forms)-1]
				}
				edits = append(edits, androidSpanEdit(sp, val, idx*100+fi))
			}
			applied++
			continue
		}

		edits = append(edits, androidSpanEdit(it.span, p.NewText, idx*100))
		applied++
	}

	if len(appends) > 0 && adoc.resourcesClosePos >= 0 {
		var buf bytes.Buffer
		for _, p := range appends {
			id := p.Key.ID
			if id == "" {
				id = p.Key.Source
			}
			if p.Numerus {
				forms := p.PluralForms
				if len(forms) == 0 {
					forms = domain.SplitNumerus(p.NewText)
				}
				if len(forms) == 0 {
					forms = []string{""}
				}
				buf.WriteString(fmt.Sprintf(`    <plurals name="%s">`, escapeXMLAttr(id)) + le)
				qtys := []string{"one", "other"}
				for fi, f := range forms {
					qty := "other"
					if fi < len(qtys) {
						qty = qtys[fi]
					}
					buf.WriteString(fmt.Sprintf(`        <item quantity="%s">%s</item>`, qty, escapeAndroidXML(f)) + le)
				}
				buf.WriteString(`    </plurals>` + le)
			} else {
				buf.WriteString(fmt.Sprintf(`    <string name="%s">%s</string>`, escapeXMLAttr(id), escapeAndroidXML(p.NewText)) + le)
			}
			appended++
		}
		edits = append(edits, byteSpanEdit{
			start: adoc.resourcesClosePos,
			end:   adoc.resourcesClosePos,
			order: len(patches) * 1000,
			data:  buf.Bytes(),
		})
	}

	out := applyByteSpanEdits(doc.Raw, edits, doc.HasBOM)
	return out, applied, appended, nil
}

func androidSpanEdit(sp androidSpan, val string, order int) byteSpanEdit {
	escaped := escapeAndroidXML(val)
	if sp.selfClosing {
		if sp.tagName == "item" {
			return byteSpanEdit{
				start: sp.elemStart,
				end:   sp.elemEnd,
				order: order,
				data:  []byte(fmt.Sprintf(`<item quantity="%s">%s</item>`, escapeXMLAttr(sp.quantity), escaped)),
			}
		}
		return byteSpanEdit{
			start: sp.elemStart,
			end:   sp.elemEnd,
			order: order,
			data:  []byte(fmt.Sprintf(`<string name="%s">%s</string>`, escapeXMLAttr(sp.attrName), escaped)),
		}
	}
	return byteSpanEdit{
		start: sp.innerStart,
		end:   sp.innerEnd,
		order: order,
		data:  []byte(escaped),
	}
}

func (a *androidXMLAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	le := "\n"
	if template != nil && template.LineEnding != "" {
		le = template.LineEnding
	}
	if template == nil || len(template.Raw) == 0 {
		raw := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>%s<resources>%s</resources>%s`, le, le, le)
		doc, err := a.ParseDoc(newRelPath, []byte(raw), "", []locale.LocaleSpec{target})
		if err != nil {
			return nil, err
		}
		doc.LocaleID = target.ID
		doc.IsTemplate = false
		return doc, nil
	}

	adoc, ok := template.Opaque.(*androidDoc)
	if !ok || adoc == nil {
		parsed, err := a.ParseDoc(template.RelPath, template.Raw, "", nil)
		if err != nil {
			return nil, err
		}
		adoc = parsed.Opaque.(*androidDoc)
	}

	var edits []byteSpanEdit
	for idx, it := range adoc.items {
		if it.entry.Numerus {
			for fi, sp := range it.itemSpans {
				edits = append(edits, androidSpanEdit(sp, "", idx*100+fi))
			}
		} else {
			edits = append(edits, androidSpanEdit(it.span, "", idx*100))
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

func (a *androidXMLAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	id := key.ID
	if id == "" {
		id = key.Source
	}
	if numerus {
		render := func(txt string) string {
			var sb strings.Builder
			fmt.Fprintf(&sb, "<plurals name=\"%s\">\n", escapeXMLAttr(id))
			for _, f := range domain.SplitNumerus(txt) {
				fmt.Fprintf(&sb, "    <item>%s</item>\n", escapeAndroidXML(f))
			}
			sb.WriteString("</plurals>")
			return sb.String()
		}
		return render(oldText), render(newText)
	}
	return fmt.Sprintf(`<string name="%s">%s</string>`, escapeXMLAttr(id), escapeAndroidXML(oldText)),
		fmt.Sprintf(`<string name="%s">%s</string>`, escapeXMLAttr(id), escapeAndroidXML(newText))
}
