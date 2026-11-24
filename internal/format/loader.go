package format

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"locloom/internal/domain"
	"locloom/internal/locale"
	"locloom/internal/platform"
	"locloom/internal/tsxml"
)

// BackupDirName is the hidden folder inside the save target directory that
// collects timestamped backup snapshots instead of scattering .bak files.
const BackupDirName = ".locloom_backup"

const (
	maxZipEntryBytes = 64 << 20  // 64 MiB per entry
	maxZipTotalBytes = 256 << 20 // 256 MiB cumulative decompressed zip limit
)

// SupportedExtensions returns all supported localization file extensions without leading dots
// for native file picker filters.
func SupportedExtensions() []string {
	canonical := []string{"ts", "po", "pot", "xcstrings", "arb", "json", "xml", "resx", "xlf", "xliff"}
	seen := make(map[string]bool, len(canonical)+4)
	out := make([]string, 0, len(canonical)+4)
	for _, ext := range canonical {
		seen[ext] = true
		out = append(out, ext)
	}
	for _, a := range Adapters() {
		for _, ext := range a.Extensions() {
			clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
			if clean != "" && !seen[clean] {
				seen[clean] = true
				out = append(out, clean)
			}
		}
	}
	if !seen["zip"] {
		out = append(out, "zip")
	}
	return out
}

// DetectAdapter returns the highest-scoring Adapter for relPath and raw bytes, or (nil, false).
func DetectAdapter(relPath string, raw []byte) (Adapter, bool) {
	var best Adapter
	bestScore := 0
	for _, a := range Adapters() {
		score := a.MatchPath(relPath, raw)
		if score > bestScore || (score == bestScore && score > 0 && best != nil && a.ID() < best.ID()) {
			best = a
			bestScore = score
		}
	}
	if best == nil || bestScore <= 0 {
		return nil, false
	}
	return best, true
}

// FormatLabel returns the localized human-readable format name.
func FormatLabel(id FormatID, lang string) string {
	if a, ok := GetAdapter(id); ok {
		return a.Name(lang)
	}
	return string(id)
}

// PreviewDiff delegates to the adapter's PreviewDiff, falling back to generic before/after if unknown.
func PreviewDiff(id FormatID, key domain.EntryKey, localeID, oldText, newText string, oldStatus domain.TransStatus, numerus bool) (beforeSnippet, afterSnippet string) {
	if a, ok := GetAdapter(id); ok {
		return a.PreviewDiff(key, localeID, oldText, newText, oldStatus, numerus)
	}
	return fmt.Sprintf("[%s] %s: %s", localeID, key.Source, oldText),
		fmt.Sprintf("[%s] %s: %s", localeID, key.Source, newText)
}

// FromTSBundle wraps a legacy *tsxml.Bundle into a *format.Bundle (FormatQtTS).
func FromTSBundle(tb *tsxml.Bundle) *Bundle {
	if tb == nil {
		return nil
	}
	b := &Bundle{
		Format:     FormatQtTS,
		Topology:   TopologyPerLocale,
		SourcePath: tb.SourcePath,
		IsZip:      tb.IsZip,
		ZipExtras:  make(map[string][]byte, len(tb.ZipExtras)),
		TSBundle:   tb,
	}
	for k, v := range tb.ZipExtras {
		b.ZipExtras[k] = v
	}
	locSet := make(map[string]bool)
	for _, tf := range tb.Files {
		if tf == nil {
			continue
		}
		doc := docFromTSFile(tf)
		b.Docs = append(b.Docs, doc)
		if doc.LocaleID != "" {
			locSet[doc.LocaleID] = true
		}
		if b.SourceLocale == "" && doc.SourceLang != "" {
			b.SourceLocale = doc.SourceLang
		}
	}
	b.Locales = sortedMapKeys(locSet)
	return b
}

type rawCandidate struct {
	fullPath string
	relPath  string
	raw      []byte
	adapter  Adapter
	score    int
}

// Load auto-detects and loads a single localization file, sibling source template if needed,
// a directory, or a .zip archive.
func Load(path string, filePattern string, known []locale.LocaleSpec) (*Bundle, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if info.IsDir() {
		return loadDir(path, filePattern, known)
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return loadZip(path, filePattern, known)
	}
	return loadSingleFile(path, filePattern, known)
}

