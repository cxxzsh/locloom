package locale

import (
	"path/filepath"
	"regexp"
	"strings"
)

// LocaleSpec describes a configurable target or reference language.
type LocaleSpec struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	EnglishName string            `json:"english_name,omitempty"`
	NativeName  string            `json:"native_name,omitempty"`
	Enabled     bool              `json:"enabled"`
	IsRef       bool              `json:"is_ref"`
	EngineCode  string            `json:"engine_code"`
	EngineCodes map[string]string `json:"engine_codes,omitempty"`
}

// DisplayName returns the locale name appropriate for the given UI language ("en" or "zh").
func (l LocaleSpec) DisplayName(lang string) string {
	if lang == "en" && l.EnglishName != "" {
		return l.EnglishName
	}
	if l.Name != "" {
		return l.Name
	}
	return l.ID
}

// FileName returns the resolved .ts filename for this locale.
// "{id}" is replaced by l.ID; an empty pattern defaults to "{id}.ts".
func (l LocaleSpec) FileName(pattern string) string {
	p := strings.TrimSpace(pattern)
	if p == "" {
		p = "{id}.ts"
	}
	if strings.Contains(p, "{id}") {
		return strings.ReplaceAll(p, "{id}", l.ID)
	}
	return l.ID + ".ts"
}

// Clone returns a deep copy of the LocaleSpec.
func (l LocaleSpec) Clone() LocaleSpec {
	out := l
	if l.EngineCodes != nil {
		out.EngineCodes = make(map[string]string, len(l.EngineCodes))
		for k, v := range l.EngineCodes {
			out.EngineCodes[k] = v
		}
	}
	return out
}

// Profile stores a named selection of locale IDs for quick switching.
type Profile struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	EnglishName string   `json:"english_name,omitempty"`
	LocaleIDs   []string `json:"locale_ids"`
}

