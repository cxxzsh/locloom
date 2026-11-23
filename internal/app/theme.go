package app

// LocLoom's visual language, adapted from the Beautiful UI design system
// (github.com/slev12397/beautiful-ui): a cool near-white canvas, white
// cards, hairline borders, soft layered shadows, a neutral ink ramp and a
// bright accent used sparingly. The oklch tokens are baked to sRGB here.
//
// Token mapping onto mygo's Theme:
//   Background     → --page     (wells & popup bodies; segmented thumbs too)
//   Surface        → --surface  (white cards, control faces, popups)
//   SurfaceHover   → --hover,   SurfacePressed → --hover-2
//   Border         → --line-strong (control rings, dividers, split lines)
//   Text/TextMuted → --ink / --ink-2; the extra --ink-3 lives in ink3()
//   Accent         → --accent,  Danger → --red, Warning → --orange,
//   Success        → --green
//   Radius         → --radius-control (8); cards use cardRadius (10) via
//   hairline()/cardBg(), pills use pillRadius (999)

import "github.com/egoist/mygo/ui"

const (
	cardRadius = float32(10)
	pillRadius = float32(999)
)

// beautifulLightTheme returns the light appearance.
func beautifulLightTheme() *ui.Theme {
	return &ui.Theme{
		Background:      ui.Hex("#ffffff"), // raised surfaces: cards, popups, segmented thumb
		Surface:         ui.Hex("#f5f6f7"), // --field: control faces, input fills, segmented track
		SurfaceHover:    ui.Hex("#f0f1f2"), // --hover
		SurfacePressed:  ui.Hex("#e7e9eb"), // --hover-2
		Border:          ui.Hex("#e0e2e5"), // --line-strong
		Text:            ui.Hex("#1f2124"), // --ink
		TextMuted:       ui.Hex("#62656b"), // --ink-2
		Accent:          ui.Hex("#0285ff"), // --accent
		AccentHover:     ui.Hex("#0070dd"), // --accent-ink
		AccentPressed:   ui.Hex("#0061c2"),
		AccentText:      ui.Hex("#ffffff"),
		InactiveAccent:  ui.Hex("#0285ff"),
		Danger:          ui.Hex("#e3474c"),   // --red
		Warning:         ui.Hex("#ef720d"),   // --orange
		Success:         ui.Hex("#199a4d"),   // --green
		Selection:       ui.Hex("#0285ff26"), // accent at 15% — near --accent-tint over white
		Focus:           ui.Hex("#0285ff66"), // accent at 40%
		FocusRingWidth:  2,
		FocusRingOffset: 1,
		Inverse:         ui.Hex("#25272b"), // --tooltip-bg
		InverseText:     ui.Hex("#f6f7f8"), // --tooltip-fg
		Scrollbar:       ui.Hex("#00000052"),
		Radius:          8, // --radius-control
		Spacing:         4,
		FontSize:        13,
	}
}

// beautifulDarkTheme returns the dark appearance: elevation through a
// hairline ring and deeper ink, accent stays the same hue, lighter.
func beautifulDarkTheme() *ui.Theme {
	return &ui.Theme{
		Dark:            true,
		Background:      ui.Hex("#232427"), // raised surfaces: cards, popups, segmented thumb
		Surface:         ui.Hex("#2b2c2f"), // --field: control faces, input fills, segmented track
		SurfaceHover:    ui.Hex("#313236"), // --hover
		SurfacePressed:  ui.Hex("#3a3c40"), // --hover-2
		Border:          ui.Hex("#3a3c40"), // --line-strong
		Text:            ui.Hex("#f2f3f4"), // --ink
		TextMuted:       ui.Hex("#a5a8ad"), // --ink-2
		Accent:          ui.Hex("#3d9aff"), // --accent
		AccentHover:     ui.Hex("#7ec0ff"), // --accent-ink
		AccentPressed:   ui.Hex("#1f7ff0"),
		AccentText:      ui.Hex("#ffffff"),
		InactiveAccent:  ui.Hex("#3d9aff"),
		Danger:          ui.Hex("#ee5c61"),   // --red
		Warning:         ui.Hex("#f68f3c"),   // --orange
		Success:         ui.Hex("#3cbb72"),   // --green
		Selection:       ui.Hex("#3d9aff59"), // accent at 35%
		Focus:           ui.Hex("#3d9aff80"), // accent at 50%
		FocusRingWidth:  2,
		FocusRingOffset: 1,
		Inverse:         ui.Hex("#111214"), // --tooltip-bg
		InverseText:     ui.Hex("#f2f3f4"), // --tooltip-fg
		Scrollbar:       ui.Hex("#ffffff59"),
		Radius:          8,
		Spacing:         4,
		FontSize:        13,
	}
}

