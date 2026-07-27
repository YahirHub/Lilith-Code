package tui

import "github.com/charmbracelet/lipgloss"

// Theme mirrors Codewolf's palette (dark, purple-accented) using ANSI-safe hex.
type Theme struct {
	Name         string
	Background   lipgloss.Color
	Surface      lipgloss.Color
	SurfaceHover lipgloss.Color
	Foreground   lipgloss.Color
	Muted        lipgloss.Color
	InputFg      lipgloss.Color
	Primary      lipgloss.Color // brand accent (violet)
	Secondary    lipgloss.Color // secondary accent (teal)
	Success      lipgloss.Color
	Warning      lipgloss.Color
	Danger       lipgloss.Color
	Info         lipgloss.Color
	Border       lipgloss.Color
	BorderFocus  lipgloss.Color
	LogoBlock    lipgloss.Color
	LogoAccent   lipgloss.Color
}

// DefaultTheme returns the dark Codewolf-inspired palette.
func DefaultTheme() Theme {
	return Theme{
		Name:         "codewolf-dark",
		Background:   lipgloss.Color("#0B0B10"),
		Surface:      lipgloss.Color("#15151F"),
		SurfaceHover: lipgloss.Color("#1F1F2E"),
		Foreground:   lipgloss.Color("#E4E4EF"),
		Muted:        lipgloss.Color("#7A7A8E"),
		InputFg:      lipgloss.Color("#B8B8C8"),
		Primary:      lipgloss.Color("#B084EB"),
		Secondary:    lipgloss.Color("#5EE8C9"),
		Success:      lipgloss.Color("#7DD87D"),
		Warning:      lipgloss.Color("#F0C070"),
		Danger:       lipgloss.Color("#FF6B7A"),
		Info:         lipgloss.Color("#7CB9F5"),
		Border:       lipgloss.Color("#2A2A3A"),
		BorderFocus:  lipgloss.Color("#B084EB"),
		LogoBlock:    lipgloss.Color("#B084EB"),
		LogoAccent:   lipgloss.Color("#5EE8C9"),
	}
}