// DisplayName returns the profile name appropriate for the given UI language ("en" or "zh").
func (p Profile) DisplayName(lang string) string {
	if lang == "en" && p.EnglishName != "" {
		return p.EnglishName
	}
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// DefaultLocales returns the 22 standard international locales.
// zh_CN is marked IsRef: true, and the 9 Tier-1 locales are enabled by default.
func DefaultLocales() []LocaleSpec {
	return []LocaleSpec{
		// Tier-1 (enabled by default)
		{ID: "zh_CN", Name: "简体中文", EnglishName: "Simplified Chinese", NativeName: "简体中文", Enabled: true, IsRef: true, EngineCode: "zh-CN"},
		{ID: "zh_TW", Name: "繁體中文", EnglishName: "Traditional Chinese", NativeName: "繁體中文", Enabled: true, EngineCode: "zh-TW"},
		{ID: "ja_JP", Name: "日本語", EnglishName: "Japanese", NativeName: "日本語", Enabled: true, EngineCode: "ja"},
		{ID: "ko_KR", Name: "한국어", EnglishName: "Korean", NativeName: "한국어", Enabled: true, EngineCode: "ko"},
		{ID: "de_DE", Name: "Deutsch", EnglishName: "German", NativeName: "Deutsch", Enabled: true, EngineCode: "de"},
		{ID: "fr_FR", Name: "Français", EnglishName: "French", NativeName: "Français", Enabled: true, EngineCode: "fr"},
		{ID: "es_ES", Name: "Español", EnglishName: "Spanish", NativeName: "Español", Enabled: true, EngineCode: "es"},
		{ID: "pt_BR", Name: "Português (BR)", EnglishName: "Portuguese (Brazil)", NativeName: "Português (Brasil)", Enabled: true, EngineCode: "pt"},
		{ID: "ru_RU", Name: "Русский", EnglishName: "Russian", NativeName: "Русский", Enabled: true, EngineCode: "ru"},

		// Additional international locales (disabled by default)
		{ID: "en_US", Name: "English (US)", EnglishName: "English (US)", NativeName: "English", Enabled: false, EngineCode: "en"},
		{ID: "zh_HK", Name: "繁體中文 (香港)", EnglishName: "Traditional Chinese (HK)", NativeName: "繁體中文 (香港)", Enabled: false, EngineCode: "zh-TW"},
		{ID: "pt_PT", Name: "Português (PT)", EnglishName: "Portuguese (Portugal)", NativeName: "Português (Portugal)", Enabled: false, EngineCode: "pt-PT"},
		{ID: "it_IT", Name: "Italiano", EnglishName: "Italian", NativeName: "Italiano", Enabled: false, EngineCode: "it"},
		{ID: "nl_NL", Name: "Nederlands", EnglishName: "Dutch", NativeName: "Nederlands", Enabled: false, EngineCode: "nl"},
		{ID: "pl_PL", Name: "Polski", EnglishName: "Polish", NativeName: "Polski", Enabled: false, EngineCode: "pl"},
		{ID: "tr_TR", Name: "Türkçe", EnglishName: "Turkish", NativeName: "Türkçe", Enabled: false, EngineCode: "tr"},
		{ID: "ar_SA", Name: "العربية", EnglishName: "Arabic", NativeName: "العربية", Enabled: false, EngineCode: "ar"},
		{ID: "vi_VN", Name: "Tiếng Việt", EnglishName: "Vietnamese", NativeName: "Tiếng Việt", Enabled: false, EngineCode: "vi"},
		{ID: "th_TH", Name: "ไทย", EnglishName: "Thai", NativeName: "ไทย", Enabled: false, EngineCode: "th"},
		{ID: "id_ID", Name: "Bahasa Indonesia", EnglishName: "Indonesian", NativeName: "Bahasa Indonesia", Enabled: false, EngineCode: "id"},
		{ID: "cs_CZ", Name: "Čeština", EnglishName: "Czech", NativeName: "Čeština", Enabled: false, EngineCode: "cs"},
		{ID: "uk_UA", Name: "Українська", EnglishName: "Ukrainian", NativeName: "Українська", Enabled: false, EngineCode: "uk"},
	}
}

// DefaultProfiles returns neutral built-in locale selection presets.
func DefaultProfiles() []Profile {
	all := DefaultLocales()
	allIDs := make([]string, 0, len(all))
	for _, l := range all {
		allIDs = append(allIDs, l.ID)
	}
	return []Profile{
		{
			ID:          "tier1",
			Name:        "主流语言 (Tier-1)",
			EnglishName: "Tier-1 (CJK + Western)",
			LocaleIDs:   []string{"zh_CN", "zh_TW", "ja_JP", "ko_KR", "de_DE", "fr_FR", "es_ES", "pt_BR", "ru_RU"},
		},
		{
			ID:          "cjk",
			Name:        "东亚语言 (CJK)",
			EnglishName: "East Asian (CJK)",
			LocaleIDs:   []string{"zh_CN", "zh_TW", "zh_HK", "ja_JP", "ko_KR"},
		},
		{
			ID:          "europe",
			Name:        "欧洲语言 (Europe)",
			EnglishName: "European",
			LocaleIDs:   []string{"de_DE", "fr_FR", "es_ES", "pt_BR", "pt_PT", "it_IT", "nl_NL", "pl_PL", "ru_RU", "cs_CZ", "uk_UA", "tr_TR"},
		},
		{
			ID:          "sea_mena",
			Name:        "东南亚与中东 (SEA & MENA)",
			EnglishName: "SEA & Middle East",
			LocaleIDs:   []string{"vi_VN", "th_TH", "id_ID", "ar_SA", "tr_TR"},
		},
		{
			ID:          "all",
			Name:        "全部语言 (All)",
			EnglishName: "All Configured",
			LocaleIDs:   allIDs,
		},
	}
}

// ResolveEngineCode maps a LocaleSpec to the target language code expected by providerID.
func ResolveEngineCode(spec LocaleSpec, providerID string) string {
	if spec.EngineCodes != nil && providerID != "" {
		if code, ok := spec.EngineCodes[providerID]; ok && strings.TrimSpace(code) != "" {
			return strings.TrimSpace(code)
		}
	}
	if strings.TrimSpace(spec.EngineCode) != "" {
		return strings.TrimSpace(spec.EngineCode)
	}
	return deriveEngineCode(spec.ID)
}

func deriveEngineCode(id string) string {
	norm := strings.ReplaceAll(strings.TrimSpace(id), "-", "_")
	lower := strings.ToLower(norm)
	switch lower {
	case "zh_cn", "zh_hans":
		return "zh-CN"
	case "zh_tw", "zh_hk", "zh_hant":
		return "zh-TW"
	}
	if idx := strings.IndexByte(norm, '_'); idx > 0 {
		return strings.ToLower(norm[:idx])
	}
	return lower
}

var (
	reLocalePair = regexp.MustCompile(`^[a-zA-Z]{2,3}[_-][a-zA-Z]{2,4}$`)
	reParenCode  = regexp.MustCompile(`[\(\[（【]\s*([a-zA-Z]{2,3}(?:[_-][a-zA-Z]{2,4})?)\s*[\)\]）】]`)
	reRefMarker  = regexp.MustCompile(`(?i)(?:[\(\[（【]\s*(?:参考|ref|reference)\s*[\)\]）】]|(?:^|\s)(?:参考|reference)(?:\s|$))`)
)

func buildKnownLookup(known []LocaleSpec) map[string]LocaleSpec {
	m := make(map[string]LocaleSpec, len(known)*2+32)
	for _, l := range DefaultLocales() {
		m[strings.ToLower(strings.ReplaceAll(l.ID, "-", "_"))] = l
	}
	for _, l := range known {
		key := strings.ToLower(strings.ReplaceAll(l.ID, "-", "_"))
		if key != "" {
			m[key] = l
		}
	}
	return m
}

func normalizeCandidate(raw string, knownMap map[string]LocaleSpec) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	underscore := strings.ReplaceAll(s, "-", "_")
	lower := strings.ToLower(underscore)
	if spec, ok := knownMap[lower]; ok {
		return spec.ID
	}
	if reLocalePair.MatchString(underscore) {
		parts := strings.SplitN(underscore, "_", 2)
		lang := strings.ToLower(parts[0])
		region := strings.ToUpper(parts[1])
		canon := lang + "_" + region
		if spec, ok := knownMap[strings.ToLower(canon)]; ok {
			return spec.ID
		}
		return canon
	}
	return ""
}

