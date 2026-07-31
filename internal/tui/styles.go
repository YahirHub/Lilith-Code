package tui

import tuistyle "github.com/lilith/li/internal/tui/uikit/style"

// Styles bundles Lilith's reusable terminal styles.
type Styles struct {
	Theme Theme

	Title    tuistyle.Style
	Subtitle tuistyle.Style
	Muted    tuistyle.Style
	Accent   tuistyle.Style
	Danger   tuistyle.Style
	Success  tuistyle.Style
	Warning  tuistyle.Style

	Card         tuistyle.Style
	CardSelected tuistyle.Style
	Badge        tuistyle.Style

	InputBox        tuistyle.Style
	InputBoxFocused tuistyle.Style
	StatusBar       tuistyle.Style

	MessageUser      tuistyle.Style
	MessageAssistant tuistyle.Style
	MessageSystem    tuistyle.Style
	MessageError     tuistyle.Style
}

func NewStyles(t Theme) Styles {
	return Styles{
		Theme:    t,
		Title:    tuistyle.NewStyle().Foreground(t.Foreground).Bold(true),
		Subtitle: tuistyle.NewStyle().Foreground(t.Muted),
		Muted:    tuistyle.NewStyle().Foreground(t.Muted),
		Accent:   tuistyle.NewStyle().Foreground(t.Primary).Bold(true),
		Danger:   tuistyle.NewStyle().Foreground(t.Danger),
		Success:  tuistyle.NewStyle().Foreground(t.Success),
		Warning:  tuistyle.NewStyle().Foreground(t.Warning),

		Card: tuistyle.NewStyle().
			Border(tuistyle.RoundedBorder()).
			BorderForeground(t.Border).
			Padding(0, 2).
			Foreground(t.Foreground),

		CardSelected: tuistyle.NewStyle().
			Border(tuistyle.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(0, 2).
			Foreground(t.Foreground).
			Bold(true),

		Badge: tuistyle.NewStyle().
			Foreground(t.Background).
			Background(t.Primary).
			Padding(0, 1).
			Bold(true),

		InputBox: tuistyle.NewStyle().
			Border(tuistyle.RoundedBorder()).
			BorderForeground(t.Border).
			Padding(0, 1),

		InputBoxFocused: tuistyle.NewStyle().
			Border(tuistyle.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(0, 1),

		StatusBar: tuistyle.NewStyle().
			Foreground(t.Muted).
			Background(t.Surface).
			Padding(0, 1),

		MessageUser:      tuistyle.NewStyle().Foreground(t.Info).Bold(true),
		MessageAssistant: tuistyle.NewStyle().Foreground(t.Foreground),
		MessageSystem:    tuistyle.NewStyle().Foreground(t.Muted).Italic(true),
		MessageError:     tuistyle.NewStyle().Foreground(t.Danger).Bold(true),
	}
}
