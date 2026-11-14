package app

import "github.com/egoist/mygo/ui"

var (
	iconFolder = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M1.8 4a1.2 1.2 0 0 1 1.2-1.2h3.4l1.5 1.7H13a1.2 1.2 0 0 1 1.2 1.2v6.6A1.2 1.2 0 0 1 13 13.5H3a1.2 1.2 0 0 1-1.2-1.2z"/></svg>`))
	iconZip    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M3.2 2h6.1l3.5 3.5v8.3a1.2 1.2 0 0 1-1.2 1.2H3.2A1.2 1.2 0 0 1 2 13.8V3.2A1.2 1.2 0 0 1 3.2 2z"/><path d="M9.2 2v3.6h3.6M5 8.5h6M5 11.2h4.2"/></svg>`))
	iconImport = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M8 2.2v7.6M5.2 7l2.8 2.8L10.8 7M2.5 11.5v1.8a1.2 1.2 0 0 0 1.2 1.2h8.6a1.2 1.2 0 0 0 1.2-1.2v-1.8"/></svg>`))
	iconExport = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M8 9.8V2.2M5.2 5l2.8-2.8L10.8 5M2.5 11.5v1.8a1.2 1.2 0 0 0 1.2 1.2h8.6a1.2 1.2 0 0 0 1.2-1.2v-1.8"/></svg>`))
	iconPlay   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><path d="M4.2 2.8a.7.7 0 0 1 1.06-.6l8 5.2a.7.7 0 0 1 0 1.2l-8 5.2a.7.7 0 0 1-1.06-.6z"/></svg>`))
	iconStop   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><rect x="3.5" y="3.5" width="9" height="9" rx="1.6"/></svg>`))
	iconSave   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M2.8 2.2h8.1l2.9 2.9v8.1a1.2 1.2 0 0 1-1.2 1.2H2.8a1.2 1.2 0 0 1-1.2-1.2V3.4a1.2 1.2 0 0 1 1.2-1.2z"/><path d="M5.2 2.2v3.4h5.2V2.2M4.8 14.4V9.8h6.4v4.6"/></svg>`))
	iconTheme  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="3"/><path d="M8 1.6v1.4M8 13v1.4M1.6 8H3M13 8h1.4M3.5 3.5l1 1M11.5 11.5l1 1M3.5 12.5l1-1M11.5 4.5l1-1"/></svg>`))
	iconHelp   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.2"/><path d="M6.3 6.2a1.8 1.8 0 0 1 3.4.8c0 1.2-1.7 1.4-1.7 2.5M8 11.9h.01"/></svg>`))
	iconLogs   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><rect x="1.8" y="2.5" width="12.4" height="11" rx="1.6"/><path d="M4.6 6l2.2 2-2.2 2M8.2 10h3.2"/></svg>`))
)
