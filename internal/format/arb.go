package format

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func init() {
	RegisterAdapter(&arbAdapter{})
}

type arbAdapter struct{}

func (a *arbAdapter) ID() FormatID { return FormatARB }

func (a *arbAdapter) Name(lang string) string {
	if lang == "zh" {
		return "Flutter ARB (.arb)"
	}
	return "Flutter ARB (.arb)"
}

func (a *arbAdapter) Topology() Topology { return TopologyPerLocale }

func (a *arbAdapter) IDAsKey() bool { return true }

func (a *arbAdapter) Extensions() []string { return []string{"arb"} }

func (a *arbAdapter) MatchPath(relPath string, raw []byte) int {
	slash := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	ext := strings.ToLower(filepath.Ext(slash))
	if ext == ".arb" {
		return 95
	}
	if ext == ".json" && len(bytes.TrimSpace(raw)) > 0 {
		doc, err := parseJSONCST(raw)
		if err == nil && doc.Root != nil && doc.Root.Kind == jsonObject {
			if _, ok := doc.Root.Prop("@@locale"); ok {
				return 90
			}
		}
	}
	return 0
}

func (a *arbAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	doc, err := parseJSONCST(raw)
	if err != nil {
		return nil, fmt.Errorf("parse arb %s: %w", relPath, err)
	}
	if doc.Root == nil || doc.Root.Kind != jsonObject {
		return nil, fmt.Errorf("arb %s root must be an object", relPath)
	}

	locID := ""
	if locNode, ok := doc.Root.Prop("@@locale"); ok && locNode.Kind == jsonString && strings.TrimSpace(locNode.StrVal) != "" {
		if norm := normalizeLocaleCandidate(locNode.StrVal, known); norm != "" {
			locID = norm
		} else {
			locID = strings.TrimSpace(locNode.StrVal)
		}
	}
	if locID == "" {
		locID = detectARBLocaleFromPath(relPath, filePattern, known)
	}
	isTemplate := isEnglishLocaleID(locID)

	var entries []NormalizedEntry
	for _, prop := range doc.Root.Props {
		if strings.HasPrefix(prop.Key, "@@") || strings.HasPrefix(prop.Key, "@") {
			continue
		}
		if prop.Val.Kind != jsonString {
			continue
		}
		comment := ""
		if metaNode, ok := doc.Root.Prop("@" + prop.Key); ok && metaNode.Kind == jsonObject {
			if descNode, ok := metaNode.Prop("description"); ok && descNode.Kind == jsonString {
				comment = descNode.StrVal
			}
		}
		status := domain.StatusEmpty
		if prop.Val.StrVal != "" {
			status = domain.StatusFinished
		}
		src := ""
		if isTemplate {
			src = prop.Val.StrVal
		}
		entries = append(entries, NormalizedEntry{
			Key: domain.EntryKey{
				ID:      prop.Key,
				Source:  src,
				Comment: comment,
			},
			LocaleID: locID,
			Text:     prop.Val.StrVal,
			Status:   status,
		})
	}

	slash := strings.ReplaceAll(relPath, "\\", "/")
	rec := &DocRecord{
		FormatID:   FormatARB,
		RelPath:    relPath,
		FileName:   filepath.Base(slash),
		LocaleID:   locID,
		IsTemplate: isTemplate,
		HasBOM:     doc.HasBOM,
		LineEnding: doc.LineEnding,
		Raw:        raw,
		Entries:    entries,
	}
	if isTemplate {
		rec.SourceLang = locID
	}
	if locID != "" {
		rec.Locales = []string{locID}
	}
	return rec, nil
}

func (a *arbAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	cst, err := parseJSONCST(doc.Raw)
	if err != nil {
		return nil, 0, 0, err
	}
	if cst.Root == nil || cst.Root.Kind != jsonObject {
		return nil, 0, 0, fmt.Errorf("arb root must be an object")
	}

	var edits []jsonEdit
	var appends []jsonPropInsert
	applied := 0
	appended := 0

	for _, p := range patches {
		if !p.Action.Mutates() || p.Key.ID == "" {
			continue
		}
		if valNode, ok := cst.Root.Prop(p.Key.ID); ok && valNode.Kind == jsonString {
			edits = append(edits, formatJSONStringEdit(valNode, p.NewText))
			applied++
		} else if !ok {
			appends = append(appends, jsonPropInsert{
				Key:        p.Key.ID,
				RawValJSON: marshalJSONString(p.NewText),
			})
			appended++
		}
	}

	if len(appends) > 0 {
		edits = append(edits, buildObjectAppendEdit(cst, cst.Root, 0, appends))
	}

	out, err := applyJSONEdits(doc.Raw, edits)
	if err != nil {
		return nil, 0, 0, err
	}
	if !validateJSONBytes(out) {
		return nil, 0, 0, fmt.Errorf("resulting arb json is invalid")
	}
	return out, applied, appended, nil
}

func (a *arbAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	if template == nil || len(template.Raw) == 0 {
		raw := []byte("{\n  \"@@locale\": " + marshalJSONString(target.ID) + "\n}\n")
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
	for _, prop := range cst.Root.Props {
		if prop.Key == "@@locale" && prop.Val.Kind == jsonString {
			edits = append(edits, formatJSONStringEdit(prop.Val, target.ID))
			continue
		}
		if strings.HasPrefix(prop.Key, "@") {
			continue
		}
		if prop.Val.Kind == jsonString {
			edits = append(edits, formatJSONStringEdit(prop.Val, ""))
		}
	}

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

func (a *arbAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	k := marshalJSONString(key.ID)
	return k + ": " + marshalJSONString(oldText), k + ": " + marshalJSONString(newText)
}

func detectARBLocaleFromPath(relPath, filePattern string, known []locale.LocaleSpec) string {
	slash := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	base := filepath.Base(slash)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		return ""
	}
	if norm := normalizeLocaleCandidate(stem, known); norm != "" {
		return norm
	}
	tsStem := stem + ".ts"
	tsPat := ""
	if strings.TrimSpace(filePattern) != "" {
		pBase := filepath.Base(strings.ReplaceAll(filePattern, "\\", "/"))
		tsPat = strings.TrimSuffix(pBase, filepath.Ext(pBase)) + ".ts"
	}
	stemmed := locale.StemLocaleID(tsStem, tsPat, known)
	if stemmed != "" && stemmed != stem {
		return stemmed
	}
	return ""
}
