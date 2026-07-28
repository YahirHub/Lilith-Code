package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/skills"
)

// ConfigScreen is the interactive `/config` screen. It shares the same
// settings controls used by `/providers` and `/login` so new preferences do not
// reinvent keyboard, mouse or responsive layout behavior.
type ConfigScreen struct {
	ctx      *AppContext
	settings config.Settings
	focus    string
	message  string
	danger   string
	loaded   []skills.Skill
}

func NewConfigScreen(ctx *AppContext) *ConfigScreen {
	s, _ := config.Load(ctx.ConfigDir)
	loaded := skills.Load(skills.DefaultLoadOptions(ctx.ConfigDir, currentProject()))
	return &ConfigScreen{ctx: ctx, settings: s, loaded: loaded, focus: "skills"}
}

func (c *ConfigScreen) Init() tea.Cmd { return nil }

func (c *ConfigScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.MouseMsg:
		e, ok := mouseLeftPress(v)
		if !ok {
			return c, nil
		}
		_, hits := c.layout()
		hit, ok := hitAt(hits, e.X, e.Y)
		if !ok {
			return c, nil
		}
		c.focus = hit.id
		switch hit.id {
		case "skills":
			return c.toggleSkills()
		case "back":
			return c, switchToChat()
		}

	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c":
			return c, tea.Quit
		case "esc", "q":
			return c, switchToChat()
		case "tab", "down", "j":
			if c.focus == "skills" {
				c.focus = "back"
			} else {
				c.focus = "skills"
			}
			return c, nil
		case "shift+tab", "up", "k":
			if c.focus == "back" {
				c.focus = "skills"
			} else {
				c.focus = "back"
			}
			return c, nil
		case "enter", " ":
			if c.focus == "back" {
				return c, switchToChat()
			}
			return c.toggleSkills()
		}
	}
	return c, nil
}

func (c *ConfigScreen) toggleSkills() (tea.Model, tea.Cmd) {
	c.settings.SkillsEnabled = !c.settings.SkillsEnabled
	if err := config.Save(c.ctx.ConfigDir, c.settings); err != nil {
		c.danger = "No se pudo guardar: " + err.Error()
		return c, nil
	}
	c.danger = ""
	state := "desactivadas"
	if c.settings.SkillsEnabled {
		state = "activadas"
		c.loaded = skills.Load(skills.DefaultLoadOptions(c.ctx.ConfigDir, currentProject()))
	}
	c.message = fmt.Sprintf("Skills %s. %d disponibles.", state, len(c.loaded))
	return c, nil
}

func (c *ConfigScreen) summarizeSkills() string {
	if len(c.loaded) == 0 {
		return "Ninguna detectada"
	}
	names := make([]string, 0, len(c.loaded))
	for _, s := range c.loaded {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

func (c *ConfigScreen) layout() (string, []settingsHit) {
	s := c.ctx.Styles
	w := settingsContentWidth(c.ctx.Width)
	canvas := newSettingsCanvas(w)
	canvas.block(settingsHeader(s, "Configuración", "Preferencias generales de Lilith. Los controles admiten teclado y clic."))
	canvas.blank()
	canvas.block(settingsSwitch(s, settingsSwitchSpec{
		ID:          "skills",
		Label:       "Skills",
		Description: fmt.Sprintf("%d skill(s) detectada(s) en rutas Lilith, Claude y Agent del usuario/proyecto", len(c.loaded)),
		Value:       c.settings.SkillsEnabled,
		Focused:     c.focus == "skills",
		Width:       w,
	}))
	canvas.blank()
	canvas.block(settingsCard(s, settingsCardSpec{
		Title:       "Ruta de configuración",
		Description: c.ctx.ConfigDir,
		Badge:       "INFO",
		Width:       w,
	}))
	canvas.blank()
	canvas.block(settingsCard(s, settingsCardSpec{
		Title:       "Skills disponibles",
		Description: c.summarizeSkills(),
		Badge:       "INFO",
		Width:       w,
	}))
	canvas.blank()
	canvas.block(settingsButtonGroup(s, w, settingsButtonSpec{ID: "back", Label: "Volver", Focused: c.focus == "back"}))
	if c.message != "" {
		canvas.blank()
		canvas.line(s.Success.Render("· " + settingsWrapPlain(c.message, w)))
	}
	if c.danger != "" {
		canvas.blank()
		canvas.line(s.Danger.Render("✗ " + settingsWrapPlain(c.danger, w)))
	}
	canvas.blank()
	canvas.block(settingsFooter(s, "Tab/↑↓ cambiar control · Enter/Espacio usar · clic izquierdo · Esc volver"))
	return canvas.render(c.ctx.Width)
}

func (c *ConfigScreen) View() string {
	view, _ := c.layout()
	return view
}

var _ tea.Model = (*ConfigScreen)(nil)

// currentProject wraps os.Getwd() para poder ser stubbeado en tests.
func currentProject() string {
	dir, err := getCwd()
	if err != nil {
		return "."
	}
	return dir
}
