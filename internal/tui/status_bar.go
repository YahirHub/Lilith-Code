package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders the bottom status line: cwd · provider/model · contexto · hints.
func RenderStatusBar(ctx *AppContext, mode string, usedTokens, maxTokens int) string {
	s := ctx.Styles
	cwd, _ := os.Getwd()
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, cwd); err == nil && !strings.HasPrefix(rel, "..") {
			cwd = "~/" + rel
		}
	}

	active := ctx.Providers.Active()
	provider := "sin proveedor"
	if active.ProviderName != "" {
		provider = active.ProviderName
		if active.ModelID != "" {
			provider += " · " + active.ModelID
		}
	}

	modeChip := ""
	if mode != "" && mode != "default" {
		modeChip = lipgloss.NewStyle().
			Background(s.Theme.Warning).
			Foreground(s.Theme.Background).
			Padding(0, 1).
			Bold(true).
			Render(strings.ToUpper(mode)) + " "
	}

	contextBar := RenderContextBar(s.Theme, usedTokens, maxTokens, 12)

	left := modeChip + s.Muted.Render(cwd)
	right := s.Accent.Render("◆ ") + s.Muted.Render(provider)
	hint := s.Muted.Render("/ comandos · ! bash · Esc cancelar · /exit salir")

	w := ctx.Width
	if w <= 0 {
		w = 80
	}
	inner := lipgloss.JoinHorizontal(lipgloss.Left, left, "   ", right)
	if contextBar != "" {
		inner = lipgloss.JoinHorizontal(lipgloss.Left, inner, "   ", contextBar)
	}
	pad := w - lipgloss.Width(inner) - lipgloss.Width(hint) - 2
	if pad < 1 {
		pad = 1
	}
	line := inner + strings.Repeat(" ", pad) + hint
	return s.StatusBar.Width(w).Render(line)
}
