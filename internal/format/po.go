package format

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

type poAdapter struct{}

func init() {
	RegisterAdapter(&poAdapter{})
}

func (a *poAdapter) ID() FormatID {
	return FormatGettextPO
}

func (a *poAdapter) Name(lang string) string {
	return "GNU Gettext (.po / .pot)"
}

func (a *poAdapter) Topology() Topology {
	return TopologyPerLocale
}

func (a *poAdapter) IDAsKey() bool {
	return false
}

func (a *poAdapter) Extensions() []string {
	return []string{"po", "pot"}
}

func (a *poAdapter) MatchPath(relPath string, raw []byte) int {
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == ".po" || ext == ".pot" {
		return 95
	}
	return 0
}

type poLine struct {
	start    int // byte offset of line start
	end      int // byte offset before \r\n or \n
	fullEnd  int // byte offset after \r\n or \n
	text     string
}

type poBlock struct {
	entry       NormalizedEntry
	fuzzy       bool
	flagLines   []poLine
	msgstrStart int
	msgstrEnd   int
	pluralCount int
}

type poDoc struct {
	blocks []poBlock
}

func splitPOLines(raw []byte) []poLine {
	var lines []poLine
	n := len(raw)
	i := 0
	for i < n {
		start := i
		for i < n && raw[i] != '\n' {
			i++
		}
		end := i
		if end > start && raw[end-1] == '\r' {
			end--
		}
		fullEnd := i
		if i < n && raw[i] == '\n' {
			fullEnd = i + 1
			i++
		}
		lines = append(lines, poLine{
			start:   start,
			end:     end,
			fullEnd: fullEnd,
			text:    string(raw[start:end]),
		})
	}
	return lines
}

func (a *poAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	normRel := strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "./")
	utf8Bytes, hasBOM := decodeUTF8WithBOM(raw)
	le := detectLineEnding(utf8Bytes)
	isTemplate := strings.EqualFold(filepath.Ext(normRel), ".pot")

	lines := splitPOLines(utf8Bytes)
	var blocks []poBlock
	var entries []NormalizedEntry
	var headerLang string

	i := 0
	for i < len(lines) {
		// Skip blank lines.
		for i < len(lines) && strings.TrimSpace(lines[i].text) == "" {
			i++
		}
		if i >= len(lines) {
			break
		}

		// Collect one block until next blank line.
		var blkLines []poLine
		for i < len(lines) && strings.TrimSpace(lines[i].text) != "" {
			blkLines = append(blkLines, lines[i])
			i++
		}

		blk, isHeader, isObsolete, hdrLang := parsePOBlock(blkLines)
		if isObsolete {
			continue
		}
		if isHeader {
			if hdrLang != "" {
				headerLang = hdrLang
			}
			continue
		}
		blocks = append(blocks, blk)
		entries = append(entries, blk.entry)
	}

	locID := resolvePOLocaleID(normRel, filePattern, headerLang, isTemplate, known)
	for idx := range entries {
		entries[idx].LocaleID = locID
		blocks[idx].entry.LocaleID = locID
	}

	return &DocRecord{
		FormatID:   FormatGettextPO,
		FilePath:   normRel,
		RelPath:    normRel,
		FileName:   filepath.Base(normRel),
		LocaleID:   locID,
		IsTemplate: isTemplate,
		HasBOM:     hasBOM,
		LineEnding: le,
		Raw:        utf8Bytes,
		Entries:    entries,
		Opaque:     &poDoc{blocks: blocks},
	}, nil
}

func resolvePOLocaleID(relPath, filePattern, headerLang string, isTemplate bool, known []locale.LocaleSpec) string {
	if isTemplate {
		return ""
	}
	if headerLang != "" {
		if id, _, ok := locale.ParseHeaderLocale(headerLang, known); ok {
			return id
		}
	}
	stemmed := stemLocaleWithExt(relPath, filePattern, known)
	base := filepath.Base(relPath)
	rawStem := strings.TrimSuffix(base, filepath.Ext(base))
	if stemmed != "" && stemmed != rawStem {
		return stemmed
	}
	if headerLang != "" {
		return strings.ReplaceAll(strings.TrimSpace(headerLang), "-", "_")
	}
	return stemmed
}

