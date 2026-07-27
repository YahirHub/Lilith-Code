// Package logx provides a minimal structured logger with secret masking.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

var (
	mu     sync.Mutex
	writer io.Writer = os.Stderr
	debug            = os.Getenv("LILITH_DEBUG") != ""
)

// SetWriter redirects log output. Used by the TUI to write to a file instead of
// stderr (which would corrupt the alternate screen).
func SetWriter(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	writer = w
	log.SetOutput(w)
}

// Mask hides all but the last 4 characters of a secret so we can safely log it.
func Mask(secret string) string {
	s := strings.TrimSpace(secret)
	if len(s) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

func Debugf(format string, args ...any) {
	if !debug {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(writer, "[debug] "+format+"\n", args...)
}

func Infof(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(writer, "[info] "+format+"\n", args...)
}

func Errorf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(writer, "[error] "+format+"\n", args...)
}
