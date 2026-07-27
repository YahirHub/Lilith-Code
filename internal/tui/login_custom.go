package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lilith/li/internal/providers"
)

type customStep int

const (
	stepName customStep = iota
	stepURL
	stepKey
	stepModels
)

// CustomLoginModel is the sequential form for adding a custom OpenAI-compatible provider.
type CustomLoginModel struct {
	ctx   *AppContext
	step  customStep
	input textinput.Model
	name  string
	url   string
	key   string
	err   string
	// fetching indica que se está consultando {baseUrl}/models.
	fetching bool
}

// modelsFetchedMsg transporta el resultado de consultar /models.
type modelsFetchedMsg struct {
	models []providers.Model
	err    error
}

func fetchModelsCmd(baseURL, key string) tea.Cmd {
	return func() tea.Msg {
		list, err := providers.FetchModels(baseURL, key)
		return modelsFetchedMsg{models: list, err: err}
	}
}

func NewCustomLogin(ctx *AppContext) CustomLoginModel {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "MyOpenAI"
	ti.Focus()
	ti.CharLimit = 128
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Primary)
	ti.TextStyle = lipgloss.NewStyle().Foreground(ctx.Styles.Theme.Foreground)
	return CustomLoginModel{ctx: ctx, input: ti}
}

func (m CustomLoginModel) Init() tea.Cmd { return textinput.Blink }

func (m CustomLoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case modelsFetchedMsg:
		m.fetching = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		return m.finish(msg.models)
	case tea.KeyMsg:
		if m.fetching {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.step > stepName {
				m.step--
				m.err = ""
				m.updateInputForStep()
				return m, nil
			}
			return m, switchTo(NewOnboarding(m.ctx, false))
		case "enter":
			return m.advance()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *CustomLoginModel) updateInputForStep() {
	m.input.SetValue("")
	switch m.step {
	case stepName:
		m.input.Placeholder = "MyOpenAI"
	case stepURL:
		m.input.Placeholder = "https://api.openai.com/v1"
		m.input.SetValue(m.url)
	case stepKey:
		m.input.Placeholder = "opcional · sk-...  |  env:OPENAI_API_KEY  |  vacío = sin token"
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
	case stepModels:
		m.input.Placeholder = "opcional · modelo, otro=1000000 · Enter vacío = consultar /models"
		m.input.EchoMode = textinput.EchoNormal
	}
}

func (m CustomLoginModel) advance() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.input.Value())
	switch m.step {
	case stepName:
		if val == "" {
			m.err = "Escribe un nombre."
			return m, nil
		}
		m.name = val
		m.step = stepURL
		m.err = ""
		m.updateInputForStep()
	case stepURL:
		if _, err := providers.NormalizeBaseURL(val); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.url = val
		m.step = stepKey
		m.err = ""
		m.updateInputForStep()
	case stepKey:
		m.key = val
		m.step = stepModels
		m.err = ""
		m.updateInputForStep()
	case stepModels:
		if val == "" {
			m.err = ""
			m.fetching = true
			return m, fetchModelsCmd(m.url, m.key)
		}
		models, err := providers.ParseModelsInput(val)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		return m.finish(models)
	}
	return m, nil
}

func (m CustomLoginModel) finish(models []providers.Model) (tea.Model, tea.Cmd) {
	p, err := providers.Upsert(m.ctx.ConfigDir, providers.UpsertParams{
		Name:        m.name,
		BaseURL:     m.url,
		APIKeyInput: m.key,
		Models:      models,
	})
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if err := m.ctx.ReloadProviders(); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, switchToChatWithSystem(fmt.Sprintf("Proveedor %q activo (%d modelos).", p.Name, len(p.Models)))
}

func (m CustomLoginModel) View() string {
	s := m.ctx.Styles
	w := min(72, m.ctx.Width-4)

	steps := []string{"Nombre", "Base URL", "API Key", "Modelos"}
	crumbs := make([]string, len(steps))
	for i, name := range steps {
		style := s.Muted
		if customStep(i) == m.step {
			style = s.Accent
		} else if customStep(i) < m.step {
			style = s.Success
		}
		crumbs[i] = style.Render(fmt.Sprintf("%d. %s", i+1, name))
	}
	crumbLine := strings.Join(crumbs, s.Muted.Render("  ›  "))

	var hint, title string
	switch m.step {
	case stepName:
		title = "¿Cómo quieres llamar a este proveedor?"
		hint = "Solo se usa para identificarlo dentro de Lilith."
	case stepURL:
		title = "URL base del endpoint"
		hint = "Ejemplos: https://api.openai.com/v1  ·  http://localhost:11434/v1"
	case stepKey:
		title = "API key (opcional)"
		hint = "Déjalo vacío si el endpoint no la necesita. También admite env:NOMBRE_VAR."
	case stepModels:
		title = "Modelos y contexto (opcional)"
		hint = "Vacío: consulta " + m.url + "/models. Manual: modelo, otro=1000000 (contexto en tokens)."
		if m.fetching {
			hint = "Consultando " + m.url + "/models…"
		}
	}

	box := s.InputBoxFocused.Width(w).Render(m.input.View())
	var errLine string
	if m.err != "" {
		errLine = "\n" + s.Danger.Render("✗ "+m.err)
	}
	footerText := "Enter Continuar   Esc Volver   Ctrl+C Salir"
	if m.step == stepModels {
		footerText = "Enter Continuar (vacío = consultar /models)   Esc Volver   Ctrl+C Salir"
	}
	footer := s.Muted.Render(footerText)

	body := strings.Join([]string{
		s.Accent.Render("Proveedor personalizado"),
		crumbLine,
		"",
		s.Title.Render(title),
		s.Subtitle.Render(hint),
		"",
		box,
		errLine,
		"",
		footer,
	}, "\n")

	return centered(m.ctx.Width, m.ctx.Height, body)
}
