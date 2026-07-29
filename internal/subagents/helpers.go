package subagents

import (
	"path/filepath"
	"time"
)

func filepathClean(s string) string { return filepath.Clean(s) }
func filepathSlash(s string) string { return filepath.ToSlash(filepath.Clean(s)) }
func timeNow() time.Time            { return time.Now() }
