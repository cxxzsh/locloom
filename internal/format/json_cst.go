package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type jsonKind int

const (
	jsonObject jsonKind = iota
	jsonArray
	jsonString
	jsonNumber
	jsonBool
	jsonNull
)

type jsonProperty struct {
	Key      string
	KeyStart int
	KeyEnd   int
	ColonPos int
	Val      *jsonNode
}

type jsonNode struct {
	Kind    jsonKind
	Start   int
	End     int
	StrVal  string
	BoolVal bool
	Props   []jsonProperty
	PropMap map[string]int
	Elems   []*jsonNode
}

func (n *jsonNode) Prop(key string) (*jsonNode, bool) {
	if n == nil || n.Kind != jsonObject || n.PropMap == nil {
		return nil, false
	}
	idx, ok := n.PropMap[key]
	if !ok || idx < 0 || idx >= len(n.Props) {
		return nil, false
	}
	return n.Props[idx].Val, true
}

func (n *jsonNode) PropEntry(key string) (jsonProperty, bool) {
	if n == nil || n.Kind != jsonObject || n.PropMap == nil {
		return jsonProperty{}, false
	}
	idx, ok := n.PropMap[key]
	if !ok || idx < 0 || idx >= len(n.Props) {
		return jsonProperty{}, false
	}
	return n.Props[idx], true
}

type jsonDoc struct {
	Raw        []byte
	HasBOM     bool
	LineEnding string
	Indent     string
	Root       *jsonNode
}

type jsonEdit struct {
	Start       int
	End         int
	Replacement string
}

type jsonScanner struct {
	src []byte
	pos int
}

func parseJSONCST(raw []byte) (*jsonDoc, error) {
	hasBOM := false
	startPos := 0
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		hasBOM = true
		startPos = 3
	}

	lineEnding := "\n"
	if idx := bytes.IndexByte(raw, '\n'); idx > 0 && raw[idx-1] == '\r' {
		lineEnding = "\r\n"
	}

	sc := &jsonScanner{src: raw, pos: startPos}
	sc.skipWS()
	if sc.pos >= len(sc.src) {
		return nil, fmt.Errorf("empty JSON input")
	}

	root, err := sc.parseValue()
	if err != nil {
		return nil, err
	}
	sc.skipWS()
	if sc.pos < len(sc.src) {
		return nil, fmt.Errorf("unexpected trailing token at byte %d", sc.pos)
	}

	indent := detectJSONIndent(raw[startPos:])
	return &jsonDoc{
		Raw:        raw,
		HasBOM:     hasBOM,
		LineEnding: lineEnding,
		Indent:     indent,
		Root:       root,
	}, nil
}

func detectJSONIndent(body []byte) string {
	lines := bytes.Split(body, []byte{'\n'})
	for _, line := range lines[1:] {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			continue
		}
		if line[0] == '\t' {
			return "\t"
		}
		if line[0] == ' ' {
			n := 0
			for n < len(line) && line[n] == ' ' {
				n++
			}
			if n < len(line) && n > 0 {
				return strings.Repeat(" ", n)
			}
		}
	}
	return "  "
}

func (s *jsonScanner) skipWS() {
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
		default:
			return
		}
	}
}

func (s *jsonScanner) parseValue() (*jsonNode, error) {
	s.skipWS()
	if s.pos >= len(s.src) {
		return nil, fmt.Errorf("unexpected EOF at byte %d", s.pos)
	}
	switch s.src[s.pos] {
	case '{':
		return s.parseObject()
	case '[':
		return s.parseArray()
	case '"':
		return s.parseStringNode()
	case 't', 'f':
		return s.parseBool()
	case 'n':
		return s.parseNull()
	default:
		if s.src[s.pos] == '-' || (s.src[s.pos] >= '0' && s.src[s.pos] <= '9') {
			return s.parseNumber()
		}
		return nil, fmt.Errorf("unexpected character %q at byte %d", s.src[s.pos], s.pos)
	}
}

