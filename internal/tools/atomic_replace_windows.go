//go:build windows

package tools

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

var (
	kernel32MoveFile = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW  = kernel32MoveFile.NewProc("MoveFileExW")
)

func commitFileAtomic(source, destination string, replaceExisting bool) (bool, error) {
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return false, err
	}
	destinationPtr, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return false, err
	}
	flags := uintptr(moveFileWriteThrough)
	if replaceExisting {
		flags |= moveFileReplaceExisting
	}
	result, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(sourcePtr)),
		uintptr(unsafe.Pointer(destinationPtr)),
		flags,
	)
	if result == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return false, callErr
		}
		return false, fmt.Errorf("MoveFileExW failed committing %s", destination)
	}
	return true, nil
}

// MOVEFILE_WRITE_THROUGH flushes the replacement on Windows. Opening and
// syncing a directory is not supported there.
func syncParentDirectory(string) error { return nil }
