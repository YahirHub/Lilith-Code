package textinput

import (
	"strings"
	"unicode/utf8"

	"github.com/lilith/li/internal/tui/uikit"
	termansi "github.com/lilith/li/internal/tui/uikit/ansi"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

type EchoMode int

const (
	EchoNormal EchoMode = iota
	EchoPassword
)

type Cursor struct{ Style tuistyle.Style }

type Model struct {
	Prompt, Placeholder                      string
	CharLimit                                int
	Width                                    int
	EchoMode                                 EchoMode
	EchoCharacter                            rune
	PromptStyle, TextStyle, PlaceholderStyle tuistyle.Style
	Cursor                                   Cursor

	value   []rune
	pos     int
	focused bool
}

func New() Model { return Model{Prompt: "> ", EchoCharacter: '•', Width: 20} }

var Blink uikit.Cmd = nil

func (m *Model) Focus() uikit.Cmd { m.focused = true; return Blink }
func (m *Model) Blur()            { m.focused = false }
func (m Model) Focused() bool     { return m.focused }
func (m Model) Value() string     { return string(m.value) }
func (m *Model) SetValue(v string) {
	r := []rune(v)
	if m.CharLimit > 0 && len(r) > m.CharLimit {
		r = r[:m.CharLimit]
	}
	m.value = append(m.value[:0], r...)
	m.pos = len(m.value)
}
func (m *Model) CursorEnd() { m.pos = len(m.value) }

func (m Model) View() string {
	prompt := m.PromptStyle.Render(m.Prompt)
	contentWidth := m.Width
	if contentWidth < 1 {
		contentWidth = 1
	}
	if len(m.value) == 0 {
		cursor := ""
		available := contentWidth
		if m.focused {
			cursor = m.Cursor.Style.Render("▌")
			available--
		}
		if available < 0 {
			available = 0
		}
		placeholder := ""
		if available > 0 {
			placeholder = fit(m.PlaceholderStyle.Render(m.Placeholder), available)
		}
		return prompt + cursor + placeholder
	}
	shown := append([]rune(nil), m.value...)
	if m.EchoMode == EchoPassword {
		ch := m.EchoCharacter
		if ch == 0 {
			ch = '•'
		}
		for i := range shown {
			shown[i] = ch
		}
	}
	before, after := horizontalWindow(shown, m.pos, contentWidth, m.focused)
	content := before
	if m.focused {
		content += m.Cursor.Style.Render("▌")
	}
	content += after
	return prompt + m.TextStyle.Render(content)
}

// horizontalWindow keeps the cursor visible without mutating or truncating the
// stored value. Single-line settings fields can therefore show the end of a
// pasted URL/API key instead of permanently rendering only its first columns.
func horizontalWindow(value []rune, pos, width int, cursorVisible bool) (string, string) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(value) {
		pos = len(value)
	}
	if width < 1 {
		width = 1
	}
	available := width
	if cursorVisible {
		available--
	}
	if available < 0 {
		available = 0
	}

	start := pos
	used := 0
	for start > 0 {
		runeWidth := tuistyle.Width(string(value[start-1]))
		if runeWidth < 0 {
			runeWidth = 0
		}
		if used+runeWidth > available {
			break
		}
		start--
		used += runeWidth
	}

	end := pos
	for end < len(value) {
		runeWidth := tuistyle.Width(string(value[end]))
		if runeWidth < 0 {
			runeWidth = 0
		}
		if used+runeWidth > available {
			break
		}
		used += runeWidth
		end++
	}
	return string(value[start:pos]), string(value[pos:end])
}

func fit(s string, width int) string {
	if width <= 0 || termansi.StringWidth(s) <= width {
		return s
	}
	return termansi.Truncate(s, width, "")
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
	case uikit.KeyBackspace:
		if m.pos > 0 {
			m.value = append(m.value[:m.pos-1], m.value[m.pos:]...)
			m.pos--
		}
	case uikit.KeyDelete:
		if m.pos < len(m.value) {
			m.value = append(m.value[:m.pos], m.value[m.pos+1:]...)
		}
	case uikit.KeyLeft:
		if m.pos > 0 {
			m.pos--
		}
	case uikit.KeyRight:
		if m.pos < len(m.value) {
			m.pos++
		}
	case uikit.KeyHome, uikit.KeyCtrlA:
		m.pos = 0
	case uikit.KeyEnd, uikit.KeyCtrlE:
		m.pos = len(m.value)
	case uikit.KeyCtrlU:
		m.value = append([]rune(nil), m.value[m.pos:]...)
		m.pos = 0
	case uikit.KeyCtrlK:
		m.value = append([]rune(nil), m.value[:m.pos]...)
	case uikit.KeyCtrlW:
		start := m.pos
		for start > 0 && m.value[start-1] == ' ' {
			start--
		}
		for start > 0 && m.value[start-1] != ' ' {
			start--
		}
		m.value = append(m.value[:start], m.value[m.pos:]...)
		m.pos = start
	}
	return m, nil
}

func (m *Model) insert(r []rune) {
	if len(r) == 0 {
		return
	}
	// Single-line fields ignore pasted line breaks.
	cleaned := []rune(strings.ReplaceAll(strings.ReplaceAll(string(r), "\r\n", " "), "\n", " "))
	cleaned = []rune(strings.ReplaceAll(string(cleaned), "\r", " "))
	if m.CharLimit > 0 {
		remain := m.CharLimit - len(m.value)
		if remain <= 0 {
			return
		}
		if len(cleaned) > remain {
			cleaned = cleaned[:remain]
		}
	}
	next := make([]rune, 0, len(m.value)+len(cleaned))
	next = append(next, m.value[:m.pos]...)
	next = append(next, cleaned...)
	next = append(next, m.value[m.pos:]...)
	m.value = next
	m.pos += len(cleaned)
}

var _ = utf8.RuneCountInString
