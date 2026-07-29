package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSelectionSurfaceUsesFullSearchAndCompactCards(t *testing.T) {
	if got := selectionSearchWidth(120); got != 118 {
		t.Fatalf("search width = %d, want 118", got)
	}
	if got := selectionCardWidth(120); got != 72 {
		t.Fatalf("card width = %d, want 72", got)
	}
	if got := selectionCardWidth(60); got != 58 {
		t.Fatalf("card width on narrow terminal = %d, want 58", got)
	}
}

func TestSelectionWindowKeepsFocusedCardVisibleByRenderedHeight(t *testing.T) {
	s := NewStyles(DefaultTheme())
	blocks := make([]settingsBlock, 0, 6)
	for i := 0; i < 6; i++ {
		blocks = append(blocks, settingsCard(s, settingsCardSpec{
			Title:       "Tarjeta " + fmtInt(i),
			Description: "Detalle",
			Width:       40,
		}))
	}
	start, end := selectionWindow(blocks, 5, 9)
	if !(start <= 5 && 5 < end) {
		t.Fatalf("window [%d,%d) does not contain selected card 5", start, end)
	}
	height := 0
	for i := start; i < end; i++ {
		height += lipgloss.Height(blocks[i].text)
		if i > start {
			height++
		}
	}
	if height > 9 && start != end-1 {
		t.Fatalf("window height = %d, want <= 9 unless selected card alone exceeds it", height)
	}
}

func TestSelectionSurfaceContainsNoSearchEmoji(t *testing.T) {
	view := renderSelectionSurface(NewStyles(DefaultTheme()), selectionSurfaceSpec{
		Title:         "Selector",
		SearchContent: "Buscar  ejemplo",
		Cards:         []selectionSurfaceCard{{Title: "Uno", Description: "Detalle"}},
		Selected:      0,
		EmptyText:     "Vacío",
		Footer:        "Esc volver",
		ScreenWidth:   100,
		ScreenHeight:  30,
	})
	if strings.Contains(view, "🔍") {
		t.Fatal("selection surface must use text instead of emoji search icon")
	}
}
