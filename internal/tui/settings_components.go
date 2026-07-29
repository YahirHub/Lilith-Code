package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsRect and settingsHit are deliberately small. Bubble Tea already
// delivers cell coordinates; we only need deterministic hit-testing for the
// controls rendered by settings screens.
type settingsRect struct {
	x int
	y int
	w int
	h int
}

func (r settingsRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

type settingsHit struct {
	id   string
	part string
	rect settingsRect
}

type settingsBlock struct {
	text string
	hits []settingsHit
}

type settingsCanvas struct {
	width int
	lines []string
	hits  []settingsHit
}

func newSettingsCanvas(width int) *settingsCanvas {
	return &settingsCanvas{width: width}
}

func (c *settingsCanvas) line(text string) {
	c.lines = append(c.lines, strings.Split(text, "\n")...)
}

func (c *settingsCanvas) blank() { c.line("") }

func (c *settingsCanvas) block(b settingsBlock) {
	y := len(c.lines)
	for _, hit := range b.hits {
		hit.rect.y += y
		c.hits = append(c.hits, hit)
	}
	if b.text == "" {
		return
	}
	c.lines = append(c.lines, strings.Split(b.text, "\n")...)
}

func (c *settingsCanvas) render(screenWidth int) (string, []settingsHit) {
	content := strings.Join(c.lines, "\n")
	x := (screenWidth - c.width) / 2
	if x < 0 {
		x = 0
	}
	// Keep a small top margin while preserving deterministic mouse coordinates.
	for i := range c.hits {
		c.hits[i].rect.x += x
		c.hits[i].rect.y++
	}
	body := lipgloss.NewStyle().Width(c.width).Render(content)
	return "\n" + lipgloss.PlaceHorizontal(screenWidth, lipgloss.Center, body), c.hits
}

func settingsContentWidth(screenWidth int) int {
	if screenWidth <= 0 {
		return 32
	}
	w := screenWidth - 4
	if w < 20 {
		w = screenWidth - 2
	}
	if w < 12 {
		w = 12
	}
	if screenWidth > 0 && w > screenWidth {
		w = screenWidth
	}
	if w > 96 {
		w = 96
	}
	return w
}

// settingsWrapPlain wraps unstyled settings text to a terminal-cell width.
// It intentionally works before styling so hitboxes and rendered width stay
// deterministic even for long URLs or paths.
func settingsWrapPlain(text string, width int) string {
	if width < 1 || text == "" {
		return text
	}
	var out []string
	for _, hardLine := range strings.Split(text, "\n") {
		if hardLine == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		for _, r := range []rune(hardLine) {
			candidate := line.String() + string(r)
			if line.Len() > 0 && lipgloss.Width(candidate) > width {
				out = append(out, line.String())
				line.Reset()
			}
			line.WriteRune(r)
		}
		out = append(out, line.String())
	}
	return strings.Join(out, "\n")
}

func settingsHeader(s Styles, title, subtitle string) settingsBlock {
	lines := []string{s.Accent.Render(title)}
	if strings.TrimSpace(subtitle) != "" {
		lines = append(lines, s.Muted.Render(subtitle))
	}
	return settingsBlock{text: strings.Join(lines, "\n")}
}

func settingsFooter(s Styles, text string) settingsBlock {
	return settingsBlock{text: s.Muted.Render(text)}
}

type settingsInputSpec struct {
	ID       string
	Content  string
	Width    int
	Focused  bool
	Disabled bool
}

// settingsInput provides the shared shell for single-line and adaptive
// multiline inputs. The actual editor stays owned by the calling screen.
func settingsInput(s Styles, spec settingsInputSpec) settingsBlock {
	w := spec.Width
	if w < 8 {
		w = 8
	}
	style := s.InputBox
	if spec.Focused && !spec.Disabled {
		style = s.InputBoxFocused
	}
	text := style.Width(w - 4).Render(spec.Content)
	hits := []settingsHit{}
	if spec.ID != "" && !spec.Disabled {
		hits = append(hits, settingsHit{id: spec.ID, rect: settingsRect{w: lipgloss.Width(text), h: lipgloss.Height(text)}})
	}
	return settingsBlock{text: text, hits: hits}
}

type settingsButtonSpec struct {
	ID       string
	Label    string
	Danger   bool
	Disabled bool
	Active   bool
	Focused  bool
}

func settingsButtonRow(s Styles, specs ...settingsButtonSpec) settingsBlock {
	parts := make([]string, 0, len(specs))
	hits := make([]settingsHit, 0, len(specs))
	x := 0
	for _, spec := range specs {
		style := lipgloss.NewStyle().
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(s.Theme.Border).
			Foreground(s.Theme.Foreground)
		if spec.Active && !spec.Disabled {
			style = style.BorderForeground(s.Theme.Secondary).Foreground(s.Theme.Secondary)
		}
		if spec.Focused && !spec.Disabled {
			style = style.BorderForeground(s.Theme.Primary).Foreground(s.Theme.Foreground).Bold(true)
		}
		if spec.Danger && !spec.Disabled {
			style = style.Foreground(s.Theme.Danger)
		}
		if spec.Disabled {
			style = style.Foreground(s.Theme.Muted).BorderForeground(s.Theme.Border)
		}
		text := style.Render(spec.Label)
		parts = append(parts, text)
		w := lipgloss.Width(text)
		if !spec.Disabled && spec.ID != "" {
			hits = append(hits, settingsHit{id: spec.ID, rect: settingsRect{x: x, y: 0, w: w, h: lipgloss.Height(text)}})
		}
		x += w + 2
	}
	return settingsBlock{text: lipgloss.JoinHorizontal(lipgloss.Top, intersperse(parts, "  ")...), hits: hits}
}

func settingsButtonGroup(s Styles, width int, specs ...settingsButtonSpec) settingsBlock {
	if width < 8 {
		width = 8
	}
	type renderedButton struct {
		text string
		spec settingsButtonSpec
		w    int
		h    int
	}
	buttons := make([]renderedButton, 0, len(specs))
	for _, spec := range specs {
		b := settingsButtonRow(s, spec)
		buttons = append(buttons, renderedButton{text: b.text, spec: spec, w: lipgloss.Width(b.text), h: lipgloss.Height(b.text)})
	}
	var rows [][]renderedButton
	current := []renderedButton{}
	used := 0
	for _, b := range buttons {
		need := b.w
		if len(current) > 0 {
			need += 2
		}
		if len(current) > 0 && used+need > width {
			rows = append(rows, current)
			current = nil
			used = 0
		}
		if len(current) > 0 {
			used += 2
		}
		current = append(current, b)
		used += b.w
	}
	if len(current) > 0 {
		rows = append(rows, current)
	}

	texts := make([]string, 0, len(rows))
	hits := []settingsHit{}
	y := 0
	for _, row := range rows {
		parts := make([]string, 0, len(row))
		x := 0
		rowHeight := 1
		for i, b := range row {
			if i > 0 {
				parts = append(parts, "  ")
				x += 2
			}
			parts = append(parts, b.text)
			if b.h > rowHeight {
				rowHeight = b.h
			}
			if !b.spec.Disabled && b.spec.ID != "" {
				hits = append(hits, settingsHit{id: b.spec.ID, rect: settingsRect{x: x, y: y, w: b.w, h: b.h}})
			}
			x += b.w
		}
		texts = append(texts, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
		y += rowHeight
	}
	return settingsBlock{text: strings.Join(texts, "\n"), hits: hits}
}

func intersperse(items []string, sep string) []string {
	if len(items) < 2 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, item)
	}
	return out
}

