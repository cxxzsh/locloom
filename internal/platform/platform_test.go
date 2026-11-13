package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveConfigAndCachePath(t *testing.T) {
	cfgPath := ResolveConfigPath("settings.json")
	if !strings.HasSuffix(cfgPath, filepath.Join(AppDirName, "settings.json")) && cfgPath != "settings.json" {
		t.Fatalf("unexpected config path: %q", cfgPath)
	}

	cachePath := ResolveCachePath("tm_cache.json")
	if !strings.HasSuffix(cachePath, filepath.Join(AppDirName, "tm_cache.json")) {
		t.Fatalf("unexpected cache path: %q", cachePath)
	}
}

func TestAtomicWriteFileAndReadFileShared(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "test.ts")

	first := []byte("<TS version=\"2.1\"></TS>\n")
	if err := AtomicWriteFile(target, first, 0o644); err != nil {
		t.Fatalf("first AtomicWriteFile failed: %v", err)
	}

	got, err := ReadFileShared(target)
	if err != nil {
		t.Fatalf("ReadFileShared failed: %v", err)
	}
	if string(got) != string(first) {
		t.Fatalf("got %q, want %q", got, first)
	}

	second := []byte("<TS version=\"2.1\" language=\"ja_JP\"></TS>\n")
	if err := AtomicWriteFile(target, second, 0o644); err != nil {
		t.Fatalf("overwrite AtomicWriteFile failed: %v", err)
	}

	got2, err := ReadFileShared(target)
	if err != nil {
		t.Fatalf("ReadFileShared after overwrite failed: %v", err)
	}
	if string(got2) != string(second) {
		t.Fatalf("got %q, want %q", got2, second)
	}

	// Ensure no leftover temp files remain in the target directory.
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "test.ts" {
		t.Fatalf("unexpected directory entries after atomic write: %v", entries)
	}
}