func (s *jsonScanner) parseObject() (*jsonNode, error) {
	start := s.pos
	s.pos++ // skip '{'
	node := &jsonNode{
		Kind:    jsonObject,
		Start:   start,
		PropMap: make(map[string]int),
	}
	s.skipWS()
	if s.pos < len(s.src) && s.src[s.pos] == '}' {
		s.pos++
		node.End = s.pos
		return node, nil
	}

	for {
		s.skipWS()
		if s.pos >= len(s.src) || s.src[s.pos] != '"' {
			return nil, fmt.Errorf("expected object key string at byte %d", s.pos)
		}
		keyNode, err := s.parseStringNode()
		if err != nil {
			return nil, err
		}
		s.skipWS()
		if s.pos >= len(s.src) || s.src[s.pos] != ':' {
			return nil, fmt.Errorf("expected ':' after key at byte %d", s.pos)
		}
		colonPos := s.pos
		s.pos++
		valNode, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		idx := len(node.Props)
		node.Props = append(node.Props, jsonProperty{
			Key:      keyNode.StrVal,
			KeyStart: keyNode.Start,
			KeyEnd:   keyNode.End,
			ColonPos: colonPos,
			Val:      valNode,
		})
		node.PropMap[keyNode.StrVal] = idx

		s.skipWS()
		if s.pos >= len(s.src) {
			return nil, fmt.Errorf("unterminated object starting at byte %d", start)
		}
		if s.src[s.pos] == '}' {
			s.pos++
			node.End = s.pos
			return node, nil
		}
		if s.src[s.pos] != ',' {
			return nil, fmt.Errorf("expected ',' or '}' in object at byte %d", s.pos)
		}
		s.pos++
	}
}

func (s *jsonScanner) parseArray() (*jsonNode, error) {
	start := s.pos
	s.pos++ // skip '['
	node := &jsonNode{
		Kind:  jsonArray,
		Start: start,
	}
	s.skipWS()
	if s.pos < len(s.src) && s.src[s.pos] == ']' {
		s.pos++
		node.End = s.pos
		return node, nil
	}

	for {
		valNode, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		node.Elems = append(node.Elems, valNode)

		s.skipWS()
		if s.pos >= len(s.src) {
			return nil, fmt.Errorf("unterminated array starting at byte %d", start)
		}
		if s.src[s.pos] == ']' {
			s.pos++
			node.End = s.pos
			return node, nil
		}
		if s.src[s.pos] != ',' {
			return nil, fmt.Errorf("expected ',' or ']' in array at byte %d", s.pos)
		}
		s.pos++
	}
}

func (s *jsonScanner) parseStringNode() (*jsonNode, error) {
	start := s.pos
	s.pos++ // skip opening '"'
	for s.pos < len(s.src) {
		b := s.src[s.pos]
		if b == '"' {
			s.pos++
			end := s.pos
			var decoded string
			if err := json.Unmarshal(s.src[start:end], &decoded); err != nil {
				return nil, fmt.Errorf("invalid JSON string at byte %d: %w", start, err)
			}
			return &jsonNode{
				Kind:   jsonString,
				Start:  start,
				End:    end,
				StrVal: decoded,
			}, nil
		}
		if b == '\\' {
			s.pos += 2
			continue
		}
		if b < 0x20 {
			return nil, fmt.Errorf("unescaped control byte 0x%02x in string at byte %d", b, s.pos)
		}
		if b < utf8.RuneSelf {
			s.pos++
		} else {
			_, size := utf8.DecodeRune(s.src[s.pos:])
			s.pos += size
		}
	}
	return nil, fmt.Errorf("unterminated string starting at byte %d", start)
}

func (s *jsonScanner) parseBool() (*jsonNode, error) {
	start := s.pos
	if bytes.HasPrefix(s.src[start:], []byte("true")) {
		s.pos += 4
		return &jsonNode{Kind: jsonBool, Start: start, End: s.pos, BoolVal: true}, nil
	}
	if bytes.HasPrefix(s.src[start:], []byte("false")) {
		s.pos += 5
		return &jsonNode{Kind: jsonBool, Start: start, End: s.pos, BoolVal: false}, nil
	}
	return nil, fmt.Errorf("invalid boolean literal at byte %d", start)
}

func (s *jsonScanner) parseNumber() (*jsonNode, error) {
	start := s.pos
	if s.src[s.pos] == '-' {
		s.pos++
	}
	if s.pos >= len(s.src) {
		return nil, fmt.Errorf("invalid number at byte %d", start)
	}
	if s.src[s.pos] == '0' {
		s.pos++
	} else if s.src[s.pos] >= '1' && s.src[s.pos] <= '9' {
		for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
			s.pos++
		}
	} else {
		return nil, fmt.Errorf("invalid number at byte %d", start)
	}
	if s.pos < len(s.src) && s.src[s.pos] == '.' {
		s.pos++
		if s.pos >= len(s.src) || s.src[s.pos] < '0' || s.src[s.pos] > '9' {
			return nil, fmt.Errorf("invalid number fraction at byte %d", start)
		}
		for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
			s.pos++
		}
	}
	if s.pos < len(s.src) && (s.src[s.pos] == 'e' || s.src[s.pos] == 'E') {
		s.pos++
		if s.pos < len(s.src) && (s.src[s.pos] == '+' || s.src[s.pos] == '-') {
			s.pos++
		}
		if s.pos >= len(s.src) || s.src[s.pos] < '0' || s.src[s.pos] > '9' {
			return nil, fmt.Errorf("invalid number exponent at byte %d", start)
		}
		for s.pos < len(s.src) && s.src[s.pos] >= '0' && s.src[s.pos] <= '9' {
			s.pos++
		}
	}
	return &jsonNode{Kind: jsonNumber, Start: start, End: s.pos}, nil
}

