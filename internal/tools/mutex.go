package tools

// Per-path write mutex, inspired by pi.dev's file-mutation-queue. Serialises
// concurrent str_replace / create_file / apply_diff calls that target the same
// real file so a burst of tool calls can never corrupt it mid-write.

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	fileLocksMu sync.Mutex
	fileLocks   = map[string]*sync.Mutex{}
)

// mutationKey canonicalizes existing paths through symlinks, matching pi.dev's
// realpath-based queue behavior. On Windows the filesystem is normally
// case-insensitive, so normalize case as well to keep aliases on one lock.
func mutationKey(absPath string) string {
	key := filepath.Clean(absPath)
	if real, err := filepath.EvalSymlinks(key); err == nil {
		key = filepath.Clean(real)
	}
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

// lockFile returns the mutex for absPath, creating it on first use. The caller
// must Lock() and defer Unlock() around the whole read/validate/write window,
// not only the final os.WriteFile call.
func lockFile(absPath string) *sync.Mutex {
	key := mutationKey(absPath)
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()
	m, ok := fileLocks[key]
	if !ok {
		m = &sync.Mutex{}
		fileLocks[key] = m
	}
	return m
}
