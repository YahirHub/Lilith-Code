package textarea

import (
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
	"github.com/rivo/uniseg"
)

type Style struct {
	CursorLine tuistyle.Style
	Prompt     tuistyle.Style
	Text       tuistyle.Style
}

type Model struct {
	Prompt, Placeholder        string
	CharLimit                  int
	ShowLineNumbers            bool
	MaxHeight                  int
	FocusedStyle, BlurredStyle Style

	width, height int
	value         []rune
	cursor        int
	focused       bool
}

func New() Model { return Model{Prompt: "> ", width: 80, height: 1} }

var Blink uikit.Cmd = nil

func (m *Model) Focus() uikit.Cmd { m.focused = true; return Blink }
func (m *Model) Blur()            { m.focused = false }
func (m Model) Focused() bool     { return m.focused }
func (m Model) Value() string     { return string(m.value) }
func (m *Model) SetValue(v string) {
	r := []rune(normalize(v))
	if m.CharLimit > 0 && len(r) > m.CharLimit {
		r = r[:m.CharLimit]
	}
	m.value = append(m.value[:0], r...)
	m.cursor = len(m.value)
}
func (m *Model) Reset()                { m.value = nil; m.cursor = 0 }
func (m *Model) CursorEnd()            { m.cursor = len(m.value) }
func (m *Model) InsertString(v string) { m.insert([]rune(normalize(v))) }
func (m *Model) SetWidth(v int) {
	if v < 1 {
		v = 1
	}
	m.width = v
}
func (m Model) Width() int { return m.width }
func (m *Model) SetHeight(v int) {
	if v < 1 {
		v = 1
	}
	m.height = v
}
func (m Model) Height() int { return m.height }
func (m Model) LineCount() int {
	if len(m.value) == 0 {
		return 1
	}
	return strings.Count(string(m.value), "\n") + 1
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

const cursorMarker = "\ue000"

func (m Model) View() string {
	promptStyle := m.BlurredStyle.Prompt
	textStyle := m.BlurredStyle.Text
	if m.focused {
		promptStyle = m.FocusedStyle.Prompt
		textStyle = m.FocusedStyle.Text
	}

	contentWidth := max(1, m.width-tuistyle.Width(m.Prompt))
	placeholder := len(m.value) == 0
	display := string(m.value)
	cursorLine := cursorVisualLine(m.value, m.cursor, contentWidth)
	if placeholder {
		display = m.Placeholder
		cursorLine = 0
	}
	if m.focused {
		if placeholder {
			display = cursorMarker + display
		} else {
			display = string(m.value[:m.cursor]) + cursorMarker + string(m.value[m.cursor:])
		}
	}

	lines := wrap(display, contentWidth)
	if m.focused {
		for index, line := range lines {
			if strings.Contains(line, cursorMarker) {
				cursorLine = index
				lines[index] = strings.Replace(line, cursorMarker, "▌", 1)
				break
			}
		}
	}

	viewHeight := max(1, m.height)
	if m.MaxHeight > 0 && viewHeight > m.MaxHeight {
		viewHeight = m.MaxHeight
	}
	start := 0
	if len(lines) > viewHeight {
		start = cursorLine - viewHeight + 1
		if start < 0 {
			start = 0
		}
		if start > len(lines)-viewHeight {
			start = len(lines) - viewHeight
		}
		lines = lines[start : start+viewHeight]
	}
	for len(lines) < viewHeight {
		lines = append(lines, "")
	}

	continuationPrompt := strings.Repeat(" ", max(1, tuistyle.Width(m.Prompt)))
	out := make([]string, len(lines))
	for index, line := range lines {
		prefix := continuationPrompt
		if start+index == 0 {
			prefix = m.Prompt
		}
		if placeholder {
			out[index] = promptStyle.Render(prefix) + m.BlurredStyle.Text.Render(line)
		} else {
			out[index] = promptStyle.Render(prefix) + textStyle.Render(line)
		}
	}
	return strings.Join(out, "\n")
}

func wrap(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	hardLines := strings.Split(s, "\n")
	out := make([]string, 0, len(hardLines))
	for _, hardLine := range hardLines {
		if hardLine == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		lineWidth := 0
		graphemes := uniseg.NewGraphemes(hardLine)
		for graphemes.Next() {
			cluster := graphemes.Str()
			clusterWidth := uniseg.StringWidth(cluster)
			if lineWidth > 0 && clusterWidth > 0 && lineWidth+clusterWidth > width {
				out = append(out, line.String())
				line.Reset()
				lineWidth = 0
			}
			line.WriteString(cluster)
			lineWidth += clusterWidth
			if lineWidth >= width {
				out = append(out, line.String())
				line.Reset()
				lineWidth = 0
			}
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func cursorVisualLine(value []rune, pos, width int) int {
	if width < 1 {
		width = 1
	}
	if pos < 0 {
		pos = 0
	}
	if pos > len(value) {
		pos = len(value)
	}
	line, column := 0, 0
	graphemes := uniseg.NewGraphemes(string(value[:pos]))
	for graphemes.Next() {
		cluster := graphemes.Str()
		if cluster == "\n" {
			line++
			column = 0
			continue
		}
		clusterWidth := uniseg.StringWidth(cluster)
		if column > 0 && clusterWidth > 0 && column+clusterWidth > width {
			line++
			column = 0
		}
		column += clusterWidth
		if column >= width {
			line++
			column = 0
		}
	}
	return line
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m Model) Update(msg uikit.Msg) (Model, uikit.Cmd) {
	if !m.focused {
		return m, nil
	}
	key, ok := msg.(uikit.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case uikit.KeyRunes:
		m.insert(key.Runes)
	case uikit.KeySpace:
		m.insert([]rune{' '})
	case uikit.KeyEnter:
		m.insert([]rune{'\n'})
	case uikit.KeyBackspace:
		if m.cursor > 0 {
			m.value = append(m.value[:m.cursor-1], m.value[m.cursor:]...)
			m.cursor--
		}
	case uikit.KeyDelete:
		if m.cursor < len(m.value) {
			m.value = append(m.value[:m.cursor], m.value[m.cursor+1:]...)
		}
	case uikit.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
	case uikit.KeyRight:
		if m.cursor < len(m.value) {
			m.cursor++
		}
	case uikit.KeyHome, uikit.KeyCtrlA:
		m.cursor = m.lineStart()
	case uikit.KeyEnd, uikit.KeyCtrlE:
		m.cursor = m.lineEnd()
	case uikit.KeyUp:
		m.moveVertical(-1)
	case uikit.KeyDown:
		m.moveVertical(1)
	case uikit.KeyCtrlU:
		start := m.lineStart()
		m.value = append(m.value[:start], m.value[m.cursor:]...)
		m.cursor = start
	case uikit.KeyCtrlK:
		end := m.lineEnd()
		m.value = append(m.value[:m.cursor], m.value[end:]...)
	case uikit.KeyCtrlW:
		start := m.cursor
		for start > 0 && (m.value[start-1] == ' ' || m.value[start-1] == '\n') {
			start--
		}
		for start > 0 && m.value[start-1] != ' ' && m.value[start-1] != '\n' {
			start--
		}
		m.value = append(m.value[:start], m.value[m.cursor:]...)
		m.cursor = start
	}
	return m, nil
}
func (m *Model) insert(r []rune) {
	r = []rune(normalize(string(r)))
	if m.CharLimit > 0 {
		remain := m.CharLimit - len(m.value)
		if remain <= 0 {
			return
		}
		if len(r) > remain {
			r = r[:remain]
		}
	}
	next := make([]rune, 0, len(m.value)+len(r))
	next = append(next, m.value[:m.cursor]...)
	next = append(next, r...)
	next = append(next, m.value[m.cursor:]...)
	m.value = next
	m.cursor += len(r)
}
func (m Model) lineStart() int {
	i := m.cursor
	for i > 0 && m.value[i-1] != '\n' {
		i--
	}
	return i
}
func (m Model) lineEnd() int {
	i := m.cursor
	for i < len(m.value) && m.value[i] != '\n' {
		i++
	}
	return i
}
func (m *Model) moveVertical(delta int) {
	start := m.lineStart()
	col := m.cursor - start
	if delta < 0 {
		if start == 0 {
			return
		}
		prevEnd := start - 1
		prevStart := prevEnd
		for prevStart > 0 && m.value[prevStart-1] != '\n' {
			prevStart--
		}
		m.cursor = prevStart + min(col, prevEnd-prevStart)
	} else {
		end := m.lineEnd()
		if end >= len(m.value) {
			return
		}
		nextStart := end + 1
		nextEnd := nextStart
		for nextEnd < len(m.value) && m.value[nextEnd] != '\n' {
			nextEnd++
		}
		m.cursor = nextStart + min(col, nextEnd-nextStart)
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