func loadSingleFile(path string, filePattern string, known []locale.LocaleSpec) (*Bundle, error) {
	raw, err := platform.ReadFileShared(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	base := filepath.Base(path)
	// Use parent/base as relPath when parent is Android values* folder so locale detection works.
	relPath := base
	parentBase := filepath.Base(filepath.Dir(path))
	if strings.HasPrefix(strings.ToLower(parentBase), "values") {
		relPath = parentBase + "/" + base
	}
	adapter, ok := DetectAdapter(relPath, raw)
	if !ok {
		return nil, fmt.Errorf("unsupported localization format: %q", path)
	}
	doc, err := adapter.ParseDoc(relPath, raw, filePattern, known)
	if err != nil {
		return nil, err
	}
	doc.FilePath = path

	bundle := &Bundle{
		Format:       adapter.ID(),
		Topology:     adapter.Topology(),
		SourcePath:   path,
		IsSingleFile: true,
		SourceLocale: doc.SourceLang,
		Docs:         []*DocRecord{doc},
		ZipExtras:    make(map[string][]byte),
	}

	// For ID-as-Key per-locale formats, scan sibling files for a source template to populate Key.Source.
	if adapter.IDAsKey() && adapter.Topology() == TopologyPerLocale && !doc.IsTemplate {
		if tmplDoc := findSiblingTemplate(path, adapter, filePattern, known); tmplDoc != nil {
			backfillFromTemplate(bundle, tmplDoc)
		}
	}

	finalizeBundle(bundle, adapter)
	return bundle, nil
}

func findSiblingTemplate(targetPath string, adapter Adapter, filePattern string, known []locale.LocaleSpec) *DocRecord {
	dir := filepath.Dir(targetPath)
	var candidatePaths []string

	// Android res/values-<loc>/strings.xml -> check ../values/strings.xml
	if adapter.ID() == FormatAndroidXML {
		parentDir := filepath.Dir(dir)
		base := filepath.Base(targetPath)
		candidatePaths = append(candidatePaths, filepath.Join(parentDir, "values", base))
	}

	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			full := filepath.Join(dir, e.Name())
			if full == targetPath {
				continue
			}
			candidatePaths = append(candidatePaths, full)
		}
	}

	var parsed []*DocRecord
	for _, p := range candidatePaths {
		raw, err := platform.ReadFileShared(p)
		if err != nil {
			continue
		}
		rel := filepath.Base(p)
		if adapter.ID() == FormatAndroidXML {
			rel = filepath.Base(filepath.Dir(p)) + "/" + rel
		}
		if adapter.MatchPath(rel, raw) <= 0 {
			continue
		}
		d, err := adapter.ParseDoc(rel, raw, filePattern, known)
		if err != nil {
			continue
		}
		parsed = append(parsed, d)
	}
	return pickSourceDoc(parsed, "")
}

