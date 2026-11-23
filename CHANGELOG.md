# Changelog

## 0.1.2 - 2026-11-23

### Added

- **Per-locale translation source ("译自" / "From")**: each target locale card gets a dropdown pinning its translation input to another locale's existing translations (e.g. `zh_TW` from `zh_CN` for Simplified→Traditional conversion, `zh_HK` from `zh_TW`), falling back to the file's source text when the pinned entry is empty.
- **Cascade translation**: batch and single-row translation now topologically sorts dependent locales into waves — `zh_CN` translates from the file source first, then `zh_TW` consumes its fresh output in the same run, chained for deeper dependencies (cycles fall back to config order).
- **Wave progress**: multi-wave runs prefix the translation banner with `Wave i/n`.
- **Signed auto-update publishing**: releases now ship `mygo` update archives, per-platform `update-*.json` manifests and delta updates, plus `install.sh`/`install.ps1` helpers, so future versions can self-update in-app.

### Fixed

- Translation progress no longer double-counts items or logs the start banner twice when a cascade runs.

## 0.1.1 - 2026-11-16

- Respect `ModeOverwriteExisting` in batch translation; pointer cursor on the write-mode switch.
- Waterfall provider order optimization with drag-and-drop reordering.
- Cache management panel: search, locale/engine filters, filtered bulk clear, single-entry delete.
- All-platform release artifacts (macOS universal `.dmg`, Windows NSIS + portable zips, Linux `.deb`/`.rpm`/`.tar.gz`).

## 0.1.0 - 2026-11-13

- Initial release: lossless localization workbench for Qt `.ts`, `.po`, JSON i18n, `.arb`, `.xcstrings`, Android XML, `.resx` and XLIFF; 10-source waterfall translation pipeline; `.xlsx` import/export; format-aware diff preview and write-back.
