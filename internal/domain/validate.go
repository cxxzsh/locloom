package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	reQtArgPlaceholder = regexp.MustCompile(`%L?[1-9][0-9]?`)
	reLeakMaskToken    = regexp.MustCompile(`\[\[\s*\d+\s*\]\]|【【\s*\d+\s*】】`)
)

// ExtractQtArgs returns sorted unique Qt positional placeholders (%1..%99, %L1..%L99).
func ExtractQtArgs(s string) []string {
	matches := reQtArgPlaceholder.FindAllString(s, -1)
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

	if strings.HasSuffix(source, "\t") != strings.HasSuffix(target, "\t") {
		if isEn {
			warnings = append(warnings, "Trailing tab \\t differs from source")
		} else {
			warnings = append(warnings, "末尾制表符 \\t 与原文不一致")
		}
	}

	return warnings
}