func loadDir(dirPath string, filePattern string, known []locale.LocaleSpec) (*Bundle, error) {
	var all []rawCandidate
	err := filepath.WalkDir(dirPath, func(fullPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == BackupDirName {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dirPath, fullPath)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		raw, err := platform.ReadFileShared(fullPath)
		if err != nil {
			return fmt.Errorf("read %q: %w", fullPath, err)
		}
		if adapter, ok := DetectAdapter(relSlash, raw); ok {
			all = append(all, rawCandidate{
				fullPath: fullPath,
				relPath:  relSlash,
				raw:      raw,
				adapter:  adapter,
				score:    adapter.MatchPath(relSlash, raw),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return &Bundle{
			SourcePath: dirPath,
			ZipExtras:  make(map[string][]byte),
		}, nil
	}

	winner, matched := selectWinningFormat(all)
	bundle := &Bundle{
		Format:     winner.ID(),
		Topology:   winner.Topology(),
		SourcePath: dirPath,
		ZipExtras:  make(map[string][]byte),
	}

	for _, c := range matched {
		doc, err := winner.ParseDoc(c.relPath, c.raw, filePattern, known)
		if err != nil {
			return nil, err
		}
		doc.FilePath = c.fullPath
		bundle.Docs = append(bundle.Docs, doc)
	}

	finalizeBundle(bundle, winner)
	return bundle, nil
}

func loadZip(zipPath string, filePattern string, known []locale.LocaleSpec) (*Bundle, error) {
	zipRaw, err := platform.ReadFileShared(zipPath)
	if err != nil {
		return nil, fmt.Errorf("read zip %q: %w", zipPath, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipRaw), int64(len(zipRaw)))
	if err != nil {
		return nil, fmt.Errorf("open zip %q: %w", zipPath, err)
	}

	extras := make(map[string][]byte)
	var all []rawCandidate
	var totalRead int64

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		relSlash, cleanErr := sanitizeRelPath(zf.Name)
		if cleanErr != nil {
			return nil, fmt.Errorf("unsafe zip entry %q: %w", zf.Name, cleanErr)
		}
		if zf.UncompressedSize64 > maxZipEntryBytes {
			return nil, fmt.Errorf("zip entry %q exceeds size limit (%d bytes)", relSlash, maxZipEntryBytes)
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %q: %w", relSlash, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, maxZipEntryBytes+1))
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read zip entry %q: %w", relSlash, readErr)
		}
		if int64(len(data)) > maxZipEntryBytes {
			return nil, fmt.Errorf("zip entry %q exceeds max entry size (%d bytes)", relSlash, maxZipEntryBytes)
		}
		totalRead += int64(len(data))
		if totalRead > maxZipTotalBytes {
			return nil, fmt.Errorf("zip archive decompressed size exceeds limit (%d bytes)", maxZipTotalBytes)
		}

		if adapter, ok := DetectAdapter(relSlash, data); ok {
			all = append(all, rawCandidate{
				fullPath: relSlash,
				relPath:  relSlash,
				raw:      data,
				adapter:  adapter,
				score:    adapter.MatchPath(relSlash, data),
			})
		} else {
			extras[relSlash] = data
		}
	}

	if len(all) == 0 {
		return &Bundle{
			SourcePath: zipPath,
			IsZip:      true,
			ZipExtras:  extras,
		}, nil
	}

	winner, matched := selectWinningFormat(all)
	// Preserve non-winning candidates in ZipExtras so zip round-trip is lossless.
	for _, c := range all {
		if c.adapter.ID() != winner.ID() {
			extras[c.relPath] = c.raw
		}
	}

	bundle := &Bundle{
		Format:     winner.ID(),
		Topology:   winner.Topology(),
		SourcePath: zipPath,
		IsZip:      true,
		ZipExtras:  extras,
	}
	for _, c := range matched {
		doc, err := winner.ParseDoc(c.relPath, c.raw, filePattern, known)
		if err != nil {
			return nil, err
		}
		doc.FilePath = c.relPath
		bundle.Docs = append(bundle.Docs, doc)
	}

	finalizeBundle(bundle, winner)
	return bundle, nil
}

// LoadFiles loads an explicit slice of file paths into a unified Bundle.
func LoadFiles(paths []string, filePattern string, known []locale.LocaleSpec) (*Bundle, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no input files provided")
	}
	if len(paths) == 1 {
		return Load(paths[0], filePattern, known)
	}

	commonDir := filepath.Dir(paths[0])
	var all []rawCandidate
	for _, p := range paths {
		raw, err := platform.ReadFileShared(p)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", p, err)
		}
		rel := filepath.Base(p)
		if r, err := filepath.Rel(commonDir, p); err == nil && !strings.HasPrefix(r, "..") {
			rel = filepath.ToSlash(r)
		} else if parentBase := filepath.Base(filepath.Dir(p)); strings.HasPrefix(strings.ToLower(parentBase), "values") {
			rel = parentBase + "/" + rel
		}
		if adapter, ok := DetectAdapter(rel, raw); ok {
			all = append(all, rawCandidate{
				fullPath: p,
				relPath:  rel,
				raw:      raw,
				adapter:  adapter,
				score:    adapter.MatchPath(rel, raw),
			})
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no supported localization files found")
	}

	winner, matched := selectWinningFormat(all)
	bundle := &Bundle{
		Format:     winner.ID(),
		Topology:   winner.Topology(),
		SourcePath: commonDir,
		ZipExtras:  make(map[string][]byte),
	}
	for _, c := range matched {
		doc, err := winner.ParseDoc(c.relPath, c.raw, filePattern, known)
		if err != nil {
			return nil, err
		}
		doc.FilePath = c.fullPath
		bundle.Docs = append(bundle.Docs, doc)
	}

	finalizeBundle(bundle, winner)
	return bundle, nil
}

func selectWinningFormat(candidates []rawCandidate) (Adapter, []rawCandidate) {
	type stats struct {
		adapter Adapter
		count   int
		score   int
		items   []rawCandidate
	}
	byID := make(map[FormatID]*stats)
	for _, c := range candidates {
		id := c.adapter.ID()
		st, ok := byID[id]
		if !ok {
			st = &stats{adapter: c.adapter}
			byID[id] = st
		}
		st.count++
		st.score += c.score
		st.items = append(st.items, c)
	}

	var best *stats
	for _, st := range byID {
		if best == nil ||
			st.count > best.count ||
			(st.count == best.count && st.score > best.score) ||
			(st.count == best.count && st.score == best.score && st.adapter.ID() < best.adapter.ID()) {
			best = st
		}
	}
	return best.adapter, best.items
}

func finalizeBundle(bundle *Bundle, adapter Adapter) {
	sort.Slice(bundle.Docs, func(i, j int) bool {
		return bundle.Docs[i].RelPath < bundle.Docs[j].RelPath
	})

	if bundle.SourceLocale == "" {
		for _, d := range bundle.Docs {
			if d.SourceLang != "" {
				bundle.SourceLocale = d.SourceLang
				break
			}
		}
	}

	if adapter != nil && adapter.IDAsKey() {
		if srcDoc := pickSourceDoc(bundle.Docs, bundle.SourceLocale); srcDoc != nil {
			backfillFromTemplate(bundle, srcDoc)
		} else {
			// Ensure Key.Source is at least Key.ID if no source template exists.
			for _, d := range bundle.Docs {
				for i := range d.Entries {
					if d.Entries[i].Key.Source == "" && d.Entries[i].Key.ID != "" {
						d.Entries[i].Key.Source = d.Entries[i].Key.ID
					}
				}
			}
		}
	}

	locSet := make(map[string]bool)
	for _, d := range bundle.Docs {
		if d.LocaleID != "" {
			locSet[d.LocaleID] = true
		}
		for _, l := range d.Locales {
			if l != "" {
				locSet[l] = true
			}
		}
	}
	bundle.Locales = sortedMapKeys(locSet)

	if bundle.Format == FormatQtTS {
		tb := &tsxml.Bundle{
			SourcePath: bundle.SourcePath,
			IsZip:      bundle.IsZip,
			ZipExtras:  bundle.ZipExtras,
		}
		for _, d := range bundle.Docs {
			if tf, ok := d.Opaque.(*tsxml.TSFile); ok && tf != nil {
				tb.Files = append(tb.Files, tf)
			}
		}
		bundle.TSBundle = tb
	}
}

func pickSourceDoc(docs []*DocRecord, sourceLocale string) *DocRecord {
	for _, d := range docs {
		if d.IsTemplate {
			return d
		}
	}
	if sourceLocale != "" {
		normSrc := normalizeLocaleKey(sourceLocale)
		for _, d := range docs {
			if normalizeLocaleKey(d.LocaleID) == normSrc {
				return d
			}
		}
	}
	for _, target := range []string{"en", "en_us", "en_gb", "zh_cn"} {
		for _, d := range docs {
			if normalizeLocaleKey(d.LocaleID) == target {
				return d
			}
		}
	}
	for _, d := range docs {
		if strings.HasPrefix(normalizeLocaleKey(d.LocaleID), "en_") {
			return d
		}
	}
	return nil
}

func backfillFromTemplate(bundle *Bundle, srcDoc *DocRecord) {
	if srcDoc == nil {
		return
	}
	if bundle.SourceLocale == "" && srcDoc.LocaleID != "" {
		bundle.SourceLocale = srcDoc.LocaleID
	}
	sourceTextByID := make(map[string]string, len(srcDoc.Entries))
	commentByID := make(map[string]string, len(srcDoc.Entries))
	for _, e := range srcDoc.Entries {
		if e.Key.ID == "" {
			continue
		}
		txt := e.Text
		if txt == "" {
			txt = e.Key.Source
		}
		if txt != "" {
			sourceTextByID[e.Key.ID] = txt
		}
		if e.Key.Comment != "" {
			commentByID[e.Key.ID] = e.Key.Comment
		}
	}

	for _, d := range bundle.Docs {
		for i := range d.Entries {
			e := &d.Entries[i]
			if e.Key.ID == "" {
				continue
			}
			if src, ok := sourceTextByID[e.Key.ID]; ok && src != "" {
				e.Key.Source = src
			} else if e.Key.Source == "" {
				e.Key.Source = e.Key.ID
			}
			if e.Key.Comment == "" {
				if cmt, ok := commentByID[e.Key.ID]; ok && cmt != "" {
					e.Key.Comment = cmt
				}
			}
		}
	}
}

// Save applies per-locale PatchOps across bundle.Docs, scaffolds missing enabled target locale docs
// when mutating patches exist for a locale not yet on disk, and writes atomically.
func Save(bundle *Bundle, patchesByLocale map[string][]PatchOp, locales []locale.LocaleSpec, filePattern string, opts SaveOptions) (*SaveReport, error) {
	if bundle == nil {
		return nil, fmt.Errorf("nil Bundle")
	}
	adapter, ok := GetAdapter(bundle.Format)
	if !ok {
		return nil, fmt.Errorf("no adapter registered for format %q", bundle.Format)
	}

	patched := make(map[string][]byte)

	if adapter.Topology() == TopologyMultiLocale {
		locKeys := make([]string, 0, len(patchesByLocale))
		for k := range patchesByLocale {
			locKeys = append(locKeys, k)
		}
		sort.Strings(locKeys)
		var merged []PatchOp
		for _, k := range locKeys {
			for _, op := range patchesByLocale[k] {
				if op.Action.Mutates() {
					if op.LocaleID == "" {
						op.LocaleID = k
					}
					merged = append(merged, op)
				}
			}
		}
		if len(merged) > 0 {
			for _, doc := range bundle.Docs {
				outRaw, applied, appended, err := adapter.ApplyPatches(doc, merged)
				if err != nil {
					return nil, err
				}
				if applied > 0 || appended > 0 {
					patched[doc.RelPath] = outRaw
				}
			}
		}
	} else {
		docsByNormLoc := make(map[string]*DocRecord, len(bundle.Docs))
		for _, doc := range bundle.Docs {
			if doc.LocaleID != "" {
				docsByNormLoc[normalizeLocaleKey(doc.LocaleID)] = doc
			}
		}

		for _, doc := range bundle.Docs {
			ops := lookupLocaleOps(patchesByLocale, doc.LocaleID)
			if !hasMutatingOp(ops) {
				continue
			}
			outRaw, applied, appended, err := adapter.ApplyPatches(doc, ops)
			if err != nil {
				return nil, err
			}
			if applied > 0 || appended > 0 {
				patched[doc.RelPath] = outRaw
			}
		}

		// Scaffold missing enabled target locale docs when patches exist.
		if !bundle.IsSingleFile {
			specByNorm := make(map[string]locale.LocaleSpec, len(locales))
			for _, l := range locales {
				specByNorm[normalizeLocaleKey(l.ID)] = l
			}
			templateDoc := pickTemplateForScaffold(bundle)
			locKeys := make([]string, 0, len(patchesByLocale))
			for k := range patchesByLocale {
				locKeys = append(locKeys, k)
			}
			sort.Strings(locKeys)

			for _, locID := range locKeys {
				norm := normalizeLocaleKey(locID)
				if _, exists := docsByNormLoc[norm]; exists {
					continue
				}
				ops := patchesByLocale[locID]
				if !hasMutatingOp(ops) {
					continue
				}
				spec, ok := specByNorm[norm]
				if !ok {
					spec = locale.LocaleSpec{ID: locID, Name: locID, Enabled: true}
				}
				targetRel := scaffoldRelPath(bundle.Format, templateDoc, spec, filePattern)
				newDoc, err := adapter.NewLocaleDoc(templateDoc, spec, targetRel)
				if err != nil {
					return nil, fmt.Errorf("scaffold locale %q: %w", locID, err)
				}
				outRaw, applied, appended, err := adapter.ApplyPatches(newDoc, ops)
				if err != nil {
					return nil, err
				}
				if applied > 0 || appended > 0 {
					patched[newDoc.RelPath] = outRaw
				}
			}
		}
	}

	return writeBundleOutput(bundle, patched, opts)
}

// DefaultOutputPath returns the default non-destructive output location for bundle:
//   - .zip archive  -> <stem>_updated.zip (reused if already ending in _updated.zip)
//   - single file   -> <dir>/<stem>_updated/<filename> (reused if parent already ends in _updated)
//   - directory     -> <dir>_updated (reused if already ending in _updated)
func DefaultOutputPath(bundle *Bundle) string {
	if bundle == nil {
		return ""
	}
	src := strings.TrimSpace(bundle.SourcePath)
	if src == "" {
		return ""
	}
	cleanSrc := filepath.Clean(src)
	if bundle.IsZip {
		ext := filepath.Ext(cleanSrc)
		stem := strings.TrimSuffix(cleanSrc, ext)
		if strings.HasSuffix(stem, "_updated") {
			return cleanSrc
		}
		return stem + "_updated.zip"
	}
	if bundle.IsSingleFile {
		dir := filepath.Dir(cleanSrc)
		base := filepath.Base(cleanSrc)
		parentBase := filepath.Base(dir)
		if bundle.Format == FormatAndroidXML && strings.HasPrefix(strings.ToLower(parentBase), "values") {
			grandParent := filepath.Dir(dir)
			if strings.HasSuffix(filepath.Base(grandParent), "_updated") {
				return cleanSrc
			}
			return filepath.Join(grandParent, parentBase+"_updated", parentBase, base)
		}
		if strings.HasSuffix(parentBase, "_updated") {
			return cleanSrc
		}
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		if stem == "" {
			stem = base
		}
		return filepath.Join(dir, stem+"_updated", base)
	}
	if strings.HasSuffix(filepath.Base(cleanSrc), "_updated") {
		return cleanSrc
	}
	return cleanSrc + "_updated"
}

func writeBundleOutput(bundle *Bundle, patched map[string][]byte, opts SaveOptions) (*SaveReport, error) {
	report := &SaveReport{}

	if bundle.IsZip {
		outZip := strings.TrimSpace(opts.OutputPath)
		if outZip == "" {
			outZip = DefaultOutputPath(bundle)
		}
		report.OutputPath = outZip

		var zipBuf bytes.Buffer
		zw := zip.NewWriter(&zipBuf)
		written := make(map[string]bool, len(bundle.Docs))

		for _, doc := range bundle.Docs {
			data, isPatched := patched[doc.RelPath]
			if isPatched {
				report.FilesWritten++
			} else {
				data = doc.Raw
				if doc.HasBOM && !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
					data = append([]byte{0xEF, 0xBB, 0xBF}, data...)
				}
			}
			w, err := zw.Create(doc.RelPath)
			if err != nil {
				_ = zw.Close()
				return nil, fmt.Errorf("create zip entry %q: %w", doc.RelPath, err)
			}
			if _, err := w.Write(data); err != nil {
				_ = zw.Close()
				return nil, fmt.Errorf("write zip entry %q: %w", doc.RelPath, err)
			}
			written[doc.RelPath] = true
		}

		extraPatched := make([]string, 0)
		for relPath := range patched {
			if !written[relPath] {
				extraPatched = append(extraPatched, relPath)
			}
		}
		sort.Strings(extraPatched)
		for _, relPath := range extraPatched {
			w, err := zw.Create(relPath)
			if err != nil {
				_ = zw.Close()
				return nil, fmt.Errorf("create zip entry %q: %w", relPath, err)
			}
			if _, err := w.Write(patched[relPath]); err != nil {
				_ = zw.Close()
				return nil, fmt.Errorf("write zip entry %q: %w", relPath, err)
			}
			report.FilesWritten++
		}

		extraKeys := make([]string, 0, len(bundle.ZipExtras))
		for k := range bundle.ZipExtras {
			extraKeys = append(extraKeys, k)
		}
		sort.Strings(extraKeys)
		for _, k := range extraKeys {
			w, err := zw.Create(k)
			if err != nil {
				_ = zw.Close()
				return nil, fmt.Errorf("create zip extra %q: %w", k, err)
			}
			if _, err := w.Write(bundle.ZipExtras[k]); err != nil {
				_ = zw.Close()
				return nil, fmt.Errorf("write zip extra %q: %w", k, err)
			}
		}

		if err := zw.Close(); err != nil {
			return nil, fmt.Errorf("close zip writer: %w", err)
		}
		if err := platform.AtomicWriteFile(outZip, zipBuf.Bytes(), 0o644); err != nil {
			return nil, fmt.Errorf("write zip %q: %w", outZip, err)
		}
		return report, nil
	}

	if bundle.IsSingleFile {
		outPath := strings.TrimSpace(opts.OutputPath)
		if outPath == "" {
			outPath = bundle.SourcePath
		} else if info, err := os.Stat(outPath); (err == nil && info.IsDir()) || (err != nil && filepath.Ext(outPath) == "") {
			outPath = filepath.Join(outPath, filepath.Base(bundle.SourcePath))
		}
		report.OutputPath = outPath
		backupRoot := newBackupSessionDir(filepath.Dir(outPath))
		for _, data := range patched {
			if opts.CreateBackup {
				if bakPath, err := writeBackupToFolder(backupRoot, filepath.Base(outPath), outPath); err != nil {
					return nil, err
				} else if bakPath != "" {
					report.BackupDir = backupRoot
					report.BackupPaths = append(report.BackupPaths, bakPath)
				}
			}
			if err := platform.AtomicWriteFile(outPath, data, 0o644); err != nil {
				return nil, fmt.Errorf("write %q: %w", outPath, err)
			}
			report.FilesWritten++
			break
		}
		return report, nil
	}

	outDir := strings.TrimSpace(opts.OutputPath)
	if outDir == "" {
		outDir = bundle.SourcePath
	}
	cleanOutDir := filepath.Clean(outDir)
	cleanSrcDir := filepath.Clean(bundle.SourcePath)
	report.OutputPath = cleanOutDir
	backupRoot := newBackupSessionDir(cleanOutDir)

	// When exporting into a separate output directory, copy untouched docs verbatim so the
	// output folder is a complete, self-contained localization bundle ready to use or reload.
	if cleanOutDir != cleanSrcDir {
		for _, doc := range bundle.Docs {
			if doc == nil {
				continue
			}
			if _, isPatched := patched[doc.RelPath]; isPatched {
				continue
			}
			cleanRel, relErr := sanitizeRelPath(doc.RelPath)
			if relErr != nil {
				return nil, fmt.Errorf("unsafe output relative path %q: %w", doc.RelPath, relErr)
			}
			targetPath := filepath.Clean(filepath.Join(cleanOutDir, filepath.FromSlash(cleanRel)))
			relCheck, checkErr := filepath.Rel(cleanOutDir, targetPath)
			if checkErr != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("output path %q escapes target directory %q", doc.RelPath, cleanOutDir)
			}
			data := doc.Raw
			if doc.HasBOM && !bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
				data = append([]byte{0xEF, 0xBB, 0xBF}, data...)
			}
			if existing, readErr := platform.ReadFileShared(targetPath); readErr == nil {
				if bytes.Equal(existing, data) {
					continue
				}
				if opts.CreateBackup {
					if bakPath, err := writeBackupToFolder(backupRoot, cleanRel, targetPath); err != nil {
						return nil, err
					} else if bakPath != "" {
						report.BackupDir = backupRoot
						report.BackupPaths = append(report.BackupPaths, bakPath)
					}
				}
			}
			if err := platform.AtomicWriteFile(targetPath, data, 0o644); err != nil {
				return nil, fmt.Errorf("copy untouched %q: %w", targetPath, err)
			}
		}
	}

	relKeys := make([]string, 0, len(patched))
	for k := range patched {
		relKeys = append(relKeys, k)
	}
	sort.Strings(relKeys)

	for _, relPath := range relKeys {
		cleanRel, relErr := sanitizeRelPath(relPath)
		if relErr != nil {
			return nil, fmt.Errorf("unsafe output relative path %q: %w", relPath, relErr)
		}
		targetPath := filepath.Join(cleanOutDir, filepath.FromSlash(cleanRel))
		cleanTarget := filepath.Clean(targetPath)
		relCheck, checkErr := filepath.Rel(cleanOutDir, cleanTarget)
		if checkErr != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("output path %q escapes target directory %q", relPath, cleanOutDir)
		}
		if opts.CreateBackup {
			if bakPath, err := writeBackupToFolder(backupRoot, cleanRel, cleanTarget); err != nil {
				return nil, err
			} else if bakPath != "" {
				report.BackupDir = backupRoot
				report.BackupPaths = append(report.BackupPaths, bakPath)
			}
		}
		if err := platform.AtomicWriteFile(cleanTarget, patched[relPath], 0o644); err != nil {
			return nil, fmt.Errorf("write %q: %w", cleanTarget, err)
		}
		report.FilesWritten++
	}
	return report, nil
}