// StemLocaleID extracts and normalizes the locale ID from a .ts filename or relative path.
// Supports "{id}.ts", "prefix_{id}.ts", "{id}_suffix.ts", and case-insensitive codes like "zh-cn".
func StemLocaleID(filenameOrRelPath, filePattern string, known []LocaleSpec) string {
	slashPath := strings.ReplaceAll(strings.TrimSpace(filenameOrRelPath), "\\", "/")
	base := filepath.Base(slashPath)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	ext := filepath.Ext(base)
	stem := base
	if strings.EqualFold(ext, ".ts") {
		stem = base[:len(base)-len(ext)]
	}
	if stem == "" {
		return ""
	}

	knownMap := buildKnownLookup(known)

	// 1. Try explicit filePattern if provided (e.g. "app_{id}.ts" or "{id}_strings.ts").
	pat := strings.TrimSpace(filePattern)
	if pat != "" && strings.Contains(pat, "{id}") {
		patBase := filepath.Base(strings.ReplaceAll(pat, "\\", "/"))
		patExt := filepath.Ext(patBase)
		if strings.EqualFold(patExt, ".ts") {
			patBase = patBase[:len(patBase)-len(patExt)]
		}
		if idx := strings.Index(patBase, "{id}"); idx >= 0 {
			prefix := patBase[:idx]
			suffix := patBase[idx+len("{id}"):]
			if strings.HasPrefix(stem, prefix) && strings.HasSuffix(stem, suffix) && len(stem) > len(prefix)+len(suffix) {
				candidate := stem[len(prefix) : len(stem)-len(suffix)]
				if id := normalizeCandidate(candidate, knownMap); id != "" {
					return id
				}
				if len(candidate) >= 2 && len(candidate) <= 5 {
					return candidate
				}
			}
		}
	}

	// 2. Direct match on full stem (e.g. "zh_CN", "zh-cn", "ja_JP").
	if id := normalizeCandidate(stem, knownMap); id != "" {
		return id
	}

	// 3. Scan adjacent underscore-separated pairs for prefix_{id}.ts / {id}_suffix.ts (e.g. "app_zh_CN" -> "zh_CN").
	normStem := strings.ReplaceAll(stem, "-", "_")
	parts := strings.Split(normStem, "_")
	for i := 0; i+1 < len(parts); i++ {
		pair := parts[i] + "_" + parts[i+1]
		if id := normalizeCandidate(pair, knownMap); id != "" {
			return id
		}
	}

	// 4. Single short language tag fallback (e.g. "ja.ts" or "app_ja.ts" -> match known locale or return "ja").
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.ToLower(parts[i])
		if len(p) == 2 {
			// Prefer exact known locale whose primary language matches if unique in known.
			var matched []string
			for _, l := range known {
				if strings.HasPrefix(strings.ToLower(l.ID), p+"_") || strings.EqualFold(l.ID, p) {
					matched = append(matched, l.ID)
				}
			}
			if len(matched) == 1 {
				return matched[0]
			}
			for _, l := range DefaultLocales() {
				if strings.HasPrefix(strings.ToLower(l.ID), p+"_") {
					matched = append(matched, l.ID)
				}
			}
			if len(matched) == 1 {
				return matched[0]
			}
			return p
		}
	}

	return stem
}

