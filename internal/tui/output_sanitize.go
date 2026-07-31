package tui

import (
	"strings"
	"unicode"

	"github.com/lilith/li/internal/tui/uikit/ansi"
)

// sanitizeOutput normalizes raw shell output so it can be rendered inside a
// internal styled box without breaking the frame.
//
// Real command output frequently mixes:
//   - ANSI/OSC escape sequences (colors, cursor moves) that the internal layout engine can
//     mostly measure but that still confuse wrapping.
//   - Carriage returns (`\r`) used for progress bars — leaving them in place
//     makes the terminal overprint the border.
//   - Tabs and backspaces that terminals resolve visually but the layout engine counts
//     as zero-width, so lines end up wider than the box.
//   - Stray control characters (VT/FF/NUL) coming from tools like `less`.
//
// We strip escape sequences, split on `\r` as if it were a newline (progress
// bars become their latest frame), replace tabs with four spaces, and drop
// any remaining non-printable rune.
func sanitizeOutput(s string) string {
	if s == "" {
		return ""
	}
	// Remove ANSI/OSC/CSI sequences up front.
	s = ansi.Strip(s)
	// Normalize CRLF → LF and treat lone CR as a line break so a progress
	// bar that overwrites itself doesn't collapse into one giant line.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune('\n')
		case r == '\t':
			b.WriteString("    ")
		case r == 0x7f || (r < 0x20 && r != '\n'):
			// Drop remaining C0 control chars (BS, BEL, VT, FF, NUL…).
			continue
		case !unicode.IsPrint(r) && !unicode.IsSpace(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// clipCols truncates s to at most `cols` display columns, appending an
// ellipsis when it had to cut. Unlike rune-based clip, this measures actual
// terminal width so wide characters (emoji, CJK) don't overflow the box.
func clipCols(s string, cols int) string {
	if cols < 4 {
		cols = 4
	}
	if ansi.StringWidth(s) <= cols {
		return s
	}
	return ansi.Truncate(s, cols-1, "…")
}
