// Package ansi contains the terminal-string helpers used by Lilith's native
// tview frontend.
package ansi

import (
	"strings"

	"github.com/rivo/uniseg"
)

const resetSequence = "\x1b[0m"

// Strip removes CSI, OSC and common single-character escape sequences.
func Strip(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		i = escapeEnd(s, i)
	}
	return b.String()
}

// StringWidth returns the largest visible width of any row in s.
func StringWidth(s string) int {
	maxWidth := 0
	for _, line := range strings.Split(Strip(s), "\n") {
		if width := uniseg.StringWidth(line); width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

// Truncate limits a terminal string to width cells without splitting ANSI
// sequences or Unicode grapheme clusters. Styling is reset before a plain tail
// so a truncation marker never inherits an unfinished source style.
func Truncate(s string, width int, tail string) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(s) <= width {
		return s
	}

	tailWidth := uniseg.StringWidth(Strip(tail))
	limit := width - tailWidth
	if limit < 0 {
		limit = 0
		tail = ""
	}

	var out strings.Builder
	used := 0
	hadANSI := false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			end := escapeEnd(s, i)
			out.WriteString(s[i:end])
			hadANSI = true
			i = end
			continue
		}
		if s[i] == '\n' {
			break
		}

		end := strings.IndexByte(s[i:], 0x1b)
		if end < 0 {
			end = len(s)
		} else {
			end += i
		}
		if newline := strings.IndexByte(s[i:end], '\n'); newline >= 0 {
			end = i + newline
		}

		graphemes := uniseg.NewGraphemes(s[i:end])
		consumed := 0
		for graphemes.Next() {
			cluster := graphemes.Str()
			clusterWidth := uniseg.StringWidth(cluster)
			if used+clusterWidth > limit {
				if hadANSI {
					out.WriteString(resetSequence)
				}
				out.WriteString(tail)
				return out.String()
			}
			out.WriteString(cluster)
			used += clusterWidth
			consumed += len(cluster)
		}
		i += consumed
	}

	if hadANSI {
		out.WriteString(resetSequence)
	}
	out.WriteString(tail)
	return out.String()
}

// Wrap inserts line breaks so every visible row fits within width cells while
// preserving ANSI escape sequences and Unicode grapheme clusters. It performs
// hard wrapping; semantic paragraph wrapping belongs to higher-level widgets.
func Wrap(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}

	var out strings.Builder
	column := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			end := escapeEnd(s, i)
			out.WriteString(s[i:end])
			i = end
			continue
		}
		if s[i] == '\n' {
			out.WriteByte('\n')
			column = 0
			i++
			continue
		}

		end := strings.IndexByte(s[i:], 0x1b)
		if end < 0 {
			end = len(s)
		} else {
			end += i
		}
		if newline := strings.IndexByte(s[i:end], '\n'); newline >= 0 {
			end = i + newline
		}

		graphemes := uniseg.NewGraphemes(s[i:end])
		consumed := 0
		for graphemes.Next() {
			cluster := graphemes.Str()
			clusterWidth := uniseg.StringWidth(cluster)
			if column > 0 && clusterWidth > 0 && column+clusterWidth > width {
				out.WriteByte('\n')
				column = 0
			}
			out.WriteString(cluster)
			column += clusterWidth
			consumed += len(cluster)
			if column >= width && i+consumed < len(s) {
				next := s[i+consumed]
				if next != '\n' && next != 0x1b {
					out.WriteByte('\n')
					column = 0
				}
			}
		}
		i += consumed
	}
	return out.String()
}

// escapeEnd returns the first byte after an ANSI/terminal escape sequence.
func escapeEnd(s string, start int) int {
	i := start + 1
	if i >= len(s) {
		return len(s)
	}
	switch s[i] {
	case '[': // CSI: ESC [ parameters/intermediates final byte.
		i++
		for i < len(s) {
			final := s[i]
			i++
			if final >= 0x40 && final <= 0x7e {
				break
			}
		}
	case ']': // OSC: ESC ] ... BEL or ST.
		i++
		for i < len(s) {
			if s[i] == 0x07 {
				i++
				break
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				i += 2
				break
			}
			i++
		}
	default:
		// Common two-byte escape sequence.
		i++
	}
	if i > len(s) {
		return len(s)
	}
	return i
}
