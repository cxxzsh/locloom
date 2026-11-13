package tsxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/platform"
)

// EscapeXMLText escapes &, <, > only (preserving quotes like Qt lupdate)
// and encodes XML-illegal control characters < 0x20 (except \t, \n, \r) as <byte value="x..."/>.
func EscapeXMLText(s string) string {
	s = ensureValidUTF8(s)
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if r == utf8.RuneError {
			continue
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			fmt.Fprintf(&buf, `<byte value="x%x"/>`, r)
			continue
		}
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func applyLineEnding(s, le string) string {
	if le == "" {
		return s
	}
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	if le == "\r\n" {
		return strings.ReplaceAll(normalized, "\n", "\r\n")
	}
	return normalized
}

type spanPatch struct {
	start int
	end   int
	order int
	data  []byte
}

func renderTranslationElement(forms []string, newText string, numerus bool, indent string, le string) []byte {
	if numerus {
		if len(forms) == 0 {
			forms = domain.SplitNumerus(newText)
		}
		if len(forms) == 0 {
			forms = []string{""}
		}
		var buf bytes.Buffer
		buf.WriteString("<translation>")
		for _, f := range forms {
			buf.WriteString(le)
			buf.WriteString(indent)
			buf.WriteString("    <numerusform>")
			buf.WriteString(applyLineEnding(EscapeXMLText(f), le))
			buf.WriteString("</numerusform>")
		}
		buf.WriteString(le)
		buf.WriteString(indent)
		buf.WriteString("</translation>")
		return buf.Bytes()
	}
	return []byte("<translation>" + applyLineEnding(EscapeXMLText(newText), le) + "</translation>")
}

func renderMessageBlock(p PatchItem, msgIndent, le string) []byte {
	var buf bytes.Buffer
	buf.WriteString(msgIndent)
	if p.Numerus {
		buf.WriteString(`<message numerus="yes">`)
	} else {
		buf.WriteString(`<message>`)
	}
	buf.WriteString(le)
	innerIndent := msgIndent + "    "
	buf.WriteString(innerIndent)
	buf.WriteString("<source>")
	buf.WriteString(applyLineEnding(EscapeXMLText(p.Key.Source), le))
	buf.WriteString("</source>")
	buf.WriteString(le)
	if p.Key.Comment != "" {
		buf.WriteString(innerIndent)
		buf.WriteString("<comment>")
		buf.WriteString(applyLineEnding(EscapeXMLText(p.Key.Comment), le))
		buf.WriteString("</comment>")
		buf.WriteString(le)
	}
	buf.WriteString(innerIndent)
	buf.Write(renderTranslationElement(p.NumerusForms, p.NewText, p.Numerus, innerIndent, le))
	buf.WriteString(le)
	buf.WriteString(msgIndent)
	buf.WriteString("</message>")
	buf.WriteString(le)
	return buf.Bytes()
}

func findMessageInContext(ctx *ContextBlock, key domain.EntryKey) *MessageEntry {
	for i := range ctx.Messages {
		m := &ctx.Messages[i]
		if m.Source == key.Source && m.Comment == key.Comment {
			return m
		}
	}
	for i := range ctx.Messages {
		m := &ctx.Messages[i]
		if m.Source == key.Source {
			return m
		}
	}
	return nil
}

// ApplyPatches applies patches to tf.Raw using strict context-scoped matching,
// validates the resulting XML with xml.NewDecoder, and restores BOM if tf.HasBOM.
func ApplyPatches(tf *TSFile, patches []PatchItem) (*PatchResult, error) {
	if tf == nil {
		return nil, fmt.Errorf("nil TSFile")
	}
	le := tf.LineEnding
	if le == "" {
		le = "\n"
	}

	ctxByName := make(map[string]*ContextBlock, len(tf.Contexts))
	for i := range tf.Contexts {
		c := &tf.Contexts[i]
		if _, exists := ctxByName[c.Name]; !exists {
			ctxByName[c.Name] = c
		}
	}

	var ops []spanPatch
	res := &PatchResult{}
	newContexts := make(map[string][]PatchItem)
	var newContextOrder []string

	for idx, p := range patches {
		if !p.Action.Mutates() {
			continue
		}

		if p.Action == domain.ActionAppendNew {
			if ctx, ok := ctxByName[p.Key.Context]; ok && ctx.CloseTagStart > 0 {
				indent := ctx.Indent
				if indent == "" {
					indent = "    "
				}
				// Ensure insertion starts at a clean line if needed
				block := renderMessageBlock(p, indent, le)
				ops = append(ops, spanPatch{
					start: ctx.CloseTagStart,
					end:   ctx.CloseTagStart,
					order: idx,
					data:  block,
				})
				res.Appended++
			} else if tf.CloseTagStart > 0 {
				if _, seen := newContexts[p.Key.Context]; !seen {
					newContextOrder = append(newContextOrder, p.Key.Context)
				}
				newContexts[p.Key.Context] = append(newContexts[p.Key.Context], p)
				res.Appended++
			} else {
				res.Skipped++
			}
			continue
		}

		// ActionFillGap or ActionOverwrite: strict same-context matching only.
		ctx, ok := ctxByName[p.Key.Context]
		if !ok {
			res.Skipped++
			continue
		}
		m := findMessageInContext(ctx, p.Key)
		if m == nil || m.TransEnd <= m.TransStart {
			res.Skipped++
			continue
		}

		indent := leadingLineIndent(tf.Raw, m.TransStart)
		if indent == "" {
			indent = "        "
		}
		isNumerus := p.Numerus || m.Numerus
		forms := p.NumerusForms
		if isNumerus && len(forms) == 0 {
			forms = domain.SplitNumerus(p.NewText)
		}
		if isNumerus && len(m.NumerusForms) > len(forms) {
			padded := make([]string, len(m.NumerusForms))
			copy(padded, forms)
			if len(forms) == 1 && forms[0] != "" {
				for k := 1; k < len(padded); k++ {
					padded[k] = forms[0]
				}
			}
			forms = padded
		}

		ops = append(ops, spanPatch{
			start: m.TransStart,
			end:   m.TransEnd,
			order: idx,
			data:  renderTranslationElement(forms, p.NewText, isNumerus, indent, le),
		})
		res.Applied++
	}

	// Render brand-new <context> blocks before </TS>.
	if len(newContextOrder) > 0 && tf.CloseTagStart > 0 {
		var buf bytes.Buffer
		for _, ctxName := range newContextOrder {
			buf.WriteString("<context>")
			buf.WriteString(le)
			buf.WriteString("    <name>")
			buf.WriteString(applyLineEnding(EscapeXMLText(ctxName), le))
			buf.WriteString("</name>")
			buf.WriteString(le)
			for _, item := range newContexts[ctxName] {
				buf.Write(renderMessageBlock(item, "    ", le))
			}
			buf.WriteString("</context>")
			buf.WriteString(le)
		}
		ops = append(ops, spanPatch{
			start: tf.CloseTagStart,
			end:   tf.CloseTagStart,
			order: len(patches) + 1,
			data:  buf.Bytes(),
		})
	}

	// Deduplicate replacement spans with identical [start, end) where start < end (last wins).
	deduped := make([]spanPatch, 0, len(ops))
	replaceIdx := make(map[[2]int]int)
	for _, op := range ops {
		if op.start < op.end {
			k := [2]int{op.start, op.end}
			if pos, exists := replaceIdx[k]; exists {
				deduped[pos] = op
				continue
			}
			replaceIdx[k] = len(deduped)
		}
		deduped = append(deduped, op)
	}

	sort.Slice(deduped, func(i, j int) bool {
		if deduped[i].start != deduped[j].start {
			return deduped[i].start < deduped[j].start
		}
		if deduped[i].end != deduped[j].end {
			return deduped[i].end < deduped[j].end
		}
		return deduped[i].order < deduped[j].order
	})

	var out bytes.Buffer
	out.Grow(len(tf.Raw) + 256)
	cursor := 0
	for _, op := range deduped {
		if op.start < cursor {
			continue
		}
		out.Write(tf.Raw[cursor:op.start])
		out.Write(op.data)
		cursor = op.end
	}
	out.Write(tf.Raw[cursor:])

	// Validate resulting XML before returning.
	if err := validateXML(out.Bytes()); err != nil {
		return nil, fmt.Errorf("patched XML validation failed for %q: %w", tf.RelPath, err)
	}

	if tf.HasBOM {
		final := make([]byte, 3+out.Len())
		final[0], final[1], final[2] = 0xEF, 0xBB, 0xBF
		copy(final[3:], out.Bytes())
		res.OutputRaw = final
	} else {
		res.OutputRaw = out.Bytes()
	}
	return res, nil
}

func validateXML(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	for {
		_, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

var (
	reTSOpenTag   = regexp.MustCompile(`(?i)<TS\b[^>]*>`)
	reLangAttrVal = regexp.MustCompile(`(?i)(\blanguage\s*=\s*)(["'])[^"']*["']`)
)

// CloneSkeleton clones a template TSFile for a target locale, updating only the <TS language="...">
// attribute and resetting all <translation> spans to unfinished empty elements.
func CloneSkeleton(template *TSFile, targetLocale locale.LocaleSpec, newRelPath string) (*TSFile, error) {
	if template == nil {
		return nil, fmt.Errorf("nil template TSFile")
	}
	le := template.LineEnding
	if le == "" {
		le = "\n"
	}

	var ops []spanPatch

	// Update or insert language="..." in <TS ...> opening tag.
	if loc := reTSOpenTag.FindIndex(template.Raw); loc != nil {
		tag := string(template.Raw[loc[0]:loc[1]])
		var updatedTag string
		if reLangAttrVal.MatchString(tag) {
			updatedTag = reLangAttrVal.ReplaceAllString(tag, `${1}${2}`+EscapeXMLText(targetLocale.ID)+`${2}`)
		} else if strings.HasSuffix(tag, "/>") {
			updatedTag = tag[:len(tag)-2] + fmt.Sprintf(` language="%s"/>`, EscapeXMLText(targetLocale.ID))
		} else {
			updatedTag = tag[:len(tag)-1] + fmt.Sprintf(` language="%s">`, EscapeXMLText(targetLocale.ID))
		}
		ops = append(ops, spanPatch{
			start: loc[0],
			end:   loc[1],
			data:  []byte(updatedTag),
		})
	}

	// Reset every <translation> span to unfinished empty.
	for _, ctx := range template.Contexts {
		for _, m := range ctx.Messages {
			if m.TransEnd <= m.TransStart {
				continue
			}
			var repl []byte
			if m.Numerus {
				indent := leadingLineIndent(template.Raw, m.TransStart)
				if indent == "" {
					indent = "        "
				}
				count := len(m.NumerusForms)
				if count == 0 {
					count = 1
				}
				var buf bytes.Buffer
				buf.WriteString(`<translation type="unfinished">`)
				for range count {
					buf.WriteString(le)
					buf.WriteString(indent)
					buf.WriteString("    <numerusform></numerusform>")
				}
				buf.WriteString(le)
				buf.WriteString(indent)
				buf.WriteString("</translation>")
				repl = buf.Bytes()
			} else {
				repl = []byte(`<translation type="unfinished"></translation>`)
			}
			ops = append(ops, spanPatch{
				start: m.TransStart,
				end:   m.TransEnd,
				data:  repl,
			})
		}
	}

	sort.Slice(ops, func(i, j int) bool {
		return ops[i].start < ops[j].start
	})

	var out bytes.Buffer
	if template.HasBOM {
		out.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	cursor := 0
	for _, op := range ops {
		if op.start < cursor {
			continue
		}
		out.Write(template.Raw[cursor:op.start])
		out.Write(op.data)
		cursor = op.end
	}
	out.Write(template.Raw[cursor:])

	cloned, err := ParseBytes(newRelPath, out.Bytes())
	if err != nil {
		return nil, err
	}
	cloned.LocaleID = targetLocale.ID
	return cloned, nil
}

// NewEmptyTSFile creates a valid empty Qt 2.1 TSFile for targetLocale.
func NewEmptyTSFile(targetLocale locale.LocaleSpec, relPath string, lineEnding string) *TSFile {
	le := lineEnding
	if le != "\r\n" {
		le = "\n"
	}
	raw := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>%s<!DOCTYPE TS>%s<TS version="2.1" language="%s">%s</TS>%s`,
		le, le, EscapeXMLText(targetLocale.ID), le, le)
	tf, _ := ParseBytes(relPath, []byte(raw))
	if tf != nil {
		tf.LocaleID = targetLocale.ID
	}
	return tf
}

// SaveBundle writes patched .ts files to a directory (using AtomicWriteFile) or a .zip archive.
func SaveBundle(bundle *Bundle, patched map[string][]byte, opts SaveOptions) (*SaveReport, error) {
	if bundle == nil {
		return nil, fmt.Errorf("nil Bundle")
	}
	report := &SaveReport{}

	if !bundle.IsZip {
		outDir := strings.TrimSpace(opts.OutputPath)
		if outDir == "" {
			outDir = bundle.SourcePath
		}
		report.OutputPath = outDir

		relKeys := make([]string, 0, len(patched))
		for k := range patched {
			relKeys = append(relKeys, k)
		}
		sort.Strings(relKeys)

		for _, relPath := range relKeys {
			data := patched[relPath]
			targetPath := filepath.Join(outDir, filepath.FromSlash(relPath))
			if opts.CreateBackup {
				if _, err := os.Stat(targetPath); err == nil {
					orig, readErr := platform.ReadFileShared(targetPath)
					if readErr == nil {
						bakPath := targetPath + ".bak"
						if err := platform.AtomicWriteFile(bakPath, orig, 0o644); err != nil {
							return nil, fmt.Errorf("write backup %q: %w", bakPath, err)
						}
						report.BackupPaths = append(report.BackupPaths, bakPath)
					}
				}
			}
			if err := platform.AtomicWriteFile(targetPath, data, 0o644); err != nil {
				return nil, fmt.Errorf("write %q: %w", targetPath, err)
			}
			report.FilesWritten++
		}
		return report, nil
	}

	// ZIP output
	outZip := strings.TrimSpace(opts.OutputPath)
	if outZip == "" {
		ext := filepath.Ext(bundle.SourcePath)
		stem := strings.TrimSuffix(bundle.SourcePath, ext)
		outZip = stem + "_updated.zip"
	}
	report.OutputPath = outZip

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	written := make(map[string]bool)
	for _, f := range bundle.Files {
		data, isPatched := patched[f.RelPath]
		if isPatched {
			report.FilesWritten++
		} else {
			if f.HasBOM {
				data = append([]byte{0xEF, 0xBB, 0xBF}, f.Raw...)
			} else {
				data = f.Raw
			}
		}
		w, err := zw.Create(f.RelPath)
		if err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("create zip entry %q: %w", f.RelPath, err)
		}
		if _, err := w.Write(data); err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("write zip entry %q: %w", f.RelPath, err)
		}
		written[f.RelPath] = true
	}

	// Also write newly created/cloned .ts files not originally in bundle.Files.
	extraPatchedKeys := make([]string, 0)
	for relPath := range patched {
		if !written[relPath] {
			extraPatchedKeys = append(extraPatchedKeys, relPath)
		}
	}
	sort.Strings(extraPatchedKeys)
	for _, relPath := range extraPatchedKeys {
		w, err := zw.Create(relPath)
		if err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("create zip entry %q: %w", relPath, err)
		}
		if _, err := w.Write(patched[relPath]); err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("write zip entry %q: %w", relPath, err)
		}
		report.FilesWritten++
	}

	// Preserve non-.ts ZipExtras verbatim.
	extraKeys := make([]string, 0, len(bundle.ZipExtras))
	for k := range bundle.ZipExtras {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		w, err := zw.Create(k)
		if err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("create zip extra %q: %w", k, err)
		}
		if _, err := w.Write(bundle.ZipExtras[k]); err != nil {
			_ = zw.Close()
			return nil, fmt.Errorf("write zip extra %q: %w", k, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip writer: %w", err)
	}
	if err := platform.AtomicWriteFile(outZip, zipBuf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("write zip %q: %w", outZip, err)
	}
	return report, nil
}

// FormatSnippetDiff renders human-readable before/after <message> XML snippets for inspector preview.
func FormatSnippetDiff(ctxName, source, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (beforeXML, afterXML string) {
	render := func(text string, unfinished bool) string {
		var b strings.Builder
		fmt.Fprintf(&b, "<context>\n    <name>%s</name>\n", EscapeXMLText(ctxName))
		if numerus {
			fmt.Fprintf(&b, "    <message numerus=\"yes\">\n        <source>%s</source>\n", EscapeXMLText(source))
			if unfinished {
				b.WriteString("        <translation type=\"unfinished\">\n")
			} else {
				b.WriteString("        <translation>\n")
			}
			forms := domain.SplitNumerus(text)
			if len(forms) == 0 {
				forms = []string{""}
			}
			for _, f := range forms {
				fmt.Fprintf(&b, "            <numerusform>%s</numerusform>\n", EscapeXMLText(f))
			}
			b.WriteString("        </translation>\n    </message>\n</context>")
		} else {
			fmt.Fprintf(&b, "    <message>\n        <source>%s</source>\n", EscapeXMLText(source))
			if unfinished {
				fmt.Fprintf(&b, "        <translation type=\"unfinished\">%s</translation>\n", EscapeXMLText(text))
			} else {
				fmt.Fprintf(&b, "        <translation>%s</translation>\n", EscapeXMLText(text))
			}
			b.WriteString("    </message>\n</context>")
		}
		return b.String()
	}

	oldUnfinished := oldStatus == domain.StatusUnfinished || oldStatus == domain.StatusEmpty || oldStatus == domain.StatusMissing
	return render(oldText, oldUnfinished), render(newText, false)
}
