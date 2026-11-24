package format

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"locloom/internal/domain"
	"locloom/internal/locale"
)

func init() {
	RegisterAdapter(&xcstringsAdapter{})
}

type xcstringsAdapter struct{}

var cldrPluralOrder = []string{"zero", "one", "two", "few", "many", "other"}

func (a *xcstringsAdapter) ID() FormatID { return FormatXCStrings }

func (a *xcstringsAdapter) Name(lang string) string {
	if lang == "zh" {
		return "Apple String Catalog (.xcstrings)"
	}
	return "Apple String Catalog (.xcstrings)"
}

func (a *xcstringsAdapter) Topology() Topology { return TopologyMultiLocale }

func (a *xcstringsAdapter) IDAsKey() bool { return true }

func (a *xcstringsAdapter) Extensions() []string { return []string{"xcstrings"} }

func (a *xcstringsAdapter) MatchPath(relPath string, raw []byte) int {
	slash := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	ext := strings.ToLower(filepath.Ext(slash))
	if ext == ".xcstrings" {
		return 100
	}
	if ext == ".json" && len(bytes.TrimSpace(raw)) > 0 {
		doc, err := parseJSONCST(raw)
		if err == nil && doc.Root != nil && doc.Root.Kind == jsonObject {
			if _, ok1 := doc.Root.Prop("sourceLanguage"); ok1 {
				if _, ok2 := doc.Root.Prop("strings"); ok2 {
					return 85
				}
			}
		}
	}
	return 0
}

