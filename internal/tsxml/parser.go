package tsxml

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/platform"
)

// decodeToUTF8 strips UTF-8 BOM or transcodes UTF-16LE/BE BOM inputs to UTF-8.
func decodeToUTF8(raw []byte) (utf8Bytes []byte, hasBOM bool) {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return raw[3:], true
	}
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		// UTF-16LE
		data := raw[2:]
		u16 := make([]uint16, 0, len(data)/2)
		for i := 0; i+1 < len(data); i += 2 {
			u16 = append(u16, binary.LittleEndian.Uint16(data[i:i+2]))
		}
		runes := utf16.Decode(u16)
		var buf bytes.Buffer
		buf.Grow(len(runes))
		for _, r := range runes {
			buf.WriteRune(r)
		}
		return buf.Bytes(), false
	}
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		// UTF-16BE
		data := raw[2:]
		u16 := make([]uint16, 0, len(data)/2)
		for i := 0; i+1 < len(data); i += 2 {
			u16 = append(u16, binary.BigEndian.Uint16(data[i:i+2]))
		}
		runes := utf16.Decode(u16)
		var buf bytes.Buffer
		buf.Grow(len(runes))
		for _, r := range runes {
			buf.WriteRune(r)
		}
		return buf.Bytes(), false
	}
	return raw, false
}

// ParseBytes parses a raw Qt .ts file into a TSFile struct with byte-accurate offsets.
func ParseBytes(relPath string, raw []byte) (*TSFile, error) {
	normRel := strings.TrimPrefix(strings.ReplaceAll(relPath, "\\", "/"), "./")
	utf8Bytes, hasBOM := decodeToUTF8(raw)

	lineEnding := "\n"
	if bytes.Contains(utf8Bytes, []byte("\r\n")) {
		lineEnding = "\r\n"
	}

	tf := &TSFile{
		FilePath:   normRel,
		RelPath:    normRel,
		FileName:   filepath.Base(normRel),
		LocaleID:   locale.StemLocaleID(normRel, "", nil),
		HasBOM:     hasBOM,
		LineEnding: lineEnding,
		Raw:        utf8Bytes,
	}

	if err := parseTSXML(tf); err != nil {
		return nil, fmt.Errorf("parse TS %q: %w", normRel, err)
	}

	return tf, nil
}