func sanitizeRelPath(rawName string) (string, error) {
	s := strings.ReplaceAll(strings.TrimSpace(rawName), "\\", "/")
	if strings.ContainsRune(s, 0) {
		return "", fmt.Errorf("contains NUL byte")
	}
	s = strings.TrimLeft(s, "/")
	for strings.HasPrefix(s, "./") {
		s = strings.TrimPrefix(s, "./")
	}
	if s == "" || s == "." {
		return "", fmt.Errorf("empty relative path")
	}
	if len(s) >= 2 && s[1] == ':' && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) {
		return "", fmt.Errorf("drive-qualified path not allowed")
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "", fmt.Errorf("path traversal segment '..' not allowed")
		}
	}
	cleaned := path.Clean(s)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("invalid normalized relative path %q", cleaned)
	}
	return cleaned, nil
}

// PickTemplateForScaffold picks the best source or fallback template document for scaffolding a missing locale.
func PickTemplateForScaffold(bundle *Bundle) *DocRecord {
	if bundle == nil {
		return nil
	}
	return pickTemplateForScaffold(bundle)
}

// ScaffoldRelPath computes the format-appropriate relative file path for scaffolding target spec.
func ScaffoldRelPath(fid FormatID, tmpl *DocRecord, spec locale.LocaleSpec, filePattern string) string {
	return scaffoldRelPath(fid, tmpl, spec, filePattern)
}

