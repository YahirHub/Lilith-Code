package tui

import (
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
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
	{"Proveedor personalizado", "Conecta cualquier endpoint OpenAI-compatible con tu API key.", "API KEY", OptionCustom},
	{"ChatGPT Codex", "Inicia sesión con tu cuenta ChatGPT Plus/Pro mediante OAuth.", "SUSCRIPCIÓN", OptionCodex},
	{"Continuar con OpenCode Free", "Usa los modelos gratuitos incluidos sin configurar una API key.", "GRATIS", OptionOpenCodeFree},
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
		title:    "Elige cómo conectar tu proveedor",
		subtitle: "Usa teclado o clic. Puedes volver aquí con /login.",
	}
	if firstRun {
		m.title = "Bienvenido a Lilith"
		m.subtitle = "Antes de empezar, elige un modo de conexión."
	}
	return m
}

func (m OnboardingModel) Init() uikit.Cmd { return nil }

func (m OnboardingModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case uikit.MouseMsg:
		e, ok := mouseLeftPress(v)
		if !ok {
			return m, nil
		}
		_, hits := m.layout()
		hit, ok := hitAt(hits, e.X, e.Y)
		if !ok {
			return m, nil
		}
		if strings.HasPrefix(hit.id, "login:") {
			for i, card := range onboardingCards {
				if hit.id == "login:"+onboardingOptionID(card.option) {
					m.selected = i
					return m.choose()
				}
			}
		}
		if hit.id == "back" && !m.firstRun {
			return m, switchToChat()
		}

	case uikit.KeyMsg:
		switch v.String() {
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

func (m OnboardingModel) choose() (uikit.Model, uikit.Cmd) {
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

func (m OnboardingModel) layout() (string, []settingsHit) {
	s := m.ctx.Styles
	w := settingsContentWidth(m.ctx.Width)
	c := newSettingsCanvas(w)
	if m.firstRun && m.ctx.Height >= 30 {
		c.block(settingsBlock{text: RenderLogo(w, m.ctx.Height, s.Theme)})
		c.blank()
	}
	c.block(settingsHeader(s, m.title, m.subtitle))
	c.blank()
	for i, card := range onboardingCards {
		c.block(settingsCard(s, settingsCardSpec{
			ID:          "login:" + onboardingOptionID(card.option),
			Title:       card.title,
			Description: card.description,
			Badge:       card.badge,
			Selected:    i == m.selected,
			Width:       w,
		}))
		c.blank()
	}
	if !m.firstRun {
		c.block(settingsButtonGroup(s, w, settingsButtonSpec{ID: "back", Label: "Volver"}))
		c.blank()
	}
	c.block(settingsFooter(s, "↑↓ navegar · 1-3 elegir · Enter confirmar · clic para abrir · Esc volver"))
	return c.render(m.ctx.Width)
}

func (m OnboardingModel) View() string {
	view, _ := m.layout()
	return view
}

func onboardingOptionID(option OnboardingOption) string {
	switch option {
	case OptionCodex:
		return "codex"
	case OptionCustom:
		return "custom"
	case OptionOpenCodeFree:
		return "free"
	default:
		return "unknown"
	}
}