func parseTSXML(tf *TSFile) error {
	raw := tf.Raw
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	var currentCtx *ContextBlock
	var currentMsg MessageEntry
	var inContextName bool
	var inMessage bool
	var inSource bool
	var inComment bool
	var inExtraComment bool
	var inTranslation bool
	var inNumerusForm bool
	var transType string

	var nameBuf bytes.Buffer
	var sourceBuf bytes.Buffer
	var commentBuf bytes.Buffer
	var extraCommentBuf bytes.Buffer
	var transBuf bytes.Buffer
	var numerusBuf bytes.Buffer

	for {
		prevOffset := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		currOffset := int(decoder.InputOffset())

		switch tok := token.(type) {
		case xml.StartElement:
			name := strings.ToLower(tok.Name.Local)
			switch name {
			case "ts":
				for _, attr := range tok.Attr {
					switch strings.ToLower(attr.Name.Local) {
					case "language":
						tf.Language = strings.TrimSpace(attr.Value)
					case "sourcelanguage":
						tf.SourceLang = strings.TrimSpace(attr.Value)
					}
				}
			case "context":
				currentCtx = &ContextBlock{Indent: "    "}
			case "name":
				if currentCtx != nil && !inMessage {
					inContextName = true
					nameBuf.Reset()
				}
			case "message":
				if currentCtx != nil {
					inMessage = true
					currentMsg = MessageEntry{
						Context: currentCtx.Name,
						Status:  domain.StatusMissing,
					}
					for _, attr := range tok.Attr {
						if strings.EqualFold(attr.Name.Local, "numerus") && strings.EqualFold(strings.TrimSpace(attr.Value), "yes") {
							currentMsg.Numerus = true
						}
					}
					sourceBuf.Reset()
					commentBuf.Reset()
					extraCommentBuf.Reset()
					transBuf.Reset()
					numerusBuf.Reset()
					// Capture message indent if not yet set
					if idx := bytes.IndexByte(raw[prevOffset:currOffset], '<'); idx >= 0 {
						tagStart := prevOffset + idx
						if ind := leadingLineIndent(raw, tagStart); ind != "" {
							currentCtx.Indent = ind
						}
					}
				}
			case "location":
				if inMessage && currentMsg.LocationFile == "" {
					for _, attr := range tok.Attr {
						switch strings.ToLower(attr.Name.Local) {
						case "filename":
							currentMsg.LocationFile = attr.Value
						case "line":
							currentMsg.LocationLine = attr.Value
						}
					}
				}
			case "source":
				if inMessage {
					inSource = true
					sourceBuf.Reset()
				}
			case "comment":
				if inMessage {
					inComment = true
					commentBuf.Reset()
				}
			case "extracomment":
				if inMessage {
					inExtraComment = true
					extraCommentBuf.Reset()
				}
			case "translation":
				if inMessage {
					inTranslation = true
					transBuf.Reset()
					transType = ""
					currentMsg.NumerusForms = nil
					for _, attr := range tok.Attr {
						if strings.EqualFold(attr.Name.Local, "type") {
							transType = strings.ToLower(strings.TrimSpace(attr.Value))
						}
					}
					if idx := bytes.IndexByte(raw[prevOffset:currOffset], '<'); idx >= 0 {
						currentMsg.TransStart = prevOffset + idx
					} else {
						currentMsg.TransStart = prevOffset
					}
				}
			case "numerusform":
				if inMessage && inTranslation {
					inNumerusForm = true
					numerusBuf.Reset()
				}
			case "byte":
				if b, ok := parseByteTag(tok.Attr); ok {
					writeRune := func(buf *bytes.Buffer) {
						buf.WriteRune(rune(b))
					}
					switch {
					case inSource:
						writeRune(&sourceBuf)
					case inComment:
						writeRune(&commentBuf)
					case inExtraComment:
						writeRune(&extraCommentBuf)
					case inNumerusForm:
						writeRune(&numerusBuf)
					case inTranslation:
						writeRune(&transBuf)
					}
				}
			}

		case xml.CharData:
			switch {
			case inContextName:
				nameBuf.Write(tok)
			case inSource:
				sourceBuf.Write(tok)
			case inComment:
				commentBuf.Write(tok)
			case inExtraComment:
				extraCommentBuf.Write(tok)
			case inNumerusForm:
				numerusBuf.Write(tok)
			case inTranslation && !currentMsg.Numerus:
				transBuf.Write(tok)
			}

		case xml.EndElement:
			name := strings.ToLower(tok.Name.Local)
			switch name {
			case "ts":
				if idx := bytes.LastIndexByte(raw[prevOffset:currOffset], '<'); idx >= 0 {
					tf.CloseTagStart = prevOffset + idx
				}
			case "context":
				if currentCtx != nil {
					if idx := bytes.LastIndexByte(raw[prevOffset:currOffset], '<'); idx >= 0 {
						currentCtx.CloseTagStart = prevOffset + idx
					}
					tf.Contexts = append(tf.Contexts, *currentCtx)
					currentCtx = nil
				}
			case "name":
				if inContextName && currentCtx != nil {
					currentCtx.Name = nameBuf.String()
					inContextName = false
				}
			case "source":
				if inSource {
					currentMsg.Source = sourceBuf.String()
					inSource = false
				}
			case "comment":
				if inComment {
					currentMsg.Comment = commentBuf.String()
					inComment = false
				}
			case "extracomment":
				if inExtraComment {
					currentMsg.ExtraComment = extraCommentBuf.String()
					inExtraComment = false
				}
			case "numerusform":
				if inNumerusForm {
					currentMsg.NumerusForms = append(currentMsg.NumerusForms, numerusBuf.String())
					inNumerusForm = false
				}
			case "translation":
				if inTranslation {
					currentMsg.TransEnd = currOffset
					if currentMsg.Numerus {
						currentMsg.Translation = domain.JoinNumerus(currentMsg.NumerusForms)
					} else {
						currentMsg.Translation = transBuf.String()
					}
					currentMsg.Status = resolveTransStatus(transType, currentMsg.Translation, currentMsg.Numerus, currentMsg.NumerusForms)
					inTranslation = false
				}
			case "message":
				if inMessage && currentCtx != nil {
					currentCtx.Messages = append(currentCtx.Messages, currentMsg)
					inMessage = false
				}
			}
		}
	}

	return nil
}