func parsePOBlock(lines []poLine) (blk poBlock, isHeader bool, isObsolete bool, headerLang string) {
	var (
		msgctxt      strings.Builder
		msgid        strings.Builder
		msgidPlural  strings.Builder
		msgstr       strings.Builder
		pluralMap    = make(map[int]*strings.Builder)
		maxPluralIdx = -1
		comments     []string
		extraCmts    []string
		locations    []string
		activeField  string
		activePlural int
		hasMsgid     bool
		hasMsgstr    bool
		hasPlural    bool
	)

	// Check if all lines are obsolete (#~).
	allObsolete := true
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l.text), "#~") {
			allObsolete = false
			break
		}
	}
	if allObsolete {
		return blk, false, true, ""
	}

	for _, l := range lines {
		trimmed := strings.TrimSpace(l.text)
		if strings.HasPrefix(trimmed, "#~") {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(trimmed, "#.") {
				comments = append(comments, strings.TrimSpace(trimmed[2:]))
			} else if strings.HasPrefix(trimmed, "#:") {
				locations = append(locations, strings.TrimSpace(trimmed[2:]))
			} else if strings.HasPrefix(trimmed, "#,") {
				blk.flagLines = append(blk.flagLines, l)
				flags := strings.Split(trimmed[2:], ",")
				for _, f := range flags {
					if strings.TrimSpace(f) == "fuzzy" {
						blk.fuzzy = true
					}
				}
			} else if strings.HasPrefix(trimmed, "#|") {
				// Previous msgid; preserve in raw file, ignore in parsed key.
			} else {
				c := strings.TrimSpace(trimmed[1:])
				if c != "" {
					extraCmts = append(extraCmts, c)
				}
			}
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "msgctxt "):
			activeField = "msgctxt"
			msgctxt.WriteString(unquotePOString(trimmed[len("msgctxt "):]))
		case strings.HasPrefix(trimmed, "msgid_plural "):
			activeField = "msgid_plural"
			hasPlural = true
			msgidPlural.WriteString(unquotePOString(trimmed[len("msgid_plural "):]))
		case strings.HasPrefix(trimmed, "msgid "):
			activeField = "msgid"
			hasMsgid = true
			msgid.WriteString(unquotePOString(trimmed[len("msgid "):]))
		case strings.HasPrefix(trimmed, "msgstr["):
			if rb := strings.IndexByte(trimmed, ']'); rb > len("msgstr[") {
				if idx, err := strconv.Atoi(trimmed[len("msgstr["):rb]); err == nil {
					activeField = "msgstr_plural"
					activePlural = idx
					hasMsgstr = true
					if blk.msgstrStart == 0 {
						blk.msgstrStart = l.start
					}
					blk.msgstrEnd = l.end
					if idx > maxPluralIdx {
						maxPluralIdx = idx
					}
					sb, ok := pluralMap[idx]
					if !ok {
						sb = &strings.Builder{}
						pluralMap[idx] = sb
					}
					rest := strings.TrimSpace(trimmed[rb+1:])
					sb.WriteString(unquotePOString(rest))
				}
			}
		case strings.HasPrefix(trimmed, "msgstr "):
			activeField = "msgstr"
			hasMsgstr = true
			if blk.msgstrStart == 0 {
				blk.msgstrStart = l.start
			}
			blk.msgstrEnd = l.end
			msgstr.WriteString(unquotePOString(trimmed[len("msgstr "):]))
		case strings.HasPrefix(trimmed, "\""):
			val := unquotePOString(trimmed)
			switch activeField {
			case "msgctxt":
				msgctxt.WriteString(val)
			case "msgid":
				msgid.WriteString(val)
			case "msgid_plural":
				msgidPlural.WriteString(val)
			case "msgstr":
				blk.msgstrEnd = l.end
				msgstr.WriteString(val)
			case "msgstr_plural":
				blk.msgstrEnd = l.end
				if sb, ok := pluralMap[activePlural]; ok {
					sb.WriteString(val)
				}
			}
		}
	}

	if !hasMsgid {
		return blk, false, true, ""
	}

	src := msgid.String()
	ctx := msgctxt.String()
	if src == "" && ctx == "" {
		// PO header block.
		hdrText := msgstr.String()
		for _, hl := range strings.Split(hdrText, "\n") {
			if strings.HasPrefix(strings.ToLower(hl), "language:") {
				headerLang = strings.TrimSpace(hl[len("language:"):])
				break
			}
		}
		return blk, true, false, headerLang
	}

	var forms []string
	var text string
	var status domain.TransStatus
	if hasPlural {
		count := maxPluralIdx + 1
		if count < 1 {
			count = 1
		}
		blk.pluralCount = count
		forms = make([]string, count)
		allEmpty := true
		for idx := 0; idx < count; idx++ {
			if sb, ok := pluralMap[idx]; ok {
				forms[idx] = sb.String()
				if strings.TrimSpace(forms[idx]) != "" {
					allEmpty = false
				}
			}
		}
		text = domain.JoinNumerus(forms)
		if blk.fuzzy {
			status = domain.StatusUnfinished
		} else if allEmpty || !hasMsgstr {
			status = domain.StatusEmpty
		} else {
			status = domain.StatusFinished
		}
	} else {
		text = msgstr.String()
		if blk.fuzzy {
			status = domain.StatusUnfinished
		} else if text == "" || !hasMsgstr {
			status = domain.StatusEmpty
		} else {
			status = domain.StatusFinished
		}
	}

	blk.entry = NormalizedEntry{
		Key: domain.EntryKey{
			Context: ctx,
			Source:  src,
			Comment: strings.Join(comments, "\n"),
		},
		Text:         text,
		Numerus:      hasPlural,
		PluralForms:  forms,
		Status:       status,
		Location:     strings.Join(locations, " "),
		ExtraComment: strings.Join(extraCmts, "\n"),
	}
	return blk, false, false, ""
}