func newBackupSessionDir(baseDir string) string {
	stamp := time.Now().Format("20060102_150405")
	cand := filepath.Join(baseDir, BackupDirName, stamp)
	if _, err := os.Stat(cand); err != nil {
		return cand
	}
	for i := 1; i < 1000; i++ {
		next := filepath.Join(baseDir, BackupDirName, fmt.Sprintf("%s_%d", stamp, i))
		if _, err := os.Stat(next); err != nil {
			return next
		}
	}
	return filepath.Join(baseDir, BackupDirName, fmt.Sprintf("%s_%d", stamp, time.Now().UnixNano()))
}

func writeBackupToFolder(backupRoot, relSlash, targetPath string) (string, error) {
	if _, err := os.Stat(targetPath); err != nil {
		return "", nil
	}
	orig, err := platform.ReadFileShared(targetPath)
	if err != nil {
		return "", nil
	}
	cleanRel, err := sanitizeRelPath(relSlash)
	if err != nil {
		cleanRel = filepath.Base(targetPath)
	}
	bakPath := filepath.Join(backupRoot, filepath.FromSlash(cleanRel))
	if err := platform.AtomicWriteFile(bakPath, orig, 0o644); err != nil {
		return "", fmt.Errorf("write backup %q: %w", bakPath, err)
	}
	return bakPath, nil
}

