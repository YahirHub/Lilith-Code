package tui

import (
	"fmt"
	"strings"

	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
	"github.com/lilith/li/internal/tui/uikit/textinput"

	"github.com/lilith/li/internal/providers"
)

type customStep int

const (
	stepName customStep = iota
	stepURL
	stepKey
	stepModels
)

const (
	customProviderNameLimit  = 2_048
	customProviderValueLimit = 16_384
)

// CustomLoginModel is the sequential form for adding a custom OpenAI-compatible provider.
type CustomLoginModel struct {
	ctx         *AppContext
	step        customStep
	input       textinput.Model
	modelsInput adaptiveTextArea
	name        string
	url         string
	key         string
	err         string
	notice      string
	// fetching indica que se está consultando {baseUrl}/models.
	fetching bool
}

// modelsFetchedMsg transporta el resultado de consultar /models.
type modelsFetchedMsg struct {
	models []providers.Model
	err    error
}

func fetchModelsCmd(baseURL, key string) uikit.Cmd {
	return func() uikit.Msg {
		list, err := providers.FetchModels(baseURL, key)
		return modelsFetchedMsg{models: list, err: err}
	}
}

func NewCustomLogin(ctx *AppContext) CustomLoginModel {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "MyOpenAI"
	ti.Focus()
	ti.CharLimit = customProviderNameLimit
	ti.PromptStyle = tuistyle.NewStyle().Foreground(ctx.Styles.Theme.Primary)
	ti.TextStyle = tuistyle.NewStyle().Foreground(ctx.Styles.Theme.Foreground)
	models := newAdaptiveTextArea("modelo, otro=1000000 · vacío = consultar /models", 1, 5)
	models.SetWidth(64)
	return CustomLoginModel{ctx: ctx, input: ti, modelsInput: models}
}

func (m CustomLoginModel) Init() uikit.Cmd { return textinput.Blink }

func (m CustomLoginModel) Update(msg uikit.Msg) (uikit.Model, uikit.Cmd) {
	switch v := msg.(type) {
	case modelsFetchedMsg:
		m.fetching = false
		if v.err != nil {
			if providers.IsModelCatalogUnavailable(v.err) {
				m.err = ""
				m.notice = "Este proveedor no expone /models. Escribe sus modelos manualmente; Lilith los conservará sin tratarlo como error."
				return m, nil
			}
			m.notice = ""
			m.err = v.err.Error()
			return m, nil
		}
		m.notice = ""
		return m.finish(v.models)

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
		switch hit.id {
		case "input":
			if m.step == stepModels {
				cmd := m.modelsInput.Focus()
				return m, cmd
			}
			cmd := m.input.Focus()
			return m, cmd
		case "continue":
			if !m.fetching {
				return m.advance()
			}
		case "back":
			if !m.fetching {
				return m.back()
			}
		case "cancel":
			return m, switchTo(NewOnboarding(m.ctx, false))
		}
		return m, nil

	case uikit.KeyMsg:
		if m.fetching {
			switch v.String() {
			case "esc":
				// Switch screens instead of pretending to cancel the HTTP command.
				// Any late result is then delivered to the new model and ignored.
				return m, switchTo(NewOnboarding(m.ctx, false))
			}
			return m, nil
		}
		switch v.String() {
		case "esc":
			return m.back()
		case "shift+enter", "alt+enter":
			if m.step == stepModels {
				m.modelsInput.InsertString("\n")
				return m, nil
			}
		case "enter":
			return m.advance()
		}
	}

	if m.step == stepModels {
		return m, m.modelsInput.Update(msg)
	}
	var cmd uikit.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m CustomLoginModel) back() (uikit.Model, uikit.Cmd) {
	if m.step > stepName {
		m.step--
		m.err = ""
		m.notice = ""
		m.updateInputForStep()
		return m, nil
	}
	return m, switchTo(NewOnboarding(m.ctx, false))
}

func (m *CustomLoginModel) updateInputForStep() {
	m.input.EchoMode = textinput.EchoNormal
	m.input.CharLimit = customProviderNameLimit
	m.input.SetValue("")
	switch m.step {
	case stepName:
		m.input.Placeholder = "MyOpenAI"
		m.input.SetValue(m.name)
	case stepURL:
		m.input.CharLimit = customProviderValueLimit
		m.input.Placeholder = "https://api.openai.com/v1"
		m.input.SetValue(m.url)
	case stepKey:
		m.input.CharLimit = customProviderValueLimit
		m.input.Placeholder = "opcional · sk-... | env:OPENAI_API_KEY | vacío = sin token"
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
		m.input.SetValue(m.key)
	case stepModels:
		m.modelsInput.SetValue("")
		_ = m.modelsInput.Focus()
	}
}

