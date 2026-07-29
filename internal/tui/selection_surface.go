package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// selectionSurface is the shared visual shell for compact searchable lists
// such as /models and /history. It deliberately reuses the same settings
// primitives as /config: a focused input and rounded cards with a moving
// border. Search spans the usable terminal width while result cards stay
// compact and centered.
type selectionSurfaceSpec struct {
	Title         string
	Subtitle      string
	SearchContent string
	Cards         []selectionSurfaceCard
	Selected      int
	EmptyText     string
	Footer        string
	Error         string
	ScreenWidth   int
	ScreenHeight  int
}

type selectionSurfaceCard struct {
	Title       string
	Description string
	Meta        string
	Active      bool
}

func renderSelectionSurface(s Styles, spec selectionSurfaceSpec) string {
	searchWidth := selectionSearchWidth(spec.ScreenWidth)
	cardWidth := selectionCardWidth(spec.ScreenWidth)

	header := settingsHeader(s, spec.Title, spec.Subtitle).text
	search := settingsInput(s, settingsInputSpec{
		Content: spec.SearchContent,
		Width:   searchWidth,
		Focused: true,
	}).text

	blocks := make([]settingsBlock, 0, len(spec.Cards))
	for i, card := range spec.Cards {
		blocks = append(blocks, settingsCard(s, settingsCardSpec{
			Title:       card.Title,
			Description: card.Description,
			Meta:        card.Meta,
			Selected:    i == spec.Selected,
			Active:      card.Active,
			Width:       cardWidth,
		}))
	}

	listHeight := spec.ScreenHeight - 10
	if listHeight < 4 {
		listHeight = 4
	}
	start, end := selectionWindow(blocks, spec.Selected, listHeight)
	listParts := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		listParts = append(listParts, blocks[i].text)
	}
	list := strings.Join(listParts, "\n")
	if strings.TrimSpace(list) == "" {
		list = s.Muted.Render(spec.EmptyText)
	}

	parts := []string{
		selectionCentered(spec.ScreenWidth, cardWidth, header),
		selectionCentered(spec.ScreenWidth, searchWidth, search),
		selectionCentered(spec.ScreenWidth, cardWidth, list),
		selectionCentered(spec.ScreenWidth, cardWidth, settingsFooter(s, spec.Footer).text),
	}
	if spec.Error != "" {
		parts = append(parts, selectionCentered(spec.ScreenWidth, cardWidth, s.Danger.Render(settingsWrapPlain(spec.Error, cardWidth))))
	}
	return "\n" + strings.Join(parts, "\n\n")
}

func selectionSearchWidth(screenWidth int) int {
	if screenWidth <= 0 {
		return 32
	}
	if screenWidth <= 4 {
		return screenWidth
	}
	// One cell of breathing room on each side. This is the full usable width
	// without risking a terminal wrap caused by drawing into the final cell.
	return screenWidth - 2
}

func selectionCardWidth(screenWidth int) int {
	w := selectionSearchWidth(screenWidth)
	if w > 72 {
		w = 72
	}
	if w < 12 {
		w = 12
		if screenWidth > 0 && w > screenWidth {
			w = screenWidth
		}
	}
	return w
}

func selectionCentered(screenWidth, width int, content string) string {
	if screenWidth <= 0 {
		return content
	}
	if width > screenWidth {
		width = screenWidth
	}
	return lipgloss.PlaceHorizontal(screenWidth, lipgloss.Center, lipgloss.NewStyle().Width(width).Render(content))
}

// selectionWindow keeps the selected card visible using actual rendered card
// heights rather than assuming every row consumes one terminal line.
func selectionWindow(blocks []settingsBlock, selected, maxHeight int) (int, int) {
	if len(blocks) == 0 {
		return 0, 0
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= len(blocks) {
		selected = len(blocks) - 1
	}
	if maxHeight < 1 {
		maxHeight = 1
	}

	height := func(i int) int {
		h := lipgloss.Height(blocks[i].text)
		if h < 1 {
			h = 1
		}
		return h
	}

	start := 0
	used := 0
	for i := 0; i <= selected; i++ {
		need := height(i)
		if i > start {
			need++ // newline separating adjacent cards
		}
		used += need
		for used > maxHeight && start < i {
			used -= height(start) + 1
			start++
		}
	}

	end := selected + 1
	for end < len(blocks) {
		need := height(end) + 1
		if used+need > maxHeight {
			break
		}
		used += need
		end++
	}
	return start, end
}
