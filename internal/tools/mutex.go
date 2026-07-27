package tools

// Per-path write mutex, inspired by pi.dev's file-mutation-queue. Serialises
// concurrent str_replace / write_file / apply_diff calls that target the same
// path so a burst of tool calls can never corrupt a file mid-write.

import (
	"path/filepath"
	"sync"
)

var (
	fileLocksMu sync.Mutex
	fileLocks   = map[string]*sync.Mutex{}
)

// lockFile returns the mutex for `absPath`, creating it on first use. The
// caller must Lock() and defer Unlock() it around any read-modify-write
// sequence that touches the file.
func lockFile(absPath string) *sync.Mutex {
	key := filepath.Clean(absPath)
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()
	m, ok := fileLocks[key]
	if !ok {
		m = &sync.Mutex{}
		fileLocks[key] = m
	}
	return m
}