func (m CustomLoginModel) currentValue() string {
	if m.step == stepModels {
		return strings.TrimSpace(m.modelsInput.Value())
	}
	return strings.TrimSpace(m.input.Value())
}

func (m CustomLoginModel) advance() (uikit.Model, uikit.Cmd) {
	val := m.currentValue()
	switch m.step {
	case stepName:
		m.notice = ""
		if val == "" {
			m.err = "Escribe un nombre."
			return m, nil
		}
		m.name = val
		m.step = stepURL
		m.err = ""
		m.updateInputForStep()
	case stepURL:
		m.notice = ""
		if _, err := providers.NormalizeBaseURL(val); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.url = val
		m.step = stepKey
		m.err = ""
		m.updateInputForStep()
	case stepKey:
		m.notice = ""
		m.key = val
		m.step = stepModels
		m.err = ""
		m.updateInputForStep()
	case stepModels:
		if val == "" {
			m.err = ""
			m.notice = ""
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

func (m CustomLoginModel) finish(models []providers.Model) (uikit.Model, uikit.Cmd) {
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

func (m CustomLoginModel) layout() (string, []settingsHit) {
	s := m.ctx.Styles
	w := settingsContentWidth(m.ctx.Width)
	c := newSettingsCanvas(w)
	c.block(settingsHeader(s, "Proveedor personalizado", "Endpoint OpenAI-compatible. Los campos usan el mismo sistema de controles de ajustes."))
	c.blank()

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
	c.line(strings.Join(crumbs, s.Muted.Render("  ›  ")))
	c.blank()

	var hint, title string
	switch m.step {
	case stepName:
		title = "Nombre del proveedor"
		hint = "Se usa sólo para identificar la conexión dentro de Lilith."
	case stepURL:
		title = "URL base"
		hint = "Ej.: https://api.openai.com/v1 · http://localhost:11434/v1"
	case stepKey:
		title = "API key (opcional)"
		hint = "Admite una key literal, env:NOMBRE_VAR o vacío si el endpoint no autentica."
	case stepModels:
		title = "Modelos y contexto (opcional)"
		hint = "Vacío consulta " + m.url + "/models · Manual: modelo, otro=1000000 · Alt+Enter agrega línea."
		if m.fetching {
			hint = "Consultando " + m.url + "/models…"
		}
	}
	c.line(s.Title.Render(title))
	c.line(s.Subtitle.Render(settingsWrapPlain(hint, w)))
	c.blank()

	if m.step == stepModels {
		copyInput := m.modelsInput
		copyInput.SetWidth(w - 4)
		c.block(settingsInput(s, settingsInputSpec{
			ID:       "input",
			Content:  copyInput.View(),
			Width:    w,
			Focused:  !m.fetching,
			Disabled: m.fetching,
		}))
	} else {
		copyInput := m.input
		inputWidth := w - 4 - tuistyle.Width(copyInput.Prompt)
		if inputWidth < 1 {
			inputWidth = 1
		}
		copyInput.Width = inputWidth
		c.block(settingsInput(s, settingsInputSpec{ID: "input", Content: copyInput.View(), Width: w, Focused: true}))
	}
	if m.notice != "" {
		c.blank()
		c.line(s.Muted.Render("• " + settingsWrapPlain(m.notice, w)))
	}
	if m.err != "" {
		c.blank()
		c.line(s.Danger.Render("✗ " + settingsWrapPlain(m.err, w)))
	}
	c.blank()

	continueLabel := "Continuar"
	if m.step == stepModels {
		continueLabel = "Guardar"
		if m.currentValue() == "" {
			continueLabel = "Consultar /models"
		}
	}
	c.block(settingsButtonGroup(s, w,
		settingsButtonSpec{ID: "continue", Label: continueLabel, Focused: true, Disabled: m.fetching},
		settingsButtonSpec{ID: "back", Label: "Volver", Disabled: m.fetching},
		settingsButtonSpec{ID: "cancel", Label: "Cancelar", Danger: true},
	))
	c.blank()
	c.block(settingsFooter(s, "Enter continuar · Alt+Enter salto en modelos · clic en botones · Esc volver"))
	return c.render(m.ctx.Width)
}

func (m CustomLoginModel) View() string {
	view, _ := m.layout()
	return view
}
