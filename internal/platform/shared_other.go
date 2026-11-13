//go:build !windows

package platform

import "os"

// ReadFileShared reads a file using os.ReadFile on non-Windows platforms.
func ReadFileShared(path string) ([]byte, error) {
	return os.ReadFile(path)
}
