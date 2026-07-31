package tui

import (
	"strings"

	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

// asciiLogo is a compact 6-row Lilith wordmark.
const asciiLogo = `
 ██╗     ██╗██╗     ██╗████████╗██╗  ██╗
 ██║     ██║██║     ██║╚══██╔══╝██║  ██║
 ██║     ██║██║     ██║   ██║   ███████║
 ██║     ██║██║     ██║   ██║   ██╔══██║
 ███████╗██║███████╗██║   ██║   ██║  ██║
 ╚══════╝╚═╝╚══════╝╚═╝   ╚═╝   ╚═╝  ╚═╝`

// RenderLogo returns the sized Lilith logo. Wide/tall terminals get the ASCII
// art; narrow ones fall back to a coloured wordmark.
func RenderLogo(width, height int, t Theme) string {
	if width < 44 || height < 10 {
		return tuistyle.NewStyle().Foreground(t.Primary).Bold(true).Render("✦ LILITH")
	}
	lines := strings.Split(strings.TrimPrefix(asciiLogo, "\n"), "\n")
	style := tuistyle.NewStyle().Foreground(t.LogoBlock).Bold(true)
	accent := tuistyle.NewStyle().Foreground(t.LogoAccent).Bold(true)
	// Colour a diagonal sheen across the middle row for interest.
	out := make([]string, len(lines))
	mid := len(lines) / 2
	for i, l := range lines {
		if i == mid {
			out[i] = accent.Render(l)
		} else {
			out[i] = style.Render(l)
		}
	}
	return strings.Join(out, "\n")
}
