package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func init() {
	RegisterAdapter(&jsonI18nAdapter{})
}

type jsonI18nAdapter struct{}

type jsonI18nOpaque struct {
	IsFlat    bool
	Namespace string
	KeyPaths  map[string][]string
}

var excludedJSONNames = map[string]bool{
	"package.json":           true,
	"package-lock.json":      true,
	"tsconfig.json":          true,
	"jsconfig.json":          true,
	"manifest.json":          true,
	"config.json":            true,
	"locloom.json":           true,
	"translation-cache.json": true,
	"composer.json":          true,
}

func (a *jsonI18nAdapter) ID() FormatID { return FormatJSON }

func (a *jsonI18nAdapter) Name(lang string) string {
	if lang == "zh" {
		return "JSON 国际化 (.json)"
	}
	return "JSON i18n (.json)"
}

func (a *jsonI18nAdapter) Topology() Topology { return TopologyPerLocale }

func (a *jsonI18nAdapter) IDAsKey() bool { return true }

func (a *jsonI18nAdapter) Extensions() []string { return []string{"json"} }

func (a *jsonI18nAdapter) MatchPath(relPath string, raw []byte) int {
	slash := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	base := strings.ToLower(filepath.Base(slash))
	if !strings.HasSuffix(base, ".json") {
		return 0
	}
	if excludedJSONNames[base] {
		return 0
	}
	if strings.HasPrefix(slash, ".vscode/") || strings.Contains(slash, "/.vscode/") {
		return 0
	}

	loc, _ := detectJSONLocaleAndContext(slash, "", nil)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if loc != "" {
			return 60
		}
		return 0
	}

	doc, err := parseJSONCST(raw)
	if err != nil || doc.Root == nil || doc.Root.Kind != jsonObject {
		return 0
	}
	if _, hasARB := doc.Root.Prop("@@locale"); hasARB {
		return 0
	}
	if _, hasSrcLang := doc.Root.Prop("sourceLanguage"); hasSrcLang {
		if _, hasStrings := doc.Root.Prop("strings"); hasStrings {
			return 0
		}
	}

	if loc != "" {
		return 60
	}
	if hasJSONStringLeaves(doc.Root) && isJSONI18nTree(doc.Root) {
		return 60
	}
	return 0
}

func hasJSONStringLeaves(n *jsonNode) bool {
	if n == nil || n.Kind != jsonObject {
		return false
	}
	for _, p := range n.Props {
		if p.Val.Kind == jsonString {
			return true
		}
		if p.Val.Kind == jsonObject && hasJSONStringLeaves(p.Val) {
			return true
		}
	}
	return false
}

