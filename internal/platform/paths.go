package platform

import (
	"os"
	"path/filepath"
)

const AppDirName = "locloom"

func portablePath(filename string) (string, bool) {
	if filename == "" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "", false
	}
	candidate := filepath.Join(filepath.Dir(exe), filename)
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() {
		return candidate, true
	}
	return "", false
}

// ResolveConfigPath checks if filename already exists next to os.Executable() (portable mode);
// otherwise returns filepath.Join(os.UserConfigDir(), AppDirName, filename), falling back to filename.
func ResolveConfigPath(filename string) string {
	if p, ok := portablePath(filename); ok {
		return p
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, AppDirName, filename)
	}
	return filename
}

// ResolveCachePath checks if filename already exists next to os.Executable() (portable mode);
// otherwise returns filepath.Join(os.UserCacheDir(), AppDirName, filename), falling back to os.TempDir().
func ResolveCachePath(filename string) string {
	if p, ok := portablePath(filename); ok {
		return p
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, AppDirName, filename)
	}
	return filepath.Join(os.TempDir(), AppDirName, filename)
}
