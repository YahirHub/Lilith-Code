package viewport

import (
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
)

type Model struct {
	Width, Height int
	YOffset       int
	content       string
	lines         []string
}

func New(width, height int) Model {
	m := Model{Width: width, Height: height}
	m.SetContent("")
	return m
}
func (m *Model) SetContent(content string) {
	m.content = content
	m.lines = strings.Split(content, "\n")
	if len(m.lines) == 0 {
		m.lines = []string{""}
	}
	m.clamp()
}
func (m Model) View() string {
	start := m.YOffset
	if start < 0 {
		start = 0
	}
	end := start + m.Height
	if end > len(m.lines) {
		end = len(m.lines)
	}
	lines := append([]string(nil), m.lines[start:end]...)
	for len(lines) < m.Height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
func (m Model) TotalLineCount() int { return len(m.lines) }
func (m Model) AtBottom() bool      { return m.YOffset >= m.maxOffset() }
func (m *Model) GotoBottom()        { m.YOffset = m.maxOffset() }
func (m *Model) GotoTop()           { m.YOffset = 0 }
func (m *Model) SetYOffset(v int)   { m.YOffset = v; m.clamp() }
func (m *Model) LineUp(n int)       { m.YOffset -= n; m.clamp() }
func (m *Model) LineDown(n int)     { m.YOffset += n; m.clamp() }
func (m Model) ScrollPercent() float64 {
	max := m.maxOffset()
	if max <= 0 {
		return 1
	}
	return float64(m.YOffset) / float64(max)
}
func (m Model) maxOffset() int {
	v := len(m.lines) - m.Height
	if v < 0 {
		return 0
	}
	return v
}
func (m *Model) clamp() {
	if m.Height < 1 {
		m.Height = 1
	}
	if m.YOffset < 0 {
		m.YOffset = 0
	}
	if m.YOffset > m.maxOffset() {
		m.YOffset = m.maxOffset()
	}
}
func (m Model) Update(msg uikit.Msg) (Model, uikit.Cmd) {
	key, ok := msg.(uikit.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.LineUp(1)
	case "down", "j":
		m.LineDown(1)
	case "pgup", "ctrl+b", "ctrl+u":
		m.LineUp(max(1, m.Height-1))
	case "pgdown", "ctrl+f", "ctrl+d":
		m.LineDown(max(1, m.Height-1))
	case "home", "ctrl+home":
		m.GotoTop()
	case "end", "ctrl+end":
		m.GotoBottom()
	}
	return m, nil
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
