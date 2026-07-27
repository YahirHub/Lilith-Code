package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// OnboardingOption identifies a first-run choice.
type OnboardingOption int

const (
	OptionCodex OnboardingOption = iota
	OptionCustom
	OptionOpenCodeFree
)

type onboardingCard struct {
	title       string
	description string
	badge       string
	option      OnboardingOption
}

var onboardingCards = []onboardingCard{
	{"Suscripción ChatGPT", "Inicia sesión con tu cuenta ChatGPT Plus/Pro (OAuth device).", "SUSCRIPCIÓN", OptionCodex},
	{"Proveedor personalizado", "Conecta cualquier endpoint OpenAI-compatible con tu API key.", "API KEY", OptionCustom},
	{"OpenCode Free", "Modelos gratuitos incluidos (Grok, Qwen3, Kimi).", "GRATIS", OptionOpenCodeFree},
}

// OnboardingModel is the first-run screen (also reused for /login).
type OnboardingModel struct {
	ctx      *AppContext
	selected int
	title    string
	subtitle string
	firstRun bool
}

func NewOnboarding(ctx *AppContext, firstRun bool) OnboardingModel {
	m := OnboardingModel{
		ctx:      ctx,
		firstRun: firstRun,
		title:    "Elige cómo conectar tu proveedor de IA",
		subtitle: "Puedes cambiarlo en cualquier momento con /login.",
	}
	if firstRun {
		m.title = "Bienvenido a Lilith"
		m.subtitle = "Antes de empezar, elige un modo de conexión."
	}
	return m
}

func (m OnboardingModel) Init() tea.Cmd { return nil }

func (m OnboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if !m.firstRun {
				return m, switchToChat()
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(onboardingCards)-1 {
				m.selected++
			}
		case "1":
			m.selected = 0
			return m.choose()
		case "2":
			m.selected = 1
			return m.choose()
		case "3":
			m.selected = 2
			return m.choose()
		case "enter":
			return m.choose()
		}
	}
	return m, nil
}

func (m OnboardingModel) choose() (tea.Model, tea.Cmd) {
	switch onboardingCards[m.selected].option {
	case OptionCodex:
		return m, switchTo(NewCodexLogin(m.ctx))
	case OptionCustom:
		return m, switchTo(NewCustomLogin(m.ctx))
	case OptionOpenCodeFree:
		if err := m.ctx.SelectOpenCodeFree(); err != nil {
			return m, showError(err)
		}
		return m, switchToChat()
	}
	return m, nil
}

func (m OnboardingModel) View() string {
	s := m.ctx.Styles
	w := m.ctx.Width
	if w < 40 {
		w = 40
	}
	logo := RenderLogo(w, m.ctx.Height, s.Theme)

	title := s.Title.Render(m.title)
	subtitle := s.Subtitle.Render(m.subtitle)

	cards := make([]string, len(onboardingCards))
	cardWidth := min(72, w-4)
	for i, c := range onboardingCards {
		style := s.Card.Width(cardWidth)
		if i == m.selected {
			style = s.CardSelected.Width(cardWidth)
		}
		badge := s.Badge.Render(" " + c.badge + " ")
		head := lipgloss.JoinHorizontal(lipgloss.Top,
			s.Title.Render((numberPrefix(i+1))+"  "+c.title),
			"  ",
			badge,
		)
		body := s.Subtitle.Render(c.description)
		cards[i] = style.Render(head + "\n" + body)
	}

	footer := s.Muted.Render("↑↓ Navegar   1-3 Elegir   Enter Confirmar   Ctrl+C Salir")

	parts := []string{
		"",
		logo,
		"",
		title,
		subtitle,
		"",
	}
	parts = append(parts, cards...)
	parts = append(parts, "", footer)
	return centered(w, m.ctx.Height, strings.Join(parts, "\n"))
}

func numberPrefix(n int) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#B084EB")).Bold(true).Render(itoa(n) + ".")
}

func itoa(n int) string {
	// small ints only
	if n < 10 {
		return string(rune('0' + n))
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func centered(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, content)
}
