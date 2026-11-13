package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var reMaskToken = regexp.MustCompile(
	`<ph\b[^>]*>[\s\S]*?</ph>|<x\b[^/>]*/>|<xliff:g\b[^>]*>[\s\S]*?</xliff:g>|</?[bui]>|<br\s*/?>|` +
		`\{\{-?\s*[a-zA-Z0-9_.-]+\s*\}\}|` +
		`%\{[a-zA-Z0-9_]+\}|` +
		`%#@?[a-zA-Z0-9_]+@|` +
		`%(?:[1-9][0-9]*\$)?(?:l{1,2}[duoxX]|[@sduoxXfFeEgGc])|` +
		`%\([a-zA-Z0-9_]+\)[sduoxXfFeEgG]|` +
		`%L?[1-9][0-9]?|%n|\{(?:[0-9]+(?::[^}\n]+)?|[a-zA-Z_][a-zA-Z0-9_]*)\}|\n|\t`,
)

// MaskedText holds a placeholder-masked string along with original tokens and edge spaces.
type MaskedText struct {
	Masked     string
	Tokens     []string
	LeadingWS  string
	TrailingWS string
}

// MaskPlaceholders records intentional leading/trailing spaces and replaces Qt placeholders
// (%1..%99, %L1..%L99, %n), newlines (\n), and tabs (\t) with numeric anchors [[0]], [[1]], ...
// so machine translation engines do not corrupt or reorder them.
func MaskPlaceholders(src string) MaskedText {
	if src == "" {
		return MaskedText{}
	}

	// Preserve intentional leading/trailing spaces (ASCII space and non-breaking space).
	// Newlines and tabs are protected as explicit [[N]] tokens below.
	leadEnd := 0
	for leadEnd < len(src) && src[leadEnd] == ' ' {
		leadEnd++
	}
	trailStart := len(src)
	for trailStart > leadEnd && src[trailStart-1] == ' ' {
		trailStart--
	}

	leadWS := src[:leadEnd]
	trailWS := src[trailStart:]
	core := src[leadEnd:trailStart]

	if core == "" {
		return MaskedText{
			Masked:     "",
			LeadingWS:  leadWS,
			TrailingWS: trailWS,
		}
	}

	idxs := reMaskToken.FindAllStringIndex(core, -1)
	if len(idxs) == 0 {
		return MaskedText{
			Masked:     core,
			LeadingWS:  leadWS,
			TrailingWS: trailWS,
		}
	}

	var b strings.Builder
	tokens := make([]string, 0, len(idxs))
	last := 0
	for i, loc := range idxs {
		b.WriteString(core[last:loc[0]])
		tok := core[loc[0]:loc[1]]
		tokens = append(tokens, tok)
		b.WriteString(fmt.Sprintf("[[%d]]", i))
		last = loc[1]
	}
	b.WriteString(core[last:])

	return MaskedText{
		Masked:     b.String(),
		Tokens:     tokens,
		LeadingWS:  leadWS,
		TrailingWS: trailWS,
	}
}

// RestorePlaceholders restores numeric anchors [[0]], [[1]], ... (including full-width or spaced
// variants produced by MT engines) back to their original tokens and restores intentional
// leading/trailing spaces from m.
func RestorePlaceholders(translated string, m MaskedText) string {
	if translated == "" {
		if m.Masked == "" && (m.LeadingWS != "" || m.TrailingWS != "") {
			return m.LeadingWS + m.TrailingWS
		}
		return ""
	}

	out := translated
	for i, tok := range m.Tokens {
		num := strconv.Itoa(i)
		// Match [[0]], [[ 0 ]], [ [ 0 ] ], 【【0】】, ［［0］］, and mixed bracket variants.
		reDouble := regexp.MustCompile(`[\[［【]\s*[\[［【]\s*` + num + `\s*[\]］】]\s*[\]］】]`)
		if reDouble.MatchString(out) {
			out = reDouble.ReplaceAllLiteralString(out, tok)
			continue
		}
		// Fallback if engine collapsed double brackets into single [0], 【0】, ［0］.
		reSingle := regexp.MustCompile(`[\[［【]\s*` + num + `\s*[\]］】]`)
		if !strings.Contains(out, tok) && reSingle.MatchString(out) {
			out = reSingle.ReplaceAllLiteralString(out, tok)
		}
	}

	// Trim any stray edge spaces introduced by the MT engine and re-apply original edge spaces.
	out = strings.Trim(out, " ")
	return m.LeadingWS + out + m.TrailingWS
}
