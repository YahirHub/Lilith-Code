package textarea

import (
	"strings"
	"testing"

	"github.com/lilith/li/internal/tui/uikit"
	termansi "github.com/lilith/li/internal/tui/uikit/ansi"
)

func TestCursorRendersAtLogicalPosition(t *testing.T) {
	model := New()
	model.SetWidth(20)
	model.SetHeight(1)
	model.SetValue("abcd")
	model.Focus()
	model, _ = model.Update(uikit.KeyMsg{Type: uikit.KeyLeft})
	model, _ = model.Update(uikit.KeyMsg{Type: uikit.KeyLeft})

	plain := termansi.Strip(model.View())
	if !strings.Contains(plain, "ab▌cd") {
		t.Fatalf("cursor fuera de posición: %q", plain)
	}
}

func TestMaxHeightLimitsOnlyVisibleRows(t *testing.T) {
	model := New()
	model.SetWidth(30)
	model.SetHeight(12)
	model.MaxHeight = 3
	model.Focus()

	content := strings.Join([]string{"uno", "dos", "tres", "cuatro", "cinco"}, "\n")
	model.InsertString(content)
	if got := model.Value(); got != content {
		t.Fatalf("MaxHeight truncó el contenido: %q", got)
	}
	if rows := strings.Count(model.View(), "\n") + 1; rows != 3 {
		t.Fatalf("altura visible = %d, se esperaban 3 filas", rows)
	}
}

func TestWrapUsesTerminalCellWidth(t *testing.T) {
	lines := wrap("a界bc", 3)
	if len(lines) != 2 || lines[0] != "a界" || lines[1] != "bc" {
		t.Fatalf("ajuste por celdas inesperado: %#v", lines)
	}
}
