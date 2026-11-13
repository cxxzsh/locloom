//go:build windows

package platform

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// ReadFileShared reads a file on Windows using FILE_SHARE_READ|WRITE|DELETE (so files open in Excel/Qt Linguist work).
func ReadFileShared(path string) ([]byte, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}

	handle, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateFile %q: %w", path, err)
	}

	f := os.NewFile(uintptr(handle), path)
	if f == nil {
		_ = syscall.CloseHandle(handle)
		return os.ReadFile(path)
	}
	defer f.Close()
	return io.ReadAll(f)
}
