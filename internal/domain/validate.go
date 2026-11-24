package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	// reUniversalPlaceholder matches placeholders across Qt (%1, %L1), C/Android/Apple printf
	// (%s, %d, %1$s, %1$@, %@, %lld, %(name)s, %#@v@), i18next ({{name}}), Rails (%{name}),
	// .NET/ARB/Vue ({0}, {0:N2}, {name}), and XLIFF/Android inline tags (<ph>, <x/>, <xliff:g>).
	reUniversalPlaceholder = regexp.MustCompile(
		`<ph\b[^>]*>[\s\S]*?</ph>|<x\b[^/>]*/>|<xliff:g\b[^>]*>[\s\S]*?</xliff:g>|</?[bui]>|<br\s*/?>|` +
			`\{\{-?\s*[a-zA-Z0-9_.-]+\s*\}\}|` +
			`%\{[a-zA-Z0-9_]+\}|` +
			`%#@?[a-zA-Z0-9_]+@|` +
			`%(?:[1-9][0-9]*\$)?(?:l{1,2}[duoxX]|[@sduoxXfFeEgGc])|` +
			`%\([a-zA-Z0-9_]+\)[sduoxXfFeEgG]|` +
			`%L?[1-9][0-9]?|` +
			`\{(?:[0-9]+(?::[^}\n]+)?|[a-zA-Z_][a-zA-Z0-9_]*)\}`,
	)
	reLeakMaskToken = regexp.MustCompile(`\[\[\s*\d+\s*\]\]|【【\s*\d+\s*】】`)
)

// ExtractQtArgs returns sorted unique placeholders across supported localization formats.
func ExtractQtArgs(s string) []string {
	return ExtractPlaceholders(s)
}

// ExtractPlaceholders returns sorted unique format placeholders found in s.
func ExtractPlaceholders(s string) []string {
	matches := reUniversalPlaceholder.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, ok := seen[m]; !ok {
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// ValidateTranslation checks Qt placeholder parity, leaked guard tokens, and structural whitespace mismatches
// using Chinese warning messages. Returns nil when valid.
func ValidateTranslation(source, target string, numerus bool) []string {
	return ValidateTranslationLang(source, target, numerus, "zh")
}

// ValidateTranslationLang checks Qt placeholder parity, leaked guard tokens, and structural whitespace mismatches.
// Warning messages are returned in English when lang == "en", and in Chinese otherwise. Returns nil when valid.
func ValidateTranslationLang(source, target string, numerus bool, lang string) []string {
	if source == "" || target == "" {
		return nil
	}
	isEn := lang == "en"
	var warnings []string

	srcArgs := ExtractQtArgs(source)
	tgtArgs := ExtractQtArgs(target)

	srcSet := make(map[string]bool, len(srcArgs))
	for _, a := range srcArgs {
		srcSet[a] = true
	}
	tgtSet := make(map[string]bool, len(tgtArgs))
	for _, a := range tgtArgs {
		tgtSet[a] = true
	}

	var missing, extra []string
	for _, a := range srcArgs {
		if !tgtSet[a] {
			missing = append(missing, a)
		}
	}
	for _, a := range tgtArgs {
		if !srcSet[a] {
			extra = append(extra, a)
		}
	}
	if len(missing) > 0 {
		if isEn {
			warnings = append(warnings, fmt.Sprintf("Missing placeholder(s): %s", strings.Join(missing, ", ")))
		} else {
			warnings = append(warnings, fmt.Sprintf("缺失占位符: %s", strings.Join(missing, ", ")))
		}
	}
	if len(extra) > 0 {
		if isEn {
			warnings = append(warnings, fmt.Sprintf("Unexpected placeholder(s): %s", strings.Join(extra, ", ")))
		} else {
			warnings = append(warnings, fmt.Sprintf("多余占位符: %s", strings.Join(extra, ", ")))
		}
	}

	// Non-numerus %n parity check.
	if !numerus {
		srcHasN := strings.Contains(source, "%n")
		tgtHasN := strings.Contains(target, "%n")
		if srcHasN && !tgtHasN {
			if isEn {
				warnings = append(warnings, "Missing plural placeholder %n")
			} else {
				warnings = append(warnings, "缺失复数占位符 %n")
			}
		} else if !srcHasN && tgtHasN {
			if isEn {
				warnings = append(warnings, "Unexpected plural placeholder %n")
			} else {
				warnings = append(warnings, "多余复数占位符 %n")
			}
		}
	}

	if reLeakMaskToken.MatchString(target) {
		if isEn {
			warnings = append(warnings, "Unrestored mask token [[N]] detected")
		} else {
			warnings = append(warnings, "疑似残留未还原的掩码标记 [[N]]")
		}
	}
	// Trailing sentence punctuation parity: engines sometimes drop a source's
	// final colon/period, which then cascades into downstream "translate from"
	// outputs. Warn so the gap surfaces instead of silently propagating.
	if ws, wt := punctClass(trailingPunct(source)), punctClass(trailingPunct(target)); ws != wt {
		src := string(trailingPunct(source))
		tgt := string(trailingPunct(target))
		if src == "" {
			src = "∅"
		}
		if tgt == "" {
			tgt = "∅"
		}
		if isEn {
			warnings = append(warnings, fmt.Sprintf("Trailing punctuation differs: source ends %q, target ends %q", src, tgt))
		} else {
			warnings = append(warnings, fmt.Sprintf("末尾标点不一致：原文以 %q 结尾，译文以 %q 结尾", src, tgt))
		}
	}

	if strings.HasSuffix(source, "\t") != strings.HasSuffix(target, "\t") {
		if isEn {
			warnings = append(warnings, "Trailing tab \\t differs from source")
		} else {
			warnings = append(warnings, "末尾制表符 \\t 与原文不一致")
		}
	}

	return warnings
}

// trailingPunct returns the last rune of s when it is a sentence terminator
// (./:/?/!/;/, and CJK 。/：/！/？/；/，); it returns 0 otherwise.
func trailingPunct(s string) rune {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return 0
	}
	runes := []rune(s)
	last := runes[len(runes)-1]
	switch last {
	case '.', ':', '?', '!', ';', ',', '。', '：', '！', '？', '；', '，':
		return last
	}
	return 0
}

// punctClass maps a trailing punctuation rune to an equivalence class so ASCII
// and CJK forms (e.g. ? / ？, : / ：, . / 。) count as the same terminator.
func punctClass(r rune) int {
	switch r {
	case '.', '。':
		return 1
	case ':', '：':
		return 2
	case '?', '？':
		return 3
	case '!', '！':
		return 4
	case ';', '；':
		return 5
	case ',', '，':
		return 6
	}
	return 0
}