func resolveTransStatus(transType, text string, numerus bool, forms []string) domain.TransStatus {
	switch transType {
	case "unfinished":
		return domain.StatusUnfinished
	case "vanished", "obsolete":
		return domain.StatusVanished
	}
	if numerus {
		allEmpty := true
		for _, f := range forms {
			if strings.TrimSpace(f) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			return domain.StatusEmpty
		}
		return domain.StatusFinished
	}
	if text == "" {
		return domain.StatusEmpty
	}
	return domain.StatusFinished
}

func parseByteTag(attrs []xml.Attr) (uint64, bool) {
	for _, a := range attrs {
		if strings.EqualFold(a.Name.Local, "value") {
			v := strings.TrimSpace(a.Value)
			if v == "" {
				return 0, false
			}
			if strings.HasPrefix(v, "x") || strings.HasPrefix(v, "X") {
				n, err := strconv.ParseUint(v[1:], 16, 32)
				return n, err == nil
			}
			if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
				n, err := strconv.ParseUint(v[2:], 16, 32)
				return n, err == nil
			}
			n, err := strconv.ParseUint(v, 10, 32)
			return n, err == nil
		}
	}
	return 0, false
}

func leadingLineIndent(raw []byte, pos int) string {
	if pos <= 0 || pos > len(raw) {
		return ""
	}
	lineStart := bytes.LastIndexByte(raw[:pos], '\n') + 1
	segment := raw[lineStart:pos]
	for _, b := range segment {
		if b != ' ' && b != '\t' {
			return ""
		}
	}
	return string(segment)
}

func resolveBundleLocaleID(tf *TSFile, filePattern string, known []locale.LocaleSpec) string {
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

// LoadBundle loads all .ts files from a directory (recursively) or a .zip archive.
func LoadBundle(dirOrZipPath string, filePattern string, knownLocales []locale.LocaleSpec) (*Bundle, error) {
	info, err := os.Stat(dirOrZipPath)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", dirOrZipPath, err)
	}

	if info.IsDir() {
		bundle := &Bundle{
			SourcePath: dirOrZipPath,
			IsZip:      false,
			ZipExtras:  make(map[string][]byte),
		}
		err := filepath.WalkDir(dirOrZipPath, func(fullPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(d.Name()), ".ts") {
				return nil
			}
			rel, err := filepath.Rel(dirOrZipPath, fullPath)
			if err != nil {
				return err
			}
			relSlash := filepath.ToSlash(rel)
			raw, err := platform.ReadFileShared(fullPath)
			if err != nil {
				return fmt.Errorf("read %q: %w", fullPath, err)
			}
			tf, err := ParseBytes(relSlash, raw)
			if err != nil {
				return err
			}
			tf.FilePath = fullPath
			tf.LocaleID = resolveBundleLocaleID(tf, filePattern, knownLocales)
			bundle.Files = append(bundle.Files, tf)
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Slice(bundle.Files, func(i, j int) bool {
			return bundle.Files[i].RelPath < bundle.Files[j].RelPath
		})
		return bundle, nil
	}

	// .zip archive
	zipRaw, err := platform.ReadFileShared(dirOrZipPath)
	if err != nil {
		return nil, fmt.Errorf("read zip %q: %w", dirOrZipPath, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipRaw), int64(len(zipRaw)))
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", dirOrZipPath, err)
	}

	bundle := &Bundle{
		SourcePath: dirOrZipPath,
		IsZip:      true,
		ZipExtras:  make(map[string][]byte),
	}

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		relSlash := strings.TrimPrefix(strings.ReplaceAll(zf.Name, "\\", "/"), "./")
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %q: %w", relSlash, err)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read zip entry %q: %w", relSlash, readErr)
		}

		if strings.EqualFold(filepath.Ext(relSlash), ".ts") {
			tf, err := ParseBytes(relSlash, data)
			if err != nil {
				return nil, err
			}
			tf.FilePath = relSlash
			tf.LocaleID = resolveBundleLocaleID(tf, filePattern, knownLocales)
			bundle.Files = append(bundle.Files, tf)
		} else {
			bundle.ZipExtras[relSlash] = data
		}
	}

	sort.Slice(bundle.Files, func(i, j int) bool {
		return bundle.Files[i].RelPath < bundle.Files[j].RelPath
	})
	return bundle, nil
}

// ensureValidUTF8 is a helper for internal callers.
func ensureValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "")
}