type settingsCardSpec struct {
	ID          string
	Title       string
	Description string
	Badge       string
	Meta        string
	Selected    bool
	Active      bool
	SingleLine  bool
	Width       int
}

func settingsFitSingleLine(text string, width int) string {
	if width < 1 || lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	var out strings.Builder
	limit := width - 1
	for _, r := range []rune(text) {
		candidate := out.String() + string(r)
		if lipgloss.Width(candidate) > limit {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

func settingsCard(s Styles, spec settingsCardSpec) settingsBlock {
	w := spec.Width
	if w < 8 {
		w = 8
	}
	innerWidth := w - 6
	if innerWidth < 1 {
		innerWidth = 1
	}
	style := s.Card.Width(innerWidth)
	if spec.Selected {
		style = s.CardSelected.Width(innerWidth)
	}
	badge := ""
	if spec.Badge != "" {
		badge = "  " + s.Badge.Render(spec.Badge)
	}
	active := ""
	if spec.Active {
		active = "  " + s.Success.Render("ACTIVO")
	}
	suffix := badge + active
	titleWidth := innerWidth - lipgloss.Width(suffix)
	if titleWidth < 1 {
		titleWidth = innerWidth
		suffix = ""
	}
	var titleLines []string
	if spec.SingleLine {
		titleLines = []string{settingsFitSingleLine(spec.Title, titleWidth)}
	} else {
		titleLines = strings.Split(settingsWrapPlain(spec.Title, titleWidth), "\n")
	}
	lines := make([]string, 0, len(titleLines)+2)
	for i, titleLine := range titleLines {
		head := s.Title.Render(titleLine)
		if i == 0 {
			head += suffix
		}
		lines = append(lines, head)
	}
	if spec.Description != "" {
		lines = append(lines, s.Subtitle.Render(settingsWrapPlain(spec.Description, innerWidth)))
	}
	if spec.Meta != "" {
		lines = append(lines, s.Muted.Render(settingsWrapPlain(spec.Meta, innerWidth)))
	}
	text := style.Render(strings.Join(lines, "\n"))
	hits := []settingsHit{}
	if spec.ID != "" {
		hits = append(hits, settingsHit{id: spec.ID, rect: settingsRect{w: lipgloss.Width(text), h: lipgloss.Height(text)}})
	}
	return settingsBlock{text: text, hits: hits}
}

type settingsSwitchSpec struct {
	ID          string
	Label       string
	Description string
	Value       bool
	Disabled    bool
	Focused     bool
	Width       int
}

func settingsSwitch(s Styles, spec settingsSwitchSpec) settingsBlock {
	state := "OFF"
	knob := "○━━"
	fg := s.Theme.Muted
	if spec.Value {
		state = "ON"
		knob = "━━●"
		fg = s.Theme.Secondary
	}
	if spec.Disabled {
		fg = s.Theme.Muted
	}
	control := lipgloss.NewStyle().Foreground(fg).Bold(!spec.Disabled).Render("[" + knob + " " + state + "]")
	label := s.Title.Render(spec.Label)
	controlWidth := lipgloss.Width(control)
	labelWidth := lipgloss.Width(label)
	line := ""
	if labelWidth+controlWidth+2 <= spec.Width {
		gap := spec.Width - labelWidth - controlWidth
		if gap < 2 {
			gap = 2
		}
		line = label + strings.Repeat(" ", gap) + control
	} else {
		line = s.Title.Render(settingsWrapPlain(spec.Label, spec.Width)) + "\n" + control
	}
	if spec.Focused && !spec.Disabled {
		line = lipgloss.NewStyle().Background(s.Theme.SurfaceHover).Width(spec.Width).Render(line)
	}
	lines := []string{line}
	if spec.Description != "" {
		lines = append(lines, s.Muted.Render(settingsWrapPlain(spec.Description, spec.Width)))
	}
	text := strings.Join(lines, "\n")
	hits := []settingsHit{}
	if !spec.Disabled && spec.ID != "" {
		hits = append(hits, settingsHit{id: spec.ID, rect: settingsRect{w: spec.Width, h: lipgloss.Height(text)}})
	}
	return settingsBlock{text: text, hits: hits}
}

type settingsSliderSpec struct {
	ID       string
	Label    string
	Value    int
	Min      int
	Max      int
	Step     int
	Track    int
	Focused  bool
	Disabled bool
}

func settingsSlider(s Styles, spec settingsSliderSpec) settingsBlock {
	if spec.Max <= spec.Min {
		spec.Max = spec.Min + 1
	}
	if spec.Track < 8 {
		spec.Track = 8
	}
	value := clampInt(spec.Value, spec.Min, spec.Max)
	pos := (value - spec.Min) * (spec.Track - 1) / (spec.Max - spec.Min)
	track := make([]rune, spec.Track)
	for i := range track {
		track[i] = '━'
	}
	track[pos] = '●'
	style := lipgloss.NewStyle().Foreground(s.Theme.Secondary)
	if spec.Disabled {
		style = style.Foreground(s.Theme.Muted)
	}
	line := s.Title.Render(spec.Label) + "  " + style.Render(string(track)) + "  " + s.Muted.Render(fmtInt(value))
	if spec.Focused && !spec.Disabled {
		line = lipgloss.NewStyle().Background(s.Theme.SurfaceHover).Render(line)
	}
	start := lipgloss.Width(s.Title.Render(spec.Label) + "  ")
	hits := []settingsHit{}
	if !spec.Disabled {
		hits = append(hits, settingsHit{id: spec.ID, part: "track", rect: settingsRect{x: start, w: spec.Track, h: 1}})
	}
	return settingsBlock{text: line, hits: hits}
}

func settingsSliderValue(spec settingsSliderSpec, relativeX int) int {
	if spec.Track < 2 || spec.Max <= spec.Min {
		return spec.Min
	}
	relativeX = clampInt(relativeX, 0, spec.Track-1)
	raw := spec.Min + relativeX*(spec.Max-spec.Min)/(spec.Track-1)
	step := spec.Step
	if step <= 0 {
		step = 1
	}
	raw = spec.Min + ((raw-spec.Min+step/2)/step)*step
	return clampInt(raw, spec.Min, spec.Max)
}

type settingsStepperSpec struct {
	ID       string
	Label    string
	Value    int
	Focused  bool
	Disabled bool
}

func settingsStepper(s Styles, spec settingsStepperSpec) settingsBlock {
	minus := settingsButtonSpec{ID: spec.ID, Label: "−", Disabled: spec.Disabled, Focused: spec.Focused}
	plus := settingsButtonSpec{ID: spec.ID, Label: "+", Disabled: spec.Disabled, Focused: spec.Focused}
	left := settingsButtonRow(s, minus)
	right := settingsButtonRow(s, plus)
	value := s.Accent.Render(fmtInt(spec.Value))
	prefix := s.Title.Render(spec.Label) + "  "
	middle := "  " + value + "  "
	text := lipgloss.JoinHorizontal(lipgloss.Center, prefix, left.text, middle, right.text)
	leftX := lipgloss.Width(prefix)
	rightX := leftX + lipgloss.Width(left.text) + lipgloss.Width(middle)
	hits := []settingsHit{}
	if !spec.Disabled {
		hits = append(hits,
			settingsHit{id: spec.ID, part: "dec", rect: settingsRect{x: leftX, w: lipgloss.Width(left.text), h: lipgloss.Height(left.text)}},
			settingsHit{id: spec.ID, part: "inc", rect: settingsRect{x: rightX, w: lipgloss.Width(right.text), h: lipgloss.Height(right.text)}},
		)
	}
	return settingsBlock{text: text, hits: hits}
}

// adaptiveTextArea wraps Bubbles' textarea and keeps only the rows required by
// its current content, up to maxHeight. This is the same density rule used by
// streaming/tool panels: content grows the control, empty reserved space does
// not.
type adaptiveTextArea struct {
	model     textarea.Model
	minHeight int
	maxHeight int
	width     int
}

func newAdaptiveTextArea(placeholder string, minHeight, maxHeight int) adaptiveTextArea {
	if minHeight < 1 {
		minHeight = 1
	}
	if maxHeight < minHeight {
		maxHeight = minHeight
	}
	ta := textarea.New()
	ta.Prompt = "❯ "
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.CharLimit = 16_384
	ta.MaxHeight = 0 // viewport height is controlled with SetHeight; do not truncate input lines.
	ta.SetHeight(minHeight)
	return adaptiveTextArea{model: ta, minHeight: minHeight, maxHeight: maxHeight}
}

func (a *adaptiveTextArea) Focus() tea.Cmd { return a.model.Focus() }
func (a *adaptiveTextArea) Blur()          { a.model.Blur() }
func (a *adaptiveTextArea) Value() string  { return a.model.Value() }
func (a *adaptiveTextArea) SetValue(v string) {
	a.model.SetValue(v)
	a.syncHeight()
}
func (a *adaptiveTextArea) InsertString(v string) {
	a.model.InsertString(v)
	a.syncHeight()
}
func (a *adaptiveTextArea) SetWidth(width int) {
	if width < 8 {
		width = 8
	}
	a.width = width
	a.model.SetWidth(width)
	a.syncHeight()
}
func (a *adaptiveTextArea) Height() int  { return a.model.Height() }
func (a *adaptiveTextArea) View() string { return a.model.View() }
func (a *adaptiveTextArea) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	a.model, cmd = a.model.Update(msg)
	a.syncHeight()
	return cmd
}

func (a *adaptiveTextArea) syncHeight() {
	lines := a.model.LineCount()
	if lines < 1 {
		lines = 1
	}
	// LineCount tracks hard lines. Account for long lines as well so pasting a
	// long model catalog grows the field rather than hiding everything on one row.
	if a.width > 4 {
		wrapped := 0
		for _, line := range strings.Split(a.model.Value(), "\n") {
			cells := lipgloss.Width(line) + 2 // prompt width
			rows := (cells + a.width - 1) / a.width
			if rows < 1 {
				rows = 1
			}
			wrapped += rows
		}
		if wrapped > lines {
			lines = wrapped
		}
	}
	lines = clampInt(lines, a.minHeight, a.maxHeight)
	a.model.SetHeight(lines)
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func mouseLeftPress(msg tea.MouseMsg) (tea.MouseEvent, bool) {
	e := tea.MouseEvent(msg)
	return e, e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft
}

func hitAt(hits []settingsHit, x, y int) (settingsHit, bool) {
	for _, hit := range hits {
		if hit.rect.contains(x, y) {
			return hit, true
		}
	}
	return settingsHit{}, false
}