func (s *jsonScanner) parseNull() (*jsonNode, error) {
	start := s.pos
	if bytes.HasPrefix(s.src[start:], []byte("null")) {
		s.pos += 4
		return &jsonNode{Kind: jsonNull, Start: start, End: s.pos}, nil
	}
	return nil, fmt.Errorf("invalid null literal at byte %d", start)
}

// marshalJSONString encodes s as a JSON string literal with HTML escaping disabled
// so '<', '>', and '&' remain readable.
func marshalJSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return string(b)
}

// formatJSONStringEdit creates a surgical edit replacing an existing jsonString node.
func formatJSONStringEdit(node *jsonNode, newVal string) jsonEdit {
	return jsonEdit{
		Start:       node.Start,
		End:         node.End,
		Replacement: marshalJSONString(newVal),
	}
}

type jsonPropInsert struct {
	Key        string
	RawValJSON string
}

// buildObjectAppendEdit creates a single surgical edit that appends one or more
// new key/value properties before objNode's closing '}'.
// depth is the nesting depth of objNode (0 for root).
func buildObjectAppendEdit(doc *jsonDoc, objNode *jsonNode, depth int, props []jsonPropInsert) jsonEdit {
	if len(props) == 0 || objNode == nil || objNode.Kind != jsonObject {
		return jsonEdit{}
	}
	propIndent := strings.Repeat(doc.Indent, depth+1)
	closeIndent := strings.Repeat(doc.Indent, depth)
	le := doc.LineEnding

	var buf bytes.Buffer
	if len(objNode.Props) > 0 {
		lastValEnd := objNode.Props[len(objNode.Props)-1].Val.End
		buf.WriteByte(',')
		for _, p := range props {
			buf.WriteString(le)
			buf.WriteString(propIndent)
			buf.WriteString(marshalJSONString(p.Key))
			buf.WriteString(": ")
			buf.WriteString(p.RawValJSON)
		}
		return jsonEdit{
			Start:       lastValEnd,
			End:         lastValEnd,
			Replacement: buf.String(),
		}
	}

	// Empty object: replace interior between '{' and '}' cleanly.
	for i, p := range props {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(le)
		buf.WriteString(propIndent)
		buf.WriteString(marshalJSONString(p.Key))
		buf.WriteString(": ")
		buf.WriteString(p.RawValJSON)
	}
	buf.WriteString(le)
	buf.WriteString(closeIndent)
	return jsonEdit{
		Start:       objNode.Start + 1,
		End:         objNode.End - 1,
		Replacement: buf.String(),
	}
}

func applyJSONEdits(raw []byte, edits []jsonEdit) ([]byte, error) {
	if len(edits) == 0 {
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, nil
	}
	// Deduplicate identical replacement spans [Start, End) (last wins) so repeated patches
	// for the same key do not trigger an overlap error.
	deduped := make([]jsonEdit, 0, len(edits))
	replaceIdx := make(map[[2]int]int, len(edits))
	for _, e := range edits {
		if e.Start < e.End {
			k := [2]int{e.Start, e.End}
			if pos, exists := replaceIdx[k]; exists {
				deduped[pos] = e
				continue
			}
			replaceIdx[k] = len(deduped)
		}
		deduped = append(deduped, e)
	}
	sorted := deduped
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].End < sorted[j].End
	})

	var out bytes.Buffer
	out.Grow(len(raw) + 256)
	cursor := 0
	for _, e := range sorted {
		if e.Start < cursor {
			return nil, fmt.Errorf("overlapping JSON edit at byte %d (cursor %d)", e.Start, cursor)
		}
		out.Write(raw[cursor:e.Start])
		out.WriteString(e.Replacement)
		cursor = e.End
	}
	out.Write(raw[cursor:])
	return out.Bytes(), nil
}

func validateJSONBytes(raw []byte) bool {
	b := bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	return json.Valid(b)
}
