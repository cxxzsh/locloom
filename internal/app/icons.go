package app

import (
	"bytes"
	_ "embed"
	"image/png"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed icon.svg
var appIconDarkSVGBytes []byte

//go:embed icon_light.svg
var appIconLightSVGBytes []byte

var (
	iconAppDark    = ui.MustParseSVG(appIconDarkSVGBytes)
	iconAppLight   = ui.MustParseSVG(appIconLightSVGBytes)
	iconBrandDark  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 176 176" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="b" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#1A1E26"/><stop offset="100%" stop-color="#0C0E13"/></linearGradient></defs><rect x="0" y="0" width="176" height="176" rx="40" fill="url(#b)"/><rect x="1" y="1" width="174" height="174" rx="39" fill="none" stroke="#2D3342" stroke-width="2"/><g transform="translate(88, 93) scale(1.14)"><polygon points="-40.7,32.5 40.7,32.5 0,-38 -6.35,-27 21.65,21.5 -34.35,21.5" fill="#F5F7FA" fill-opacity="0.38" transform="rotate(240)"/><polygon points="-40.7,32.5 40.7,32.5 0,-38 -6.35,-27 21.65,21.5 -34.35,21.5" fill="#F5F7FA" fill-opacity="0.68" transform="rotate(120)"/><polygon points="-40.7,32.5 40.7,32.5 0,-38 -6.35,-27 21.65,21.5 -34.35,21.5" fill="#F5F7FA" transform="rotate(0)"/></g></svg>`))
	iconBrandLight = ui.MustParseSVG([]byte(`<svg viewBox="0 0 176 176" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="w" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#FFFFFF"/><stop offset="100%" stop-color="#EDF0F6"/></linearGradient></defs><rect x="0" y="0" width="176" height="176" rx="40" fill="url(#w)"/><rect x="1" y="1" width="174" height="174" rx="39" fill="none" stroke="#CDD4E0" stroke-width="2.2"/><g transform="translate(88, 93) scale(1.14)"><polygon points="-40.7,32.5 40.7,32.5 0,-38 -6.35,-27 21.65,21.5 -34.35,21.5" fill="#101318" fill-opacity="0.34" transform="rotate(240)"/><polygon points="-40.7,32.5 40.7,32.5 0,-38 -6.35,-27 21.65,21.5 -34.35,21.5" fill="#101318" fill-opacity="0.64" transform="rotate(120)"/><polygon points="-40.7,32.5 40.7,32.5 0,-38 -6.35,-27 21.65,21.5 -34.35,21.5" fill="#101318" transform="rotate(0)"/></g></svg>`))
	iconFolder     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M1.8 4a1.2 1.2 0 0 1 1.2-1.2h3.4l1.5 1.7H13a1.2 1.2 0 0 1 1.2 1.2v6.6A1.2 1.2 0 0 1 13 13.5H3a1.2 1.2 0 0 1-1.2-1.2z"/></svg>`))
	iconZip        = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M3.2 2h6.1l3.5 3.5v8.3a1.2 1.2 0 0 1-1.2 1.2H3.2A1.2 1.2 0 0 1 2 13.8V3.2A1.2 1.2 0 0 1 3.2 2z"/><path d="M9.2 2v3.6h3.6M5 8.5h6M5 11.2h4.2"/></svg>`))
	iconImport     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M8 2.2v7.6M5.2 7l2.8 2.8L10.8 7M2.5 11.5v1.8a1.2 1.2 0 0 0 1.2 1.2h8.6a1.2 1.2 0 0 0 1.2-1.2v-1.8"/></svg>`))
	iconExport     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M8 9.8V2.2M5.2 5l2.8-2.8L10.8 5M2.5 11.5v1.8a1.2 1.2 0 0 0 1.2 1.2h8.6a1.2 1.2 0 0 0 1.2-1.2v-1.8"/></svg>`))
	iconPlay       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><path d="M4.2 2.8a.7.7 0 0 1 1.06-.6l8 5.2a.7.7 0 0 1 0 1.2l-8 5.2a.7.7 0 0 1-1.06-.6z"/></svg>`))
	iconStop       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><rect x="3.5" y="3.5" width="9" height="9" rx="1.6"/></svg>`))
	iconSave       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M2.8 2.2h8.1l2.9 2.9v8.1a1.2 1.2 0 0 1-1.2 1.2H2.8a1.2 1.2 0 0 1-1.2-1.2V3.4a1.2 1.2 0 0 1 1.2-1.2z"/><path d="M5.2 2.2v3.4h5.2V2.2M4.8 14.4V9.8h6.4v4.6"/></svg>`))
	iconTheme      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="3"/><path d="M8 1.6v1.4M8 13v1.4M1.6 8H3M13 8h1.4M3.5 3.5l1 1M11.5 11.5l1 1M3.5 12.5l1-1M11.5 4.5l1-1"/></svg>`))
	iconHelp       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.2"/><path d="M6.3 6.2a1.8 1.8 0 0 1 3.4.8c0 1.2-1.7 1.4-1.7 2.5M8 11.9h.01"/></svg>`))
	iconLogs       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><rect x="1.8" y="2.5" width="12.4" height="11" rx="1.6"/><path d="M4.6 6l2.2 2-2.2 2M8.2 10h3.2"/></svg>`))
	iconGrip       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><circle cx="5.5" cy="3.5" r="1.3"/><circle cx="10.5" cy="3.5" r="1.3"/><circle cx="5.5" cy="8" r="1.3"/><circle cx="10.5" cy="8" r="1.3"/><circle cx="5.5" cy="12.5" r="1.3"/><circle cx="10.5" cy="12.5" r="1.3"/></svg>`))
	iconRefresh    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M13.5 8a5.5 5.5 0 1 1-1.61-3.89"/><path d="M13.5 2.2v2.4h-2.4"/></svg>`))
)

func renderSVGIconPNG(svgDoc *ui.SVG) ([]byte, error) {
	img := ui.Render(func(c *ui.Context) {
		th := ui.DarkTheme()
		th.Background = ui.Transparent
		c.SetTheme(th)
		ui.Image(c, svgDoc).Size(512, 512)
	}, 512, 512, 1.0)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	appIconDarkPNGOnce  = sync.OnceValues(func() ([]byte, error) { return renderSVGIconPNG(iconAppDark) })
	appIconLightPNGOnce = sync.OnceValues(func() ([]byte, error) { return renderSVGIconPNG(iconAppLight) })
)

// AppIconPNG returns a cached 512x512 RGBA PNG of the Dark obsidian LocLoom app icon.
func AppIconPNG() ([]byte, error) {
	return appIconDarkPNGOnce()
}

// AppIconLightPNG returns a cached 512x512 RGBA PNG of the Light porcelain-white LocLoom app icon.
func AppIconLightPNG() ([]byte, error) {
	return appIconLightPNGOnce()
}

// AppIconPNGForTheme returns the Dark obsidian or Light porcelain-white 512x512 PNG app icon.
func AppIconPNGForTheme(dark bool) ([]byte, error) {
	if dark {
		return appIconDarkPNGOnce()
	}
	return appIconLightPNGOnce()
}
