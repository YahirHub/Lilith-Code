package tui

import (
	"os"
	"path/filepath"
	"strings"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

// RenderStatusBar keeps the bottom line intentionally minimal: cwd, provider,
// model and numeric context usage. Commands/shortcuts live in /help instead of
// permanently consuming terminal width.
func RenderStatusBar(ctx *AppContext, _ string, usedTokens, maxTokens int) string {
	s := ctx.Styles
	cwd, _ := os.Getwd()
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, cwd); err == nil && !strings.HasPrefix(rel, "..") {
			cwd = "~/" + filepath.ToSlash(rel)
		}
	}
	cwd = filepath.ToSlash(cwd)

	active := ctx.Providers.Active()
	provider := active.ProviderName
	if provider == "" {
		provider = "sin proveedor"
	}
	model := active.ModelID
	if model == "" {
		model = "sin modelo"
	}

	providerStyle := tuistyle.NewStyle().Foreground(s.Theme.Secondary).Bold(true)
	modelStyle := tuistyle.NewStyle().Foreground(s.Theme.Primary).Bold(true)
	contextBar := RenderContextBar(s.Theme, usedTokens, maxTokens, 12)

	fixed := "   " + providerStyle.Render(provider) + " · " + modelStyle.Render(model)
	if contextBar != "" {
		fixed += "   " + contextBar
	}

	w := ctx.Width
	if w <= 0 {
		w = 80
	}
	availableCWD := w - tuistyle.Width(fixed) - 2
	if availableCWD < 4 {
		availableCWD = 4
	}
	cwd = truncateStatusPath(cwd, availableCWD)
	line := s.Muted.Render(cwd) + fixed
	return s.StatusBar.Width(w).Render(line)
}

func truncateStatusPath(path string, maxWidth int) string {
	if maxWidth <= 0 || tuistyle.Width(path) <= maxWidth {
		return path
	}
	r := []rune(path)
	if maxWidth <= 1 {
		return "…"
	}
	keep := maxWidth - 1
	if keep > len(r) {
		keep = len(r)
	}
	return "…" + string(r[len(r)-keep:])
}