// IsSourceHeader reports whether an Excel column header represents the source text column.
func IsSourceHeader(header string) bool {
	h := strings.ToLower(strings.TrimSpace(header))
	switch h {
	case "source", "en", "en_us", "en-us", "english", "原文", "英文", "源文本", "source text", "msgid":
		return true
	default:
		return false
	}
}

// IsContextHeader reports whether an Excel column header represents the Qt <context> column.
func IsContextHeader(header string) bool {
	h := strings.ToLower(strings.TrimSpace(header))
	switch h {
	case "context", "ctx", "上下文", "语境", "模块":
		return true
	default:
		return false
	}
}

// IsCommentHeader reports whether an Excel column header represents the disambiguation <comment> column.
func IsCommentHeader(header string) bool {
	h := strings.ToLower(strings.TrimSpace(header))
	switch h {
	case "comment", "disambiguation", "note", "notes", "备注", "注释", "消歧义":
		return true
	default:
		return false
	}
}

var aliasToLocaleID = map[string]string{
	"简体中文": "zh_CN", "中文": "zh_CN", "chinese": "zh_CN", "simplified chinese": "zh_CN", "chinese (simplified)": "zh_CN", "zh-hans": "zh_CN", "chs": "zh_CN",
	"繁体中文": "zh_TW", "繁體中文": "zh_TW", "traditional chinese": "zh_TW", "chinese (traditional)": "zh_TW", "zh-hant": "zh_TW", "cht": "zh_TW",
	"繁體中文 (香港)": "zh_HK", "繁体中文(香港)": "zh_HK", "香港繁体": "zh_HK",
	"日语": "ja_JP", "日文": "ja_JP", "日本語": "ja_JP", "japanese": "ja_JP", "ja": "ja_JP",
	"韩语": "ko_KR", "韩文": "ko_KR", "한국어": "ko_KR", "korean": "ko_KR", "ko": "ko_KR",
	"德语": "de_DE", "德文": "de_DE", "deutsch": "de_DE", "german": "de_DE", "de": "de_DE",
	"法语": "fr_FR", "法文": "fr_FR", "français": "fr_FR", "francais": "fr_FR", "french": "fr_FR", "fr": "fr_FR",
	"西班牙语": "es_ES", "西语": "es_ES", "español": "es_ES", "espanol": "es_ES", "spanish": "es_ES", "es": "es_ES",
	"葡萄牙语": "pt_BR", "葡语": "pt_BR", "português": "pt_BR", "portugues": "pt_BR", "portuguese": "pt_BR", "portuguese (brazil)": "pt_BR", "português (br)": "pt_BR",
	"葡萄牙语 (葡萄牙)": "pt_PT", "português (pt)": "pt_PT", "portuguese (portugal)": "pt_PT",
	"俄语": "ru_RU", "俄文": "ru_RU", "русский": "ru_RU", "russian": "ru_RU", "ru": "ru_RU",
	"意大利语": "it_IT", "意语": "it_IT", "italiano": "it_IT", "italian": "it_IT", "it": "it_IT",
	"荷兰语": "nl_NL", "nederlands": "nl_NL", "dutch": "nl_NL", "nl": "nl_NL",
	"波兰语": "pl_PL", "polski": "pl_PL", "polish": "pl_PL", "pl": "pl_PL",
	"土耳其语": "tr_TR", "türkçe": "tr_TR", "turkce": "tr_TR", "turkish": "tr_TR", "tr": "tr_TR",
	"阿拉伯语": "ar_SA", "العربية": "ar_SA", "arabic": "ar_SA", "ar": "ar_SA",
	"越南语": "vi_VN", "tiếng việt": "vi_VN", "tieng viet": "vi_VN", "vietnamese": "vi_VN", "vi": "vi_VN",
	"泰语": "th_TH", "ไทย": "th_TH", "thai": "th_TH", "th": "th_TH",
	"印尼语": "id_ID", "印度尼西亚语": "id_ID", "bahasa indonesia": "id_ID", "indonesian": "id_ID", "id": "id_ID",
	"捷克语": "cs_CZ", "čeština": "cs_CZ", "cestina": "cs_CZ", "czech": "cs_CZ", "cs": "cs_CZ",
	"乌克兰语": "uk_UA", "українська": "uk_UA", "ukrainian": "uk_UA", "uk": "uk_UA",
	"英语": "en_US", "英文": "en_US", "english (us)": "en_US",
}

