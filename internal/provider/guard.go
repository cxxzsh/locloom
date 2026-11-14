package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
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

var (
	reRestoreDouble = regexp.MustCompile(`[\[［【]\s*[\[［【]\s*(\d+)\s*[\]］】]\s*[\]］】]`)
	reRestoreSingle = regexp.MustCompile(`[\[［【]\s*(\d+)\s*[\]］】]`)
)

func isEdgeSpaceRune(r rune) bool {
	return r == ' ' || r == '\u00a0'
}

func splitEdgeSpaces(s string) (leadWS, core, trailWS string) {
	leadEnd := 0
	for leadEnd < len(s) {
		r, sz := utf8.DecodeRuneInString(s[leadEnd:])
		if !isEdgeSpaceRune(r) {
			break
		}
		leadEnd += sz
	}
	trailStart := len(s)
	for trailStart > leadEnd {
		r, sz := utf8.DecodeLastRuneInString(s[leadEnd:trailStart])
		if !isEdgeSpaceRune(r) {
			break
		}
		trailStart -= sz
	}
	return s[:leadEnd], s[leadEnd:trailStart], s[trailStart:]
}

func hasLiteralSingleBracket(masked string, idx int) bool {
	if masked == "" {
		return false
	}
	doubleTok := "[[" + strconv.Itoa(idx) + "]]"
	singleTok := "[" + strconv.Itoa(idx) + "]"
	withoutDouble := strings.ReplaceAll(masked, doubleTok, "")
	return strings.Contains(withoutDouble, singleTok)
}

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
	leadWS, core, trailWS := splitEdgeSpaces(src)

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
	if len(m.Tokens) > 0 {
		restored := make([]bool, len(m.Tokens))

		// Pass 1: single-pass replacement of double-bracket anchors [[N]], 【【N】】, ［［N］］.
		// Using ReplaceAllStringFunc ensures replacement tokens containing [N] never re-trigger later tokens.
		out = reRestoreDouble.ReplaceAllStringFunc(out, func(match string) string {
			sub := reRestoreDouble.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			idx, err := strconv.Atoi(sub[1])
			if err != nil || idx < 0 || idx >= len(m.Tokens) {
				return match
			}
			restored[idx] = true
			return m.Tokens[idx]
		})

		// Pass 2: single-pass fallback for collapsed single-bracket anchors [N], only for tokens not yet
		// restored and whose literal [N] did not exist in the masked source text.
		needFallback := false
		for _, ok := range restored {
			if !ok {
				needFallback = true
				break
			}
		}
		if needFallback {
			out = reRestoreSingle.ReplaceAllStringFunc(out, func(match string) string {
				sub := reRestoreSingle.FindStringSubmatch(match)
				if len(sub) < 2 {
					return match
				}
				idx, err := strconv.Atoi(sub[1])
				if err != nil || idx < 0 || idx >= len(m.Tokens) {
					return match
				}
				if restored[idx] || hasLiteralSingleBracket(m.Masked, idx) {
					return match
				}
				if strings.Contains(translated, m.Tokens[idx]) {
					return match
				}
				return m.Tokens[idx]
			})
		}
	}

	// Trim any stray edge spaces introduced by the MT engine and re-apply original edge spaces.
	_, out, _ = splitEdgeSpaces(out)
	return m.LeadingWS + out + m.TrailingWS
}
