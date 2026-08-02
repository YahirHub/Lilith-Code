// Package platformargs normalizes process arguments emitted by platform-specific launchers.
package platformargs

import (
	"path/filepath"
	"strings"
)

// Normalize repairs the extra executable argument injected by Android/Termux
// when launching some Go binaries. Without this correction Cobra interprets
// the absolute path to li as a subcommand.
func Normalize(args []string, goos string) []string {
	if goos != "android" || len(args) < 2 {
		return args
	}
	first := strings.TrimSpace(args[0])
	second := strings.TrimSpace(args[1])
	if first == "" || second == "" {
		return args
	}
	if filepath.Base(first) != filepath.Base(second) {
		return args
	}
	if !strings.ContainsAny(second, `/\`) {
		return args
	}
	out := make([]string, 0, len(args)-1)
	out = append(out, second)
	out = append(out, args[2:]...)
	return out
}