func pickTemplateForScaffold(bundle *Bundle) *DocRecord {
	if src := pickSourceDoc(bundle.Docs, bundle.SourceLocale); src != nil {
		return src
	}
	if len(bundle.Docs) > 0 {
		return bundle.Docs[0]
	}
	return nil
}

func scaffoldRelPath(fid FormatID, tmpl *DocRecord, spec locale.LocaleSpec, filePattern string) string {
	dir := ""
	base := ""
	if tmpl != nil {
		d := filepath.ToSlash(filepath.Dir(tmpl.RelPath))
		if d != "." && d != "" {
			dir = d
		}
		base = tmpl.FileName
	}

	switch fid {
	case FormatAndroidXML:
		parent := ""
		if dir != "" {
			parent = filepath.ToSlash(filepath.Dir(dir))
			if parent == "." {
				parent = ""
			}
		}
		qual := androidValuesFolder(spec.ID)
		fileName := "strings.xml"
		if base != "" {
			fileName = base
		}
		if parent != "" {
			return parent + "/" + qual + "/" + fileName
		}
		return qual + "/" + fileName

	case FormatResX:
		stem := "Resources"
		if base != "" {
			ext := filepath.Ext(base)
			noExt := strings.TrimSuffix(base, ext)
			if idx := strings.IndexByte(noExt, '.'); idx > 0 {
				stem = noExt[:idx]
			} else if noExt != "" {
				stem = noExt
			}
		}
		bcp := strings.ReplaceAll(spec.ID, "_", "-")
		name := stem + "." + bcp + ".resx"
		if dir != "" {
			return dir + "/" + name
		}
		return name

	case FormatQtTS:
		name := spec.FileName(filePattern)
		if dir != "" {
			return dir + "/" + name
		}
		return name

	default:
		ext := ".txt"
		switch fid {
		case FormatGettextPO:
			ext = ".po"
		case FormatJSON:
			ext = ".json"
		case FormatARB:
			ext = ".arb"
		case FormatXLIFF:
			ext = ".xlf"
		}
		if base != "" {
			if e := filepath.Ext(base); e != "" && !(fid == FormatGettextPO && strings.EqualFold(e, ".pot")) {
				ext = e
			}
		}
		name := spec.ID + ext
		if strings.Contains(filePattern, "{id}") {
			patBase := filepath.Base(strings.ReplaceAll(filePattern, "\\", "/"))
			patExt := filepath.Ext(patBase)
			patStem := strings.TrimSuffix(patBase, patExt)
			name = strings.ReplaceAll(patStem, "{id}", spec.ID) + ext
		}
		if dir != "" {
			return dir + "/" + name
		}
		return name
	}
}

