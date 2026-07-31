package viewport

import (
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
)

type Model struct {
	Width, Height int
	YOffset       int

	// segments lets the chat keep a large, immutable history prefix separate
	// from the small mutable streaming tail. View and scrolling address the
	// segments directly, avoiding a full transcript concatenation + strings.Split
	// on every provider chunk.
	segments  [][]string
	lineCount int
}

func New(width, height int) Model {
	m := Model{Width: width, Height: height}
	m.SetContent("")
	return m
}

func (m *Model) SetContent(content string) {
	m.SetLineSegments(strings.Split(content, "\n"))
}

// SetLineSegments replaces viewport content with already-split immutable line
// groups. The slices are retained without copying; callers must replace a
// segment rather than mutate its existing elements after this call.
func (m *Model) SetLineSegments(segments ...[]string) {
	m.segments = make([][]string, 0, len(segments))
	m.lineCount = 0
	for _, segment := range segments {
		if len(segment) == 0 {
			continue
		}
		m.segments = append(m.segments, segment)
		m.lineCount += len(segment)
	}
	if m.lineCount == 0 {
		m.segments = [][]string{{""}}
		m.lineCount = 1
	}
	m.clamp()
}

func (m Model) View() string {
	start := m.YOffset
	if start < 0 {
		start = 0
	}
	end := start + m.Height
	if end > m.lineCount {
		end = m.lineCount
	}

	lines := make([]string, 0, m.Height)
	position := 0
	for _, segment := range m.segments {
		segmentEnd := position + len(segment)
		if segmentEnd <= start {
			position = segmentEnd
			continue
		}
		if position >= end {
			break
		}
		from := 0
		if start > position {
			from = start - position
		}
		to := len(segment)
		if end < segmentEnd {
			to = end - position
		}
		if from < to {
			lines = append(lines, segment[from:to]...)
		}
		position = segmentEnd
	}
	for len(lines) < m.Height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m Model) TotalLineCount() int { return m.lineCount }
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
	v := m.lineCount - m.Height
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
