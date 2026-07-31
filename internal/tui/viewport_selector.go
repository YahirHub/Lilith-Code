package tui

import (
	"strings"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
	"github.com/lilith/li/internal/tui/uikit/viewport"
)

// viewportSelector is the shared compact picker used by searchable terminal
// lists such as /models and /history. It gives all remaining terminal height
// to the internal viewport and keeps the complete result set as viewport
// content; only YOffset changes to keep the focused row visible.
type viewportSelectorSpec struct {
	Title         string
	Subtitle      string
	SearchContent string
	Items         []viewportSelectorItem
	Selected      int
	EmptyText     string
	Footer        string
	Error         string
	ScreenWidth   int
	ScreenHeight  int
}

type viewportSelectorItem struct {
	Primary   string
	Secondary string
	Active    bool
}

type viewportSelectorRenderedItems struct {
	content       string
	selectedStart int
	selectedEnd   int // exclusive
	lineCount     int
}

func renderViewportSelector(s Styles, spec viewportSelectorSpec) string {
	width := viewportSelectorWidth(spec.ScreenWidth)

	headerParts := []string{s.Accent.Render(spec.Title)}
	if strings.TrimSpace(spec.Subtitle) != "" {
		headerParts = append(headerParts, s.Muted.Render(spec.Subtitle))
	}
	header := strings.Join(headerParts, "\n")

	search := viewportSelectorSearch(s, spec.SearchContent, width)
	rule := s.Muted.Render(strings.Repeat("─", width))
	footer := s.Muted.Render(settingsFitSingleLine(spec.Footer, width))

	errorText := ""
	if strings.TrimSpace(spec.Error) != "" {
		errorText = s.Danger.Render(settingsFitSingleLine(spec.Error, width))
	}

	// Every component is joined with exactly one newline, so its rendered
	// height is simply the sum of the component heights. There are no guessed
	// separator rows and no padding strings masquerading as a viewport.
	fixedHeight := 1 + tuistyle.Height(header) + tuistyle.Height(search) + tuistyle.Height(rule) + tuistyle.Height(footer)
	if errorText != "" {
		fixedHeight += tuistyle.Height(errorText)
	}
	listHeight := spec.ScreenHeight - fixedHeight
	if listHeight < 1 {
		listHeight = 1
	}

	rendered := renderViewportSelectorItems(s, spec.Items, spec.Selected, width)
	content := rendered.content
	if strings.TrimSpace(content) == "" {
		content = s.Muted.Render(spec.EmptyText)
		rendered.lineCount = tuistyle.Height(content)
		rendered.selectedStart = 0
		rendered.selectedEnd = rendered.lineCount
	}

	vp := viewport.New(width, listHeight)
	vp.SetContent(content)
	vp.YOffset = viewportSelectorOffset(rendered, listHeight)
	list := vp.View()

	parts := []string{header, search, rule, list}
	if errorText != "" {
		parts = append(parts, errorText)
	}
	parts = append(parts, footer)

	body := "\n" + strings.Join(parts, "\n")
	return tuistyle.PlaceHorizontal(spec.ScreenWidth, tuistyle.Center, tuistyle.NewStyle().Width(width).Render(body))
}

func viewportSelectorWidth(screenWidth int) int {
	if screenWidth <= 0 {
		return 32
	}
	if screenWidth <= 2 {
		return screenWidth
	}
	return screenWidth - 2
}

func viewportSelectorSearch(s Styles, content string, width int) string {
	if width < 1 {
		return content
	}
	inner := width - 2
	if inner < 1 {
		inner = 1
	}
	return tuistyle.NewStyle().
		Foreground(s.Theme.Foreground).
		Padding(0, 1).
		Width(inner).
		Render(content)
}

func renderViewportSelectorItems(s Styles, items []viewportSelectorItem, selected, width int) viewportSelectorRenderedItems {
	if len(items) == 0 {
		return viewportSelectorRenderedItems{}
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= len(items) {
		selected = len(items) - 1
	}

	lines := make([]string, 0, len(items)*2)
	selectedStart := 0
	selectedEnd := 0
	for i, item := range items {
		start := len(lines)
		itemLines := viewportSelectorItemLines(s, item, i == selected, width)
		lines = append(lines, itemLines...)
		if i == selected {
			selectedStart = start
			selectedEnd = len(lines)
		}
	}
	return viewportSelectorRenderedItems{
		content:       strings.Join(lines, "\n"),
		selectedStart: selectedStart,
		selectedEnd:   selectedEnd,
		lineCount:     len(lines),
	}
}

func viewportSelectorItemLines(s Styles, item viewportSelectorItem, selected bool, width int) []string {
	if width < 4 {
		width = 4
	}
	prefix := "  "
	if selected {
		prefix = "> "
	}
	active := ""
	if item.Active {
		active = "  [ACTIVO]"
	}
	primaryWidth := width - tuistyle.Width(prefix) - tuistyle.Width(active) - 1
	if primaryWidth < 1 {
		primaryWidth = 1
	}
	primary := settingsFitSingleLine(item.Primary, primaryWidth)
	line := prefix + primary + active

	rowStyle := tuistyle.NewStyle().Width(width)
	if selected {
		rowStyle = rowStyle.Background(s.Theme.Surface).Foreground(s.Theme.Foreground).Bold(true)
		line = rowStyle.Render(line)
	} else if item.Active {
		line = s.Title.Render(prefix+primary) + s.Success.Render(active)
	} else {
		line = s.Title.Render(line)
	}

	out := []string{line}
	if strings.TrimSpace(item.Secondary) != "" {
		secondaryWidth := width - 4
		if secondaryWidth < 1 {
			secondaryWidth = 1
		}
		secondary := "    " + settingsFitSingleLine(item.Secondary, secondaryWidth)
		if selected {
			secondary = tuistyle.NewStyle().
				Width(width).
				Background(s.Theme.Surface).
				Foreground(s.Theme.Muted).
				Render(secondary)
		} else {
			secondary = s.Muted.Render(secondary)
		}
		out = append(out, secondary)
	}
	return out
}

// viewportSelectorOffset keeps the focused item inside the physical viewport.
// The viewport receives the full result set; this function only chooses the
// initial Y offset and never drops items from the content.
func viewportSelectorOffset(items viewportSelectorRenderedItems, viewportHeight int) int {
	if viewportHeight < 1 || items.lineCount <= viewportHeight {
		return 0
	}
	offset := 0
	if items.selectedEnd > viewportHeight {
		offset = items.selectedEnd - viewportHeight
	}
	maxOffset := items.lineCount - viewportHeight
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}
