package tui

import (
	"strings"
	"testing"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

func TestViewportSelectorOccupiesExactTerminalHeight(t *testing.T) {
	const height = 42
	items := make([]viewportSelectorItem, 0, 30)
	for i := 0; i < 30; i++ {
		items = append(items, viewportSelectorItem{Primary: "Elemento " + fmtInt(i)})
	}
	view := renderViewportSelector(NewStyles(DefaultTheme()), viewportSelectorSpec{
		Title:         "Selector",
		Subtitle:      "Prueba",
		SearchContent: "Buscar  ejemplo",
		Items:         items,
		Selected:      0,
		EmptyText:     "Vacío",
		Footer:        "Esc volver",
		ScreenWidth:   100,
		ScreenHeight:  height,
	})
	if got := tuistyle.Height(view); got != height {
		t.Fatalf("claude selector height = %d, want %d", got, height)
	}
}

func TestViewportSelectorKeepsCompleteResultSetInsideViewportContent(t *testing.T) {
	items := make([]viewportSelectorItem, 0, 20)
	for i := 0; i < 20; i++ {
		items = append(items, viewportSelectorItem{Primary: "Modelo " + fmtInt(i)})
	}
	rendered := renderViewportSelectorItems(NewStyles(DefaultTheme()), items, 19, 80)
	if rendered.lineCount != 20 {
		t.Fatalf("line count = %d, want 20", rendered.lineCount)
	}
	if !strings.Contains(rendered.content, "Modelo 0") || !strings.Contains(rendered.content, "Modelo 19") {
		t.Fatal("viewport content must retain the full result set")
	}
	if got := viewportSelectorOffset(rendered, 8); got != 12 {
		t.Fatalf("offset = %d, want 12", got)
	}
}

func TestViewportSelectorHistoryRowsUseTwoDenseLines(t *testing.T) {
	rendered := renderViewportSelectorItems(NewStyles(DefaultTheme()), []viewportSelectorItem{
		{Primary: "Conversación uno", Secondary: "hace 1 h · 3 turnos"},
		{Primary: "Conversación dos", Secondary: "hace 2 h · 4 turnos"},
	}, 0, 80)
	if rendered.lineCount != 4 {
		t.Fatalf("history line count = %d, want 4", rendered.lineCount)
	}
	if strings.Contains(rendered.content, "╭") || strings.Contains(rendered.content, "╰") {
		t.Fatal("experimental Claude-style selector must not render per-item cards")
	}
}
