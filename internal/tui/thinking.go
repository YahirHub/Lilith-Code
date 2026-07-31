package tui

import (
	"strings"
	"time"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

// thinkingTickMsg avanza un frame de la animación "pensando".
type thinkingTickMsg struct{ frame int }

// thinkingTick programa el siguiente frame.
func thinkingTick(frame int) uikit.Cmd {
	return uikit.Tick(90*time.Millisecond, func(time.Time) uikit.Msg {
		return thinkingTickMsg{frame: frame + 1}
	})
}

// glifos rotativos que se ven bien en cualquier terminal monoespaciada.
var thinkingSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// palette es una rampa oklab-ish alrededor del primary de Lilith.
var thinkingPalette = []string{
	"#8b5cf6", "#a78bfa", "#c4b5fd", "#e9d5ff",
	"#c4b5fd", "#a78bfa", "#8b5cf6", "#7c3aed",
	"#6d28d9", "#7c3aed",
}

// workingPalette usa verdes/teal para diferenciar visualmente el estado
// "Trabajando" (ejecutando herramientas) del "Pensando" (esperando modelo).
var workingPalette = []string{
	"#10b981", "#34d399", "#6ee7b7", "#a7f3d0",
	"#6ee7b7", "#34d399", "#10b981", "#059669",
	"#047857", "#059669",
}

func renderShimmer(frame int, label string, palette []string) string {
	spin := thinkingSpinner[frame%len(thinkingSpinner)]
	spinStyled := tuistyle.NewStyle().Foreground(tuistyle.Color(palette[frame%len(palette)])).Render(spin)

	var b strings.Builder
	for i, r := range label {
		c := palette[(i+frame)%len(palette)]
		b.WriteString(tuistyle.NewStyle().Foreground(tuistyle.Color(c)).Bold(true).Render(string(r)))
	}
	dots := strings.Repeat(".", (frame % 4))
	dotsStyled := tuistyle.NewStyle().Foreground(tuistyle.Color(palette[1])).Render(dots + strings.Repeat(" ", 3-len(dots)))
	return spinStyled + "  " + b.String() + dotsStyled
}

// RenderThinking dibuja "Pensando..." con un shimmer púrpura.
func RenderThinking(frame int) string {
	return renderShimmer(frame, "Pensando", thinkingPalette)
}

// RenderWorking dibuja "Trabajando..." con un shimmer verde para señalar
// que el CLI está ejecutando herramientas activamente.
func RenderWorking(frame int) string {
	return renderShimmer(frame, "Trabajando", workingPalette)
}
