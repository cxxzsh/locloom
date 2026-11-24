# Changelog

## 0.1.7 - 2026-11-24

### Added

- **Folder-based Save Back & centralized backups**: Save Back now defaults to writing a complete, standalone copy into a sibling `*_updated/` folder (untouched files copied verbatim, originals untouched, and repeated saves reuse the same folder); switching to In Place or overwriting existing files archives timestamped backups cleanly under `.locloom_backup/<timestamp>/` instead of scattering `.bak` files.
- **Redesigned interactive User Guide**: replaced the raw embedded README dump with a spacious two-column native guide (`980 × 690`) featuring 5 structured chapters (Quick Start, Pipeline & Locales, Save & Formats, Shortcuts & Tips, FAQ & Paths), status badge cards, keyboard shortcut caps, and instant EN/ZH + light/dark switching.
- **Toggleable Reference locale (`★ Ref`)**: clicking the active `★ Ref` button clears reference lock so locales like `zh_CN` can participate in batch translation when they have gaps.
- **Expanded European locale metadata & engine codes**: added rich metadata, plural labels (Slovenian 4-form), and Google/Baidu engine mappings for 20 additional locales (`da_DK`, `el_GR`, `hr_HR`, `hu_HU`, `nb_NO`, `sk_SK`, `sl_SI`, `sv_SE`, `fi_FI`, `ro_RO`, `bg_BG`, etc.).

### Fixed

- **Locale preset selection**: project-scoped preset buttons (`Tier-1`, `CJK`, `Europe`, `SEA & MENA`, `All`) no longer inject non-project ghost locales; `All` dynamically includes discovered/custom locales; `Europe` includes Nordic/Eastern/Southern European locales; `Only Project Locales` excludes `SourceLocale` and non-project `IsRef`.
- **Write-back safety & concurrency**: snapshotted patches synchronously on the UI thread to eliminate data races, guarded against concurrent double-save, preserved Android single-file `values-<lang>` folders and template sources on reload, and pruned fallback-matched Excel catalog entries after write-back.
- **Retranslate row & cleared cells**: fixed `retranslateRowAtAsync` no-op when retranslating all locales, fixed clearing/retranslating Finished cells reverting in Fill mode via `ForceOverwrite`, fixed sidebar remove-locale slice panic, Excel import missing `rebuildPlan()`, and project-switch catalog leaks.
- **Validation, formats & providers**: fixed trailing punctuation null-rune (`\x00`) bug and Greek `;`/`;` false positives; fixed multi-parenthesis Excel header parsing (`pt_PT`/`pt_BR`/`en_US`/`zh_HK`); fixed Qt TS high-byte `<byte/>`, duplicate `<context>`, HTML entities, Android XML backslash escaping, JSON multi-namespace isolation, ARB/JSON nil-template guards, and `.xcstrings` plural insertion; preserved partial batch translation results on errors, scoped cache keys by `SourceLocale`, added Google Web 414 splitting/retry, OpenAI JSON retry/split, MyMemory per-item source locale, TS TM fingerprinting, and memoized cache inspector snapshots.

## 0.1.6 - 2026-11-24

### Added

- **Retranslate row (all enabled locales)**: new row-level force-retranslate button (🔁 in the editor, ⚡ in the context menu) re-runs every enabled locale through the waterfall, bypassing fill-mode skips and the cache. One click replaces "clear row → translate".
- **Refresh project**: new header button reloads the current project directory via `format.Load`, picking up externally edited `.ts` / `.po` / `.json` files, newly added locale files, or removed entries. Staged (unwritten) catalog edits are preserved.

### Changed

- **Cache bypass now fully disables the cache**: `BypassCache` previously skipped cache *reads* but still wrote new hits. It now also gates `cache.Put`, so checking "Disable local cache" produces zero reads and zero writes to `translation-cache.json`.

## 0.1.5 - 2026-11-24

### Fixed

- **Default "translate from" actually uses the file source**: the default branch fed every locale the reference locale's own translation (`row.RefText`), so an engine-dropped punctuation (e.g. `:` in `Patient Views:`) cascaded into all targets. Default now uses `row.Key.Source`; only locales pinned via "译自" cascade.
- **Cleared cells re-translate**: manually clearing a Finished cell left `Status=Finished` with empty `Effective`, so "Fill gaps" skipped it forever. Skip now keys on `Effective != ""` only.
- **Trailing punctuation warning**: new validation flags when a translation's final sentence punctuation diverges from the source (`?`/`？`, `!`/`！`, `.`/`。`, `;`/`；`, `,`/`，` count as equivalent ASCII↔CJK classes), surfacing dropped colons instead of silent propagation.

## 0.1.3 - 2026-11-24

### Added

- **Project-scoped locale list**: the sidebar Locales tab now shows only languages present in the loaded project (plus enabled-but-unfiled additions); searching still surfaces hidden presets so they can be re-enabled as a manual add, and Select All only affects the visible set.
- **Warnings as Excel comments**: every cell with validation warnings in an `.xlsx` export now carries the message as a hover comment (yellow fill unchanged).
- **Relevant-locale export columns**: Gaps / Pending Changes / Warnings exports can drop locale columns with no matching rows ("仅导出相关语种的列", on by default); the reference column is always kept.

### Packaging

- Arch Linux: per-arch `PKGBUILD` (amd64/arm64) ships in the release and the required resized icon PNGs are now included in the source archive.
- Self-update plumbing (`mygo.Updater`): signed update manifests, delta archives and `install.sh`/`install.ps1` helpers wired into the release pipeline.

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
