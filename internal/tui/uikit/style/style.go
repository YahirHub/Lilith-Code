// Package style is Lilith's lightweight styling/layout layer. It intentionally
// implements only the operations used by the application and emits standard
// ANSI SGR sequences which tview translates to native cell styles.
package style

import (
	"fmt"
	"strconv"
	"strings"

	termansi "github.com/lilith/li/internal/tui/uikit/ansi"
)

type Color string

type Position int

const (
	Left Position = iota
	Center
	Right
	Top
	Bottom
)

type Border struct {
	Top, Bottom, Left, Right                   rune
	TopLeft, TopRight, BottomLeft, BottomRight rune
}

func RoundedBorder() Border {
	return Border{Top: '─', Bottom: '─', Left: '│', Right: '│', TopLeft: '╭', TopRight: '╮', BottomLeft: '╰', BottomRight: '╯'}
}

type Style struct {
	fg, bg                               Color
	borderFG                             Color
	bold, italic, underline, strike      bool
	border                               *Border
	padTop, padRight, padBottom, padLeft int
	width, height                        int
}

func NewStyle() Style                          { return Style{} }
func (s Style) Foreground(c Color) Style       { s.fg = c; return s }
func (s Style) Background(c Color) Style       { s.bg = c; return s }
func (s Style) BorderForeground(c Color) Style { s.borderFG = c; return s }
func (s Style) Bold(v bool) Style              { s.bold = v; return s }
func (s Style) Italic(v bool) Style            { s.italic = v; return s }
func (s Style) Underline(v bool) Style         { s.underline = v; return s }
func (s Style) Strikethrough(v bool) Style     { s.strike = v; return s }
func (s Style) Width(v int) Style              { s.width = max0(v); return s }
func (s Style) Height(v int) Style             { s.height = max0(v); return s }
func (s Style) Border(b Border) Style          { s.border = &b; return s }
func (s Style) Padding(values ...int) Style {
	switch len(values) {
	case 1:
		s.padTop, s.padRight, s.padBottom, s.padLeft = values[0], values[0], values[0], values[0]
	case 2:
		s.padTop, s.padBottom = values[0], values[0]
		s.padLeft, s.padRight = values[1], values[1]
	case 3:
		s.padTop, s.padRight, s.padLeft, s.padBottom = values[0], values[1], values[1], values[2]
	default:
		if len(values) >= 4 {
			s.padTop, s.padRight, s.padBottom, s.padLeft = values[0], values[1], values[2], values[3]
		}
	}
	return s
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func (s Style) Render(parts ...string) string {
	content := strings.Join(parts, " ")
	if s.width > 0 {
		content = termansi.Wrap(content, s.width)
	}
	lines := strings.Split(content, "\n")
	contentWidth := 0
	for _, line := range lines {
		if w := Width(line); w > contentWidth {
			contentWidth = w
		}
	}
	targetWidth := contentWidth
	if s.width > 0 {
		targetWidth = s.width
	}
	targetHeight := len(lines)
	if s.height > targetHeight {
		targetHeight = s.height
	}
	for len(lines) < targetHeight {
		lines = append(lines, "")
	}

	styled := make([]string, 0, targetHeight+s.padTop+s.padBottom+2)
	blank := strings.Repeat(" ", targetWidth+s.padLeft+s.padRight)
	for i := 0; i < s.padTop; i++ {
		styled = append(styled, s.apply(blank))
	}
	for _, line := range lines {
		line = padRight(line, targetWidth)
		row := strings.Repeat(" ", s.padLeft) + line + strings.Repeat(" ", s.padRight)
		styled = append(styled, s.apply(row))
	}
	for i := 0; i < s.padBottom; i++ {
		styled = append(styled, s.apply(blank))
	}

	if s.border == nil {
		return strings.Join(styled, "\n")
	}
	b := *s.border
	innerWidth := targetWidth + s.padLeft + s.padRight
	top := string(b.TopLeft) + strings.Repeat(string(b.Top), innerWidth) + string(b.TopRight)
	bottom := string(b.BottomLeft) + strings.Repeat(string(b.Bottom), innerWidth) + string(b.BottomRight)
	out := make([]string, 0, len(styled)+2)
	out = append(out, s.applyBorder(top))
	for _, row := range styled {
		out = append(out, s.applyBorder(string(b.Left))+row+s.applyBorder(string(b.Right)))
	}
	out = append(out, s.applyBorder(bottom))
	return strings.Join(out, "\n")
}

func (s Style) apply(text string) string {
	codes := make([]string, 0, 6)
	if s.bold {
		codes = append(codes, "1")
	}
	if s.italic {
		codes = append(codes, "3")
	}
	if s.underline {
		codes = append(codes, "4")
	}
	if s.strike {
		codes = append(codes, "9")
	}
	if c := colorCode(s.fg, false); c != "" {
		codes = append(codes, c)
	}
	if c := colorCode(s.bg, true); c != "" {
		codes = append(codes, c)
	}
	if len(codes) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

func (s Style) applyBorder(text string) string {
	if s.borderFG == "" {
		return text
	}
	code := colorCode(s.borderFG, false)
	if code == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func colorCode(c Color, background bool) string {
	value := strings.TrimSpace(string(c))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "#") && len(value) == 7 {
		r, e1 := strconv.ParseUint(value[1:3], 16, 8)
		g, e2 := strconv.ParseUint(value[3:5], 16, 8)
		b, e3 := strconv.ParseUint(value[5:7], 16, 8)
		if e1 == nil && e2 == nil && e3 == nil {
			prefix := 38
			if background {
				prefix = 48
			}
			return fmt.Sprintf("%d;2;%d;%d;%d", prefix, r, g, b)
		}
	}
	return ""
}

func padRight(s string, width int) string {
	missing := width - Width(s)
	if missing <= 0 {
		return s
	}
	return s + strings.Repeat(" ", missing)
}

func Width(s string) int { return termansi.StringWidth(s) }
func Height(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func JoinHorizontal(pos Position, blocks ...string) string {
	maxH := 0
	split := make([][]string, len(blocks))
	widths := make([]int, len(blocks))
	for i, block := range blocks {
		split[i] = strings.Split(block, "\n")
		if len(split[i]) > maxH {
			maxH = len(split[i])
		}
		widths[i] = Width(block)
	}
	rows := make([]string, maxH)
	for y := 0; y < maxH; y++ {
		var b strings.Builder
		for i, lines := range split {
			offset := 0
			switch pos {
			case Center:
				offset = (maxH - len(lines)) / 2
			case Bottom:
				offset = maxH - len(lines)
			}
			if y >= offset && y < offset+len(lines) {
				b.WriteString(padRight(lines[y-offset], widths[i]))
			} else {
				b.WriteString(strings.Repeat(" ", widths[i]))
			}
		}
		rows[y] = b.String()
	}
	return strings.Join(rows, "\n")
}

func JoinVertical(pos Position, blocks ...string) string {
	maxW := 0
	for _, block := range blocks {
		if w := Width(block); w > maxW {
			maxW = w
		}
	}
	out := make([]string, 0)
	for _, block := range blocks {
		for _, line := range strings.Split(block, "\n") {
			left := 0
			switch pos {
			case Center:
				left = (maxW - Width(line)) / 2
			case Right:
				left = maxW - Width(line)
			}
			if left < 0 {
				left = 0
			}
			out = append(out, strings.Repeat(" ", left)+line)
		}
	}
	return strings.Join(out, "\n")
}

func PlaceHorizontal(width int, pos Position, block string) string {
	if width <= 0 {
		return block
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		w := Width(line)
		if w >= width {
			continue
		}
		left := 0
		switch pos {
		case Center:
			left = (width - w) / 2
		case Right:
			left = width - w
		}
		lines[i] = strings.Repeat(" ", left) + line + strings.Repeat(" ", width-w-left)
	}
	return strings.Join(lines, "\n")
}