// beautifulTheme resolves the palette for the current theme mode.
func beautifulTheme(dark bool) *ui.Theme {
	if dark {
		return beautifulDarkTheme()
	}
	return beautifulLightTheme()
}

// ── Semantic helpers ──────────────────────────────────────────────

// ink3 is the tertiary text token (--ink-3): hints, placeholders, empty states.
func ink3(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#6c6f75")
	}
	return ui.Hex("#9a9da3")
}

// accentInk is the accent used for text on light surfaces (--accent-ink).
func accentInk(t *ui.Theme) ui.Color {
	return t.AccentHover
}

// accentTint is the solid --accent-tint chip background (a real tint in
// light, an accent wash in dark).
func accentTint(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Accent.Alpha(0.16)
	}
	return ui.Hex("#e9f3ff")
}

// insetBg is --inset: a sunken well inside a white card (lists, code, previews).
// canvasBg is --canvas: the page gutter that shows around the cards.
// It is kept off Theme.Background because the segmented control paints
// its selected thumb with Theme.Background.
func canvasBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#1c1d1f")
	}
	return ui.Hex("#f1f2f3")
}

func insetBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#1f2022")
	}
	return ui.Hex("#f7f8f9")
}

// orangeTint is --orange-tint: soft chip background under --orange text.
func orangeTint(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Warning.Alpha(0.14)
	}
	return ui.Hex("#fdf1e5")
}

// redTint is --red-tint: soft chip background under --red text.
func redTint(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Danger.Alpha(0.14)
	}
	return ui.Hex("#fcecec")
}

// greenTint is --green-tint: soft chip background under --green text.
func greenTint(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Success.Alpha(0.14)
	}
	return ui.Hex("#e8f5ed")
}

// statusTint picks the matching --{color}-tint chip background for a
// semantic foreground (Danger/Warning/Success/Accent); anything else gets
// the neutral --inset wash used by tone "neutral" StatusPills.
func statusTint(t *ui.Theme, fg ui.Color) ui.Color {
	switch fg {
	case t.Danger:
		return redTint(t)
	case t.Warning:
		return orangeTint(t)
	case t.Success:
		return greenTint(t)
	case t.Accent, t.AccentHover:
		return accentTint(t)
	default:
		if t.Dark {
			return t.Text.Alpha(0.10)
		}
		return insetBg(t)
	}
}

// hairline returns the card edge --line (softer than the control ring t.Border).
func hairline(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#2e3033")
	}
	return ui.Hex("#ecedef")
}

// fieldBg is --field, the fill of text inputs inside white cards.
func fieldBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#2b2c2f")
	}
	return ui.Hex("#f2f2f3")
}

// ── Component styles ──────────────────────────────────────────────

// cardStyles styles e as a Beautiful UI card: white --surface, --radius-card,
// the --line hairline ring and a soft one-stop drop shadow (the mygo painter
// draws a single shadow, so the shadow-plugin stack is collapsed to its
// strongest layer). Dark mode swaps the ring for a subtle light edge.
func cardStyles(t *ui.Theme, e ui.Element) ui.Element {
	e = e.Background(t.Background).Radius(cardRadius)
	if t.Dark {
		return e.Border(1, ui.RGBA(255, 255, 255, 0.10)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.25))
	}
	return e.Border(1, hairline(t)).Shadow(0, 1, 3, 0, ui.RGBA(31, 33, 36, 0.06))
}

// card wraps the children of the calling context in a card surface.
// Usage mirrors the plain containers: card(c, t).Padding(…).Children(fn).
func card(c *ui.Context, t *ui.Theme) ui.Element {
	return cardStyles(t, ui.Column(c))
}

// insetWell styles e as a recessed --inset region inside a card.
func insetWell(t *ui.Theme, e ui.Element) ui.Element {
	return e.Background(insetBg(t)).Border(1, hairline(t)).Radius(t.Radius)
}