func isJSONI18nTree(n *jsonNode) bool {
	if n == nil || n.Kind != jsonObject {
		return false
	}
	for _, p := range n.Props {
		switch p.Val.Kind {
		case jsonString:
			continue
		case jsonObject:
			if !isJSONI18nTree(p.Val) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (a *jsonI18nAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	doc, err := parseJSONCST(raw)
	if err != nil {
		return nil, fmt.Errorf("parse json %s: %w", relPath, err)
	}
	if doc.Root == nil || doc.Root.Kind != jsonObject {
		return nil, fmt.Errorf("json %s root must be an object", relPath)
	}

	locID, nsContext := detectJSONLocaleAndContext(relPath, filePattern, known)
	isTemplate := isEnglishLocaleID(locID)

	isFlat := true
	for _, p := range doc.Root.Props {
		if p.Val.Kind == jsonObject {
			isFlat = false
			break
		}
	}

	opaque := &jsonI18nOpaque{
		IsFlat:    isFlat,
		Namespace: nsContext,
		KeyPaths:  make(map[string][]string),
	}
	var entries []NormalizedEntry

	var walk func(obj *jsonNode, prefix []string)
	walk = func(obj *jsonNode, prefix []string) {
		for _, p := range obj.Props {
			segPath := make([]string, len(prefix)+1)
			copy(segPath, prefix)
			segPath[len(prefix)] = p.Key

			switch p.Val.Kind {
			case jsonString:
				var id string
				if isFlat && len(segPath) == 1 {
					id = segPath[0]
				} else {
					id = joinJSONKeyPath(segPath)
				}
				opaque.KeyPaths[id] = segPath
				status := domain.StatusEmpty
				if p.Val.StrVal != "" {
					status = domain.StatusFinished
				}
				src := ""
				if isTemplate {
					src = p.Val.StrVal
				}
				entries = append(entries, NormalizedEntry{
					Key: domain.EntryKey{
						ID:      id,
						Context: nsContext,
						Source:  src,
					},
					LocaleID: locID,
					Text:     p.Val.StrVal,
					Status:   status,
				})
			case jsonObject:
				walk(p.Val, segPath)
			}
		}
	}
	walk(doc.Root, nil)

	slash := strings.ReplaceAll(relPath, "\\", "/")
	rec := &DocRecord{
		FormatID:   FormatJSON,
		RelPath:    relPath,
		FileName:   filepath.Base(slash),
		LocaleID:   locID,
		IsTemplate: isTemplate,
		HasBOM:     doc.HasBOM,
		LineEnding: doc.LineEnding,
		Raw:        raw,
		Entries:    entries,
		Opaque:     opaque,
	}
	if isTemplate {
		rec.SourceLang = locID
	}
	if locID != "" {
		rec.Locales = []string{locID}
	}
	return rec, nil
}

func (a *jsonI18nAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	cst, err := parseJSONCST(doc.Raw)
	if err != nil {
		return nil, 0, 0, err
	}
	if cst.Root == nil || cst.Root.Kind != jsonObject {
		return nil, 0, 0, fmt.Errorf("json root must be an object")
	}

	opaque, _ := doc.Opaque.(*jsonI18nOpaque)
	var edits []jsonEdit
	applied := 0
	appended := 0

	// Group new insertions by parent object node to avoid conflicting offsets.
	type pendingInsert struct {
		obj   *jsonNode
		depth int
		props []jsonPropInsert
	}
	insertsByObj := make(map[*jsonNode]*pendingInsert)
	var insertOrder []*jsonNode
	// Track newly created nested objects under an existing parent to merge sibling appends.
	newNestedByParent := make(map[*jsonNode]map[string]any)

	addInsert := func(obj *jsonNode, depth int, key string, rawVal string) {
		pi, ok := insertsByObj[obj]
		if !ok {
			pi = &pendingInsert{obj: obj, depth: depth}
			insertsByObj[obj] = pi
			insertOrder = append(insertOrder, obj)
		}
		pi.props = append(pi.props, jsonPropInsert{Key: key, RawValJSON: rawVal})
	}

	for _, p := range patches {
		if !p.Action.Mutates() {
			continue
		}
		if opaque != nil && opaque.Namespace != "" && p.Key.Context != "" && p.Key.Context != opaque.Namespace {
			continue
		}
		var path []string
		if opaque != nil {
			if knownPath, ok := opaque.KeyPaths[p.Key.ID]; ok {
				path = knownPath
			}
		}
		if len(path) == 0 {
			if opaque != nil && opaque.IsFlat && !strings.Contains(p.Key.ID, `\.`) {
				path = []string{p.Key.ID}
			} else {
				path = splitJSONKeyPath(p.Key.ID)
			}
		}
		if len(path) == 0 {
			continue
		}

		// Walk existing CST objects as far as possible.
		cur := cst.Root
		depth := 0
		matched := true
		for i := 0; i < len(path)-1; i++ {
			child, ok := cur.Prop(path[i])
			if !ok || child.Kind != jsonObject {
				// Deepest existing object is cur at depth; remaining path is path[i:].
				rem := path[i:]
				if len(rem) == 1 {
					addInsert(cur, depth, rem[0], marshalJSONString(p.NewText))
				} else {
					m, ok := newNestedByParent[cur]
					if !ok {
						m = make(map[string]any)
						newNestedByParent[cur] = m
					}
					setNestedMapValue(m, rem, p.NewText)
				}
				appended++
				matched = false
				break
			}
			cur = child
			depth++
		}
		if !matched {
			continue
		}

		leafKey := path[len(path)-1]
		if leafNode, ok := cur.Prop(leafKey); ok && leafNode.Kind == jsonString {
			edits = append(edits, formatJSONStringEdit(leafNode, p.NewText))
			applied++
		} else if !ok {
			addInsert(cur, depth, leafKey, marshalJSONString(p.NewText))
			appended++
		}
	}

	// Materialize any nested new subtrees into their deepest existing parent objects.
	// Find depth of each parent node.
	nodeDepth := map[*jsonNode]int{cst.Root: 0}
	var indexDepths func(n *jsonNode, d int)
	indexDepths = func(n *jsonNode, d int) {
		nodeDepth[n] = d
		for _, prop := range n.Props {
			if prop.Val.Kind == jsonObject {
				indexDepths(prop.Val, d+1)
			}
		}
	}
	indexDepths(cst.Root, 0)

	for parent, subMap := range newNestedByParent {
		d := nodeDepth[parent]
		for k, v := range subMap {
			rawVal := marshalIndentedJSON(v, strings.Repeat(cst.Indent, d+1), cst.Indent, cst.LineEnding)
			addInsert(parent, d, k, rawVal)
		}
	}

	for _, obj := range insertOrder {
		pi := insertsByObj[obj]
		edits = append(edits, buildObjectAppendEdit(cst, pi.obj, pi.depth, pi.props))
	}

	out, err := applyJSONEdits(doc.Raw, edits)
	if err != nil {
		return nil, 0, 0, err
	}
	if !validateJSONBytes(out) {
		return nil, 0, 0, fmt.Errorf("resulting json is invalid")
	}
	return out, applied, appended, nil
}

func (a *jsonI18nAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	if template == nil || len(template.Raw) == 0 {
		raw := []byte("{}\n")
		doc, err := a.ParseDoc(newRelPath, raw, "", []locale.LocaleSpec{target})
		if err != nil {
			return nil, err
		}
		doc.LocaleID = target.ID
		doc.IsTemplate = false
		doc.SourceLang = ""
		doc.Locales = []string{target.ID}
		return doc, nil
	}
	cst, err := parseJSONCST(template.Raw)
	if err != nil {
		return nil, err
	}
	var edits []jsonEdit
	var clearStrings func(n *jsonNode)
	clearStrings = func(n *jsonNode) {
		for _, p := range n.Props {
			switch p.Val.Kind {
			case jsonString:
				edits = append(edits, formatJSONStringEdit(p.Val, ""))
			case jsonObject:
				clearStrings(p.Val)
			}
		}
	}
	clearStrings(cst.Root)

	newRaw, err := applyJSONEdits(template.Raw, edits)
	if err != nil {
		return nil, err
	}
	doc, err := a.ParseDoc(newRelPath, newRaw, "", []locale.LocaleSpec{target})
	if err != nil {
		return nil, err
	}
	doc.LocaleID = target.ID
	doc.IsTemplate = false
	doc.SourceLang = ""
	doc.Locales = []string{target.ID}
	for i := range doc.Entries {
		doc.Entries[i].LocaleID = target.ID
		doc.Entries[i].Key.Source = ""
	}
	return doc, nil
}

func (a *jsonI18nAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	k := marshalJSONString(key.ID)
	return k + ": " + marshalJSONString(oldText), k + ": " + marshalJSONString(newText)
}

func joinJSONKeyPath(segs []string) string {
	escaped := make([]string, len(segs))
	for i, s := range segs {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `.`, `\.`)
		escaped[i] = s
	}
	return strings.Join(escaped, ".")
}

func splitJSONKeyPath(id string) []string {
	if id == "" {
		return nil
	}
	var segs []string
	var cur strings.Builder
	for i := 0; i < len(id); i++ {
		if id[i] == '\\' && i+1 < len(id) {
			cur.WriteByte(id[i+1])
			i++
			continue
		}
		if id[i] == '.' {
			segs = append(segs, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(id[i])
	}
	segs = append(segs, cur.String())
	return segs
}

func setNestedMapValue(root map[string]any, path []string, val string) {
	cur := root
	for i := 0; i < len(path)-1; i++ {
		next, ok := cur[path[i]].(map[string]any)
		if !ok {
			next = make(map[string]any)
			cur[path[i]] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = val
}

func marshalIndentedJSON(v any, baseIndent, indentUnit, lineEnding string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(baseIndent, indentUnit)
	_ = enc.Encode(v)
	b := bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})
	if lineEnding == "\r\n" {
		b = bytes.ReplaceAll(b, []byte{'\n'}, []byte("\r\n"))
	}
	return string(b)
}

func detectJSONLocaleAndContext(relPath, filePattern string, known []locale.LocaleSpec) (string, string) {
	slash := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	base := filepath.Base(slash)
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	// 1. Try explicit filePattern if provided.
	if pat := strings.TrimSpace(filePattern); pat != "" && strings.Contains(pat, "{id}") {
		patSlash := strings.ReplaceAll(pat, "\\", "/")
		patNoExt := strings.TrimSuffix(patSlash, filepath.Ext(patSlash))
		relNoExt := strings.TrimSuffix(slash, filepath.Ext(slash))
		if idx := strings.Index(patNoExt, "{id}"); idx >= 0 {
			prefix := patNoExt[:idx]
			suffix := patNoExt[idx+len("{id}"):]
			if strings.HasPrefix(relNoExt, prefix) && strings.HasSuffix(relNoExt, suffix) && len(relNoExt) > len(prefix)+len(suffix) {
				cand := relNoExt[len(prefix) : len(relNoExt)-len(suffix)]
				if !strings.Contains(cand, "/") {
					if norm := normalizeLocaleCandidate(cand, known); norm != "" {
						return norm, ""
					}
				}
			}
		}
	}

	// 2. Check filename stem directly (e.g. en.json, zh-CN.json, messages_ja.json).
	if norm := normalizeLocaleCandidate(stem, known); norm != "" {
		return norm, ""
	}
	if stemmed := locale.StemLocaleID(stem+".ts", "", known); stemmed != "" && stemmed != stem {
		return stemmed, ""
	}

	// 3. Check parent folder(s) for locale (e.g. locales/ja/common.json -> locale="ja_JP", context="common").
	dir := filepath.ToSlash(filepath.Dir(slash))
	if dir != "." && dir != "/" {
		parts := strings.Split(dir, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			seg := parts[i]
			if seg == "" || seg == "." {
				continue
			}
			if norm := normalizeLocaleCandidate(seg, known); norm != "" {
				return norm, stem
			}
		}
	}

	return "", ""
}

var commonShortLangs = map[string]bool{
	"en": true, "zh": true, "ja": true, "ko": true, "de": true, "fr": true,
	"es": true, "pt": true, "ru": true, "it": true, "nl": true, "pl": true,
	"tr": true, "ar": true, "vi": true, "th": true, "id": true, "cs": true,
	"uk": true, "sv": true, "da": true, "fi": true, "no": true, "nb": true,
	"nn": true, "el": true, "he": true, "hi": true, "bn": true, "ms": true,
	"ro": true, "hu": true, "sk": true, "bg": true, "hr": true, "sr": true,
	"sl": true, "et": true, "lv": true, "lt": true, "ca": true, "fa": true,
	"ur": true, "ta": true, "te": true, "ml": true,
}

func normalizeLocaleCandidate(raw string, known []locale.LocaleSpec) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	norm := strings.ReplaceAll(s, "-", "_")
	lower := strings.ToLower(norm)

	for _, l := range known {
		if strings.EqualFold(strings.ReplaceAll(l.ID, "-", "_"), norm) {
			return l.ID
		}
	}
	for _, l := range locale.DefaultLocales() {
		if strings.EqualFold(strings.ReplaceAll(l.ID, "-", "_"), norm) {
			return l.ID
		}
	}

	// Check xx_YY or xx_Script_YY patterns.
	parts := strings.Split(norm, "_")
	if len(parts) == 2 && len(parts[0]) >= 2 && len(parts[0]) <= 3 && len(parts[1]) >= 2 && len(parts[1]) <= 4 {
		lang := strings.ToLower(parts[0])
		reg := strings.ToUpper(parts[1])
		if len(parts[1]) == 4 {
			// Script subtag like Hans / Hant -> TitleCase
			reg = strings.ToUpper(parts[1][:1]) + strings.ToLower(parts[1][1:])
		}
		cand := lang + "_" + reg
		for _, l := range known {
			if strings.EqualFold(l.ID, cand) {
				return l.ID
			}
		}
		for _, l := range locale.DefaultLocales() {
			if strings.EqualFold(l.ID, cand) {
				return l.ID
			}
		}
		return cand
	}

	if commonShortLangs[lower] {
		// If unique match in known or DefaultLocales, return that; otherwise return short code.
		var matches []string
		for _, l := range known {
			if strings.HasPrefix(strings.ToLower(l.ID), lower+"_") || strings.EqualFold(l.ID, lower) {
				matches = append(matches, l.ID)
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
		for _, l := range locale.DefaultLocales() {
			if strings.HasPrefix(strings.ToLower(l.ID), lower+"_") {
				matches = append(matches, l.ID)
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
		return lower
	}

	return ""
}

func isEnglishLocaleID(loc string) bool {
	l := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(loc), "-", "_"))
	return l == "en" || strings.HasPrefix(l, "en_")
}