func androidValuesFolder(localeID string) string {
	norm := strings.ReplaceAll(strings.TrimSpace(localeID), "-", "_")
	parts := strings.Split(norm, "_")
	if len(parts) == 2 && len(parts[0]) >= 2 && len(parts[1]) == 2 {
		return "values-" + strings.ToLower(parts[0]) + "-r" + strings.ToUpper(parts[1])
	}
	return "values-" + strings.ToLower(norm)
}

func lookupLocaleOps(patchesByLocale map[string][]PatchOp, locID string) []PatchOp {
	if ops, ok := patchesByLocale[locID]; ok {
		return ops
	}
	norm := normalizeLocaleKey(locID)
	for k, ops := range patchesByLocale {
		if normalizeLocaleKey(k) == norm {
			return ops
		}
	}
	return nil
}

func hasMutatingOp(ops []PatchOp) bool {
	for _, op := range ops {
		if op.Action.Mutates() {
			return true
		}
	}
	return false
}

func normalizeLocaleKey(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", "_"))
}

func sortedMapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- Shared internal helpers for adapters in internal/format ---

func decodeUTF8WithBOM(raw []byte) (utf8Bytes []byte, hasBOM bool) {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return raw[3:], true
	}
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		data := raw[2:]
		u16 := make([]uint16, 0, len(data)/2)
		for i := 0; i+1 < len(data); i += 2 {
			u16 = append(u16, binary.LittleEndian.Uint16(data[i:i+2]))
		}
		runes := utf16.Decode(u16)
		var buf bytes.Buffer
		buf.Grow(len(runes))
		for _, r := range runes {
			buf.WriteRune(r)
		}
		return buf.Bytes(), false
	}
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		data := raw[2:]
		u16 := make([]uint16, 0, len(data)/2)
		for i := 0; i+1 < len(data); i += 2 {
			u16 = append(u16, binary.BigEndian.Uint16(data[i:i+2]))
		}
		runes := utf16.Decode(u16)
		var buf bytes.Buffer
		buf.Grow(len(runes))
		for _, r := range runes {
			buf.WriteRune(r)
		}
		return buf.Bytes(), false
	}
	return raw, false
}