func (a *xcstringsAdapter) ParseDoc(relPath string, raw []byte, filePattern string, known []locale.LocaleSpec) (*DocRecord, error) {
	cst, err := parseJSONCST(raw)
	if err != nil {
		return nil, fmt.Errorf("parse xcstrings %s: %w", relPath, err)
	}
	if cst.Root == nil || cst.Root.Kind != jsonObject {
		return nil, fmt.Errorf("xcstrings %s root must be an object", relPath)
	}

	sourceLang := "en"
	if slNode, ok := cst.Root.Prop("sourceLanguage"); ok && slNode.Kind == jsonString && slNode.StrVal != "" {
		sourceLang = slNode.StrVal
	}

	stringsNode, _ := cst.Root.Prop("strings")
	localeSet := make(map[string]bool)
	var entries []NormalizedEntry

	if stringsNode != nil && stringsNode.Kind == jsonObject {
		for _, strProp := range stringsNode.Props {
			keyName := strProp.Key
			entryNode := strProp.Val
			if entryNode.Kind != jsonObject {
				continue
			}
			if stNode, ok := entryNode.Prop("shouldTranslate"); ok && stNode.Kind == jsonBool && !stNode.BoolVal {
				continue
			}

			comment := ""
			if cNode, ok := entryNode.Prop("comment"); ok && cNode.Kind == jsonString {
				comment = cNode.StrVal
			}

			locsNode, hasLocs := entryNode.Prop("localizations")
			sourceText := keyName
			if hasLocs && locsNode.Kind == jsonObject {
				if srcLocNode, ok := locsNode.Prop(sourceLang); ok && srcLocNode.Kind == jsonObject {
					if suNode, ok := srcLocNode.Prop("stringUnit"); ok && suNode.Kind == jsonObject {
						if valNode, ok := suNode.Prop("value"); ok && valNode.Kind == jsonString && valNode.StrVal != "" {
							sourceText = valNode.StrVal
						}
					}
				}
			}

			if !hasLocs || locsNode.Kind != jsonObject {
				continue
			}

			for _, locProp := range locsNode.Props {
				locID := locProp.Key
				locNode := locProp.Val
				if locNode.Kind != jsonObject {
					continue
				}
				localeSet[locID] = true

				if suNode, ok := locNode.Prop("stringUnit"); ok && suNode.Kind == jsonObject {
					val := ""
					state := ""
					if vNode, ok := suNode.Prop("value"); ok && vNode.Kind == jsonString {
						val = vNode.StrVal
					}
					if sNode, ok := suNode.Prop("state"); ok && sNode.Kind == jsonString {
						state = sNode.StrVal
					}
					entries = append(entries, NormalizedEntry{
						Key: domain.EntryKey{
							ID:      keyName,
							Source:  sourceText,
							Comment: comment,
						},
						LocaleID: locID,
						Text:     val,
						Status:   mapXCStringsStatus(state, val),
					})
					continue
				}

				if varNode, ok := locNode.Prop("variations"); ok && varNode.Kind == jsonObject {
					if pluralNode, ok := varNode.Prop("plural"); ok && pluralNode.Kind == jsonObject {
						var pKeys []string
						var pForms []string
						allTranslated := true
						anyUnfinished := false
						allEmpty := true

						for _, cat := range cldrPluralOrder {
							catNode, ok := pluralNode.Prop(cat)
							if !ok || catNode.Kind != jsonObject {
								continue
							}
							suNode, ok := catNode.Prop("stringUnit")
							if !ok || suNode.Kind != jsonObject {
								continue
							}
							val := ""
							state := ""
							if vNode, ok := suNode.Prop("value"); ok && vNode.Kind == jsonString {
								val = vNode.StrVal
							}
							if sNode, ok := suNode.Prop("state"); ok && sNode.Kind == jsonString {
								state = sNode.StrVal
							}
							pKeys = append(pKeys, cat)
							pForms = append(pForms, val)
							if val != "" {
								allEmpty = false
							}
							if state != "translated" {
								allTranslated = false
							}
							if state == "needs_review" || state == "new" {
								anyUnfinished = true
							}
						}

						if len(pKeys) > 0 {
							status := domain.StatusEmpty
							if allTranslated && !allEmpty {
								status = domain.StatusFinished
							} else if anyUnfinished || !allEmpty {
								status = domain.StatusUnfinished
							}
							entries = append(entries, NormalizedEntry{
								Key: domain.EntryKey{
									ID:      keyName,
									Source:  sourceText,
									Comment: comment,
								},
								LocaleID:    locID,
								Text:        domain.JoinNumerus(pForms),
								Numerus:     true,
								PluralKeys:  pKeys,
								PluralForms: pForms,
								Status:      status,
							})
						}
					}
				}
			}
		}
	}

	locales := make([]string, 0, len(localeSet))
	for loc := range localeSet {
		locales = append(locales, loc)
	}
	sort.Strings(locales)

	slash := strings.ReplaceAll(relPath, "\\", "/")
	return &DocRecord{
		FormatID:   FormatXCStrings,
		RelPath:    relPath,
		FileName:   filepath.Base(slash),
		Locales:    locales,
		SourceLang: sourceLang,
		HasBOM:     cst.HasBOM,
		LineEnding: cst.LineEnding,
		Raw:        raw,
		Entries:    entries,
	}, nil
}

func mapXCStringsStatus(state, val string) domain.TransStatus {
	switch state {
	case "translated":
		return domain.StatusFinished
	case "needs_review", "new":
		return domain.StatusUnfinished
	case "":
		if val != "" {
			return domain.StatusFinished
		}
		return domain.StatusEmpty
	default:
		if val != "" {
			return domain.StatusUnfinished
		}
		return domain.StatusEmpty
	}
}