// ParseHeaderLocale extracts a locale ID and whether it is marked as a reference column from an Excel header.
func ParseHeaderLocale(header string, known []LocaleSpec) (localeID string, isRef bool, ok bool) {
	raw := strings.TrimSpace(header)
	if raw == "" || IsSourceHeader(raw) || IsContextHeader(raw) || IsCommentHeader(raw) {
		return "", false, false
	}

	if reRefMarker.MatchString(raw) {
		isRef = true
	}
	cleaned := strings.TrimSpace(reRefMarker.ReplaceAllString(raw, " "))
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return "", false, false
	}

	knownMap := buildKnownLookup(known)

	// 1. Direct normalized locale code (e.g. "zh_CN", "zh-cn", "ja_JP").
	if id := normalizeCandidate(cleaned, knownMap); id != "" {
		return id, isRef, true
	}

	// 2. Match against known LocaleSpec Name / NativeName / EngineCode.
	for _, l := range known {
		if strings.EqualFold(cleaned, l.Name) ||
			(l.NativeName != "" && strings.EqualFold(cleaned, l.NativeName)) ||
			(l.EngineCode != "" && strings.EqualFold(cleaned, l.EngineCode)) {
			return l.ID, isRef, true
		}
	}
	for _, l := range DefaultLocales() {
		if strings.EqualFold(cleaned, l.Name) ||
			(l.NativeName != "" && strings.EqualFold(cleaned, l.NativeName)) ||
			(l.EngineCode != "" && strings.EqualFold(cleaned, l.EngineCode)) {
			return l.ID, isRef, true
		}
	}

	// 3. Code in parentheses/brackets (e.g. "Japanese (ja_JP)" or "日本語 [ja]").
	if m := reParenCode.FindStringSubmatch(cleaned); len(m) > 1 {
		code := m[1]
		if id := normalizeCandidate(code, knownMap); id != "" {
			return id, isRef, true
		}
		if id, exists := aliasToLocaleID[strings.ToLower(code)]; exists {
			return id, isRef, true
		}
	}

	// 4. Common English/Chinese/native alias table.
	if id, exists := aliasToLocaleID[strings.ToLower(cleaned)]; exists {
		return id, isRef, true
	}

	return "", false, false
}

// MergeDiscovered merges newly discovered locale IDs into existing without losing existing settings.
// Returns the merged slice and the list of newly added IDs.
func MergeDiscovered(existing []LocaleSpec, discoveredIDs []string) (merged []LocaleSpec, added []string) {
	merged = make([]LocaleSpec, 0, len(existing)+len(discoveredIDs))
	seen := make(map[string]bool, len(existing)+len(discoveredIDs))
	for _, l := range existing {
		merged = append(merged, l.Clone())
		seen[strings.ToLower(strings.ReplaceAll(l.ID, "-", "_"))] = true
	}

	defaults := make(map[string]LocaleSpec)
	for _, l := range DefaultLocales() {
		defaults[strings.ToLower(strings.ReplaceAll(l.ID, "-", "_"))] = l
	}

	for _, id := range discoveredIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(strings.ReplaceAll(trimmed, "-", "_"))
		if seen[key] {
			continue
		}
		seen[key] = true

		if def, ok := defaults[key]; ok {
			def.Enabled = true
			merged = append(merged, def)
			added = append(added, def.ID)
		} else {
			spec := LocaleSpec{
				ID:         trimmed,
				Name:       trimmed,
				Enabled:    true,
				EngineCode: deriveEngineCode(trimmed),
			}
			merged = append(merged, spec)
			added = append(added, trimmed)
		}
	}

	return merged, added
}