func detectLineEnding(data []byte) string {
	if bytes.Contains(data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func applyLineEnding(s, le string) string {
	if le == "" {
		return s
	}
	norm := strings.ReplaceAll(s, "\r\n", "\n")
	if le == "\r\n" {
		return strings.ReplaceAll(norm, "\n", "\r\n")
	}
	return norm
}

func stemLocaleWithExt(relPath, filePattern string, known []locale.LocaleSpec) string {
	slash := strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/")
	ext := filepath.Ext(slash)
	fakeTS := strings.TrimSuffix(slash, ext) + ".ts"
	pat := strings.TrimSpace(filePattern)
	if pat != "" {
		patExt := filepath.Ext(pat)
		pat = strings.TrimSuffix(pat, patExt) + ".ts"
	}
	return locale.StemLocaleID(fakeTS, pat, known)
}

type byteSpanEdit struct {
	start int
	end   int
	order int
	data  []byte
}

func applyByteSpanEdits(raw []byte, edits []byteSpanEdit, hasBOM bool) []byte {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		if edits[i].end != edits[j].end {
			return edits[i].end < edits[j].end
		}
		return edits[i].order < edits[j].order
	})

	var out bytes.Buffer
	out.Grow(len(raw) + 256)
	if hasBOM {
		out.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	cursor := 0
	for _, ed := range edits {
		if ed.start < cursor || ed.start < 0 || ed.end > len(raw) || ed.end < ed.start {
			continue
		}
		out.Write(raw[cursor:ed.start])
		out.Write(ed.data)
		cursor = ed.end
	}
	out.Write(raw[cursor:])
	return out.Bytes()
}

func escapeXMLText(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeXMLAttr(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func leadingIndentAt(raw []byte, pos int) string {
	if pos <= 0 || pos > len(raw) {
		return ""
	}
	lineStart := bytes.LastIndexByte(raw[:pos], '\n') + 1
	seg := raw[lineStart:pos]
	for _, b := range seg {
		if b != ' ' && b != '\t' {
			return ""
		}
	}
	return string(seg)
}