func (a *xcstringsAdapter) ApplyPatches(doc *DocRecord, patches []PatchOp) ([]byte, int, int, error) {
	cst, err := parseJSONCST(doc.Raw)
	if err != nil {
		return nil, 0, 0, err
	}
	if cst.Root == nil || cst.Root.Kind != jsonObject {
		return nil, 0, 0, fmt.Errorf("xcstrings root must be an object")
	}

	stringsNode, hasStrings := cst.Root.Prop("strings")
	if !hasStrings || stringsNode.Kind != jsonObject {
		return nil, 0, 0, fmt.Errorf("xcstrings missing 'strings' object")
	}

	nodeDepth := make(map[*jsonNode]int)
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

	var edits []jsonEdit
	type pendingInsert struct {
		obj   *jsonNode
		depth int
		props []jsonPropInsert
	}
	insertsByObj := make(map[*jsonNode]*pendingInsert)
	var insertOrder []*jsonNode
	addInsert := func(obj *jsonNode, depth int, key, rawVal string) {
		pi, ok := insertsByObj[obj]
		if !ok {
			pi = &pendingInsert{obj: obj, depth: depth}
			insertsByObj[obj] = pi
			insertOrder = append(insertOrder, obj)
		}
		pi.props = append(pi.props, jsonPropInsert{Key: key, RawValJSON: rawVal})
	}

	// Group missing "localizations" blocks on existing entries.
	newLocsOnEntry := make(map[*jsonNode]map[string]any)
	var newLocsEntryOrder []*jsonNode
	// Group brand-new entries missing from "strings".
	newEntries := make(map[string]map[string]any)
	var newEntryOrder []string

	applied := 0
	appended := 0

	buildLocPayload := func(p PatchOp) map[string]any {
		if p.Numerus || len(p.PluralForms) > 0 {
			forms := p.PluralForms
			if len(forms) == 0 {
				forms = domain.SplitNumerus(p.NewText)
			}
			pluralMap := make(map[string]any)
			// Default to "one" + "other" for 2 forms, or CLDR subset ending with "other".
			cats := selectPluralCategories(len(forms))
			for i, cat := range cats {
				val := ""
				if i < len(forms) {
					val = forms[i]
				}
				pluralMap[cat] = map[string]any{
					"stringUnit": map[string]any{
						"state": "translated",
						"value": val,
					},
				}
			}
			return map[string]any{
				"variations": map[string]any{
					"plural": pluralMap,
				},
			}
		}
		return map[string]any{
			"stringUnit": map[string]any{
				"state": "translated",
				"value": p.NewText,
			},
		}
	}

	patchStringUnit := func(suNode *jsonNode, newVal string) {
		suDepth := nodeDepth[suNode]
		if sNode, ok := suNode.Prop("state"); ok && sNode.Kind == jsonString {
			edits = append(edits, formatJSONStringEdit(sNode, "translated"))
		} else {
			addInsert(suNode, suDepth, "state", marshalJSONString("translated"))
		}
		if vNode, ok := suNode.Prop("value"); ok && vNode.Kind == jsonString {
			edits = append(edits, formatJSONStringEdit(vNode, newVal))
		} else {
			addInsert(suNode, suDepth, "value", marshalJSONString(newVal))
		}
	}

	for _, p := range patches {
		if !p.Action.Mutates() || p.Key.ID == "" || p.LocaleID == "" {
			continue
		}
		entryNode, hasEntry := stringsNode.Prop(p.Key.ID)
		if !hasEntry || entryNode.Kind != jsonObject {
			locs, ok := newEntries[p.Key.ID]
			if !ok {
				locs = make(map[string]any)
				newEntries[p.Key.ID] = locs
				newEntryOrder = append(newEntryOrder, p.Key.ID)
			}
			locs[p.LocaleID] = buildLocPayload(p)
			appended++
			continue
		}

		locsNode, hasLocs := entryNode.Prop("localizations")
		if !hasLocs || locsNode.Kind != jsonObject {
			locs, ok := newLocsOnEntry[entryNode]
			if !ok {
				locs = make(map[string]any)
				newLocsOnEntry[entryNode] = locs
				newLocsEntryOrder = append(newLocsEntryOrder, entryNode)
			}
			locs[p.LocaleID] = buildLocPayload(p)
			appended++
			continue
		}

		locNode, hasLoc := locsNode.Prop(p.LocaleID)
		if !hasLoc || locNode.Kind != jsonObject {
			locsDepth := nodeDepth[locsNode]
			rawLocVal := marshalIndentedJSON(buildLocPayload(p), strings.Repeat(cst.Indent, locsDepth+1), cst.Indent, cst.LineEnding)
			addInsert(locsNode, locsDepth, p.LocaleID, rawLocVal)
			appended++
			continue
		}

		if p.Numerus || len(p.PluralForms) > 0 {
			forms := p.PluralForms
			if len(forms) == 0 {
				forms = domain.SplitNumerus(p.NewText)
			}
			if varNode, ok := locNode.Prop("variations"); ok && varNode.Kind == jsonObject {
				if pluralNode, ok := varNode.Prop("plural"); ok && pluralNode.Kind == jsonObject {
					idx := 0
					for _, cat := range cldrPluralOrder {
						catNode, ok := pluralNode.Prop(cat)
						if !ok || catNode.Kind != jsonObject {
							continue
						}
						if suNode, ok := catNode.Prop("stringUnit"); ok && suNode.Kind == jsonObject {
							val := ""
							if idx < len(forms) {
								val = forms[idx]
							}
							patchStringUnit(suNode, val)
							idx++
						}
					}
					applied++
					continue
				}
			}
			if _, hasVar := locNode.Prop("variations"); !hasVar {
				locDepth := nodeDepth[locNode]
				payload := buildLocPayload(p)
				if varPayload, ok := payload["variations"]; ok {
					rawVar := marshalIndentedJSON(varPayload, strings.Repeat(cst.Indent, locDepth+1), cst.Indent, cst.LineEnding)
					addInsert(locNode, locDepth, "variations", rawVar)
					applied++
					continue
				}
			}
		}

		if suNode, ok := locNode.Prop("stringUnit"); ok && suNode.Kind == jsonObject {
			patchStringUnit(suNode, p.NewText)
			applied++
			continue
		}

		locDepth := nodeDepth[locNode]
		suPayload := map[string]any{
			"state": "translated",
			"value": p.NewText,
		}
		rawSU := marshalIndentedJSON(suPayload, strings.Repeat(cst.Indent, locDepth+1), cst.Indent, cst.LineEnding)
		addInsert(locNode, locDepth, "stringUnit", rawSU)
		applied++
	}

	for _, entryNode := range newLocsEntryOrder {
		d := nodeDepth[entryNode]
		rawLocs := marshalIndentedJSON(newLocsOnEntry[entryNode], strings.Repeat(cst.Indent, d+1), cst.Indent, cst.LineEnding)
		addInsert(entryNode, d, "localizations", rawLocs)
	}

	stringsDepth := nodeDepth[stringsNode]
	for _, keyID := range newEntryOrder {
		entryPayload := map[string]any{
			"localizations": newEntries[keyID],
		}
		rawEntry := marshalIndentedJSON(entryPayload, strings.Repeat(cst.Indent, stringsDepth+1), cst.Indent, cst.LineEnding)
		addInsert(stringsNode, stringsDepth, keyID, rawEntry)
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
		return nil, 0, 0, fmt.Errorf("resulting xcstrings json is invalid")
	}
	return out, applied, appended, nil
}

