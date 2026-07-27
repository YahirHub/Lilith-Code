package tui

import "github.com/charmbracelet/lipgloss"

// Styles bundles the reusable lipgloss styles.
type Styles struct {
	Theme Theme

	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Muted    lipgloss.Style
	Accent   lipgloss.Style
	Danger   lipgloss.Style
	Success  lipgloss.Style
	Warning  lipgloss.Style

	Card         lipgloss.Style
	CardSelected lipgloss.Style
	Badge        lipgloss.Style

	InputBox        lipgloss.Style
	InputBoxFocused lipgloss.Style
	StatusBar       lipgloss.Style

	MessageUser      lipgloss.Style
	MessageAssistant lipgloss.Style
	MessageSystem    lipgloss.Style
	MessageError     lipgloss.Style
}

func NewStyles(t Theme) Styles {
	return Styles{
		Theme:    t,
		Title:    lipgloss.NewStyle().Foreground(t.Foreground).Bold(true),
		Subtitle: lipgloss.NewStyle().Foreground(t.Muted),
		Muted:    lipgloss.NewStyle().Foreground(t.Muted),
		Accent:   lipgloss.NewStyle().Foreground(t.Primary).Bold(true),
		Danger:   lipgloss.NewStyle().Foreground(t.Danger),
		Success:  lipgloss.NewStyle().Foreground(t.Success),
		Warning:  lipgloss.NewStyle().Foreground(t.Warning),

		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Border).
			Padding(0, 2).
			Foreground(t.Foreground),

		CardSelected: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(0, 2).
			Foreground(t.Foreground).
			Bold(true),

		Badge: lipgloss.NewStyle().
			Foreground(t.Background).
			Background(t.Primary).
			Padding(0, 1).
			Bold(true),

		InputBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Border).
			Padding(0, 1),

		InputBoxFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Foreground(t.Muted).
			Background(t.Surface).
			Padding(0, 1),

		MessageUser:      lipgloss.NewStyle().Foreground(t.Info).Bold(true),
		MessageAssistant: lipgloss.NewStyle().Foreground(t.Foreground),
		MessageSystem:    lipgloss.NewStyle().Foreground(t.Muted).Italic(true),
		MessageError:     lipgloss.NewStyle().Foreground(t.Danger).Bold(true),
	}
}
