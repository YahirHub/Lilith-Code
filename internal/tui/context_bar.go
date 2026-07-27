package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/providers/openai"
)

// EstimateTokens aproxima el consumo de contexto del historial. Se usa una
// heurística local (≈4 caracteres por token) más un pequeño coste fijo por
// mensaje y por llamada a herramienta; es suficiente para decidir cuándo
// compactar sin añadir dependencias de tokenizadores.
func EstimateTokens(msgs []openai.Message) int {
	total := 0
	for _, msg := range msgs {
		total += 4 + len(msg.Content)/4
		for _, call := range msg.ToolCalls {
			total += 8 + (len(call.Function.Name)+len(call.Function.Arguments))/4
		}
	}
	return total
}

// contextLevel clasifica el uso: 0 normal, 1 advertencia (≥75 %), 2 crítico (≥90 %).
func contextLevel(used, max int) int {
	if max <= 0 {
		return 0
	}
	pct := float64(used) / float64(max)
	switch {
	case pct >= 0.90:
		return 2
	case pct >= 0.75:
		return 1
	}
	return 0
}

// RenderContextBar dibuja la barra de contexto: la parte usada en color según
// el nivel y la parte disponible en un tono apagado.
func RenderContextBar(theme Theme, used, max, cells int) string {
	if max <= 0 || cells < 4 {
		return ""
	}
	if used < 0 {
		used = 0
	}
	if used > max {
		used = max
	}
	filled := used * cells / max
	if filled == 0 && used > 0 {
		filled = 1
	}
	if filled > cells {
		filled = cells
	}

	usedColor := theme.Success
	switch contextLevel(used, max) {
	case 1:
		usedColor = theme.Warning
	case 2:
		usedColor = theme.Danger
	}

	bar := lipgloss.NewStyle().Foreground(usedColor).Render(strings.Repeat("▰", filled)) +
		lipgloss.NewStyle().Foreground(theme.Muted).Render(strings.Repeat("▱", cells-filled))

	pct := used * 100 / max
	label := lipgloss.NewStyle().Foreground(theme.Muted).
		Render(fmt.Sprintf(" %s/%s (%d%%)", humanTokens(used), humanTokens(max), pct))

	out := bar + label
	if contextLevel(used, max) >= 1 {
		out += lipgloss.NewStyle().Foreground(theme.Warning).Render(" /compact")
	}
	return out
}