func selectPluralCategories(n int) []string {
	switch n {
	case 1:
		return []string{"other"}
	case 2:
		return []string{"one", "other"}
	case 3:
		return []string{"zero", "one", "other"}
	case 4:
		return []string{"one", "few", "many", "other"}
	case 5:
		return []string{"zero", "one", "few", "many", "other"}
	default:
		if n <= 0 {
			return []string{"other"}
		}
		return cldrPluralOrder
	}
}

func (a *xcstringsAdapter) NewLocaleDoc(template *DocRecord, target locale.LocaleSpec, newRelPath string) (*DocRecord, error) {
	return nil, fmt.Errorf("apple-xcstrings is multi-locale and stores all locales in a single file")
}

func (a *xcstringsAdapter) PreviewDiff(key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (string, string) {
	oldState := "translated"
	switch oldStatus {
	case domain.StatusEmpty, domain.StatusMissing:
		oldState = "new"
	case domain.StatusUnfinished:
		oldState = "needs_review"
	}
	before := fmt.Sprintf("%s: {\n  \"stringUnit\": {\n    \"state\": %s,\n    \"value\": %s\n  }\n}", marshalJSONString(localeID), marshalJSONString(oldState), marshalJSONString(oldText))
	after := fmt.Sprintf("%s: {\n  \"stringUnit\": {\n    \"state\": \"translated\",\n    \"value\": %s\n  }\n}", marshalJSONString(localeID), marshalJSONString(newText))
	return before, after
}
