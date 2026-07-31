package tui

import tuistyle "github.com/lilith/li/internal/tui/uikit/style"

// Theme mirrors Codewolf's palette (dark, purple-accented) using ANSI-safe hex.
type Theme struct {
	Name         string
	Background   tuistyle.Color
	Surface      tuistyle.Color
	SurfaceHover tuistyle.Color
	Foreground   tuistyle.Color
	Muted        tuistyle.Color
	InputFg      tuistyle.Color
	Primary      tuistyle.Color // brand accent (violet)
	Secondary    tuistyle.Color // secondary accent (teal)
	Success      tuistyle.Color
	Warning      tuistyle.Color
	Danger       tuistyle.Color
	Info         tuistyle.Color
	Border       tuistyle.Color
	BorderFocus  tuistyle.Color
	LogoBlock    tuistyle.Color
	LogoAccent   tuistyle.Color
}

// DefaultTheme returns the dark Codewolf-inspired palette.
func DefaultTheme() Theme {
	return Theme{
		Name:         "codewolf-dark",
		Background:   tuistyle.Color("#0B0B10"),
		Surface:      tuistyle.Color("#15151F"),
		SurfaceHover: tuistyle.Color("#1F1F2E"),
		Foreground:   tuistyle.Color("#E4E4EF"),
		Muted:        tuistyle.Color("#7A7A8E"),
		InputFg:      tuistyle.Color("#B8B8C8"),
		Primary:      tuistyle.Color("#B084EB"),
		Secondary:    tuistyle.Color("#5EE8C9"),
		Success:      tuistyle.Color("#7DD87D"),
		Warning:      tuistyle.Color("#F0C070"),
		Danger:       tuistyle.Color("#FF6B7A"),
		Info:         tuistyle.Color("#7CB9F5"),
		Border:       tuistyle.Color("#2A2A3A"),
		BorderFocus:  tuistyle.Color("#B084EB"),
		LogoBlock:    tuistyle.Color("#B084EB"),
		LogoAccent:   tuistyle.Color("#5EE8C9"),
	}
}