func unquotePOString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func escapePOString(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func formatPOField(keyword, val, le string) string {
	norm := strings.ReplaceAll(val, "\r\n", "\n")
	if !strings.ContainsRune(norm, '\n') {
		return keyword + ` "` + escapePOString(norm) + `"`
	}
	lines := strings.SplitAfter(norm, "\n")
	var b strings.Builder
	b.WriteString(keyword)
	b.WriteString(` ""`)
	for _, l := range lines {
		if l == "" {
			continue
		}
		b.WriteString(le)
		b.WriteByte('"')
		b.WriteString(escapePOString(l))
		b.WriteByte('"')
	}
	return b.String()
}

func (a *poAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	if doc == nil {
		return nil, 0, 0, fmt.Errorf("nil DocRecord")
	}
	pdoc, ok := doc.Opaque.(*poDoc)
	if !ok || pdoc == nil {
		parsed, err := a.ParseDoc(doc.RelPath, doc.Raw, "", nil)
		if err != nil {
			return nil, 0, 0, err
		}
		pdoc = parsed.Opaque.(*poDoc)
	}

	le := doc.LineEnding
	if le == "" {
		le = "\n"
	}

	byExact := make(map[string]*poBlock, len(pdoc.blocks))
	byCtxSrc := make(map[domain.EntryKey]*poBlock, len(pdoc.blocks))
	for i := range pdoc.blocks {
		b := &pdoc.blocks[i]
		byExact[b.entry.Key.Canonical()] = b
		cs := b.entry.Key.ContextSource()
		if _, exists := byCtxSrc[cs]; !exists {
			byCtxSrc[cs] = b
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
		blk := byExact[p.Key.Canonical()]
		if blk == nil {
			blk = byCtxSrc[p.Key.ContextSource()]
		}
		if blk == nil || blk.msgstrEnd <= blk.msgstrStart {
			continue
		}

		// Remove fuzzy flag from #, lines if present.
		if blk.fuzzy {
			for _, fl := range blk.flagLines {
				trimmed := strings.TrimSpace(fl.text)
				if !strings.HasPrefix(trimmed, "#,") {
					continue
				}
				parts := strings.Split(trimmed[2:], ",")
				var kept []string
				for _, part := range parts {
					pTrim := strings.TrimSpace(part)
					if pTrim != "" && pTrim != "fuzzy" {
						kept = append(kept, pTrim)
					}
				}
				if len(kept) == 0 {
					edits = append(edits, byteSpanEdit{
						start: fl.start,
						end:   fl.fullEnd,
						order: idx*10 + 1,
						data:  nil,
					})
				} else {
					edits = append(edits, byteSpanEdit{
						start: fl.start,
						end:   fl.end,
						order: idx*10 + 1,
						data:  []byte("#, " + strings.Join(kept, ", ")),
					})
				}
			}
		}

		var repl string
		if blk.entry.Numerus || p.Numerus {
			forms := p.PluralForms
			if len(forms) == 0 {
				forms = domain.SplitNumerus(p.NewText)
			}
			if len(forms) == 0 {
				forms = []string{""}
			}
			var sb strings.Builder
			for fi, f := range forms {
				if fi > 0 {
					sb.WriteString(le)
				}
				sb.WriteString(formatPOField(fmt.Sprintf("msgstr[%d]", fi), f, le))
			}
			repl = sb.String()
		} else {
			repl = formatPOField("msgstr", p.NewText, le)
		}

		edits = append(edits, byteSpanEdit{
			start: blk.msgstrStart,
			end:   blk.msgstrEnd,
			order: idx*10 + 2,
			data:  []byte(repl),
		})
		applied++
	}

	if len(appends) > 0 {
		var appBuf bytes.Buffer
		if len(doc.Raw) > 0 && !bytes.HasSuffix(doc.Raw, []byte("\n")) {
			appBuf.WriteString(le)
		}
		for _, p := range appends {
			appBuf.WriteString(le)
			if p.Key.Comment != "" {
				for _, cl := range strings.Split(p.Key.Comment, "\n") {
					appBuf.WriteString("#. " + cl + le)
				}
			}
			if p.Key.Context != "" {
				appBuf.WriteString(formatPOField("msgctxt", p.Key.Context, le) + le)
			}
			appBuf.WriteString(formatPOField("msgid", p.Key.Source, le) + le)
			if p.Numerus {
				appBuf.WriteString(formatPOField("msgid_plural", p.Key.Source, le) + le)
				forms := p.PluralForms
				if len(forms) == 0 {
					forms = domain.SplitNumerus(p.NewText)
				}
				if len(forms) == 0 {
					forms = []string{""}
				}
				for fi, f := range forms {
					appBuf.WriteString(formatPOField(fmt.Sprintf("msgstr[%d]", fi), f, le) + le)
				}
			} else {
				appBuf.WriteString(formatPOField("msgstr", p.NewText, le) + le)
			}
			appended++
		}
		edits = append(edits, byteSpanEdit{
			start: len(doc.Raw),
			end:   len(doc.Raw),
			order: len(patches) * 100,
			data:  appBuf.Bytes(),
		})
	}

	out := applyByteSpanEdits(doc.Raw, edits, doc.HasBOM)
	return out, applied, appended, nil
}

func (a *poAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	le := "\n"
	if template != nil && template.LineEnding != "" {
		le = template.LineEnding
	}
	if template == nil || len(template.Raw) == 0 {
		raw := fmt.Sprintf(`msgid ""%smsgstr ""%s"Content-Type: text/plain; charset=UTF-8\n"%s"Language: %s\n"%s`,
			le, le, le, target.ID, le)
		return a.ParseDoc(newRelPath, []byte(raw), "", []locale.LocaleSpec{target})
	}

	pdoc, ok := template.Opaque.(*poDoc)
	if !ok || pdoc == nil {
		parsed, err := a.ParseDoc(template.RelPath, template.Raw, "", nil)
		if err != nil {
			return nil, err
		}
		pdoc = parsed.Opaque.(*poDoc)
	}

	var edits []byteSpanEdit
	for idx, blk := range pdoc.blocks {
		if blk.fuzzy {
			for _, fl := range blk.flagLines {
				trimmed := strings.TrimSpace(fl.text)
				if !strings.HasPrefix(trimmed, "#,") {
					continue
				}
				parts := strings.Split(trimmed[2:], ",")
				var kept []string
				for _, part := range parts {
					pTrim := strings.TrimSpace(part)
					if pTrim != "" && pTrim != "fuzzy" {
						kept = append(kept, pTrim)
					}
				}
				if len(kept) == 0 {
					edits = append(edits, byteSpanEdit{start: fl.start, end: fl.fullEnd, order: idx*10 + 1})
				} else {
					edits = append(edits, byteSpanEdit{start: fl.start, end: fl.end, order: idx*10 + 1, data: []byte("#, " + strings.Join(kept, ", "))})
				}
			}
		}
		if blk.msgstrEnd > blk.msgstrStart {
			if blk.entry.Numerus {
				count := blk.pluralCount
				if count < 1 {
					count = 1
				}
				var sb strings.Builder
				for fi := 0; fi < count; fi++ {
					if fi > 0 {
						sb.WriteString(le)
					}
					sb.WriteString(fmt.Sprintf(`msgstr[%d] ""`, fi))
				}
				edits = append(edits, byteSpanEdit{start: blk.msgstrStart, end: blk.msgstrEnd, order: idx*10 + 2, data: []byte(sb.String())})
			} else {
				edits = append(edits, byteSpanEdit{start: blk.msgstrStart, end: blk.msgstrEnd, order: idx*10 + 2, data: []byte(`msgstr ""`)})
			}
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

func (a *poAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	render := func(txt string, fuzzy bool) string {
		var sb strings.Builder
		if fuzzy {
			sb.WriteString("#, fuzzy\n")
		}
		if key.Context != "" {
			sb.WriteString(formatPOField("msgctxt", key.Context, "\n") + "\n")
		}
		sb.WriteString(formatPOField("msgid", key.Source, "\n") + "\n")
		if numerus {
			forms := domain.SplitNumerus(txt)
			if len(forms) == 0 {
				forms = []string{""}
			}
			for i, f := range forms {
				sb.WriteString(formatPOField(fmt.Sprintf("msgstr[%d]", i), f, "\n") + "\n")
			}
		} else {
			sb.WriteString(formatPOField("msgstr", txt, "\n"))
		}
		return strings.TrimRight(sb.String(), "\n")
	}
	return render(oldText, oldStatus == domain.StatusUnfinished), render(newText, false)
}
